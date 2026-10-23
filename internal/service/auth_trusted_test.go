package service

import (
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// 代理登录：线上已验证过身份，本地只建镜像用户并签发本地会话。
// 这条路径不能要求验证码——码已经在线上核销过了。
func TestLoginTrustedSkipsCode(t *testing.T) {
	st := store.NewMemory()
	a := NewAuth(st, "test-secret", nil, false)

	u, token, err := a.LoginTrusted("phone", "13800001234", "")
	if err != nil {
		t.Fatalf("LoginTrusted 失败：%v", err)
	}
	if u == nil || u.ID == "" {
		t.Fatal("应该建出镜像用户")
	}
	if token == "" {
		t.Fatal("应该签发本地令牌")
	}

	// 本地令牌必须能用
	got, err := a.Verify(token)
	if err != nil {
		t.Fatalf("本地令牌校验失败：%v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("令牌对应的用户不对：%s != %s", got.ID, u.ID)
	}
}

// 同一个手机号再次登录要复用同一个本地用户，
// 否则每次登录都会多出一个镜像账号，工作区和执行记录就对不上了。
func TestLoginTrustedReusesUser(t *testing.T) {
	st := store.NewMemory()
	a := NewAuth(st, "test-secret", nil, false)

	first, _, err := a.LoginTrusted("phone", "13800005678", "")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := a.LoginTrusted("phone", "13800005678", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("同号应复用用户：%s != %s", first.ID, second.ID)
	}
}

// 注册关闭也不能挡住代理登录：账号是线上建的，本地没资格拒绝。
func TestLoginTrustedIgnoresSignupPolicy(t *testing.T) {
	st := store.NewMemory()
	a := NewAuth(st, "test-secret", nil, false)
	a.SetSignupPolicy(func() SignupPolicy { return SignupPolicy{Open: false} })

	if _, _, err := a.LoginTrusted("phone", "13800007777", ""); err != nil {
		t.Fatalf("注册关闭不该影响代理登录：%v", err)
	}
}
