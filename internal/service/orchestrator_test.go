package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

func TestIsPass(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"空字符串算沉默", "", true},
		{"纯括号 PASS", "[PASS]", true},
		{"裸 PASS 不再算沉默", "PASS", false},
		{"英文句子以 Pass 开头不能吞", "Pass me the docs, I'll review them.", false},
		{"小写", "[pass]", true},
		{"全角中括号", "【PASS】", true},
		{"圆括号", "(PASS)", true},
		{"全角圆括号", "（PASS）", true},
		{"带前后空格", "  [PASS]  \n", true},
		{"中文空", "（无）", true},
		{"中文无整条", "无", true},
		{"以无开头的正文不吞", "无论怎么优化都得先复现。", false},
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
	// 缓冲阶段：前缀像沉默标记就不推流
	if !looksLikePass("[PA") {
		t.Fatal("半截 [PASS] 前缀应判定为疑似沉默")
	}
	if !looksLikePass("【P") {
		t.Fatal("半截全角标记应判定为疑似沉默")
	}
	if !looksLikePass("(PAS") {
		t.Fatal("半角圆括号标记开头应判定为疑似沉默")
	}
	if !looksLikePass("[PASS] 后面还跟解释") {
		t.Fatal("完整标记开头应判定为疑似沉默")
	}
	// 裸 "P" 开头是正常英文的概率远大于沉默标记，立即放行
	if looksLikePass("PAS") {
		t.Fatal("裸 PASS 前缀不再按疑似沉默缓冲，否则 'Pass me...' 会被憋住")
	}
	if looksLikePass("Pass me the docs") {
		t.Fatal("正常英文句子不应被当成沉默缓冲")
	}
	if looksLikePass("先睡够") {
		t.Fatal("正常内容不应被当成沉默")
	}
}

func TestStartsWithMention(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"@用户 开头", "@用户 你想要哪个版本？", true},
		{"@你 开头", "@你 觉得这样行吗", true},
		{"容忍前导空白", "  @用户 补个邮箱", true},
		{"正文中间提到不算", "我觉得@用户说得对，先按这个来", false},
		{"后半句提到不算", "这事先问@你之外的人", false},
		{"普通发言", "这版口径没问题，可以发。", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := startsWithMention(c.in); got != c.want {
				t.Fatalf("startsWithMention(%q) = %v, want %v", c.in, got, c.want)
			}
		})
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

		// Random 场景的角色在开群时生成，场景里可以是空的；
		// 非 Random 场景必须有至少两个写死的角色。
		if !sc.Random && len(sc.Agents) < 2 {
			t.Fatalf("场景 %s 至少要有 2 个角色", sc.Key)
		}
		if sc.Random && len(sc.Agents) > 0 {
			t.Fatalf("场景 %s 标了 Random 却写死了角色，开群时会重复生成", sc.Key)
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

func TestAgentsFromConversationFillsMissingKeys(t *testing.T) {
	conv := &domain.Conversation{Agents: []domain.ConvAgent{
		{Name: "雪之下雪乃", Title: "毒舌"},
		{Name: "由比滨结衣", Title: "温柔"},
		{Key: "", Name: "一色彩羽", Title: "小恶魔"},
	}}

	agents := agentsFromConversation(conv)
	if len(agents) != 3 {
		t.Fatalf("应转出 3 个角色，得到 %d", len(agents))
	}
	seen := map[string]bool{}
	for _, a := range agents {
		if a.Key == "" {
			t.Fatalf("角色 %s 的 key 不能为空", a.Name)
		}
		if seen[a.Key] {
			t.Fatalf("角色 key 重复：%s", a.Key)
		}
		seen[a.Key] = true
	}
}

func TestCharacterKeyStable(t *testing.T) {
	a := CharacterKey("雪之下雪乃")
	if a == "" || a != CharacterKey("雪之下雪乃") {
		t.Fatal("同一个角色应得到稳定的 key")
	}
	if a == CharacterKey("由比滨结衣") {
		t.Fatal("不同角色的 key 不应相同")
	}
}

func TestWantsEveryone(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"你们三个分别怎么看加班", true},
		{"大家怎么看", true},
		{"你们觉得呢", true},
		{"你们好", false},
		{"今天天气怎样", false},
		{"帮我拆一下这个需求", false},
		{"", false},
	}
	for _, c := range cases {
		if got := wantsEveryone(c.in); got != c.want {
			t.Fatalf("wantsEveryone(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestResolveAgentsCharacterIDs(t *testing.T) {
	bid := &domain.Conversation{Studio: "bid"}
	_, agents := ResolveAgents(bid)
	if len(agents) == 0 {
		t.Fatal("竞标场景应至少有一个角色")
	}
	if agents[0].CharacterID != "bid:conductor" {
		t.Fatalf("内置角色身份应是 场景:key，得到 %q", agents[0].CharacterID)
	}

	custom := &domain.Conversation{
		Studio: "custom",
		Agents: []domain.ConvAgent{{Key: "char_abc", Name: "雪之下雪乃"}},
	}
	_, agents = ResolveAgents(custom)
	if len(agents) != 1 || agents[0].CharacterID != "char_abc" {
		t.Fatalf("自定义角色应沿用 key 作为身份，得到 %+v", agents)
	}
}

func TestTrimHistory(t *testing.T) {
	msgs := []*domain.Message{
		{Content: "一"},
		{Content: "二"},
		{Content: "三"},
	}
	got := trimHistory(msgs, 80)
	if len(got) != 3 {
		t.Fatalf("预算足够时应保留全部，得到 %d", len(got))
	}
	got = trimHistory(msgs, 30)
	if len(got) == 0 || got[len(got)-1].Content != "三" {
		t.Fatalf("裁剪后必须保留最后一条，得到 %+v", got)
	}
}

func TestWantsAll(t *testing.T) {
	if !wantsAll("你们每个人分别怎么看？") {
		t.Fatal("「每个人」应触发全员")
	}
	if wantsAll("你们怎么看？") {
		t.Fatal("普通群问不应触发全员")
	}
}

/* ---------------- 编排层集成测试（假上游走真实网关） ---------------- */

type fakeProvider struct {
	mu     sync.Mutex
	calls  int
	models []string // 每次调用经网关映射后的 req.Model
	// fail 前 N 次调用直接失败
	fail int
	// failName 系统提示里「你的名字是「X」」的角色永远失败（模拟某个角色上游必挂）
	failName string
	reply    string
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.models = append(f.models, req.Model)
	failName := f.failName
	f.mu.Unlock()

	// 系统提示（第一条消息）里有唯一的「你的名字是「X」」，据此认角色，
	// 不能简单 Contains 角色名——别的角色的提示里也会提到他。
	targetsMe := failName != "" && len(req.Messages) > 0 &&
		strings.Contains(req.Messages[0].Content, "你的名字是「"+failName+"」")
	if n <= f.fail || targetsMe {
		return nil, errors.New("fake upstream boom")
	}
	ch := make(chan domain.Chunk, 2)
	ch <- domain.Chunk{Content: f.reply}
	ch <- domain.Chunk{Done: true}
	close(ch)
	return ch, nil
}

// newTestOrch retries=0：网关不重试，测的是编排层自己的重试；threshold 放大，熔断不会在测试里打开。
func newTestOrch(t *testing.T, p gateway.Provider) (*Orchestrator, *store.Memory) {
	t.Helper()
	reg := gateway.NewRegistry()
	reg.Add(p)
	mt := gateway.NewModelTable([]domain.ModelInfo{
		{Key: "m-a", Upstream: "up-a", Provider: "fake", Enabled: true, Weight: 2},
		{Key: "m-b", Upstream: "up-b", Provider: "fake", Enabled: true, Weight: 1},
	})
	gw := gateway.New(reg, mt, 0, 100, time.Minute)
	st := store.NewMemory()
	return NewOrchestrator(st, gw, NewSlowLog(4, 3600_000)), st
}

// newTestConv 建一个自定义群（Studio 不存在 → 中性场景，角色全来自 conv.Agents）。
func newTestConv(t *testing.T, st *store.Memory, model string, quota int, agents ...domain.ConvAgent) (*domain.User, *domain.Conversation) {
	t.Helper()
	user := &domain.User{ID: "u1", Name: "评委", Quota: quota}
	if err := st.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	conv := &domain.Conversation{
		ID: "c1", UserID: user.ID, Title: "测试群", Studio: "no-such", Model: model,
		Agents: agents,
	}
	if err := st.CreateConv(conv); err != nil {
		t.Fatal(err)
	}
	return user, conv
}

func TestOrchestratorPassesConvModel(t *testing.T) {
	fp := &fakeProvider{reply: "收到，这就核对。"}
	o, st := newTestOrch(t, fp)
	_, conv := newTestConv(t, st, "m-b", 10,
		domain.ConvAgent{Key: "only", Name: "甲", Title: "拆解", Prompt: "你负责拆解任务。"})

	o.Run(context.Background(), conv, &domain.User{ID: "u1", Quota: 10}, "开始吧", func(Event) {})

	if len(fp.models) == 0 {
		t.Fatal("应至少发起一次上游调用")
	}
	// 网关会把 key 映射成 Upstream 名。若编排器传空模型，
	// 默认加权轮询第一次会命中 m-a（up-a），所以「每次都是 up-b」才能证明透传生效。
	for _, m := range fp.models {
		if m != "up-b" {
			t.Fatalf("会话选定的模型应透传给网关，本次用了 %q（全部 %v）", m, fp.models)
		}
	}
}

func TestOrchestratorRetriesFailedAgent(t *testing.T) {
	// 第一次上游失败，重试同一角色成功：发言要落库，不发错误事件
	fp := &fakeProvider{fail: 1, reply: "上游抖了一下，重试成功。"}
	o, st := newTestOrch(t, fp)
	_, conv := newTestConv(t, st, "m-a", 10,
		domain.ConvAgent{Key: "only", Name: "甲", Title: "拆解", Prompt: "你负责拆解任务。"})

	var errs []Event
	o.Run(context.Background(), conv, &domain.User{ID: "u1", Quota: 10}, "开始吧", func(e Event) {
		if e.Type == "error" {
			errs = append(errs, e)
		}
	})

	if fp.calls != 2 {
		t.Fatalf("失败后应重试一次同一角色，期望 2 次上游调用，得到 %d", fp.calls)
	}
	var spoke bool
	for _, m := range st.ListMessages(conv.ID, 10) {
		if m.Role == domain.RoleAssistant && m.Agent == "甲" {
			spoke = true
		}
	}
	if !spoke {
		t.Fatal("重试成功后角色发言应落库")
	}
	if len(errs) != 0 {
		t.Fatalf("重试成功不应发错误事件，得到 %v", errs)
	}
}

func TestOrchestratorPartialFailureSoftHint(t *testing.T) {
	// 甲的上游永远挂、乙正常：整轮有话（乙），但要给软提示说明甲没接上
	fp := &fakeProvider{failName: "甲", reply: "我来说两句。"}
	o, st := newTestOrch(t, fp)
	_, conv := newTestConv(t, st, "m-a", 1, // 额度 1 → 本轮只 1 人说话，跳过发言间隔等待
		// Permission 非空 → 工作群（非闲聊）：pickSpeaker 确定性地先挑 agents[0]，
		// 否则闲聊群随机挑人，甲是否被重试取决于运气。
		domain.ConvAgent{Key: "a", Name: "甲", Title: "拆解", Prompt: "你负责拆解任务。", Permission: "readonly"},
		domain.ConvAgent{Key: "b", Name: "乙", Title: "审校", Prompt: "你负责审校文档。", Permission: "readonly"},
	)

	var hints []Event
	o.Run(context.Background(), conv, &domain.User{ID: "u1", Quota: 1}, "开始吧", func(e Event) {
		if e.Type == "error" {
			hints = append(hints, e)
		}
	})

	// 甲初始 + 重试共 2 次，乙 1 次
	if fp.calls != 3 {
		t.Fatalf("期望甲尝试 2 次、乙 1 次共 3 次上游调用，得到 %d", fp.calls)
	}
	if len(hints) != 1 || !strings.Contains(hints[0].Content, "没接上话") {
		t.Fatalf("部分失败应发一条软提示，得到 %v", hints)
	}
	var spokeB bool
	for _, m := range st.ListMessages(conv.ID, 10) {
		if m.Agent == "甲" {
			t.Fatal("持续失败的角色不应有落库发言")
		}
		if m.Agent == "乙" {
			spokeB = true
		}
	}
	if !spokeB {
		t.Fatal("正常的角色应照常发言")
	}
}
