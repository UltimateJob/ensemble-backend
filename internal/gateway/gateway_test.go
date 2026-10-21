package gateway

import (
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
