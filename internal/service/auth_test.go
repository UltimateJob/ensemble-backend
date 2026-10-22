package service

import (
	"context"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// newClockAuth 带可控时钟的 Auth：节流与过期都依赖时间，测试里手动拨。
func newClockAuth(bypass bool) (*Auth, func(time.Time)) {
	a := NewAuth(store.NewMemory(), "s", LogSender{}, bypass)
	cur := time.Now()
	a.now = func() time.Time { return cur }
	return a, func(t time.Time) { cur = t }
}

func TestCodeAttemptLimit(t *testing.T) {
	a, setNow := newClockAuth(false)
	code, err := a.SendCode(context.Background(), "13811112222")
	if err != nil {
		t.Fatal(err)
	}

	// 猜错 2 次还在上限内：正确码仍可用
	for i := 0; i < 2; i++ {
		if _, _, err := a.Login("phone", "13811112222", "000000", "评委"); err != ErrInvalidCode {
			t.Fatalf("第 %d 次猜错应返回 ErrInvalidCode，得到 %v", i+1, err)
		}
	}
	if _, _, err := a.Login("phone", "13811112222", code, "评委"); err != nil {
		t.Fatalf("两次猜错后正确码仍应有效：%v", err)
	}

	// 换新码：第 3 次猜错就作废，正确码也登不进去
	setNow(time.Now().Add(2 * time.Minute)) // 过节流窗口
	code2, err := a.SendCode(context.Background(), "13811112222")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := a.Login("phone", "13811112222", "000000", "评委"); err != ErrInvalidCode {
			t.Fatalf("第 %d 次猜错应返回 ErrInvalidCode，得到 %v", i+1, err)
		}
	}
	if _, _, err := a.Login("phone", "13811112222", code2, "评委"); err != ErrInvalidCode {
		t.Fatal("试满 3 次后验证码应已作废，只能重新发送")
	}
}

func TestSendCodeThrottle(t *testing.T) {
	a, _ := newClockAuth(false)
	if _, err := a.SendCode(context.Background(), "13800001111"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendCode(context.Background(), "13800001111"); err != ErrCodeTooFrequent {
		t.Fatalf("60 秒内重发应被节流，得到 %v", err)
	}
	// 别的目标不受连带影响
	if _, err := a.SendCode(context.Background(), "13800002222"); err != nil {
		t.Fatalf("不同目标不应被节流：%v", err)
	}

	// bypass 开着时，演示账号不受节流（演示路径无感）
	b, _ := newClockAuth(true)
	if _, err := b.SendCode(context.Background(), "13800000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SendCode(context.Background(), "13800000000"); err != nil {
		t.Fatalf("演示账号在 bypass 下不应被节流：%v", err)
	}

	// bypass 关着时，演示号码同样受节流
	c, _ := newClockAuth(false)
	if _, err := c.SendCode(context.Background(), "13800000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SendCode(context.Background(), "13800000000"); err != ErrCodeTooFrequent {
		t.Fatalf("bypass 关闭时演示号码也应被节流，得到 %v", err)
	}
}

func TestExpiredCodeSweptOnUse(t *testing.T) {
	a, setNow := newClockAuth(false)
	code, err := a.SendCode(context.Background(), "13811113333")
	if err != nil {
		t.Fatal(err)
	}

	// 过了有效期再给别的目标发码：顺带清扫过期条目
	setNow(time.Now().Add(6 * time.Minute))
	if _, err := a.SendCode(context.Background(), "13811114444"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Login("phone", "13811113333", code, "评委"); err != ErrInvalidCode {
		t.Fatal("过期验证码不应通过")
	}
	if len(a.codes) != 1 {
		t.Fatalf("过期条目应被惰性清扫，codes 里应只剩 1 条，得到 %d", len(a.codes))
	}
}

func TestDefaultQuota(t *testing.T) {
	t.Setenv("DEFAULT_QUOTA", "")
	if got := defaultQuota(); got != 800 {
		t.Fatalf("默认额度应为 800，得到 %d", got)
	}
	t.Setenv("DEFAULT_QUOTA", "1500")
	if got := defaultQuota(); got != 1500 {
		t.Fatalf("环境变量应生效，得到 %d", got)
	}
	t.Setenv("DEFAULT_QUOTA", "abc")
	if got := defaultQuota(); got != 800 {
		t.Fatalf("非法值应回退 800，得到 %d", got)
	}
	t.Setenv("DEFAULT_QUOTA", "-3")
	if got := defaultQuota(); got != 800 {
		t.Fatalf("非正数应回退 800，得到 %d", got)
	}

	// 注册新用户时实际生效
	t.Setenv("DEFAULT_QUOTA", "4321")
	a, _ := newClockAuth(false)
	code, err := a.SendCode(context.Background(), "13811115555")
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := a.Login("phone", "13811115555", code, "评委")
	if err != nil {
		t.Fatal(err)
	}
	if u.Quota != 4321 {
		t.Fatalf("新用户额度应为 4321，得到 %d", u.Quota)
	}
}
