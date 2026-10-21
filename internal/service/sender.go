package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// CodeSender 把验证码送到用户手上。
//
// 默认实现只写日志并把 code 回给调用方（方便本地调试）；
// 接真短信时实现这个接口并换掉配置即可，业务层不动。
type CodeSender interface {
	// Name 供日志与运维台展示
	Name() string
	// Send 发送验证码。返回的 devCode 只在内置实现里非空。
	Send(ctx context.Context, target, code string) (devCode string, err error)
}

// LogSender 不真的发短信，把验证码写进日志。
// 本地开发与 CI 用这个。
type LogSender struct{}

func (LogSender) Name() string { return "log" }

func (LogSender) Send(ctx context.Context, target, code string) (string, error) {
	logx.Req(ctx).Info("验证码已生成", "target", mask(target), "code", code)
	return code, nil
}

// SmsSender 真短信接入的骨架。
//
// 换成阿里云 / 腾讯云时，把下面的 Send 换成对应 SDK 调用：
//
//	client.SendSms(&dysmsapi.SendSmsRequest{
//	    PhoneNumbers: &target,
//	    SignName:     &s.sign,
//	    TemplateCode: &s.template,
//	    TemplateParam: aws.String(`{"code":"` + code + `"}`),
//	})
//
// 这里保留结构是为了让依赖注入和配置已经就位。
type SmsSender struct {
	Provider string
	Sign     string
	Template string
	KeyID    string
	Secret   string
}

func (s *SmsSender) Name() string { return s.Provider }

func (s *SmsSender) Send(ctx context.Context, target, code string) (string, error) {
	if s.KeyID == "" || s.Secret == "" {
		return "", errors.New("短信通道未配置：缺少 KeyID / Secret")
	}
	if s.Sign == "" || s.Template == "" {
		return "", errors.New("短信通道未配置：缺少签名或模板")
	}
	// 真实实现放在这里
	logx.Req(ctx).Warn("短信通道尚未接入真实 SDK", "provider", s.Provider, "target", mask(target))
	return "", fmt.Errorf("%s 短信 SDK 未接入", s.Provider)
}

// NewSender 按配置选择发送方式。
func NewSender(provider, sign, template, keyID, secret string) CodeSender {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "log", "none":
		return LogSender{}
	default:
		return &SmsSender{
			Provider: provider, Sign: sign, Template: template,
			KeyID: keyID, Secret: secret,
		}
	}
}

// randomCode 生成 6 位随机验证码。
func randomCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "123456"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// mask 打日志时脱敏。
func mask(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	if strings.Contains(s, "@") {
		at := strings.Index(s, "@")
		if at <= 2 {
			return "***" + s[at:]
		}
		return s[:2] + "***" + s[at:]
	}
	return s[:3] + "****" + s[len(s)-2:]
}
