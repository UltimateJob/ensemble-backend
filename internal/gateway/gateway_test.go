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

// 自动选路应避开在途占满的渠道：slow 渠道忙时，第二个请求走 fast 渠道。
func TestLeastBusyRouting(t *testing.T) {
	reg := NewRegistry()
	slow := &gatingProvider{name: "slow", gate: make(chan struct{})}
	fast := &echoProvider{name: "fast"}
	reg.Add(slow)
	reg.Add(fast)
	mt := NewModelTable([]domain.ModelInfo{
		{Key: "m-slow", Provider: "slow", Upstream: "up-slow", Enabled: true, Weight: 5},
		{Key: "m-fast", Provider: "fast", Upstream: "up-fast", Enabled: true, Weight: 5},
	})
	gw := New(reg, mt, 0, 100, time.Minute)

	// 第一个请求：两边都空闲，按权重轮询落在其中一个；
	// 用在途计数确认它真的占住了渠道。
	s1, err := gw.Stream(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// 等选路落地（goroutine 启动后 BusyAdd 生效需要一点时间）
	deadline := time.Now().Add(2 * time.Second)
	var busySlow, busyFast int64
	for time.Now().Before(deadline) {
		busySlow, busyFast = reg.Busy("slow"), reg.Busy("fast")
		if busySlow+busyFast == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if busySlow+busyFast != 1 {
		t.Fatalf("第一个请求应占用恰好一个渠道，slow=%d fast=%d", busySlow, busyFast)
	}
	busyName := "slow"
	idleName := "fast"
	if busyFast == 1 {
		busyName, idleName = "fast", "slow"
	}

	// 第二个请求：必须落在空闲的那个渠道上。
	s2, err := gw.Stream(context.Background(), domain.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s2
	deadline = time.Now().Add(2 * time.Second)
	var busyIdle int64
	for time.Now().Before(deadline) {
		if busyIdle = reg.Busy(idleName); busyIdle == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if busyIdle != 1 {
		t.Fatalf("第二个请求应路由到空闲渠道 %s（在途=%d）", idleName, busyIdle)
	}
	if b := reg.Busy(busyName); b != 1 {
		t.Fatalf("忙渠道 %s 在途应为 1，得到 %d", busyName, b)
	}
	close(slow.gate)
	<-s1
	<-s2
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
	m, p, ok := gw.pickModel(domain.ChatRequest{})
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
		m, p, ok := gw.pickModel(domain.ChatRequest{})
		if !ok || m.Key != "chat-model" {
			t.Fatalf("聊天自动选路应始终落在 chat-model，得到 %s/%s (ok=%v)", m.Key, p, ok)
		}
	}
	for i := 0; i < 6; i++ {
		m, _, ok := gw.pickModel(domain.ChatRequest{Task: true})
		if !ok || m.Key != "task-model" {
			t.Fatalf("任务选路应始终落在 task-model，得到 %s (ok=%v)", m.Key, ok)
		}
	}
	// 池空回退：全部标成任务专用时，聊天请求退回全量而不是失败
	mt2 := NewModelTable([]domain.ModelInfo{
		{Key: "only-task", Provider: "p1", Enabled: true, TaskOnly: true},
	})
	gw2 := New(reg, mt2, 0, 100, time.Minute)
	if m, _, ok := gw2.pickModel(domain.ChatRequest{}); !ok || m.Key != "only-task" {
		t.Fatalf("池空回退失败：%v %v", m.Key, ok)
	}
}
