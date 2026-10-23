package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

func TestPlanStoreDefaultsAndPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plans.json")
	p, err := NewPlanStore(path)
	if err != nil {
		t.Fatal(err)
	}
	list := p.List()
	if len(list) != 3 {
		t.Fatalf("默认应有 3 个套餐，得到 %d", len(list))
	}
	if _, ok := p.Get("free"); !ok {
		t.Fatal("默认应有 free 套餐")
	}

	if err := p.Upsert(domain.PlanConfig{
		ID: "starter", Name: "入门版", PriceMonthly: 9.9, MonthlyQuota: 1200,
		Enabled: true, Sort: 15, Features: []string{"每月 1,200 次", "  ", "每月 1,200 次"},
	}); err != nil {
		t.Fatal(err)
	}
	v, ok := p.Get("starter")
	if !ok {
		t.Fatal("新增套餐后应能查到")
	}
	if v.Name != "入门版" || v.PriceMonthly != 9.9 || v.MonthlyQuota != 1200 {
		t.Fatalf("套餐字段不正确：%+v", v)
	}
	if len(v.Features) != 1 {
		t.Fatalf("权益应去重并去掉空项：%+v", v.Features)
	}

	reloaded, err := NewPlanStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.Get("starter"); !ok || got.PriceMonthly != 9.9 {
		t.Fatalf("重启后套餐未恢复：%+v", got)
	}

	if err := p.Upsert(domain.PlanConfig{ID: "Bad ID", Name: "x"}); err == nil {
		t.Fatal("非法 ID 应被拒绝")
	}
	if err := p.Upsert(domain.PlanConfig{ID: "negative", Name: "x", PriceMonthly: -1}); err == nil {
		t.Fatal("负数价格应被拒绝")
	}
}

func TestPlanStoreDeleteProtectsLast(t *testing.T) {
	p, _ := NewPlanStore("")
	p.mu.Lock()
	p.rows = map[string]domain.PlanConfig{"only": {ID: "only", Name: "唯一", MonthlyQuota: 1}}
	p.mu.Unlock()
	if err := p.Delete("only"); err == nil {
		t.Fatal("最后一个套餐不应该被删除")
	}
	if err := p.Delete("nope"); err == nil {
		t.Fatal("删除不存在的套餐应报错")
	}
}

func TestBillingRecordStatsAndPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "billing.json")
	b, err := NewBilling(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	b.Record(domain.UsageEvent{
		UserID: "u1", Model: "m1", Provider: "p1",
		PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500,
		CostCNY: 0.12, At: now,
	})
	b.Record(domain.UsageEvent{
		UserID: "u1", Model: "m1", Provider: "p1",
		PromptTokens: 2000, CompletionTokens: 1000, TotalTokens: 3000,
		CostCNY: 0.24, At: now,
	})
	b.Record(domain.UsageEvent{
		UserID: "u2", Model: "m2", Provider: "p2",
		PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
		CostCNY: 0.01, At: now,
	})

	st := b.Stats(7)
	if st.Totals.Requests != 3 || st.Totals.TotalTokens() != 4650 {
		t.Fatalf("总用量不正确：%+v", st.Totals)
	}
	if st.Month.Requests != 3 || st.Month.CostCNY < 0.369 || st.Month.CostCNY > 0.371 {
		t.Fatalf("本月用量不正确：%+v", st.Month)
	}
	if len(st.Days) != 7 {
		t.Fatalf("趋势应补齐 7 天，得到 %d", len(st.Days))
	}
	if st.Days[len(st.Days)-1].Requests != 3 {
		t.Fatalf("今天应有 3 次调用：%+v", st.Days[len(st.Days)-1])
	}
	if len(st.Models) != 2 || st.Models[0].Model != "m1" {
		t.Fatalf("模型应按成本排序：%+v", st.Models)
	}
	if got := b.User("u1"); got.Month.Requests != 2 || got.Month.TotalTokens() != 4500 {
		t.Fatalf("用户本月用量不正确：%+v", got.Month)
	}

	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewBilling(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.User("u1")
	if got.CostCNY < 0.359 || got.CostCNY > 0.361 {
		t.Fatalf("重启后累计成本未恢复：%+v", got)
	}
	if reloaded.Status().Requests != 3 {
		t.Fatalf("重启后请求数未恢复：%+v", reloaded.Status())
	}
}

func TestCommerceRevenueAndProfit(t *testing.T) {
	st := store.NewMemory()
	_ = st.CreateUser(&domain.User{
		ID: "u1", Name: "付费用户", Plan: "pro", Quota: 6000,
		Status: "active", CreatedAt: time.Now(),
	})
	gw := commerceTestGateway()
	admin := NewAdmin(st, gw, &Shared{Version: "test"})
	b, _ := NewBilling("")
	admin.SetCommercial(nil, b)
	b.Record(domain.UsageEvent{
		UserID: "u1", Model: "m1", PromptTokens: 1000, CompletionTokens: 500,
		CostCNY: 10, At: time.Now(),
	})

	rep := admin.Commerce(30)
	if rep.Revenue.MRR != 39 {
		t.Fatalf("MRR 应为 pro 套餐价格 39，得到 %v", rep.Revenue.MRR)
	}
	if rep.Revenue.PaidUsers != 1 || rep.Revenue.ActiveUsers != 1 {
		t.Fatalf("付费/活跃用户数不正确：%+v", rep.Revenue)
	}
	if rep.Profit.Cost != 10 || rep.Profit.Gross != 29 {
		t.Fatalf("毛利计算不正确：%+v", rep.Profit)
	}
	if len(rep.TopUsers) != 1 || rep.TopUsers[0].UserID != "u1" || rep.TopUsers[0].CostCNY != 10 {
		t.Fatalf("Top 用户不正确：%+v", rep.TopUsers)
	}
	if len(rep.Plans) == 0 {
		t.Fatal("应返回套餐收入")
	}
}

func commerceTestGateway() *gateway.Gateway {
	reg := gateway.NewRegistry()
	reg.Add(&gateway.Mock{})
	table := gateway.NewModelTable([]domain.ModelInfo{
		{Key: "m", Provider: "mock", Upstream: "mock", Enabled: true, Weight: 1},
	})
	return gateway.New(reg, table, 0, 3, time.Second)
}
