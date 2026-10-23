package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 这个文件把 /v1/chat/completions 真正做成 OpenAI 兼容协议。
//
// 之前它只是「长得像」：请求体直接按内部结构（扁平的 Tool）解码，
// 响应也只拼 Content。于是：
//   - 外部按 OpenAI 规范发的 {"type":"function","function":{...}} 工具
//     解出来是空名字，上游 400；
//   - 模型返回 tool_calls 时，非流式响应把它们丢了，调用方只看到空正文。
//
// 桌面端把线上当模型上游用，正好就是外部调用方，所以这两处必须补上。
// 内部结构保持不变：只在边界上做一次转换。

// oaTool OpenAI 的工具声明。
type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

// oaToolCall OpenAI 的工具调用。
type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// oaMessage OpenAI 的消息体。tool_calls 用嵌套结构。
type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Name       string       `json:"name,omitempty"`
}

// oaRequest OpenAI 的请求体。只收我们支持的字段，
// 其余字段（top_p / stop 等）被忽略而不是报错——上游客户端的请求
// 往往带一堆可选参数，为它们拒绝请求没有意义。
type oaRequest struct {
	Model       string      `json:"model"`
	Messages    []oaMessage `json:"messages"`
	Stream      bool        `json:"stream"`
	Temperature float64     `json:"temperature"`
	MaxTokens   int         `json:"max_tokens"`
	Tools       []oaTool    `json:"tools"`
	ToolChoice  any         `json:"tool_choice"`
}

// toChatRequest 把 OpenAI 请求转成内部结构。
func toChatRequest(in oaRequest) (domain.ChatRequest, error) {
	out := domain.ChatRequest{
		Model:       in.Model,
		Stream:      in.Stream,
		Temperature: in.Temperature,
		MaxTokens:   in.MaxTokens,
	}
	for _, m := range in.Messages {
		dm := domain.ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, tc := range m.ToolCalls {
			dm.ToolCalls = append(dm.ToolCalls, domain.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		out.Messages = append(out.Messages, dm)
	}
	for _, t := range in.Tools {
		if t.Function.Name == "" {
			return out, fmt.Errorf("tools[].function.name 不能为空")
		}
		out.Tools = append(out.Tools, domain.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		})
	}
	// tool_choice 有两种形态：字符串（auto/none/required）或
	// {"type":"function","function":{"name":"..."}}。后者表意更准，
	// 用 ToolChoiceName 表达，避免上游把 required 当成 auto。
	switch v := in.ToolChoice.(type) {
	case string:
		out.ToolChoice = v
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				out.ToolChoiceName = name
				out.ToolChoice = "required"
			}
		}
	}
	return out, nil
}

// writeChatOK 输出非流式响应。工具调用与用量都必须带出去：
// 少了前者调用方拿到 200 + 空正文，少了后者计费恒为 0。
func writeChatOK(w http.ResponseWriter, msg oaMessage, usage *domain.Usage) {
	data := map[string]any{
		"choices": []map[string]any{{"message": msg}},
	}
	if usage != nil {
		data["usage"] = map[string]int{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	}
	ok(w, data)
}

// handleGatewayChat 网关直连接口，协议对齐 OpenAI /v1/chat/completions。
// 客户端可指定 model，网关按模型表与权重选路。
func (s *Server) handleGatewayChat(w http.ResponseWriter, r *http.Request) {
	// 这个接口之前完全绕过额度：不查剩余、不计数，
	// 拿到 token 的人可以无限免费刷模型。和其它入口一视同仁。
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}

	var in oaRequest
	if err := decode(r, &in); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req, err := toChatRequest(in)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if len(req.Messages) == 0 {
		badRequest(w, "messages 不能为空")
		return
	}

	stream, err := s.gw.Stream(r.Context(), req)
	if err != nil {
		fail(w, 503, "no_provider", err.Error())
		return
	}

	if !req.Stream {
		msg := oaMessage{Role: "assistant"}
		var lastErr string
		var calls []*oaToolCall
		var usage *domain.Usage
		for c := range stream {
			if c.Err != nil {
				lastErr = c.Err.Error()
				break
			}
			msg.Content += c.Content
			msg.ToolCalls, calls = mergeToolCall(msg.ToolCalls, calls, c)
			if c.Usage != nil {
				usage = c.Usage
			}
		}
		if lastErr != "" {
			fail(w, 502, "upstream_error", lastErr)
			return
		}
		_ = s.store.IncrementUsed(u.ID, 1)
		writeChatOK(w, msg, usage)
		return
	}

	// 心跳：等首 token 期间连接可能长时间无字节，
	// 有空闲超时的代理会掐断连接，见 sseWriter。
	sw := startSSE(w)
	defer sw.stop()

	var calls []*oaToolCall
	for c := range stream {
		if c.Err != nil {
			b, _ := json.Marshal(map[string]string{"error": c.Err.Error()})
			sw.writeRaw(fmt.Sprintf("data: %s\n\n", b))
			break
		}
		delta := map[string]any{}
		if c.Content != "" {
			delta["content"] = c.Content
		}
		if tc := toolCallDelta(calls, c); tc != nil {
			delta["tool_calls"] = []any{tc}
		}
		if len(delta) > 0 {
			b, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": delta}},
			})
			// 写失败＝客户端已断开，继续把流读完即可，不需要扣费
			if !sw.writeRaw(fmt.Sprintf("data: %s\n\n", b)) {
				return
			}
		}
		if c.Done {
			// 用量必须带出去：桌面端把这里当上游，靠这一块统计
			// token 与成本。漏了就是全线 0，计费失效。
			if c.Usage != nil {
				b, _ := json.Marshal(map[string]any{
					"choices": []any{},
					"usage": map[string]int{
						"prompt_tokens":     c.Usage.PromptTokens,
						"completion_tokens": c.Usage.CompletionTokens,
						"total_tokens":      c.Usage.TotalTokens,
					},
				})
				sw.writeRaw(fmt.Sprintf("data: %s\n\n", b))
			}
			sw.writeRaw("data: [DONE]\n\n")
			// 流正常走完才计数；中途出错或断开不扣——
			// 用户没拿到完整回复就不该付费。
			_ = s.store.IncrementUsed(u.ID, 1)
			break
		}
	}
}

// mergeToolCall 处理非流式累积：把增量合并成完整的 tool_call 列表。
//
// 返回合并后的列表和新的指针尾部，调用方原样带回下一轮。
func mergeToolCall(out []oaToolCall, live []*oaToolCall, c domain.Chunk) ([]oaToolCall, []*oaToolCall) {
	if c.ToolCall == nil {
		return out, live
	}
	d := c.ToolCall
	// 新调用：补齐列表到 index 位置
	for len(live) <= d.Index {
		live = append(live, &oaToolCall{Type: "function"})
	}
	tc := live[d.Index]
	if d.ID != "" {
		tc.ID = d.ID
	}
	if d.Name != "" {
		tc.Function.Name = d.Name
	}
	tc.Function.Arguments += d.Arguments

	out = out[:0]
	for _, l := range live {
		out = append(out, *l)
	}
	return out, live
}

// toolCallDelta 把一块工具调用增量转成 SSE 的 tool_calls 元素。
// OpenAI 的流式格式要求每块都带 index。
func toolCallDelta(live []*oaToolCall, c domain.Chunk) map[string]any {
	if c.ToolCall == nil {
		return nil
	}
	d := c.ToolCall
	for len(live) <= d.Index {
		live = append(live, &oaToolCall{Type: "function"})
	}
	fn := map[string]any{}
	if d.Name != "" {
		fn["name"] = d.Name
	}
	if d.Arguments != "" {
		fn["arguments"] = d.Arguments
	}
	item := map[string]any{
		"index":    d.Index,
		"type":     "function",
		"function": fn,
	}
	if d.ID != "" {
		item["id"] = d.ID
	}
	return item
}

// handleUserModels 给普通用户的可选模型列表（建群/换模型的选择器用）。
// 只给 key 和备注，不给成本、上游名这些内部账——那是运维台的事。
func (s *Server) handleUserModels(w http.ResponseWriter, r *http.Request) {
	type userModel struct {
		Key  string `json:"key"`
		Note string `json:"note,omitempty"`
	}
	allowed := domain.AllowedModelsFromContext(r.Context())
	out := []userModel{}
	for _, m := range s.gw.Models().List() {
		// 任务专用模型不进用户选择器——那是执行器的池子
		if !m.Enabled || m.TaskOnly {
			continue
		}
		// 套餐白名单之外的模型不展示，避免用户选了发不出去。
		if len(allowed) > 0 && !containsString(allowed, m.Key) {
			continue
		}
		out = append(out, userModel{Key: m.Key, Note: m.Note})
	}
	ok(w, out)
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
