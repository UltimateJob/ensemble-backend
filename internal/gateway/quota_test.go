package gateway

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func TestQuotaStorePersistsAndRedactsControlKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.json")
	s, err := OpenQuotaStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(QuotaConfig{
		Provider: "opendesign", Kind: "opendesign",
		ControlKey: "ctl-secret", WorkspaceID: "ws-1",
	}); err != nil {
		t.Fatal(err)
	}
	// 留空 ControlKey 时沿用原值，只改工作区。
	if err := s.Upsert(QuotaConfig{Provider: "opendesign", WorkspaceID: "ws-2"}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("opendesign")
	if !ok || got.ControlKey != "ctl-secret" || got.WorkspaceID != "ws-2" || got.Kind != "auto" {
		t.Fatalf("配置未按预期保存：%+v", got)
	}
	list := s.Snapshot()
	if len(list) != 1 || !list[0].HasControlKey {
		t.Fatalf("配置快照应标记 Control Key 已配置：%+v", list)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "ctl-secret") {
		t.Fatalf("配置快照泄漏了 Control Key：%s", raw)
	}

	reloaded, err := OpenQuotaStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reloaded.Get("opendesign"); got.ControlKey != "ctl-secret" {
		t.Fatalf("重启后 Control Key 未恢复：%+v", got)
	}
	if err := reloaded.Delete("opendesign"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("opendesign"); ok {
		t.Fatal("删除后不应再返回配置")
	}
}

func TestQuotaStoreRejectsBadInput(t *testing.T) {
	s, _ := OpenQuotaStore("")
	if err := s.Upsert(QuotaConfig{Provider: "Bad Name"}); err == nil {
		t.Fatal("非法渠道名应被拒绝")
	}
	if err := s.Upsert(QuotaConfig{Provider: "x", Kind: "nope"}); err == nil {
		t.Fatal("未知查询类型应被拒绝")
	}
}

func TestDetectQuotaKind(t *testing.T) {
	cases := []struct {
		name, base, override, want string
	}{
		{"deepseek", "https://api.deepseek.com", "", "deepseek"},
		{"commandcode", "https://api.commandcode.ai/provider/v1", "", "commandcode"},
		{"opendesign", "https://amr-link.open-design.ai/v1", "", "opendesign"},
		{"glm", "https://open.bigmodel.cn/api/paas/v4", "", "glm"},
		{"relay", "https://d1api.xin/v1", "", ""},
		{"relay", "https://d1api.xin/v1", "deepseek", "deepseek"},
		{"deepseek", "https://api.deepseek.com", "off", "off"},
	}
	for _, c := range cases {
		if got := detectQuotaKind(c.name, c.base, c.override); got != c.want {
			t.Fatalf("detectQuotaKind(%q,%q,%q)=%q，期望 %q", c.name, c.base, c.override, got, c.want)
		}
	}
}

func TestParseDeepSeekBalancePrefersCNY(t *testing.T) {
	body := []byte(`{"is_available":true,"balance_infos":[
		{"currency":"USD","total_balance":"10.00"},
		{"currency":"CNY","total_balance":"80.81","granted_balance":"0.00","topped_up_balance":"80.81"}]}`)
	q := parseDeepSeekBalance(body, domain.ChannelQuota{Provider: "deepseek"})
	if q.Err != "" || q.Currency != "CNY" || q.Remaining == nil || *q.Remaining != 80.81 {
		t.Fatalf("余额解析不正确：%+v", q)
	}
}

func TestParseCommandCodeCredits(t *testing.T) {
	body := []byte(`{"credits":{"monthlyCredits":42.1,"purchasedCredits":1,"freeCredits":0,"planId":"individual-goat"},
		"windowLimits":{"limited":true,"fiveHour":{"used":8.04,"cap":14,"exceeded":false,"resetAt":1792732966016},
		"weekly":{"used":27.87,"cap":35,"exceeded":false,"resetAt":1793201237783}}}`)
	q := parseCommandCodeCredits(body, domain.ChannelQuota{Provider: "commandcode"})
	if q.Err != "" || q.Currency != "USD" || q.Remaining == nil || *q.Remaining != 43.1 {
		t.Fatalf("Command Code 余额不正确：%+v", q)
	}
	if q.Plan != "GOAT" || len(q.Windows) != 2 {
		t.Fatalf("Command Code 套餐/窗口不正确：%+v", q)
	}
	if q.Windows[0].Label != "5 小时" || q.Windows[0].Remaining < 5.95 || q.Windows[0].Remaining > 5.97 || q.Windows[0].Percent < 57 || q.Windows[0].Percent > 58 {
		t.Fatalf("5 小时窗口不正确：%+v", q.Windows[0])
	}
	if q.Windows[1].Label != "7 天" || q.Windows[1].Remaining < 7.12 || q.Windows[1].Remaining > 7.14 {
		t.Fatalf("7 天窗口不正确：%+v", q.Windows[1])
	}
	if q.Windows[0].ResetAt.IsZero() {
		t.Fatal("应解析重置时间")
	}
}

func TestParseOpenDesignPreflightWindows(t *testing.T) {
	body := []byte(`{"workspaceId":"ws-1","workspaceMemberId":"m1","eligible":true,
		"windows":[
			{"policyId":"p5","durationSeconds":18000,"usedCredits":"4.00","remainingCredits":"6.00","limitCredits":"10.00","resetsAt":"2026-10-23T08:00:00.000Z"},
			{"policyId":"p7","durationSeconds":604800,"usedCredits":"30.00","remainingCredits":"70.00","limitCredits":"100.00","resetsAt":"2026-10-28T08:00:00.000Z"}
		],"balanceUsd":"12.34"}`)
	q := parseOpenDesignPreflight(body, "ws-1", domain.ChannelQuota{Provider: "opendesign"})
	if q.Err != "" || q.Currency != "USD" || q.Remaining == nil || *q.Remaining != 12.34 {
		t.Fatalf("OpenDesign 解析不正确：%+v", q)
	}
	if len(q.Windows) != 2 || q.Windows[0].Label != "5 小时" || q.Windows[0].Percent != 40 ||
		q.Windows[1].Label != "7 天" || q.Windows[1].Remaining != 70 {
		t.Fatalf("OpenDesign 窗口不正确：%+v", q.Windows)
	}
}

func TestParseOpenDesignCodingPlanWrapper(t *testing.T) {
	body := []byte(`{"workspaceId":"ws-1","eligible":true,"codingPlan":{
		"workspaceId":"ws-1","eligible":true,"windows":[
			{"policyId":"p5","durationSeconds":18000,"usedCredits":"2","remainingCredits":"8","limitCredits":"10","resetsAt":"2026-10-23T08:00:00Z"}
		],"balanceUsd":"5.5"}}`)
	q := parseOpenDesignPreflight(body, "ws-1", domain.ChannelQuota{Provider: "opendesign"})
	if q.Err != "" || len(q.Windows) != 1 || q.Remaining == nil || *q.Remaining != 5.5 {
		t.Fatalf("codingPlan 包装解析不正确：%+v", q)
	}
	other := parseOpenDesignPreflight(body, "ws-2", domain.ChannelQuota{Provider: "opendesign"})
	if other.Err == "" {
		t.Fatal("工作区不匹配时应报错")
	}
}

func TestParseGLMQuotaWindows(t *testing.T) {
	body := []byte(`{"code":200,"success":true,"data":{"level":"Max","limits":[
		{"type":"TOKENS_LIMIT","unit":5,"percentage":32.5,"nextResetTime":1792732966016},
		{"type":"TOKENS_LIMIT","unit":6,"percentage":41,"nextResetTime":1793201237783},
		{"type":"TIME_LIMIT","percentage":10}]}}`)
	q := parseGLMQuota(body, domain.ChannelQuota{Provider: "glm"})
	if q.Err != "" || q.Plan != "Max" || len(q.Windows) != 2 {
		t.Fatalf("GLM 解析不正确：%+v", q)
	}
	if q.Windows[0].Label != "5 小时" || q.Windows[0].Remaining != 67.5 {
		t.Fatalf("GLM 5 小时窗口不正确：%+v", q.Windows[0])
	}
	if q.Windows[1].Label != "7 天" || q.Windows[1].Remaining != 59 {
		t.Fatalf("GLM 7 天窗口不正确：%+v", q.Windows[1])
	}
}

func TestParseQuotaErrors(t *testing.T) {
	q := parseGLMQuota([]byte(`{"code":401,"msg":"token expired"}`), domain.ChannelQuota{})
	if q.Err == "" {
		t.Fatal("GLM 错误码应转为可读错误")
	}
	q = parseCommandCodeCredits([]byte(`not json`), domain.ChannelQuota{})
	if q.Err == "" {
		t.Fatal("非法 JSON 应报错")
	}
	if resetFromMillis(0) != (time.Time{}) {
		t.Fatal("0 毫秒不应产生重置时间")
	}
}
