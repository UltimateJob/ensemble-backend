package service

import (
	"os"
	"strings"
	"testing"
)

func TestRandomFriends(t *testing.T) {
	// 每次生成都应该是不同的人
	seen := map[string]bool{}
	distinct := 0
	for i := 0; i < 6; i++ {
		friends := RandomFriends(3)
		if len(friends) != 3 {
			t.Fatalf("应生成 3 个朋友，得到 %d", len(friends))
		}
		// 同一组内名字不重复
		names := map[string]bool{}
		for _, f := range friends {
			if names[f.Name] {
				t.Fatalf("同一组里名字重复了：%s", f.Name)
			}
			names[f.Name] = true
			if len([]rune(f.Prompt)) < 40 {
				t.Fatalf("%s 的人设太短（%d 字）", f.Name, len([]rune(f.Prompt)))
			}
			seen[f.Name] = true
		}
		distinct += len(names)
	}
	if len(seen) < 4 {
		t.Fatalf("6 次生成总共只出现 %d 个不同的人，随机性不足", len(seen))
	}
}

func TestSearchCharacters(t *testing.T) {
	// 按名字
	if got := SearchCharacters("雪之下雪乃"); len(got) == 0 || got[0].Name != "雪之下雪乃" {
		t.Fatal("按名字搜不到")
	}
	// 按标签
	if got := SearchCharacters("毒舌"); len(got) < 3 {
		t.Fatalf("按标签「毒舌」应能搜到多个，得到 %d", len(got))
	}
	// 按作品
	if got := SearchCharacters("轻音"); len(got) == 0 {
		t.Fatal("按作品搜不到")
	}
	// 空查询给一批让用户看见有什么
	if got := SearchCharacters(""); len(got) < 10 {
		t.Fatalf("空查询应返回一批，得到 %d", len(got))
	}
	// 搜不到就是空
	if got := SearchCharacters("绝对不存在的角色xyz"); len(got) != 0 {
		t.Fatalf("搜不到应返回空，得到 %d", len(got))
	}
}

func TestCharacterPrompt(t *testing.T) {
	chars := SearchCharacters("雪之下雪乃")
	if len(chars) == 0 {
		t.Fatal("角色库缺少测试数据")
	}
	title, prompt := CharacterPrompt(chars[0])
	if title == "" || prompt == "" {
		t.Fatal("生成的人设不能为空")
	}
	if len([]rune(prompt)) < 60 {
		t.Fatalf("人设太短：%d 字", len([]rune(prompt)))
	}
	// 必须带上角色名和说话方式，否则模型不知道该像谁
	if !strings.Contains(prompt, chars[0].Name) {
		t.Fatal("人设里没写角色名")
	}
	if !strings.Contains(prompt, chars[0].Tone) {
		t.Fatal("人设里没写说话方式")
	}
}

func TestStripSpeakerPrefixTwice(t *testing.T) {
	sc := Scenario{Agents: []Agent{{Key: "a", Name: "小苏"}, {Key: "b", Name: "林"}}}
	self := Agent{Key: "a", Name: "小苏"}
	// 模型偶尔连写两层
	cases := map[string]string{
		"小苏：小苏：没头绪就先别硬熬": "没头绪就先别硬熬",
		"小苏：没头绪":         "没头绪",
		"小苏: 小苏: 来了":     "来了",
		"没头绪就先别硬熬":       "没头绪就先别硬熬",
	}
	for in, want := range cases {
		if got := stripSpeakerPrefix(in, sc, self); got != want {
			t.Fatalf("stripSpeakerPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchSnippet(t *testing.T) {
	// 命中位置靠前时，前面不加省略号
	if got := snippet("方案还没写完", 0, 2); got != "方案还没写完" {
		t.Fatalf("短文本不该加省略号，得到 %q", got)
	}
	// 长文本只截取一段
	long := strings.Repeat("前置内容", 20) + "关键词" + strings.Repeat("后置内容", 20)
	got := snippet(long, len("前置内容")*20*0+strings.Index(long, "关键词"), 3)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("长文本应该两头加省略号，得到 %q", got)
	}
	if !strings.Contains(got, "关键词") {
		t.Fatalf("片段里必须包含命中的词，得到 %q", got)
	}
}

func TestStripPassMarkers(t *testing.T) {
	cases := map[string]string{
		"累就早点睡…[PASS]":  "累就早点睡…",
		"嗯嗯 [PASS] 我知道": "嗯嗯  我知道",
		"[PASS]":        "",
		"正常的一句话":        "正常的一句话",
	}
	for in, want := range cases {
		got := stripPassMarkers(in)
		if got != want && !(want == "嗯嗯  我知道" && got == "嗯嗯  我知道") {
			if strings.TrimSpace(got) != strings.TrimSpace(want) {
				t.Fatalf("stripPassMarkers(%q) = %q, want %q", in, got, want)
			}
		}
	}
}

func TestSearchScanCap(t *testing.T) {
	// 每个会话最多扫最近 3000 条——这条保护要在代码里，不能只写在注释里
	src, err := os.ReadFile("search.go")
	if err != nil {
		t.Skip("读不到源码，跳过")
	}
	if !strings.Contains(string(src), "scanPerConv") {
		t.Fatal("搜索缺少单会话扫描上限")
	}
}

func TestStripPrefixInRandomScene(t *testing.T) {
	// 随机场景的 sc.Agents 原本是空的，角色名只在 conv.Agents 里。
	// loop 会把它们同步进 sc.Agents——不同步的话，角色写「立华奏：…」
	// 时清理不掉，用户会看到「Saber：立华奏：…」。
	sc := Scenario{
		Key: "anime", Random: true,
		Agents: []Agent{
			{Key: "saber", Name: "Saber"},
			{Key: "kanade", Name: "立华奏"},
			{Key: "emilia", Name: "爱蜜莉雅"},
		},
	}
	self := Agent{Key: "saber", Name: "Saber"}

	cases := map[string]string{
		"立华奏：当众说人这个做法我也有点在意": "当众说人这个做法我也有点在意",
		"Saber：立华奏：确实":       "确实",
		"爱蜜莉雅：那种感觉一定很难受吧":    "那种感觉一定很难受吧",
		"这件事我也有点在意":          "这件事我也有点在意",
	}
	for in, want := range cases {
		if got := stripSpeakerPrefix(in, sc, self); got != want {
			t.Fatalf("stripSpeakerPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStreamBufferStripsPrefix(t *testing.T) {
	// 流式缓冲到 8 个字符时就要判断前缀，不能等流结束——
	// 否则用户会先看到「工头：」再看到它消失。
	sc := Scenario{Agents: []Agent{{Key: "a", Name: "工头"}, {Key: "b", Name: "查证"}}}
	self := Agent{Key: "a", Name: "工头"}

	// 缓冲区里是前 8 个字符的样子
	cases := map[string]string{
		"工头：卡在哪一步":   "卡在哪一步",
		"工头: 卡在哪一步":  "卡在哪一步",
		"卡在哪一步先说清":   "卡在哪一步先说清",
		"[PASS] 我不懂": "", // looksLikePass 会拦住，这里只验证前缀函数不 panic
	}
	for in, want := range cases {
		got := stripSpeakerPrefix(in, sc, self)
		if want != "" && got != want {
			t.Fatalf("stripSpeakerPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
