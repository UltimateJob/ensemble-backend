package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// chunkWithToolCall 造一块工具调用增量。参数分段到达是常态，
// 合并逻辑必须能跨块拼接。
func chunkWithToolCall(index int, id, name, args string) domain.Chunk {
	return domain.Chunk{ToolCall: &domain.ToolCallDelta{
		Index: index, ID: id, Name: name, Arguments: args,
	}}
}

// 这个接口对外声称兼容 OpenAI，所以必须真的按 OpenAI 的请求体解码。
// 之前它按内部结构（扁平 Tool）解，外部按规范发来的
// {"type":"function","function":{"name":...}} 会解出空名字，上游 400。
func TestToChatRequestOpenAIToolShape(t *testing.T) {
	var in oaRequest
	raw := `{
	  "model":"deepseek-chat",
	  "messages":[{"role":"user","content":"列目录"}],
	  "tools":[{"type":"function","function":{
	     "name":"list","description":"列目录",
	     "parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}]
	}`
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	req, err := toChatRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("应有 1 个工具，实际 %d", len(req.Tools))
	}
	if req.Tools[0].Name != "list" {
		t.Fatalf("工具名应为 list，实际 %q", req.Tools[0].Name)
	}
	if len(req.Tools[0].Parameters) == 0 {
		t.Fatal("工具参数丢了")
	}
	if req.ToolChoice != "" {
		t.Fatalf("没指定 tool_choice 时应为空，实际 %q", req.ToolChoice)
	}
}

// 空工具名要在入口就拒绝，而不是转给上游换个 400 回来。
func TestToChatRequestRejectsEmptyToolName(t *testing.T) {
	in := oaRequest{Tools: []oaTool{{Type: "function"}}}
	if _, err := toChatRequest(in); err == nil {
		t.Fatal("空工具名应该报错")
	}
}

// tool_choice 的对象形态要转成 ToolChoiceName，
// 这样上游才不会被 required 糊弄成 auto。
func TestToChatRequestToolChoiceObject(t *testing.T) {
	var in oaRequest
	raw := `{"model":"m","messages":[{"role":"user","content":"x"}],
	  "tools":[{"type":"function","function":{"name":"read"}}],
	  "tool_choice":{"type":"function","function":{"name":"read"}}}`
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	req, err := toChatRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	if req.ToolChoiceName != "read" {
		t.Fatalf("ToolChoiceName 应为 read，实际 %q", req.ToolChoiceName)
	}
}

// 非流式响应必须把 tool_calls 带出去。
// 只拼 Content 会让调用方拿到 200 + 空正文。
func TestMergeToolCallAccumulates(t *testing.T) {
	var live []*oaToolCall
	var out []oaToolCall

	steps := []struct {
		id, name, args string
	}{
		{"call_1", "list", ""},
		{"", "", `{"path":`},
		{"", "", `"/tmp"}`},
	}
	for _, s := range steps {
		c := chunkWithToolCall(0, s.id, s.name, s.args)
		out, live = mergeToolCall(out, live, c)
	}

	if len(out) != 1 {
		t.Fatalf("应有 1 个 tool_call，实际 %d", len(out))
	}
	if out[0].ID != "call_1" {
		t.Fatalf("id 丢了：%q", out[0].ID)
	}
	if out[0].Function.Name != "list" {
		t.Fatalf("name 丢了：%q", out[0].Function.Name)
	}
	if out[0].Function.Arguments != `{"path":"/tmp"}` {
		t.Fatalf("参数没有拼起来：%q", out[0].Function.Arguments)
	}
}
