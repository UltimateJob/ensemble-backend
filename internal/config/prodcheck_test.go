package config

import (
	"strings"
	"testing"
)

func TestProdProblems(t *testing.T) {
	// 非生产模式：怎么都不该报
	c := Config{Prod: false, DevBypass: true, AdminToken: DefaultAdminToken}
	if p := c.ProdProblems(); len(p) != 0 {
		t.Fatalf("非生产模式不该报问题，得到 %v", p)
	}

	// 生产模式 + 全套默认值：四个问题都该报
	c = Config{Prod: true, DevBypass: true, AdminToken: DefaultAdminToken, DataDir: "data"}
	probs := c.ProdProblems()
	if len(probs) < 3 {
		t.Fatalf("生产模式下的默认配置应至少报 3 个问题，得到 %d: %v", len(probs), probs)
	}
	joined := strings.Join(probs, " | ")
	for _, want := range []string{"DEV_BYPASS", "ADMIN_TOKEN"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("应提到 %s，实际：%s", want, joined)
		}
	}

	// 短令牌也要报
	c = Config{Prod: true, DevBypass: false, AdminToken: "short123456789", DataDir: "data"}
	found := false
	for _, p := range c.ProdProblems() {
		if strings.Contains(p, "太短") {
			found = true
		}
	}
	if !found {
		t.Fatal("14 字符的令牌应报太短")
	}
}

func TestProdProblemsAllGood(t *testing.T) {
	// 全部配好：不该报（这里不测 Key，因为 UpstreamConfigured 依赖别的字段）
	c := Config{
		Prod:       true,
		DevBypass:  false,
		AdminToken: "0123456789abcdef0123456789abcdef",
		DataDir:    "data",
	}
	probs := c.ProdProblems()
	// 唯一允许剩下的是「没配 Key」——这个测试不设 Key
	for _, p := range probs {
		if strings.Contains(p, "DEV_BYPASS") || strings.Contains(p, "ADMIN_TOKEN") {
			t.Fatalf("配好之后不该再报这两项：%s", p)
		}
	}
}
