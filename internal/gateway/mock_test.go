package gateway

import (
	"strings"
	"testing"
)

func TestExtractName(t *testing.T) {
	cases := map[string]string{
		"你正在群聊里，你的名字是「林」，身份是话少但靠谱。": "林",
		"你的名字是「雪见」。":                "雪见",
		"没有名字的提示词":                  "",
		"你的名字是「":                    "",
	}
	for in, want := range cases {
		if got := extractName(in); got != want {
			t.Fatalf("extractName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMockLinePerRole(t *testing.T) {
	// 不同角色应拿到各自的台词，不能所有人说同一句
	sys := func(name string) string {
		return "你的名字是「" + name + "」，身份是测试。"
	}
	seen := map[string]bool{}
	for _, name := range []string{"林", "周", "陈", "雪见", "桃桃", "凛", "葵", "工头", "查证", "审校"} {
		line := mockLine(sys(name), "今天有点累")
		if line == "" {
			t.Fatalf("%s 没有拿到台词", name)
		}
		if strings.Contains(line, "mock") || strings.Contains(line, "收到任务") {
			t.Fatalf("%s 的台词还在复读：%q", name, line)
		}
		seen[line] = true
	}
	if len(seen) < 6 {
		t.Fatalf("不同角色的台词重复太多，只有 %d 种", len(seen))
	}
}

func TestMockLineFallback(t *testing.T) {
	// 用户自造的角色不在库里，应该有兜底而不是空
	line := mockLine("你的名字是「自定义角色」", "随便说点什么")
	if line == "" {
		t.Fatal("兜底台词不能为空")
	}
}

func TestMockLineVariesBySeed(t *testing.T) {
	sys := "你的名字是「林」，身份是测试。"
	a := mockLine(sys, "短")
	b := mockLine(sys, "长一点的消息内容")
	// 不一定不同（取模会撞），但至少要能取到多个候选
	lines := map[string]bool{}
	for _, seed := range []string{"a", "bb", "ccc", "dddd", "eeeee", "ffffff"} {
		lines[mockLine(sys, seed)] = true
	}
	if len(lines) < 2 {
		t.Fatalf("同一角色在不同输入下应能说出不同的话，实际只有 %d 种", len(lines))
	}
	_ = a
	_ = b
}
