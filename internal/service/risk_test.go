package service

import (
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

func newTestAdmin(t *testing.T) (*Admin, *store.Memory, *Billing, *Wallet, *SettingsStore) {
	t.Helper()
	st := store.NewMemory()
	gw := gateway.New(gateway.NewRegistry(), gateway.NewModelTable([]domain.ModelInfo{
		{Key: "deepseek-chat", Provider: "deepseek", Enabled: true, Weight: 1},
	}), 0, 5, time.Minute)
	admin := NewAdmin(st, gw, &Shared{})
	plans, _ := NewPlanStore("")
	billing, _ := NewBilling("")
	settings, _ := OpenSettings("")
	wallet, _ := OpenWallet("")
	admin.SetCommercial(plans, billing)
	admin.SetControl(settings, wallet)
	return admin, st, billing, wallet, settings
}

func TestRiskReportFlagsMarginCostAndBalance(t *testing.T) {
	admin, st, billing, wallet, settings := newTestAdmin(t)
	u := &domain.User{
		ID: "u-risk", Name: "高风险用户", Plan: "pro", Quota: 1000,
		Status: "active", Role: "user",
	}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	billing.Record(domain.UsageEvent{
		UserID: u.ID, Model: "deepseek-chat", Provider: "deepseek",
		PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500,
		CostCNY: 120, At: now,
	})
	if _, err := wallet.TopUp(u.ID, 10, "测试", "admin"); err != nil {
		t.Fatal(err)
	}
	wallet.Spend(u.ID, 15, "deepseek-chat")

	next := settings.Get()
	next.RiskUserCostCNY = 10
	next.RiskDailySpendCNY = 50
	if _, err := settings.Update(next); err != nil {
		t.Fatal(err)
	}

	rep := admin.Risk(30)
	if rep.Summary.MonthCostCNY != 120 {
		t.Fatalf("本月成本应为 120，得到 %v", rep.Summary.MonthCostCNY)
	}
	if rep.Summary.MRRCNY != 39 {
		t.Fatalf("MRR 应为 pro 的 39，得到 %v", rep.Summary.MRRCNY)
	}
	if rep.Summary.MarginPct >= 0 {
		t.Fatalf("毛利率应为负，得到 %v", rep.Summary.MarginPct)
	}
	if rep.Summary.UsersOverThreshold != 1 || rep.Summary.NegativeBalances != 1 {
		t.Fatalf("用户阈值/负余额计数不正确：%+v", rep.Summary)
	}
	kinds := map[string]bool{}
	for _, al := range rep.Alerts {
		kinds[al.Kind] = true
	}
	for _, want := range []string{"margin", "daily_spend", "user_cost", "share", "balance"} {
		if !kinds[want] {
			t.Fatalf("缺少 %s 告警：%+v", want, rep.Alerts)
		}
	}
	if len(rep.Users) != 1 || rep.Users[0].UserID != u.ID || rep.Users[0].Level == "normal" {
		t.Fatalf("风险用户不正确：%+v", rep.Users)
	}
	if ru, ok := admin.RiskForUser(u.ID); !ok || ru.BalanceCNY != -5 {
		t.Fatalf("单用户风险画像不正确：%+v %v", ru, ok)
	}
}

func TestSignupPolicyClosedAndDefaults(t *testing.T) {
	st := store.NewMemory()
	auth := NewAuth(st, "secret", LogSender{}, true)

	auth.SetSignupPolicy(func() SignupPolicy {
		return SignupPolicy{Open: false, Plan: "free", Quota: 800}
	})
	if _, _, err := auth.Login("phone", "13800000000", "", ""); err != ErrRegistrationClosed {
		t.Fatalf("关闭注册后应拒绝新账号，得到 %v", err)
	}

	auth.SetSignupPolicy(func() SignupPolicy {
		return SignupPolicy{Open: true, Plan: "pro", Quota: 4321}
	})
	u, _, err := auth.Login("phone", "13800000000", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if u.Plan != "pro" || u.Quota != 4321 {
		t.Fatalf("新用户应使用注册策略：%+v", u)
	}

	// 已有用户不受关闭注册影响。
	auth.SetSignupPolicy(func() SignupPolicy { return SignupPolicy{Open: false} })
	if _, _, err := auth.Login("phone", "13800000000", "", ""); err != nil {
		t.Fatalf("存量用户登录不应被注册开关影响：%v", err)
	}
}

func TestPlanPermissionFields(t *testing.T) {
	p, err := NewPlanStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Upsert(domain.PlanConfig{
		ID: "pro", Name: "专业版", MonthlyQuota: 6000, Enabled: true,
		AllowedModels: []string{"deepseek-chat", " deepseek-chat ", "deepseek-reasoner"},
		RatePerMinute: 120, MaxConcurrency: 4,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := p.Get("pro")
	if len(got.AllowedModels) != 2 || got.RatePerMinute != 120 || got.MaxConcurrency != 4 {
		t.Fatalf("权限字段未保存：%+v", got)
	}
	if err := p.Upsert(domain.PlanConfig{ID: "pro", Name: "x", RatePerMinute: -1}); err == nil {
		t.Fatal("负限流值应被拒绝")
	}
	if err := p.Upsert(domain.PlanConfig{ID: "pro", Name: "x", MaxConcurrency: -1}); err == nil {
		t.Fatal("负并发值应被拒绝")
	}
}
