package gateway

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func TestModelTablePick(t *testing.T) {
	table := NewModelTable([]domain.ModelInfo{
		{Key: "a", Provider: "mock", Enabled: true, Weight: 1},
		{Key: "b", Provider: "mock", Enabled: true, Weight: 1},
		{Key: "off", Provider: "mock", Enabled: false, Weight: 100},
	})

	// 停用的模型永远不该被选中，即使权重很高
	for i := 0; i < 200; i++ {
		m, ok := table.Pick("")
		if !ok {
			t.Fatal("应有可用模型")
		}
		if m.Key == "off" {
			t.Fatal("已停用的模型不应被选中")
		}
	}

	// 指定 key 优先
	if m, _ := table.Pick("a"); m.Key != "a" {
		t.Fatalf("指定模型应精确匹配，得到 %s", m.Key)
	}

	// 指定一个停用的 → 回退到可用池
	if m, ok := table.Pick("off"); !ok || m.Key == "off" {
		t.Fatal("指定停用模型时应回退到可用池")
	}

	// 加权：权重 9:1 时，a 出现的次数应明显多于 b
	weighted := NewModelTable([]domain.ModelInfo{
		{Key: "a", Provider: "mock", Enabled: true, Weight: 9},
		{Key: "b", Provider: "mock", Enabled: true, Weight: 1},
	})
	count := map[string]int{}
	for i := 0; i < 1000; i++ {
		m, _ := weighted.Pick("")
		count[m.Key]++
	}
	if count["a"] <= count["b"]*3 {
		t.Fatalf("加权轮询未生效：a=%d b=%d", count["a"], count["b"])
	}
}

func TestModelTableEmpty(t *testing.T) {
	table := NewModelTable(nil)
	if _, ok := table.Pick(""); ok {
		t.Fatal("空模型表不应返回可用模型")
	}
}

func TestModelTableUpsertDelete(t *testing.T) {
	table := NewModelTable(nil)
	table.Upsert(domain.ModelInfo{Key: "x", Provider: "mock", Enabled: true})
	if _, ok := table.Get("x"); !ok {
		t.Fatal("upsert 后应能查到")
	}
	table.Upsert(domain.ModelInfo{Key: "x", Provider: "mock", Enabled: false})
	if m, _ := table.Get("x"); m.Enabled {
		t.Fatal("第二次 upsert 应覆盖")
	}
	if !table.Delete("x") {
		t.Fatal("删除已存在的模型应返回 true")
	}
	if table.Delete("x") {
		t.Fatal("重复删除应返回 false")
	}
}

func TestCircuitBreaker(t *testing.T) {
	reg := NewRegistry()
	reg.Add(&Mock{})

	const threshold = 2
	const openFor = 60 * time.Millisecond

	// 初始放行
	if !reg.Allow("mock", threshold, openFor) {
		t.Fatal("初始状态应放行")
	}

	// 连续失败达阈值 → 打开
	reg.Report("mock", false, 10*time.Millisecond, "boom", threshold, openFor)
	reg.Report("mock", false, 10*time.Millisecond, "boom", threshold, openFor)
	if reg.Allow("mock", threshold, openFor) {
		t.Fatal("熔断打开后应拒绝")
	}

	// 超时后进入半开，放一个试探
	time.Sleep(openFor + 10*time.Millisecond)
	if !reg.Allow("mock", threshold, openFor) {
		t.Fatal("熔断超时后应放行一个试探")
	}

	// 试探成功 → 恢复
	reg.Report("mock", true, 5*time.Millisecond, "", threshold, openFor)
	if !reg.Allow("mock", threshold, openFor) {
		t.Fatal("试探成功后应恢复为关闭")
	}

	stats := reg.Stats()
	if len(stats) != 1 {
		t.Fatalf("应有一个上游统计，得到 %d", len(stats))
	}
	if stats[0].Success == 0 {
		t.Fatal("成功次数未记录")
	}
}

func TestRegistryReset(t *testing.T) {
	reg := NewRegistry()
	reg.Add(&Mock{})

	reg.Report("mock", false, time.Millisecond, "x", 1, time.Hour)
	if reg.Allow("mock", 1, time.Hour) {
		t.Fatal("应处于熔断")
	}
	if !reg.Reset("mock") {
		t.Fatal("重置已存在的上游应成功")
	}
	if !reg.Allow("mock", 1, time.Hour) {
		t.Fatal("重置后应放行")
	}
	if reg.Reset("nope") {
		t.Fatal("重置不存在的上游应失败")
	}
}

func TestMockProviderStreams(t *testing.T) {
	m := &Mock{}
	ch, err := m.Chat(t.Context(), domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "你好"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	sawDone := false
	for c := range ch {
		if c.Err != nil {
			t.Fatal(c.Err)
		}
		got += c.Content
		if c.Usage != nil {
			sawDone = true
		}
	}
	if got == "" {
		t.Fatal("mock 应输出内容")
	}
	if !sawDone {
		t.Fatal("应有一个带 usage 的收尾块")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(60, 3) // 每秒 1 个，容量 3

	for i := 0; i < 3; i++ {
		if !l.Allow("u1") {
			t.Fatalf("容量内第 %d 次应放行", i+1)
		}
	}
	if l.Allow("u1") {
		t.Fatal("超出容量应拒绝")
	}
	// 不同用户互不影响
	if !l.Allow("u2") {
		t.Fatal("其他用户不应被限流")
	}
}

func TestLatencyPercentile(t *testing.T) {
	w := newLatWindow(100)
	if got := w.percentile(0.95); got != 0 {
		t.Fatalf("空窗口应返回 0，得到 %v", got)
	}

	// 1..100 ms
	for i := int64(1); i <= 100; i++ {
		w.record(i)
	}
	if c := w.count(); c != 100 {
		t.Fatalf("样本数应为 100，得到 %d", c)
	}

	p50 := w.percentile(0.5)
	p95 := w.percentile(0.95)
	p100 := w.percentile(1.0)

	if p50 < 49 || p50 > 51 {
		t.Fatalf("P50 应接近 50，得到 %v", p50)
	}
	if p95 < 94 || p95 > 96 {
		t.Fatalf("P95 应接近 95，得到 %v", p95)
	}
	if p100 != 100 {
		t.Fatalf("最大值应为 100，得到 %v", p100)
	}

	// 环形覆盖：超过容量后只保留最近的
	small := newLatWindow(10)
	for i := int64(1); i <= 20; i++ {
		small.record(i)
	}
	if c := small.count(); c != 10 {
		t.Fatalf("环形窗口应只保留 10 个，得到 %d", c)
	}
	if max := small.percentile(1.0); max != 20 {
		t.Fatalf("最后写入的应是最大值 20，得到 %v", max)
	}
}

func TestMetricsP95NotEstimated(t *testing.T) {
	// P95 不应该再是「均值 × 1.8」
	reg := NewRegistry()
	reg.Add(&Mock{})
	table := NewModelTable([]domain.ModelInfo{
		{Key: "m", Provider: "mock", Upstream: "mock", Enabled: true, Weight: 1},
	})
	gw := New(reg, table, 1, 3, time.Second)

	// 直接塞几个极端样本，验证 P95 走的是分位数而不是倍数估算
	gw.latency.record(10)
	gw.latency.record(20)
	gw.latency.record(30)
	gw.latency.record(900)
	gw.latency.record(1000)

	p95 := gw.latency.percentile(0.95)
	if p95 < 900 {
		t.Fatalf("P95 应反映尾部延迟（接近 1000），得到 %v", p95)
	}
}

// scriptedProvider 按脚本回放 chunk 序列，每次调用消费一组。
// 用于构造「流到一半断掉」「第一次失败、第二次成功」这类上游行为。
type scriptedProvider struct {
	calls   atomic.Int32
	streams [][]domain.Chunk
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	n := int(p.calls.Add(1))
	script := p.streams[(n-1)%len(p.streams)]
	ch := make(chan domain.Chunk, len(script)+1)
	for _, c := range script {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func newScriptedGateway(t *testing.T, retries int, streams [][]domain.Chunk) (*Gateway, *scriptedProvider) {
	t.Helper()
	p := &scriptedProvider{streams: streams}
	reg := NewRegistry()
	reg.Add(p)
	table := NewModelTable([]domain.ModelInfo{
		{Key: "m", Provider: "scripted", Enabled: true, Weight: 1},
	})
	// 阈值给高一点，测的是重试语义，别让熔断插手
	return New(reg, table, retries, 100, time.Second), p
}

func TestStreamBreakNoRetryAfterForwarded(t *testing.T) {
	// P0：回答转发到一半流断了，绝不能整单重试——
	// 第二次的完整回答会拼在半截回答后面，落库就是「半截+完整再来一遍」。
	gw, p := newScriptedGateway(t, 2, [][]domain.Chunk{
		{
			{Content: "回答的前半截"},
			{Err: errors.New("broken pipe"), Done: true},
		},
	})

	out, err := gw.Stream(t.Context(), domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var text string
	var streamErr error
	for c := range out {
		if c.Err != nil {
			streamErr = c.Err
		}
		text += c.Content
	}

	if streamErr == nil {
		t.Fatal("中途断流应以错误结束，而不是悄悄吞掉")
	}
	if text != "回答的前半截" {
		t.Fatalf("断流后不应重试拼接，得到 %q", text)
	}
	if n := p.calls.Load(); n != 1 {
		t.Fatalf("已转发过内容就不应重试，上游被调了 %d 次", n)
	}
}

func TestStreamRetryOnlyWhenNothingForwarded(t *testing.T) {
	// 一行都没转发出去时重试是无副作用的，应该照常重试
	gw, p := newScriptedGateway(t, 1, [][]domain.Chunk{
		{
			{Err: errors.New("first attempt dies"), Done: true},
		},
		{
			{Content: "第二次的完整回答"},
			{Done: true, Usage: &domain.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
		},
	})

	out, err := gw.Stream(t.Context(), domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var text string
	for c := range out {
		if c.Err != nil {
			t.Fatalf("重试后应成功，却收到错误：%v", c.Err)
		}
		text += c.Content
	}
	if text != "第二次的完整回答" {
		t.Fatalf("应拿到第二次的完整回答，得到 %q", text)
	}
	if n := p.calls.Load(); n != 2 {
		t.Fatalf("没转发过内容应重试一次，上游被调了 %d 次", n)
	}
}

func TestHalfOpenSingleProbe(t *testing.T) {
	reg := NewRegistry()
	reg.Add(&Mock{})

	const threshold = 1
	const openFor = 30 * time.Millisecond

	reg.Report("mock", false, time.Millisecond, "boom", threshold, openFor)
	if reg.Allow("mock", threshold, openFor) {
		t.Fatal("熔断打开后应拒绝")
	}

	time.Sleep(openFor + 5*time.Millisecond)
	if !reg.Allow("mock", threshold, openFor) {
		t.Fatal("冷却结束后应放行一个探测")
	}
	// 探测在途中：其他调用必须被拒，否则半开等于全放
	if reg.Allow("mock", threshold, openFor) {
		t.Fatal("半开状态同时只能放一个探测")
	}

	// 探测失败 → 立刻回熔断
	reg.Report("mock", false, time.Millisecond, "probe failed", threshold, openFor)
	if reg.Allow("mock", threshold, openFor) {
		t.Fatal("探测失败应重新熔断")
	}

	// 再冷却后能放新探测；探测成功恢复正常放行
	time.Sleep(openFor + 5*time.Millisecond)
	if !reg.Allow("mock", threshold, openFor) {
		t.Fatal("冷却后应放行新探测")
	}
	reg.Report("mock", true, time.Millisecond, "", threshold, openFor)
	if !reg.Allow("mock", threshold, openFor) {
		t.Fatal("探测成功后应恢复为关闭")
	}
}

func TestHalfOpenProbeTimeout(t *testing.T) {
	// 探测请求可能永远不上报结果（比如半路被取消），
	// 名额必须能超时释放，否则永远卡死在半开。
	reg := NewRegistry()
	reg.Add(&Mock{})

	const openFor = 30 * time.Millisecond
	reg.Report("mock", false, time.Millisecond, "boom", 1, openFor)
	time.Sleep(openFor + 5*time.Millisecond)
	if !reg.Allow("mock", 1, openFor) {
		t.Fatal("应放行探测")
	}
	if reg.Allow("mock", 1, openFor) {
		t.Fatal("探测在途中应拒绝其他人")
	}

	time.Sleep(openFor + 10*time.Millisecond)
	if !reg.Allow("mock", 1, openFor) {
		t.Fatal("探测超时后应放行新探测")
	}
}

// echoProvider 立即返回一句话的可命名假上游。
type echoProvider struct{ name string }

func (p *echoProvider) Name() string { return p.name }
func (p *echoProvider) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	ch := make(chan domain.Chunk, 2)
	ch <- domain.Chunk{Content: "ok"}
	ch <- domain.Chunk{Done: true}
	close(ch)
	return ch, nil
}

// gatingProvider 慢渠道：第一个 chunk 要等 release 才发——用来占住在途名额。
type gatingProvider struct {
	name string
	gate chan struct{}
}

func (g *gatingProvider) Name() string { return g.name }
func (g *gatingProvider) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	ch := make(chan domain.Chunk, 2)
	go func() {
		defer close(ch)
		<-g.gate
		ch <- domain.Chunk{Content: "slow reply"}
		ch <- domain.Chunk{Done: true}
	}()
	return ch, nil
}

// 自动选路应避开在途占满的渠道：一个渠道被占住时，第二个请求走空闲的那个。
//
// 两个渠道都必须阻塞（gating），否则先完成的那条流会立刻归还计数，
// 测试就变成在赌选路顺序。之前成功路径不归还计数，这个测试靠泄漏
// 才「稳定」通过；修好计数后必须让两边都真实占用。
func TestLeastBusyRouting(t *testing.T) {
	reg := NewRegistry()
	a := &gatingProvider{name: "a", gate: make(chan struct{})}
	b := &gatingProvider{name: "b", gate: make(chan struct{})}
	reg.Add(a)
	reg.Add(b)
	mt := NewModelTable([]domain.ModelInfo{
		{Key: "m-a", Provider: "a", Upstream: "up-a", Enabled: true, Weight: 5},
		{Key: "m-b", Provider: "b", Upstream: "up-b", Enabled: true, Weight: 5},
	})
	gw := New(reg, mt, 0, 100, time.Minute)

	// 第一个请求占住其中一个渠道。
	s1, err := gw.Stream(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitBusy(t, reg, func(slow, fast int64) bool { return slow+fast == 1 })
	first, second := "a", "b"
	if reg.Busy("b") == 1 {
		first, second = "b", "a"
	}

	// 第二个请求必须落在空闲的那个上。
	s2, err := gw.Stream(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitBusy(t, reg, func(slow, fast int64) bool {
		if first == "a" {
			return slow == 1 && fast == 1
		}
		return slow == 1 && fast == 1
	})
	if reg.Busy(second) != 1 {
		t.Fatalf("第二个请求应路由到空闲渠道 %s（在途=%d）", second, reg.Busy(second))
	}

	close(a.gate)
	close(b.gate)
	<-s1
	<-s2
}

// waitBusy 等待两个渠道的在途数满足条件。
func waitBusy(t *testing.T, reg *Registry, ok func(a, b int64) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok(reg.Busy("a"), reg.Busy("b")) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("在途数未达到预期：a=%d b=%d", reg.Busy("a"), reg.Busy("b"))
}

// 熔断打开的渠道不接新流量；全部打开时退回全量候选（由 Allow 报错）。
func TestOpenCircuitExcludedFromPick(t *testing.T) {
	reg := NewRegistry()
	reg.Add(&echoProvider{name: "a"})
	reg.Add(&echoProvider{name: "b"})
	mt := NewModelTable([]domain.ModelInfo{
		{Key: "m-a", Provider: "a", Enabled: true, Weight: 1},
		{Key: "m-b", Provider: "b", Enabled: true, Weight: 1},
	})
	gw := New(reg, mt, 0, 2, time.Minute)

	// 连续失败把 a 打开熔断
	for i := 0; i < 3; i++ {
		reg.Report("a", false, time.Millisecond, "boom", 2, time.Minute)
	}
	if st := reg.State("a"); st != "open" {
		t.Fatalf("a 应已熔断，状态 %q", st)
	}
	m, p, ok := gw.pickModel(domain.ChatRequest{}, nil)
	if !ok {
		t.Fatal("应能选出模型")
	}
	if p == "a" {
		t.Fatalf("已熔断的渠道 a 不应被自动选中（选中 %s/%s）", p, m.Key)
	}
	_ = m
}

// 聊天自动选路只走对话模型；任务请求只走任务专用模型；
// 对应池为空时回退全量（标错池不该让请求失败）。
func TestTaskModelRoutingPools(t *testing.T) {
	reg := NewRegistry()
	reg.Add(&echoProvider{name: "p1"})
	reg.Add(&echoProvider{name: "p2"})
	mt := NewModelTable([]domain.ModelInfo{
		{Key: "chat-model", Provider: "p1", Enabled: true, Weight: 1},
		{Key: "task-model", Provider: "p2", Enabled: true, Weight: 1, TaskOnly: true},
	})
	gw := New(reg, mt, 0, 100, time.Minute)

	for i := 0; i < 6; i++ {
		m, p, ok := gw.pickModel(domain.ChatRequest{}, nil)
		if !ok || m.Key != "chat-model" {
			t.Fatalf("聊天自动选路应始终落在 chat-model，得到 %s/%s (ok=%v)", m.Key, p, ok)
		}
	}
	for i := 0; i < 6; i++ {
		m, _, ok := gw.pickModel(domain.ChatRequest{Task: true}, nil)
		if !ok || m.Key != "task-model" {
			t.Fatalf("任务选路应始终落在 task-model，得到 %s (ok=%v)", m.Key, ok)
		}
	}
	// 池空回退：全部标成任务专用时，聊天请求退回全量而不是失败
	mt2 := NewModelTable([]domain.ModelInfo{
		{Key: "only-task", Provider: "p1", Enabled: true, TaskOnly: true},
	})
	gw2 := New(reg, mt2, 0, 100, time.Minute)
	if m, _, ok := gw2.pickModel(domain.ChatRequest{}, nil); !ok || m.Key != "only-task" {
		t.Fatalf("池空回退失败：%v %v", m.Key, ok)
	}
}

// namedProvider 用固定名字注册，测试多渠道选路时用。
type namedProvider struct {
	name    string
	streams [][]domain.Chunk
	calls   atomic.Int32
}

func (p *namedProvider) Name() string { return p.name }
func (p *namedProvider) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	p.calls.Add(1)
	ch := make(chan domain.Chunk, 4)
	ch <- domain.Chunk{Content: "ok"}
	ch <- domain.Chunk{Done: true}
	close(ch)
	return ch, nil
}

// failingProvider 总是返回错误，用来验证「换渠道」而不是「同渠道重试」。
type failingProvider struct{ calls atomic.Int32 }

func (p *failingProvider) Name() string { return "failing" }
func (p *failingProvider) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	p.calls.Add(1)
	return nil, errors.New("503 no available channel")
}

func TestStreamFailsOverToBackupProvider(t *testing.T) {
	// 中转挂了（503）时必须切到保底渠道，而不是在同一个上游重试。
	bad := &failingProvider{}
	good := &scriptedProvider{streams: [][]domain.Chunk{
		{{Content: "保底渠道的回答"}, {Done: true, Usage: &domain.Usage{TotalTokens: 3}}},
	}}
	reg := NewRegistry()
	reg.Add(bad)
	reg.Add(good)
	table := NewModelTable([]domain.ModelInfo{
		{Key: "primary", Provider: "failing", Enabled: true, Weight: 1},
		{Key: "backup", Provider: "scripted", Enabled: true, Weight: 1},
	})
	gw := New(reg, table, 2, 100, time.Second)

	out, err := gw.Stream(t.Context(), domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for c := range out {
		if c.Err != nil {
			t.Fatalf("应切到保底渠道成功，却收到错误：%v", c.Err)
		}
		text += c.Content
	}
	if text != "保底渠道的回答" {
		t.Fatalf("text=%q", text)
	}
	if bad.calls.Load() > 1 {
		t.Fatalf("同一个失败渠道不应被反复重试：%d 次", bad.calls.Load())
	}
}

func TestStreamFailoverWhenNamedModelFails(t *testing.T) {
	// 用户点名了一个渠道；它挂了仍然要走保底，而不是直接失败。
	bad := &failingProvider{}
	good := &scriptedProvider{streams: [][]domain.Chunk{
		{{Content: "backup"}, {Done: true}},
	}}
	reg := NewRegistry()
	reg.Add(bad)
	reg.Add(good)
	table := NewModelTable([]domain.ModelInfo{
		{Key: "named", Provider: "failing", Enabled: true, Weight: 1},
		{Key: "spare", Provider: "scripted", Enabled: true, Weight: 1},
	})
	gw := New(reg, table, 2, 100, time.Second)

	out, err := gw.Stream(t.Context(), domain.ChatRequest{
		Model:    "named",
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for c := range out {
		if c.Err != nil {
			t.Fatalf("点名的渠道失败后应走保底，得到 %v", c.Err)
		}
		text += c.Content
	}
	if text != "backup" {
		t.Fatalf("text=%q", text)
	}
}

func TestPickPrefersLowerRank(t *testing.T) {
	table := NewModelTable([]domain.ModelInfo{
		{Key: "relay", Provider: "a", Enabled: true, Weight: 10, Rank: 2},
		{Key: "official", Provider: "b", Enabled: true, Weight: 1, Rank: 1},
	})
	reg := NewRegistry()
	reg.Add(&namedProvider{name: "a"})
	reg.Add(&namedProvider{name: "b"})
	gw := New(reg, table, 0, 100, time.Second)
	for i := 0; i < 50; i++ {
		m, _, ok := gw.pickModel(domain.ChatRequest{}, nil)
		if !ok || m.Key != "official" {
			t.Fatalf("应优先排名第 1 的官方渠道，得到 %s", m.Key)
		}
	}
}

func TestPickSpillsToNextRankWhenSaturated(t *testing.T) {
	table := NewModelTable([]domain.ModelInfo{
		{Key: "official", Provider: "a", Enabled: true, Weight: 1, Rank: 1},
		{Key: "relay", Provider: "b", Enabled: true, Weight: 5, Rank: 2},
	})
	reg := NewRegistry()
	reg.Add(&namedProvider{name: "a"})
	reg.Add(&namedProvider{name: "b"})
	gw := New(reg, table, 0, 100, time.Second)

	// 官方渠道占满默认并发上限后，应自动溢出到中转。
	reg.BusyAdd("a", defaultChannelConcurrency)
	for i := 0; i < 20; i++ {
		m, _, ok := gw.pickModel(domain.ChatRequest{}, nil)
		if !ok || m.Key != "relay" {
			t.Fatalf("官方饱和后应溢出到中转，得到 %s", m.Key)
		}
	}
}

func TestPickRoundRobinWithinTier(t *testing.T) {
	table := NewModelTable([]domain.ModelInfo{
		{Key: "a", Provider: "a", Enabled: true, Weight: 1, Rank: 1},
		{Key: "b", Provider: "b", Enabled: true, Weight: 1, Rank: 1},
	})
	reg := NewRegistry()
	reg.Add(&namedProvider{name: "a"})
	reg.Add(&namedProvider{name: "b"})
	gw := New(reg, table, 0, 100, time.Second)
	count := map[string]int{}
	for i := 0; i < 40; i++ {
		m, _, _ := gw.pickModel(domain.ChatRequest{}, nil)
		count[m.Key]++
	}
	if count["a"] == 0 || count["b"] == 0 {
		t.Fatalf("同排名应轮询，得到 %v", count)
	}
}

func TestPickSkipsOpenCircuit(t *testing.T) {
	table := NewModelTable([]domain.ModelInfo{
		{Key: "official", Provider: "a", Enabled: true, Weight: 1, Rank: 1},
		{Key: "relay", Provider: "b", Enabled: true, Weight: 1, Rank: 2},
	})
	reg := NewRegistry()
	reg.Add(&namedProvider{name: "a"})
	reg.Add(&namedProvider{name: "b"})
	gw := New(reg, table, 0, 1, time.Minute)
	reg.Report("a", false, time.Millisecond, "503", 1, time.Minute)
	if reg.State("a") != "open" {
		t.Fatal("阈值 1 时应熔断")
	}
	m, _, ok := gw.pickModel(domain.ChatRequest{}, nil)
	if !ok || m.Key != "relay" {
		t.Fatalf("熔断渠道不应被选，得到 %s", m.Key)
	}
}

func TestPickUsesConfiguredConcurrency(t *testing.T) {
	table := NewModelTable([]domain.ModelInfo{
		{Key: "official", Provider: "a", Enabled: true, Weight: 1, Rank: 1, Concurrency: 2},
		{Key: "relay", Provider: "b", Enabled: true, Weight: 5, Rank: 2},
	})
	reg := NewRegistry()
	reg.Add(&namedProvider{name: "a"})
	reg.Add(&namedProvider{name: "b"})
	gw := New(reg, table, 0, 100, time.Second)

	// 未到配置的并发上限（2）：仍优先官方。
	reg.BusyAdd("a", 1)
	if m, _, _ := gw.pickModel(domain.ChatRequest{}, nil); m.Key != "official" {
		t.Fatalf("未饱和时应走官方，得到 %s", m.Key)
	}
	// 到达上限：溢出到中转。
	reg.BusyAdd("a", 1)
	if m, _, _ := gw.pickModel(domain.ChatRequest{}, nil); m.Key != "relay" {
		t.Fatalf("达到并发上限应溢出，得到 %s", m.Key)
	}
}

// 回归：成功路径必须归还并发计数。
// 之前 chunk.Done 直接 return，计数只加不减；跑满默认上限后
// 所有渠道都被当成饱和，网关开始返回 no provider。
func TestStreamReleasesBusyOnSuccess(t *testing.T) {
	p := &namedProvider{name: "a"}
	reg := NewRegistry()
	reg.Add(p)
	table := NewModelTable([]domain.ModelInfo{
		{Key: "m", Provider: "a", Enabled: true, Weight: 1, Rank: 1},
	})
	gw := New(reg, table, 1, 100, time.Second)

	for i := 0; i < defaultChannelConcurrency*3; i++ {
		out, err := gw.Stream(t.Context(), domain.ChatRequest{
			Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("第 %d 次请求就失败：%v", i+1, err)
		}
		for c := range out {
			if c.Err != nil {
				t.Fatalf("第 %d 次请求出错：%v", i+1, c.Err)
			}
		}
		if b := reg.Busy("a"); b != 0 {
			t.Fatalf("第 %d 次请求后 in-flight=%d，应为 0", i+1, b)
		}
	}
}

func TestUsageObserverCost(t *testing.T) {
	p := &scriptedProvider{streams: [][]domain.Chunk{{
		{Content: "ok"},
		{Done: true, Usage: &domain.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}},
	}}}
	reg := NewRegistry()
	reg.Add(p)
	table := NewModelTable([]domain.ModelInfo{{
		Key: "m", Provider: "scripted", Enabled: true, Weight: 1,
		InputCostPer1M: 10, OutputCostPer1M: 20,
	}})
	gw := New(reg, table, 0, 3, time.Second)
	var events []domain.UsageEvent
	gw.SetUsageObserver(func(ev domain.UsageEvent) { events = append(events, ev) })

	out, err := gw.Stream(context.Background(), domain.ChatRequest{
		UserID: "u1", Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range out {
	}
	if len(events) != 1 {
		t.Fatalf("应上报一次用量，得到 %d", len(events))
	}
	ev := events[0]
	if ev.UserID != "u1" || ev.Model != "m" || ev.Provider != "scripted" {
		t.Fatalf("归因字段不正确：%+v", ev)
	}
	if ev.PromptTokens != 1000 || ev.CompletionTokens != 500 || ev.TotalTokens != 1500 {
		t.Fatalf("token 字段不正确：%+v", ev)
	}
	// 1000/1e6*10 + 500/1e6*20 = 0.02
	if ev.CostCNY < 0.0199 || ev.CostCNY > 0.0201 {
		t.Fatalf("成本计算不正确：%v", ev.CostCNY)
	}
}

// 套餐模型白名单：显式指定白名单外的模型时退回白名单内自动选路，
// 白名单内没有可用模型时不选路（请求会以 no_provider 失败）。
func TestPickModelAllowedList(t *testing.T) {
	reg := NewRegistry()
	reg.Add(&echoProvider{name: "p1"})
	reg.Add(&echoProvider{name: "p2"})
	mt := NewModelTable([]domain.ModelInfo{
		{Key: "cheap", Provider: "p1", Enabled: true, Weight: 1},
		{Key: "premium", Provider: "p2", Enabled: true, Weight: 1},
	})
	gw := New(reg, mt, 0, 100, time.Minute)

	// 会话存的是 premium，但套餐只允许 cheap：不报错，退回 cheap。
	m, p, ok := gw.pickModel(domain.ChatRequest{Model: "premium"}, []string{"cheap"})
	if !ok || m.Key != "cheap" || p != "p1" {
		t.Fatalf("白名单外的显式模型应退回白名单内选路，得到 %s/%s ok=%v", m.Key, p, ok)
	}
	// 白名单内模型可被显式指定。
	m, _, ok = gw.pickModel(domain.ChatRequest{Model: "cheap"}, []string{"cheap"})
	if !ok || m.Key != "cheap" {
		t.Fatalf("白名单内显式模型应被尊重，得到 %s ok=%v", m.Key, ok)
	}
	// 白名单里没有任何已启用模型：不选路。
	if _, _, ok := gw.pickModel(domain.ChatRequest{}, []string{"missing"}); ok {
		t.Fatal("白名单全部不可用时应返回 false")
	}
}
