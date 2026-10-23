package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsDefaultsPersistAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := OpenSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	def := s.Get()
	if !def.RegistrationOpen || def.DefaultPlan != "free" || def.MaintenanceMode {
		t.Fatalf("默认设置不符合预期：%+v", def)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("首次打开应写出默认设置：%v", err)
	}

	next := def
	next.RegistrationOpen = false
	next.DefaultPlan = "pro"
	next.TrialQuota = 123
	next.Announcement = "  今晚维护  "
	next.AnnouncementLevel = "WARNING"
	next.MaintenanceMode = true
	next.MaintenanceMessage = "  稍后再来  "
	saved, err := s.Update(next)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RegistrationOpen || saved.DefaultPlan != "pro" || saved.TrialQuota != 123 {
		t.Fatalf("设置未保存：%+v", saved)
	}
	if saved.Announcement != "今晚维护" || saved.AnnouncementLevel != "warning" ||
		saved.MaintenanceMessage != "稍后再来" {
		t.Fatalf("文本字段未清洗：%+v", saved)
	}
	if saved.AnnouncementAt.IsZero() {
		t.Fatal("公告应有发布时间")
	}

	reloaded, err := OpenSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get(); got.TrialQuota != 123 || got.RegistrationOpen {
		t.Fatalf("重启后设置未恢复：%+v", got)
	}

	if _, err := s.Update(Settings{Announcement: string(make([]rune, 501))}); err == nil {
		t.Fatal("超长公告应被拒绝")
	}
	if _, err := s.Update(Settings{RiskErrorRatePct: 101}); err == nil {
		t.Fatal("错误率阈值越界应被拒绝")
	}
}

func TestSettingsCorruptFileQuarantined(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Get().RegistrationOpen {
		t.Fatal("坏文件应回退默认设置")
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("坏文件应被隔离，找到 %v", matches)
	}
}

func TestWalletTopUpSpendAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallet.json")
	w, err := OpenWallet(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.TopUp("u1", 50, "首充", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Adjust("u1", -5, "手工修正", "admin"); err != nil {
		t.Fatal(err)
	}
	w.Spend("u1", 3.5, "deepseek-chat")

	acct := w.Account("u1")
	if acct.BalanceCNY != 41.5 || acct.TotalTopup != 50 || acct.TotalSpent != 3.5 {
		t.Fatalf("账户金额不正确：%+v", acct)
	}
	if got := w.Balance("unknown"); got != 0 {
		t.Fatalf("未知用户余额应为 0，得到 %v", got)
	}
	txns := w.Txns("u1", 10)
	if len(txns) != 3 || txns[0].Kind != "usage" || txns[0].AmountCNY != -3.5 {
		t.Fatalf("流水不正确：%+v", txns)
	}
	orders := w.Orders("u1", 10)
	if len(orders) != 1 || orders[0].Kind != "topup" || orders[0].AmountCNY != 50 {
		t.Fatalf("订单不正确：%+v", orders)
	}
	st := w.Stats()
	if st.Accounts != 1 || st.BalanceCNY != 41.5 || st.TotalTopupCNY != 50 || st.Orders != 1 {
		t.Fatalf("汇总不正确：%+v", st)
	}

	reloaded, err := OpenWallet(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Account("u1"); got.BalanceCNY != 41.5 || got.TotalSpent != 3.5 {
		t.Fatalf("重启后账户未恢复：%+v", got)
	}
}

func TestWalletRedeemCodes(t *testing.T) {
	w, err := OpenWallet("")
	if err != nil {
		t.Fatal(err)
	}
	code, err := w.CreateCode(RedeemCode{Code: "tw-test-1", AmountCNY: 20, MaxUses: 1})
	if err != nil {
		t.Fatal(err)
	}
	if code.Code != "TW-TEST-1" || code.Status(time.Now()) != "active" {
		t.Fatalf("兑换码规范化不正确：%+v", code)
	}
	res, err := w.Redeem("u1", "tw-test-1")
	if err != nil || res.AmountCNY != 20 || res.BalanceCNY != 20 {
		t.Fatalf("兑换失败：%+v %v", res, err)
	}
	if _, err := w.Redeem("u1", "TW-TEST-1"); err != ErrCodeUsedByUser {
		t.Fatalf("同一用户重复兑换应被拒绝，得到 %v", err)
	}
	if _, err := w.Redeem("u2", "TW-TEST-1"); err != ErrCodeUsedUp {
		t.Fatalf("超过次数应被拒绝，得到 %v", err)
	}
	if err := w.DeleteCode("TW-TEST-1"); err == nil {
		t.Fatal("已使用的兑换码不能删除")
	}

	planCode, err := w.CreateCode(RedeemCode{Code: "PLAN2026", Plan: "pro", MaxUses: 5})
	if err != nil {
		t.Fatal(err)
	}
	if planCode.AmountCNY != 0 || planCode.Plan != "pro" {
		t.Fatalf("套餐兑换码不正确：%+v", planCode)
	}
	res, err = w.Redeem("u3", "PLAN2026")
	if err != nil || res.Plan != "pro" || res.AmountCNY != 0 {
		t.Fatalf("套餐兑换失败：%+v %v", res, err)
	}

	if _, err := w.SetCodeDisabled("PLAN2026", true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Redeem("u4", "PLAN2026"); err != ErrCodeDisabled {
		t.Fatalf("停用后应拒绝兑换，得到 %v", err)
	}
	if _, err := w.Redeem("u4", "NOPE"); err != ErrCodeNotFound {
		t.Fatalf("不存在的码应返回 not found，得到 %v", err)
	}
	if _, err := w.CreateCode(RedeemCode{Code: "bad code!", AmountCNY: 1}); err == nil {
		t.Fatal("非法兑换码应被拒绝")
	}
	if _, err := w.CreateCode(RedeemCode{Code: "ZERO"}); err == nil {
		t.Fatal("既无金额也无套餐的码应被拒绝")
	}
}
