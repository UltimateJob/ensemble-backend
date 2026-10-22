package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"ADDR", "ADMIN_TOKEN", "DEEPSEEK_API_KEY", "DATA_DIR", "RATE_PER_MINUTE"} {
		t.Setenv(k, "")
	}
	c := Load()
	if c.Addr != ":8080" {
		t.Fatalf("默认监听地址应为 :8080，得到 %s", c.Addr)
	}
	if c.AllowMock != true {
		t.Fatal("默认应允许 mock 上游，否则没有 Key 时起不来")
	}
	if c.DataDir == "" {
		t.Fatal("数据目录不能为空")
	}
	if c.ShutdownGrace <= 0 {
		t.Fatal("优雅关闭时长必须为正")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("ADDR", ":9090")
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("RATE_PER_MINUTE", "42")
	t.Setenv("BREAKER_OPEN", "5s")
	t.Setenv("ALLOW_MOCK", "false")

	c := Load()
	if c.Addr != ":9090" {
		t.Fatalf("ADDR 未生效：%s", c.Addr)
	}
	if !c.UpstreamConfigured() {
		t.Fatal("有 Key 时应判定为已配置上游")
	}
	if c.RatePerMinute != 42 {
		t.Fatalf("整型环境变量未生效：%d", c.RatePerMinute)
	}
	if c.BreakerOpen != 5*time.Second {
		t.Fatalf("时长环境变量未生效：%v", c.BreakerOpen)
	}
	if c.AllowMock {
		t.Fatal("ALLOW_MOCK=false 未生效")
	}
}

func TestUpstreamConfigured(t *testing.T) {
	c := Config{DeepSeekAPIKey: ""}
	if c.UpstreamConfigured() {
		t.Fatal("空 Key 应判定为未配置")
	}
	c.DeepSeekAPIKey = "   "
	if c.UpstreamConfigured() {
		t.Fatal("全空格 Key 应判定为未配置")
	}
	c.DeepSeekAPIKey = "sk-x"
	if !c.UpstreamConfigured() {
		t.Fatal("有 Key 应判定为已配置")
	}
}

// EXTRA_PROVIDERS 是请求池的入口：格式错一条跳一条，
// 但写对的那些必须一个不落地注册上。
func TestParseExtraProviders(t *testing.T) {
	got := parseExtraProviders("a|https://a.com|sk-1|deepseek-chat, b |https://b.com|sk-2|,bad|https://c.com,a|https://dup.com|sk-3,,d||sk-4")
	if len(got) != 2 {
		t.Fatalf("应解析出 2 个（缺 key/重名/缺段全跳过），得到 %d: %+v", len(got), got)
	}
	if got[0].Name != "a" || got[0].BaseURL != "https://a.com" || got[0].APIKey != "sk-1" || got[0].Model != "deepseek-chat" {
		t.Fatalf("第一条解析错误: %+v", got[0])
	}
	if got[1].Name != "b" || got[1].Model != "" {
		t.Fatalf("第二条应允许省默认模型: %+v", got[1])
	}
	if parseExtraProviders("") != nil {
		t.Fatal("空串应返回 nil")
	}
}

// 配了中转池就算真实上游，不该降级 mock。
func TestUpstreamConfiguredWithRelays(t *testing.T) {
	t.Setenv("EXTRA_PROVIDERS", "r|https://r.com|sk-x")
	t.Setenv("DEEPSEEK_API_KEY", "")
	c := Load()
	if !c.UpstreamConfigured() {
		t.Fatal("只配中转池也应算已配置上游")
	}
}
