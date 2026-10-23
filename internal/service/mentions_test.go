package service

import (
	"reflect"
	"testing"
)

func mentionTestAgents() []Agent {
	return []Agent{
		{Key: "backend", Name: "后端", Title: "接口与数据", Aliases: []string{"服务端"}},
		{Key: "frontend", Name: "前端", Title: "界面与交互", Aliases: []string{"客户端"}},
		{Key: "qa", Name: "qa", Title: "测试与验收"},
	}
}

func mentionKeys(agents []*Agent) []string {
	if len(agents) == 0 {
		return nil
	}
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.Key)
	}
	return out
}

func TestMentionedAgents(t *testing.T) {
	agents := mentionTestAgents()
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single chinese mention",
			text: "@后端 修一下",
			want: []string{"backend"},
		},
		{
			name: "two mentions keep order",
			text: "@后端 @前端 一起看",
			want: []string{"backend", "frontend"},
		},
		{
			name: "chinese prefix matches longest name",
			text: "@后端接口 先对齐字段",
			want: []string{"backend"},
		},
		{
			name: "ascii mention requires word boundary",
			text: "@qaBot 跑一下",
			want: nil,
		},
		{
			name: "ascii mention with punctuation boundary",
			text: "@qa: 跑测试",
			want: []string{"qa"},
		},
		{
			name: "incidental name without at sign is ignored",
			text: "测试一下这个功能",
			want: nil,
		},
		{
			name: "all mention is not a directed mention",
			text: "@所有人 你们怎么看",
			want: nil,
		},
		{
			name: "duplicate mentions are unique",
			text: "@后端 先看 @后端 再看",
			want: []string{"backend"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mentionKeys(MentionedAgents(agents, tc.text))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MentionedAgents(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestHasAllMention(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"@所有人 你们怎么看", true},
		{"＠大家 看一下", true},
		{"@全员 开始吧", true},
		{"@后端 看下", false},
		{"大家好", false},
	}
	for _, tc := range cases {
		if got := hasAllMention(tc.text); got != tc.want {
			t.Fatalf("hasAllMention(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestAgentsByKeys(t *testing.T) {
	agents := mentionTestAgents()
	cases := []struct {
		name string
		keys []string
		want []string
	}{
		{
			name: "resolves by key and by name in request order",
			keys: []string{"backend", "前端"},
			want: []string{"backend", "frontend"},
		},
		{
			name: "ignores unknown and duplicate keys",
			keys: []string{"frontend", "nope", "frontend", "qa"},
			want: []string{"frontend", "qa"},
		},
		{
			name: "empty keys returns nothing",
			keys: nil,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mentionKeys(AgentsByKeys(agents, tc.keys))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("AgentsByKeys(%v) = %v, want %v", tc.keys, got, tc.want)
			}
		})
	}
}
