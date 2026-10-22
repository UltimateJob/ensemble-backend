package service

import (
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

func newTestDevices(t *testing.T) (*Devices, *store.Memory, *domain.User) {
	t.Helper()
	st := store.NewMemory()
	u := &domain.User{ID: "u1", Name: "测试", Status: "active"}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	return NewDevices(st, "test-secret"), st, u
}

func TestPairCodeSingleUse(t *testing.T) {
	d, _, u := newTestDevices(t)
	code, _, err := d.IssueCode(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Pair(code, "iPhone", "ios"); err != nil {
		t.Fatalf("第一次配对应该成功：%v", err)
	}
	// 同一个码不能再换一台设备
	if _, _, err := d.Pair(code, "另一个手机", "android"); err == nil {
		t.Fatal("配对码被重复使用了")
	}
}

func TestPairCodeExpires(t *testing.T) {
	d, _, u := newTestDevices(t)
	now := time.Now()
	d.now = func() time.Time { return now }

	code, _, err := d.IssueCode(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 把时间推到 TTL 之后
	d.now = func() time.Time { return now.Add(pairCodeTTL + time.Second) }
	if _, _, err := d.Pair(code, "iPhone", "ios"); err == nil {
		t.Fatal("过期的配对码仍然可用")
	}
}

func TestPairCodeWrongCodeRejected(t *testing.T) {
	d, _, _ := newTestDevices(t)
	if _, _, err := d.Pair("000000", "iPhone", "ios"); err == nil {
		t.Fatal("不存在的码被接受了")
	}
}

func TestPairCodeTooManyTriesBurnsCode(t *testing.T) {
	d, _, u := newTestDevices(t)
	code, _, err := d.IssueCode(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 直接操作内部状态模拟猜错：把 tries 顶到上限
	d.mu.Lock()
	d.codes[code].Tries = pairCodeMaxTries
	d.mu.Unlock()

	if _, _, err := d.Pair(code, "iPhone", "ios"); err == nil {
		t.Fatal("试满次数后仍然配对成功")
	}
	// 码已被烧掉，再用正确的码也不行
	if _, _, err := d.Pair(code, "iPhone", "ios"); err == nil {
		t.Fatal("烧掉的配对码仍然可用")
	}
}

func TestPairCodePerUserLimit(t *testing.T) {
	d, _, u := newTestDevices(t)
	for i := 0; i < pairCodePerUser; i++ {
		if _, _, err := d.IssueCode(u.ID); err != nil {
			t.Fatalf("第 %d 个码应该能生成：%v", i+1, err)
		}
	}
	if _, _, err := d.IssueCode(u.ID); err != ErrTooManyCodes {
		t.Fatalf("超出上限应该报 ErrTooManyCodes，实际：%v", err)
	}
}

func TestDeviceTokenVerify(t *testing.T) {
	d, _, u := newTestDevices(t)
	code, _, _ := d.IssueCode(u.ID)
	dev, token, err := d.Pair(code, "iPhone 15", "ios")
	if err != nil {
		t.Fatal(err)
	}
	gotDev, gotUser, err := d.VerifyDevice(token)
	if err != nil {
		t.Fatalf("刚签发的令牌应该有效：%v", err)
	}
	if gotDev.ID != dev.ID || gotUser.ID != u.ID {
		t.Fatal("令牌解析出的设备或用户不对")
	}
	// 令牌不能以明文存进库里
	stored, _ := d.store.GetDevice(dev.ID)
	if stored.TokenHash == token {
		t.Fatal("库里存了明文设备令牌")
	}
	if len(stored.TokenHash) != 64 {
		t.Fatalf("TokenHash 应该是 SHA-256 十六进制，实际长度 %d", len(stored.TokenHash))
	}
}

func TestDeviceRevokeIsImmediate(t *testing.T) {
	d, _, u := newTestDevices(t)
	code, _, _ := d.IssueCode(u.ID)
	dev, token, err := d.Pair(code, "iPhone", "ios")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Revoke(u.ID, dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.VerifyDevice(token); err == nil {
		t.Fatal("吊销后设备令牌仍然可用")
	}
}

func TestDeviceRevokeOtherUserForbidden(t *testing.T) {
	d, _, u := newTestDevices(t)
	code, _, _ := d.IssueCode(u.ID)
	dev, token, _ := d.Pair(code, "iPhone", "ios")

	// 另一个用户不能吊销这台设备
	if err := d.Revoke("u2", dev.ID); err == nil {
		t.Fatal("别的用户可以吊销我的设备")
	}
	if _, _, err := d.VerifyDevice(token); err != nil {
		t.Fatal("越权吊销把设备弄失效了")
	}
}

func TestDeviceDisabledUserRejected(t *testing.T) {
	d, st, u := newTestDevices(t)
	code, _, _ := d.IssueCode(u.ID)
	_, token, _ := d.Pair(code, "iPhone", "ios")

	// 停用账号：所有设备一起失效
	u.Status = "disabled"
	if err := st.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.VerifyDevice(token); err == nil {
		t.Fatal("账号停用后设备令牌仍然可用")
	}
}

func TestDeviceApprovalDefaultOnAndToggle(t *testing.T) {
	d, _, u := newTestDevices(t)
	code, _, _ := d.IssueCode(u.ID)
	dev, _, _ := d.Pair(code, "iPhone", "ios")

	if !dev.RequireApproval {
		t.Fatal("新设备默认应该要求审批")
	}
	// 用户可以关掉——这是他的决定，服务端不阻拦
	got, err := d.SetRequireApproval(u.ID, dev.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequireApproval {
		t.Fatal("关闭审批没生效")
	}
	// 再打开
	got, _ = d.SetRequireApproval(u.ID, dev.ID, true)
	if !got.RequireApproval {
		t.Fatal("重新打开审批没生效")
	}
}

func TestPairCodeRandomnessAndFormat(t *testing.T) {
	d, _, u := newTestDevices(t)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		code, _, err := d.IssueCode(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 {
			t.Fatalf("配对码应该是 6 位，实际 %q", code)
		}
		if strings.Trim(code, "0123456789") != "" {
			t.Fatalf("配对码应该是纯数字，实际 %q", code)
		}
		if seen[code] {
			t.Fatalf("生成了重复的配对码 %q", code)
		}
		seen[code] = true
		// 清掉，避免撞上每用户上限
		for k := range d.codes {
			delete(d.codes, k)
		}
	}
}
