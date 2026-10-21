package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"ADDR", "ADMIN_TOKEN", "DEEPSEEK_API_KEY", "DATA_FILE", "RATE_PER_MINUTE"} {
		t.Setenv(k, "")
	}
	c := Load()
	if c.Addr != ":8080" {
		t.Fatalf("默认监听地址应为 :8080，得到 %s", c.Addr)
	}
	if c.AllowMock != true {
		t.Fatal("默认应允许 mock 上游，否则没有 Key 时起不来")
	}
	if c.DataFile == "" {
		t.Fatal("数据文件路径不能为空")
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
