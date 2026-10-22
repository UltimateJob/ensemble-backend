package gateway

import (
	"slices"
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

func TestMockLineCoversRandFriendPool(t *testing.T) {
	// 名单必须与 internal/service/randfriend.go 的 friendPool 完全一致：
	// 日常陪聊开群时随机生成名字，台词表漏一个就多一台复读机。
	names := []string{
		"林", "林野", "阿野",
		"周", "周末", "小周",
		"陈", "陈默", "老陈",
		"苏", "苏苏", "小苏",
		"许", "许知", "知知",
		"罗", "大罗", "老罗",
	}
	for _, name := range names {
		got := mockLine("你的名字是「"+name+"」，身份是测试。", "今天有点累")
		want, ok := mockLines[name]
		if !ok {
			t.Fatalf("%s 不在台词表里", name)
		}
		if !slices.Contains(want, got) {
			t.Fatalf("%s 应命中自己的台词，却拿到 %q", name, got)
		}
		if slices.Contains(mockFallback, got) || slices.Contains(mockAnimeLines, got) {
			t.Fatalf("%s 落进了兜底/通用池：%q", name, got)
		}
	}
}

func TestMockLineAnimeScene(t *testing.T) {
	// 二次元场景从 61 个角色库随机挑人，不逐个配台词；
	// 命中场景特征（群名「二次元茶话会」或人设里的「出自《作品》」）
	// 应落进通用二次元池，而不是「我在听。」
	systems := []string{
		"你是远坂凛（出自《Fate/stay night》）。你正在一个群聊里，群名「二次元茶话会」，你的名字是「远坂凛」，身份是「傲娇」。",
		"你是雪之下雪乃（出自《我的青春恋爱物语果然有问题》）。你正在一个群聊里，群名「老友们」，你的名字是「雪之下雪乃」，身份是「毒舌」。",
	}
	for _, sys := range systems {
		if got := mockLine(sys, "今天聊点什么好"); !slices.Contains(mockAnimeLines, got) {
			t.Fatalf("二次元场景应命中通用池，得到 %q（system=%q）", got, sys)
		}
	}

	// 同场景多次调用应轮换，不能几连击同一句
	seen := map[string]bool{}
	for _, seed := range []string{"a", "bb", "ccc", "dddd", "eeeee", "ffffff"} {
		seen[mockLine(systems[0], seed)] = true
	}
	if len(seen) < 3 {
		t.Fatalf("二次元池应轮换出至少 3 句，实际 %d 句", len(seen))
	}
}

func TestMockLineNoImmediateRepeat(t *testing.T) {
	// 同一角色连续两轮不能说同一句——哪怕用户连发几条一样的消息，
	// 兜底演示里连着两条一模一样最致命。
	systems := []string{
		"你的名字是「苏苏」，身份是测试。",                 // 命中专属台词
		"你是加藤惠（出自《路人女主的养成方法》）。群名「二次元茶话会」。", // 通用二次元池
		"你的名字是「无名氏」，身份是测试。",                // 兜底池
	}
	for _, sys := range systems {
		prev := ""
		for i := 0; i < 4; i++ {
			line := mockLine(sys, "嗯")
			if line == prev {
				t.Fatalf("连续两轮说了同一句 %q（system=%q）", line, sys)
			}
			prev = line
		}
	}
}
