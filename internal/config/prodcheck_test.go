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

	// 生产模式 + 全套默认值：所有问题都该报
	c = Config{Prod: true, DevBypass: true, AdminToken: DefaultAdminToken, DataDir: "data"}
	probs := c.ProdProblems()
	if len(probs) < 3 {
		t.Fatalf("生产模式下的默认配置应至少报 3 个问题，得到 %d: %v", len(probs), probs)
	}
	joined := strings.Join(probs, " | ")
	for _, want := range []string{"DEV_BYPASS", "ADMIN_TOKEN", "JWT_SECRET"} {
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

func TestProdProblemsJWT(t *testing.T) {
	// JWT_SECRET：默认值 / 空 / 过短都要报——
	// 会话 token 全靠它签名，密钥不安全等于任何人都能伪造登录态。
	cases := []struct {
		name string
		sec  string
		want string
	}{
		{"默认值", DefaultJWTSecret, "JWT_SECRET"},
		{"空", "", "JWT_SECRET"},
		{"太短（16 字节）", "0123456789abcdef", "太短"},
	}
	for _, c := range cases {
		cfg := Config{Prod: true, DevBypass: false,
			AdminToken: "0123456789abcdef0123456789abcdef",
			JWTSecret:  c.sec, DataDir: "data"}
		found := false
		for _, p := range cfg.ProdProblems() {
			if strings.Contains(p, c.want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s 的 JWT_SECRET 应报「%s」，实际：%v", c.name, c.want, cfg.ProdProblems())
		}
	}

	// 32 字节随机串不该报
	cfg := Config{Prod: true, DevBypass: false,
		AdminToken: "0123456789abcdef0123456789abcdef",
		JWTSecret:  "0123456789abcdef0123456789abcdef", DataDir: "data"}
	for _, p := range cfg.ProdProblems() {
		if strings.Contains(p, "JWT_SECRET") {
			t.Fatalf("合格的 JWT_SECRET 不该再报：%s", p)
		}
	}
}

func TestProdProblemsAllGood(t *testing.T) {
	// 全部配好：不该报（这里不测 Key，因为 UpstreamConfigured 依赖别的字段）
	c := Config{
		Prod:       true,
		DevBypass:  false,
		AdminToken: "0123456789abcdef0123456789abcdef",
		JWTSecret:  "0123456789abcdef0123456789abcdef",
		DataDir:    "data",
	}
	probs := c.ProdProblems()
	// 唯一允许剩下的是「没配 Key」——这个测试不设 Key
	for _, p := range probs {
		if strings.Contains(p, "DEV_BYPASS") || strings.Contains(p, "ADMIN_TOKEN") || strings.Contains(p, "JWT_SECRET") {
			t.Fatalf("配好之后不该再报这三项：%s", p)
		}
	}
}
