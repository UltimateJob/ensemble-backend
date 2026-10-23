package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// DeepSeek DeepSeek 上游（OpenAI 兼容协议）。
type DeepSeek struct {
	name   string
	base   string
	key    string
	model  string
	client *http.Client
}

func NewDeepSeek(base, key, model string, timeout time.Duration) *DeepSeek {
	if base == "" {
		base = "https://api.deepseek.com"
	}
	return &DeepSeek{
		name:  "deepseek",
		base:  strings.TrimRight(base, "/"),
		key:   key,
		model: model,
		client: &http.Client{
			// 不能用 http.Client.Timeout：它覆盖整个响应体读取时间。
			// 流式生成本来就可能超过两分钟，一到点就被掐断（基准里验收
			// Agent 在 2 分 13 秒处报 context deadline exceeded）。
			// 用 ResponseHeaderTimeout 只限制「等响应头」，正文由 Run /
			// 规划 / 验收各自的 context 截止时间控制。
			Transport: &http.Transport{
				MaxIdleConns:          200,
				MaxIdleConnsPerHost:   50,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: timeout,
				ForceAttemptHTTP2:     true,
			},
		},
	}
}

// WithName 自定义注册名。EXTRA_PROVIDERS 注册的 OpenAI 兼容中转
// 各占一个名字：注册表、熔断统计、管理台的 provider 下拉都按名字区分。
func (d *DeepSeek) WithName(name string) *DeepSeek {
	if name != "" {
		d.name = name
	}
	return d
}

func (d *DeepSeek) Name() string { return d.name }

type dsRequest struct {
	Model       string      `json:"model"`
	Messages    []dsMessage `json:"messages"`
	Stream      bool        `json:"stream"`
	Temperature float64     `json:"temperature,omitempty"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Tools       []dsTool    `json:"tools,omitempty"`
	ToolChoice  any         `json:"tool_choice,omitempty"`
	// 流式必须显式要 usage，否则部分兼容上游不吐用量、计费统计全为 0
	StreamOptions *dsStreamOptions `json:"stream_options,omitempty"`
}

type dsStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// dsMessage 上游消息格式。
//
// 不能直接把 domain.ChatMessage 序列化发出去：OpenAI 的 tool_calls 要求
// `{"id","type":"function","function":{"name","arguments"}}` 的嵌套结构，
// 而内部结构是扁平的。DeepSeek 的反序列化器会直接报 422（missing field `type`）。
type dsMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content"`
	ToolCalls  []dsToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Name       string       `json:"name,omitempty"`
}

type dsToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// toDSMessages 把内部消息转成上游格式。
func toDSMessages(in []domain.ChatMessage) []dsMessage {
	out := make([]dsMessage, 0, len(in))
	for _, m := range in {
		dm := dsMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
		for _, tc := range m.ToolCalls {
			var dt dsToolCall
			dt.ID = tc.ID
			dt.Type = "function"
			dt.Function.Name = tc.Name
			dt.Function.Arguments = tc.Arguments
			dm.ToolCalls = append(dm.ToolCalls, dt)
		}
		out = append(out, dm)
	}
	return out
}

// dsTool OpenAI 兼容的工具声明。
type dsTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type dsChunk struct {
	Choices []struct {
		Delta struct {
			Content    string `json:"content"`
			Reasoning  string `json:"reasoning"`         // 思考型模型的推理增量（部分中转）
			Reasoning2 string `json:"reasoning_content"` // 同上，另一种字段名
			ToolCalls  []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (d *DeepSeek) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	model := req.Model
	if model == "" {
		model = d.model
	}
	var tools []dsTool
	for _, t := range req.Tools {
		var dt dsTool
		dt.Type = "function"
		dt.Function.Name = t.Name
		dt.Function.Description = t.Description
		dt.Function.Parameters = t.Parameters
		tools = append(tools, dt)
	}
	var toolChoice any = req.ToolChoice
	if req.ToolChoiceName != "" {
		toolChoice = map[string]any{
			"type":     "function",
			"function": map[string]string{"name": req.ToolChoiceName},
		}
	}
	body, err := json.Marshal(dsRequest{
		Model:       model,
		Messages:    toDSMessages(req.Messages),
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Tools:       tools,
		ToolChoice:  toolChoice,
		// 流式必须显式要 usage，否则上游不吐、计费统计全为 0
		StreamOptions: &dsStreamOptions{IncludeUsage: true},
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.key)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("deepseek: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	ch := make(chan domain.Chunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		finish := ""
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				ch <- domain.Chunk{Done: true, FinishReason: finish}
				return
			}
			var c dsChunk
			if err := json.Unmarshal([]byte(payload), &c); err != nil {
				continue
			}
			// 先转发正文再处理 usage：usage chunk 正常是 [DONE] 前那个
			// choices 为空的独立块，但万一上游把用量合并在最后一个正文
			// delta 里，先查 usage 的话那半个字就丢了。
			for _, pick := range c.Choices {
				if pick.FinishReason != "" {
					finish = pick.FinishReason
				}
				for _, tc := range pick.Delta.ToolCalls {
					d := &domain.ToolCallDelta{
						Index:     tc.Index,
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					}
					select {
					case ch <- domain.Chunk{ToolCall: d, FinishReason: finish}:
					case <-ctx.Done():
						return
					}
				}
				if pick.Delta.Content != "" {
					select {
					case ch <- domain.Chunk{Content: pick.Delta.Content, FinishReason: finish}:
					case <-ctx.Done():
						return
					}
				}
			}
			if c.Usage != nil {
				ch <- domain.Chunk{Done: true, FinishReason: finish, Usage: &domain.Usage{
					PromptTokens:     c.Usage.PromptTokens,
					CompletionTokens: c.Usage.CompletionTokens,
					TotalTokens:      c.Usage.TotalTokens,
				}}
				return
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			ch <- domain.Chunk{Err: err, Done: true}
		}
	}()

	return ch, nil
}

// Mock 本地演示上游：未配置 API Key 时使用，保证整条链路可跑通。
type Mock struct{}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	reply := mockReply(req)
	ch := make(chan domain.Chunk, len(reply)+2)
	go func() {
		defer close(ch)
		for _, r := range reply {
			select {
			case ch <- domain.Chunk{Content: string(r)}:
			case <-ctx.Done():
				return
			}
			time.Sleep(18 * time.Millisecond)
		}
		ch <- domain.Chunk{Done: true, Usage: &domain.Usage{
			PromptTokens:     len(req.Messages) * 12,
			CompletionTokens: len(reply),
			TotalTokens:      len(req.Messages)*12 + len(reply),
		}}
	}()
	return ch, nil
}

// mockReply 生成演示台词。
//
// 从 system prompt 里取角色名，查预置台词库。取不到就用兜底。
// seed 用最近一条用户消息，让同一角色在不同轮次说不同的话。
func mockReply(req domain.ChatRequest) []rune {
	system := ""
	seed := ""
	for _, m := range req.Messages {
		if m.Role == "system" && system == "" {
			system = m.Content
		}
		if m.Role == "user" {
			seed = m.Content
		}
	}
	line := mockLine(system, seed)

	// 首屏给人一句提示，别让人以为这是真实模型
	if strings.Contains(system, "演示") || len(req.Messages) <= 2 {
		// 不加标记，保持对话自然；提示放在启动日志里
	}
	return []rune(line)
}

// Prober 支持体检的渠道：真发一次小请求，量连通、首字、吐字。
type Prober interface {
	Probe(ctx context.Context, model string) domain.ChannelProbe
}

// Probe 渠道体检：发一个小流式请求，量连通、首字延迟、输出 token 速度。
// 让模型连续吐一串数字，避免「回复一个字」测不出吞吐。
// 思考型模型可能把预算花在推理上——思考字段有内容也算连通。
func (d *DeepSeek) Probe(ctx context.Context, model string) domain.ChannelProbe {
	if model == "" {
		model = d.model
	}
	var pr domain.ChannelProbe
	pr.Model = model
	start := time.Now()

	body, _ := json.Marshal(dsRequest{
		Model:         model,
		Messages:      []dsMessage{{Role: "user", Content: "请从 1 数到 60，只输出数字，用逗号分隔"}},
		Stream:        true,
		MaxTokens:     160,
		StreamOptions: &dsStreamOptions{IncludeUsage: true},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		pr.Err = err.Error()
		pr.TotalMS = time.Since(start).Milliseconds()
		return pr
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.key)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := d.client.Do(req)
	if err != nil {
		pr.Err = err.Error()
		pr.TotalMS = time.Since(start).Milliseconds()
		return pr
	}
	defer resp.Body.Close()
	pr.HTTPStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		pr.Err = strings.TrimSpace(string(b))
		pr.TotalMS = time.Since(start).Milliseconds()
		return pr
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := int64(0)
	chars := 0
	tokens := 0
	usageTokens := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var c dsChunk
		if json.Unmarshal([]byte(payload), &c) != nil {
			continue
		}
		if c.Usage != nil {
			// 上游给了准确用量就以它为准
			usageTokens = c.Usage.CompletionTokens
			continue
		}
		for _, pick := range c.Choices {
			text := pick.Delta.Content
			if text == "" {
				// 思考型模型可能把预算全花在推理上：思考增量也算「吐字」，
				// 否则慢思考渠道会被误报成不通。
				text = pick.Delta.Reasoning + pick.Delta.Reasoning2
			}
			if text != "" {
				if first == 0 {
					first = time.Since(start).Milliseconds()
				}
				chars += len([]rune(text))
				tokens++ // 没有 usage 时的近似值：一个增量按一个 token 算
			}
		}
	}
	if usageTokens > 0 {
		tokens = usageTokens
	}
	pr.FirstByteMS = first
	pr.Chars = chars
	pr.Tokens = tokens
	pr.TotalMS = time.Since(start).Milliseconds()
	pr.OK = chars > 0
	if pr.OK && tokens > 0 && pr.TotalMS > pr.FirstByteMS {
		// 首字之后的纯输出速度
		pr.TokPerSec = math.Round(float64(tokens)*1000/float64(pr.TotalMS-pr.FirstByteMS)*10) / 10
	}
	if !pr.OK {
		pr.Err = "连接成功但没有收到任何内容（可能额度耗尽或模型无响应）"
	}
	pr.BillingUSD = d.probeBilling(ctx)
	return pr
}

// probeBilling 查剩余额度（new-api/one-api 风格），best-effort：
// 官方 DeepSeek 没有这个端点，返回 nil 即可。
func (d *DeepSeek) probeBilling(ctx context.Context) *float64 {
	for _, p := range []string{"/v1/dashboard/billing/subscription", "/dashboard/billing/subscription"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+p, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+d.key)
		resp, err := d.client.Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		var sub struct {
			HardLimitUSD float64 `json:"hard_limit_usd"`
		}
		if json.Unmarshal(b, &sub) == nil && sub.HardLimitUSD > 0 {
			// one-api 的「无限额度」占位值，显示出来只会误导
			if sub.HardLimitUSD >= 1000000 {
				return nil
			}
			v := sub.HardLimitUSD
			return &v
		}
	}
	return nil
}

// Probe mock 渠道体检：本地永远连通。
func (m *Mock) Probe(ctx context.Context, model string) domain.ChannelProbe {
	return domain.ChannelProbe{OK: true, HTTPStatus: 200, Chars: 2, Tokens: 2, TokPerSec: 1000, Model: "mock", FirstByteMS: 1, TotalMS: 2}
}
