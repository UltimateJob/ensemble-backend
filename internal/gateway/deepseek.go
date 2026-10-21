package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// DeepSeek DeepSeek 上游（OpenAI 兼容协议）。
type DeepSeek struct {
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
		base:  strings.TrimRight(base, "/"),
		key:   key,
		model: model,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 50,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
		},
	}
}

func (d *DeepSeek) Name() string { return "deepseek" }

type dsRequest struct {
	Model       string               `json:"model"`
	Messages    []domain.ChatMessage `json:"messages"`
	Stream      bool                 `json:"stream"`
	Temperature float64              `json:"temperature,omitempty"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
}

type dsChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
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
	body, err := json.Marshal(dsRequest{
		Model:       model,
		Messages:    req.Messages,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
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

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				ch <- domain.Chunk{Done: true}
				return
			}
			var c dsChunk
			if err := json.Unmarshal([]byte(payload), &c); err != nil {
				continue
			}
			if c.Usage != nil {
				ch <- domain.Chunk{Done: true, Usage: &domain.Usage{
					PromptTokens:     c.Usage.PromptTokens,
					CompletionTokens: c.Usage.CompletionTokens,
					TotalTokens:      c.Usage.TotalTokens,
				}}
				return
			}
			for _, pick := range c.Choices {
				if pick.Delta.Content != "" {
					select {
					case ch <- domain.Chunk{Content: pick.Delta.Content}:
					case <-ctx.Done():
						return
					}
				}
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
