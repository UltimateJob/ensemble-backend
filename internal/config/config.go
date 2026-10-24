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
	// DeepSeek Search API（同一个 Key）。默认开启；DEEPSEEK_SEARCH=false 可关。
	SearchEnabled bool

	// 是否允许无 Key 时使用 mock 上游（本地演示）
	AllowMock bool

	// RemoteBase 线上后端地址。桌面端用它把模型调用转给服务器，
	// 这样 Key 只存在服务器上，客户端不需要也不应该持有。
	// 留空表示不启用（本地开发、纯离线场景）。
	RemoteBase string

	// ExtraProviders OpenAI 兼容中转池：EXTRA_PROVIDERS=
	//   name1|https://a.example.com|sk-xxx|deepseek-chat,name2|https://b.example|sk-yyy
	// 第 4 段（默认模型）可省。管理台再按名字建模型、调权重，即成请求池。
	ExtraProviders []ExtraProvider

	// 数据目录（分片存储）
	DataDir string

	// 单次发言超过这个毫秒数就记一条慢日志
	SlowThresholdMS int

	// Prod 为真时启用生产自检：不安全的默认值会导致启动失败
	Prod bool

	// 验证码通道：log（默认，写日志并回显） / aliyun / tencent …
	SmsProvider string
	SmsSign     string
	SmsTemplate string
	SmsKeyID    string
	SmsSecret   string

	// DevBypass 为真时演示账号可跳过验证码，生产必须关掉
	DevBypass bool
}

func Load() Config {
	c := Config{
		Addr:            env("ADDR", ":8080"),
		AdminToken:      env("ADMIN_TOKEN", "admin-dev-token"),
		JWTSecret:       env("JWT_SECRET", DefaultJWTSecret),
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
		SearchEnabled:   boolEnv("DEEPSEEK_SEARCH", true),

		AllowMock:      boolEnv("ALLOW_MOCK", true),
		RemoteBase:     env("REMOTE_BASE", ""),
		ExtraProviders: parseExtraProviders(os.Getenv("EXTRA_PROVIDERS")),
		DataDir:        env("DATA_DIR", "data"),

		SlowThresholdMS: intEnv("SLOW_THRESHOLD_MS", 10000),

		Prod: boolEnv("PROD", false),

		SmsProvider: env("SMS_PROVIDER", "log"),
		SmsSign:     env("SMS_SIGN", ""),
		SmsTemplate: env("SMS_TEMPLATE", ""),
		SmsKeyID:    env("SMS_KEY_ID", ""),
		SmsSecret:   env("SMS_SECRET", ""),

		DevBypass: boolEnv("DEV_BYPASS", true),
	}
	return c
}

// UpstreamConfigured 上游是否已配置真实 Key。
// 中转池也算真实上游：配了 EXTRA_PROVIDERS 就不该再降级 mock。
func (c Config) UpstreamConfigured() bool {
	if strings.TrimSpace(c.DeepSeekAPIKey) != "" {
		return true
	}
	// 指向线上后端时也算「配好了上游」：
	// 模型由服务器提供，本地不需要自己的 Key。
	if strings.TrimSpace(c.RemoteBase) != "" {
		return true
	}
	return len(c.ExtraProviders) > 0
}

// ExtraProvider 一个 OpenAI 兼容中转。
type ExtraProvider struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string // 默认模型，可空（建模型时在管理台指定 Upstream）
}

// parseExtraProviders 解析 EXTRA_PROVIDERS。格式不对的条目跳过并容忍——
// 写错一个名字不该让整个服务起不来，日志里能看出来（main 会打注册数）。
func parseExtraProviders(raw string) []ExtraProvider {
	var out []ExtraProvider
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, "|")
		if len(parts) < 3 {
			continue
		}
		p := ExtraProvider{
			Name:    strings.TrimSpace(parts[0]),
			BaseURL: strings.TrimSpace(parts[1]),
			APIKey:  strings.TrimSpace(parts[2]),
		}
		if len(parts) >= 4 {
			p.Model = strings.TrimSpace(parts[3])
		}
		if p.Name == "" || p.BaseURL == "" || p.APIKey == "" || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out
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
