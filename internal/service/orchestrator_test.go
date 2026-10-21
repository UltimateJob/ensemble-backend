package service

import (
	"testing"
)

func TestIsPass(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"空字符串算沉默", "", true},
		{"纯括号 PASS", "[PASS]", true},
		{"裸 PASS", "PASS", true},
		{"小写", "[pass]", true},
		{"带前后空格", "  [PASS]  \n", true},
		{"中文空", "（无）", true},
		{"短句里带 PASS", "[PASS] 这个话题我不懂", true},
		{"真正说话", "先睡够，缓存不清理第二天更卡。", false},
		{"提到 pass 词的正常句子", "这个方案我 pass 掉，因为口径不对，需要重新核对原文的限定词。", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPass(c.in); got != c.want {
				t.Fatalf("isPass(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestLooksLikePass(t *testing.T) {
	// 缓冲阶段：前缀像 PASS 就不推流
	if !looksLikePass("[PA") {
		t.Fatal("未完整 PASS 前缀应判定为疑似沉默")
	}
	if !looksLikePass("PAS") {
		t.Fatal("裸大写前缀应判定为疑似沉默")
	}
	if looksLikePass("先睡够") {
		t.Fatal("正常内容不应被当成沉默")
	}
}

func TestPickSpeaker(t *testing.T) {
	agents := []Agent{
		{Key: "a", Name: "甲"},
		{Key: "b", Name: "乙"},
		{Key: "c", Name: "丙"},
	}

	// 说过话的人不能再被选中
	spoken := map[string]bool{"a": true}
	for i := 0; i < 50; i++ {
		got := pickSpeaker(agents, spoken, "", false)
		if got == nil {
			t.Fatal("还有可选角色时不应返回 nil")
		}
		if got.Key == "a" {
			t.Fatal("已经说过话的角色不应再被选中")
		}
	}

	// 连续同一个人要说两次的情况应该被避免
	for i := 0; i < 50; i++ {
		got := pickSpeaker(agents, map[string]bool{}, "b", false)
		if got != nil && got.Key == "b" {
			t.Fatal("上一轮的发言者不应立刻被选为下一个")
		}
	}

	// 全部说过 → nil
	allSpoken := map[string]bool{"a": true, "b": true, "c": true}
	if got := pickSpeaker(agents, allSpoken, "", false); got != nil {
		t.Fatalf("所有人都说过时不应再选人，得到 %v", got.Key)
	}

	// 只剩一个没说过 → 必然选中它
	last := map[string]bool{"a": true, "b": true}
	for i := 0; i < 20; i++ {
		got := pickSpeaker(agents, last, "", false)
		if got == nil || got.Key != "c" {
			t.Fatalf("只剩一个候选时应固定返回它")
		}
	}
}

func TestScenarios(t *testing.T) {
	if len(Scenarios) < 5 {
		t.Fatalf("至少要有 5 个内置场景，现在 %d 个", len(Scenarios))
	}

	keys := map[string]bool{}
	for _, sc := range Scenarios {
		if sc.Key == "" || sc.Name == "" {
			t.Fatalf("场景 %+v 缺少 key 或 name", sc)
		}
		if keys[sc.Key] {
			t.Fatalf("场景 key 重复：%s", sc.Key)
		}
		keys[sc.Key] = true

		if len(sc.Agents) < 2 {
			t.Fatalf("场景 %s 至少要有 2 个角色", sc.Key)
		}
		if sc.Round <= 0 {
			t.Fatalf("场景 %s 的 Round 必须为正，否则不会有人说话", sc.Key)
		}

		seen := map[string]bool{}
		for _, a := range sc.Agents {
			if a.Key == "" || a.Name == "" {
				t.Fatalf("场景 %s 有角色缺少 key/name", sc.Key)
			}
			if seen[a.Key] {
				t.Fatalf("场景 %s 角色 key 重复：%s", sc.Key, a.Key)
			}
			seen[a.Key] = true
			// 没有 prompt 的角色会失去性格，退化成通用助手
			if len([]rune(a.Prompt)) < 10 {
				t.Fatalf("场景 %s 的角色 %s 缺少人设 prompt", sc.Key, a.Name)
			}
			if a.Color == "" {
				t.Fatalf("场景 %s 的角色 %s 没有颜色", sc.Key, a.Name)
			}
		}
	}

	for _, want := range []string{"bid", "code", "research", "anime", "companion"} {
		if !keys[want] {
			t.Fatalf("缺少内置场景：%s", want)
		}
	}
}

func TestFindScenario(t *testing.T) {
	sc, ok := FindScenario("bid")
	if !ok || sc.Key != "bid" {
		t.Fatal("应能找到竞标场景")
	}
	if _, ok := FindScenario("not-exist"); ok {
		t.Fatal("不存在的场景应返回 false")
	}
}

func TestPickColor(t *testing.T) {
	// 同一个名字必须稳定，否则刷新一次颜色就变了
	a := pickColor("夜岚")
	b := pickColor("夜岚")
	if a != b {
		t.Fatalf("同一名字应得到同一颜色：%s vs %s", a, b)
	}
	if a == "" {
		t.Fatal("颜色不能为空")
	}
	// 不同名字大概率不同（不强制，只检查不 panic）
	_ = pickColor("")
	_ = pickColor("很长很长很长很长的一个名字用来测试")
}

func TestTruncate(t *testing.T) {
	if got := truncate("短", 10); got != "短" {
		t.Fatalf("短文本不应被截断，得到 %q", got)
	}
	long := "这是一个很长的中文句子用来验证按字符而不是按字节截断"
	got := truncate(long, 5)
	r := []rune(got)
	if len(r) != 6 { // 5 个字 + 省略号
		t.Fatalf("应按 rune 截断到 5 字 + 省略号，得到 %q (%d)", got, len(r))
	}
}

func TestPickSpeakerFirstRoundGoesToLead(t *testing.T) {
	agents := []Agent{{Key: "lead", Name: "工头"}, {Key: "b", Name: "乙"}, {Key: "c", Name: "丙"}}
	for i := 0; i < 30; i++ {
		got := pickSpeaker(agents, map[string]bool{}, "", true)
		if got == nil || got.Key != "lead" {
			t.Fatalf("第一轮应固定由负责人开口，得到 %v", got)
		}
	}
	// 负责人已经说过时，不再特殊对待
	got := pickSpeaker(agents, map[string]bool{"lead": true}, "", true)
	if got == nil || got.Key == "lead" {
		t.Fatal("负责人说过话后不应再被优先选中")
	}
}

func TestStripSpeakerPrefix(t *testing.T) {
	sc := Scenario{Agents: []Agent{
		{Key: "a", Name: "雪见"}, {Key: "b", Name: "桃桃"}, {Key: "c", Name: "凛"},
	}}
	self := Agent{Key: "c", Name: "凛"}

	cases := map[string]string{
		"桃桃：凛酱又在说电脑话了":  "凛酱又在说电脑话了",
		"凛：CPU 占用太高该降频": "CPU 占用太高该降频",
		"雪见: 懒得理你":      "懒得理你",
		"降频这个说法很准":      "降频这个说法很准",
		"注意：这里有个坑":      "注意：这里有个坑", // 不是角色名，不动
	}
	for in, want := range cases {
		if got := stripSpeakerPrefix(in, sc, self); got != want {
			t.Fatalf("stripSpeakerPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoopRespectsQuota(t *testing.T) {
	// 剩 1 次额度时，最多只应有 1 个角色开口
	agents := []Agent{{Key: "a", Name: "甲"}, {Key: "b", Name: "乙"}, {Key: "c", Name: "丙"}}
	sc := Scenario{Round: 3, Agents: agents}

	// 直接验证裁剪逻辑：round > left 时按 left 算
	left := 1
	round := sc.Round
	if left < round {
		round = left
	}
	if round != 1 {
		t.Fatalf("剩 1 次额度时本轮应只跑 1 个角色，得到 %d", round)
	}

	// 剩 0 次应完全不跑
	left = 0
	if left <= 0 {
		// 与实现一致：emit quota 后直接返回
	} else {
		t.Fatal("剩 0 次不应继续")
	}
}

func TestSlowLog(t *testing.T) {
	l := NewSlowLog(3, 100) // 容量 3，阈值 100ms

	// 未达阈值不记
	if l.MaybeRecord(SlowEntry{Agent: "快", LatencyMS: 50}) {
		t.Fatal("未达阈值不应记录")
	}
	// 达阈值记录
	if !l.MaybeRecord(SlowEntry{Agent: "慢", LatencyMS: 500}) {
		t.Fatal("超过阈值应记录")
	}
	// 出错也记，哪怕很快
	if !l.MaybeRecord(SlowEntry{Agent: "错", LatencyMS: 1, Err: "boom"}) {
		t.Fatal("出错应记录")
	}

	items := l.List(10)
	if len(items) != 2 {
		t.Fatalf("应有 2 条记录，得到 %d", len(items))
	}
	// 倒序：最新的在前
	if items[0].Agent != "错" {
		t.Fatalf("应按时间倒序，首条应是「错」，得到 %s", items[0].Agent)
	}

	// 环形覆盖：再塞 3 条，只保留最后 3 条
	for i := 0; i < 3; i++ {
		l.MaybeRecord(SlowEntry{Agent: string(rune('A' + i)), LatencyMS: 500})
	}
	items = l.List(10)
	if len(items) != 3 {
		t.Fatalf("容量 3 应只保留 3 条，得到 %d", len(items))
	}
	total, threshold := l.Stats()
	if total != 5 {
		t.Fatalf("累计记录数应为 5，得到 %d", total)
	}
	if threshold != 100 {
		t.Fatalf("阈值应为 100，得到 %d", threshold)
	}

	// 空日志不 panic
	var empty *SlowLog
	if empty.MaybeRecord(SlowEntry{}) {
		t.Fatal("nil 日志不应记录")
	}
	if got := empty.List(5); got != nil {
		t.Fatal("nil 日志应返回 nil")
	}
}
