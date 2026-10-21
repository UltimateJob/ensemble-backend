package service

import (
	"context"
	"strings"
	"testing"
)

func TestRandomCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		c := randomCode()
		if len(c) != 6 {
			t.Fatalf("验证码应为 6 位，得到 %q", c)
		}
		for _, r := range c {
			if r < '0' || r > '9' {
				t.Fatalf("验证码应全为数字，得到 %q", c)
			}
		}
		seen[c] = true
	}
	// 50 次里不该全是同一个（不是严格概率断言，只防「写死 123456」回归）
	if len(seen) < 10 {
		t.Fatalf("验证码疑似不随机：50 次只出现 %d 种", len(seen))
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"13800000000": "138****00",
		"linmo@a.com": "li***@a.com",
		"a@b.com":     "***@b.com",
		"123":         "***",
	}
	for in, want := range cases {
		if got := mask(in); got != want {
			t.Fatalf("mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogSenderReturnsDevCode(t *testing.T) {
	s := NewSender("log", "", "", "", "")
	if s.Name() != "log" {
		t.Fatalf("默认通道应为 log，得到 %s", s.Name())
	}
	code, err := s.Send(context.Background(), "13800000000", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if code != "123456" {
		t.Fatalf("log 通道应回显验证码，得到 %q", code)
	}
}

func TestSmsSenderRejectsUnconfigured(t *testing.T) {
	s := NewSender("aliyun", "", "", "", "")
	if s.Name() != "aliyun" {
		t.Fatal("应选择 aliyun 通道")
	}
	if _, err := s.Send(context.Background(), "13800000000", "123456"); err == nil {
		t.Fatal("未配置密钥时应报错")
	}

	// 配了密钥但没接 SDK，应明确报未接入，而不是假装成功
	s2 := NewSender("aliyun", "签名", "TPL", "key", "secret")
	code, err := s2.Send(context.Background(), "13800000000", "123456")
	if err == nil {
		t.Fatal("未接入 SDK 时应报错，不能静默成功")
	}
	if code != "" {
		t.Fatal("真实通道不应把验证码回给调用方")
	}
	if !strings.Contains(err.Error(), "未接入") {
		t.Fatalf("错误信息应说明未接入，得到 %v", err)
	}
}

func TestAuthBypassFlag(t *testing.T) {
	// bypass 关闭时，演示账号也必须走验证码
	a := NewAuth(nil, "s", LogSender{}, false)
	if a.bypass {
		t.Fatal("bypass 应为 false")
	}
	// 打开时才是 true
	b := NewAuth(nil, "s", LogSender{}, true)
	if !b.bypass {
		t.Fatal("bypass 应为 true")
	}
}
