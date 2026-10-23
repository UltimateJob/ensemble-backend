package gateway

import (
	"encoding/json"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 回归：tool_choice 为空时不能出现在请求体里。
//
// dsRequest.ToolChoice 是 any。encoding/json 的 omitempty 对接口类型
// 只看接口本身是否为 nil，不看它装的值——装着一个空字符串的接口仍会
// 被序列化成 "tool_choice":""，上游 DeepSeek 直接 422：
//
//	tool_choice: expected one of `none`, `auto`, `required` or a tool
//
// 这曾经让线上所有聊天（包括不带 tools 的）全部失败，重试后熔断打开。
func TestToolChoiceEmptyIsOmitted(t *testing.T) {
	tc := toDSToolChoice(domain.ChatRequest{})
	body, err := json.Marshal(dsRequest{Model: "deepseek-chat", ToolChoice: tc})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["tool_choice"]; ok {
		t.Fatalf("空 ToolChoice 不该出现在请求体里，实际 tool_choice=%q，body=%s", v, body)
	}
}

// 非空字符串原样透传（auto / none / required）。
func TestToolChoiceStringPassesThrough(t *testing.T) {
	for _, want := range []string{"auto", "none", "required"} {
		tc := toDSToolChoice(domain.ChatRequest{ToolChoice: want})
		if tc != want {
			t.Fatalf("ToolChoice=%q 应原样返回，实际 %v", want, tc)
		}
	}
}

// 指定工具名时必须发对象形式，而且优先于字符串形式：
// DeepSeek 不接受字符串形式的函数名（同样 422）。
func TestToolChoiceNameWins(t *testing.T) {
	tc := toDSToolChoice(domain.ChatRequest{ToolChoice: "required", ToolChoiceName: "read"})
	body, _ := json.Marshal(dsRequest{Model: "m", ToolChoice: tc})
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	obj, ok := got["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("指定工具名时应为对象，实际：%s", body)
	}
	fn, _ := obj["function"].(map[string]any)
	if fn["name"] != "read" {
		t.Fatalf("工具名应为 read，实际：%s", body)
	}
}
