package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 验证：工具声明会被发往上游，流式 tool_calls 增量能被正确聚合。
func TestDeepSeekToolCalling(t *testing.T) {
	var gotTools bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req dsRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("bad request body: %v", err)
		}
		if len(req.Tools) == 1 && req.Tools[0].Function.Name == "read" && req.ToolChoice == "auto" {
			gotTools = true
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		lines := []string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.txt\"}"}}]}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`,
		}
		for _, l := range lines {
			io.WriteString(w, l+"\n\n")
			flusher.Flush()
		}
	}))
	defer srv.Close()

	d := NewDeepSeek(srv.URL, "test-key", "deepseek-chat", 5*time.Second)
	ch, err := d.Chat(context.Background(), domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "read a.txt"}},
		Tools: []domain.Tool{{
			Name:        "read",
			Description: "read a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
		ToolChoice: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	var finish string
	var usage *domain.Usage
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		if c.ToolCall != nil {
			if c.ToolCall.ID != "" {
				sb.WriteString(c.ToolCall.Name + ":" + c.ToolCall.Arguments)
			} else {
				sb.WriteString(c.ToolCall.Arguments)
			}
		}
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
		if c.Usage != nil {
			usage = c.Usage
		}
	}
	if !gotTools {
		t.Fatal("tools were not sent upstream")
	}
	if got := sb.String(); got != `read:{"path":"a.txt"}` {
		t.Fatalf("aggregated tool call = %q", got)
	}
	if finish != "tool_calls" {
		t.Fatalf("finish reason = %q", finish)
	}
	if usage == nil || usage.TotalTokens != 18 {
		t.Fatalf("usage = %+v", usage)
	}
}

// 回归：tool_calls 必须序列化成 OpenAI 的嵌套结构。
//
// 曾经因为直接序列化内部扁平结构，导致 DeepSeek 返回
// 422 missing field `type`，工具循环在第二步就断了。
func TestDSMessageShape(t *testing.T) {
	msgs := toDSMessages([]domain.ChatMessage{
		{Role: "assistant", Content: "", ToolCalls: []domain.ToolCall{
			{ID: "call_1", Name: "read", Arguments: `{"path":"a.txt"}`},
		}},
		{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`},
	})
	b, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	calls, ok := out[0]["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls missing: %s", b)
	}
	call := calls[0].(map[string]any)
	if call["type"] != "function" {
		t.Fatalf("type=%v", call["type"])
	}
	fn, ok := call["function"].(map[string]any)
	if !ok || fn["name"] != "read" || fn["arguments"] != `{"path":"a.txt"}` {
		t.Fatalf("function=%v", call["function"])
	}
	if out[1]["tool_call_id"] != "call_1" {
		t.Fatalf("tool_call_id missing: %s", b)
	}
}
