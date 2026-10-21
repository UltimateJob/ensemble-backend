// Package config 从环境变量加载配置，带合理默认值，零依赖。
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr            string // HTTP 监听地址
	AdminToken      string // 管理员令牌
	JWTSecret       string // 会话签名密钥
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	UpstreamTimeout time.Duration
	ShutdownGrace   time.Duration

	// 限流
	RatePerMinute int // 每用户每分钟请求数
	Burst         int

	// 网关
	MaxRetries   int
	BreakerError int           // 熔断阈值（连续失败次数）
	BreakerOpen  time.Duration // 熔断保持时长

	// DeepSeek
	DeepSeekBaseURL string
	DeepSeekAPIKey  string
	DeepSeekModel   string

	// 是否允许无 Key 时使用 mock 上游（本地演示）
	AllowMock bool

	// 数据落盘路径
	DataFile string
}

func Load() Config {
	c := Config{
		Addr:            env("ADDR", ":8080"),
		AdminToken:      env("ADMIN_TOKEN", "admin-dev-token"),
		JWTSecret:       env("JWT_SECRET", "change-me-in-production"),
		ReadTimeout:     dur("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:    dur("WRITE_TIMEOUT", 5*time.Minute),
		IdleTimeout:     dur("IDLE_TIMEOUT", 120*time.Second),
		UpstreamTimeout: dur("UPSTREAM_TIMEOUT", 120*time.Second),
		ShutdownGrace:   dur("SHUTDOWN_GRACE", 20*time.Second),

		RatePerMinute: intEnv("RATE_PER_MINUTE", 120),
		Burst:         intEnv("RATE_BURST", 30),

		MaxRetries:   intEnv("MAX_RETRIES", 2),
		BreakerError: intEnv("BREAKER_ERRORS", 5),
		BreakerOpen:  dur("BREAKER_OPEN", 20*time.Second),

		DeepSeekBaseURL: env("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekAPIKey:  env("DEEPSEEK_API_KEY", ""),
		DeepSeekModel:   env("DEEPSEEK_MODEL", "deepseek-chat"),

		AllowMock: boolEnv("ALLOW_MOCK", true),
		DataFile:  env("DATA_FILE", "data/ensemble.json"),
	}
	return c
}

// UpstreamConfigured 上游是否已配置真实 Key。
func (c Config) UpstreamConfigured() bool {
	return strings.TrimSpace(c.DeepSeekAPIKey) != ""
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func intEnv(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func boolEnv(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func dur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
