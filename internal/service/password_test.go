package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// newPasswordAuth 组装一套带凭据文件与测试用户的 Auth。
func newPasswordAuth(t *testing.T, bypass bool) (*Auth, *store.Memory, *domain.User, string) {
	t.Helper()
	st := store.NewMemory()
	a := NewAuth(st, "test-secret", LogSender{}, bypass)
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := a.SetCredentialsPath(path); err != nil {
		t.Fatalf("设置凭据路径失败：%v", err)
	}
	u := &domain.User{
		Phone:  "13800138000",
		Email:  "tester@example.com",
		Name:   "测试用户",
		Plan:   "free",
		Quota:  100,
		Role:   "user",
		Status: "active",
	}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("创建用户失败：%v", err)
	}
	return a, st, u, path
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := hashPassword("s3cret-password")
	if err != nil {
		t.Fatalf("散列失败：%v", err)
	}
	if !strings.HasPrefix(hash, pbkdf2Prefix+"$") {
		t.Fatalf("散列格式不对：%s", hash)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[1] != "120000" {
		t.Fatalf("散列应包含 120000 次迭代：%s", hash)
	}
	if !verifyPassword(hash, "s3cret-password") {
		t.Fatal("正确密码应校验通过")
	}
	if verifyPassword(hash, "s3cret-password2") {
		t.Fatal("错误密码不应校验通过")
	}
}

func TestPasswordMalformedHashReturnsFalse(t *testing.T) {
	cases := []string{
		"",
		"plaintext",
		"pbkdf2-sha256$120000$abc",
		"pbkdf2-sha256$notanint$c2FsdA$aGFzaA",
		"pbkdf2-sha256$120000$!!!$!!!",
		"pbkdf2-sha256$120000$c2FsdA$!!!",
		"pbkdf2-sha256$0$c2FsdA$aGFzaA",
		"pbkdf2-sha256$999999999$c2FsdA$aGFzaA",
		"other-algo$120000$c2FsdA$aGFzaA",
	}
	for _, encoded := range cases {
		if verifyPassword(encoded, "whatever") {
			t.Fatalf("损坏的散列 %q 不应校验通过", encoded)
		}
	}
}

func TestPasswordDifferentSalts(t *testing.T) {
	h1, err := hashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hashPassword("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("同一密码两次散列应因随机 salt 而不同")
	}
	if !verifyPassword(h1, "same-password") || !verifyPassword(h2, "same-password") {
		t.Fatal("两份散列都应能校验原密码")
	}
}

func TestSetPasswordFlow(t *testing.T) {
	a, _, u, _ := newPasswordAuth(t, false)

	if a.HasPassword(u.ID) {
		t.Fatal("新用户不应有密码")
	}
	if err := a.SetPassword(u.ID, "", "first-password"); err != nil {
		t.Fatalf("首次设置密码失败：%v", err)
	}
	if !a.HasPassword(u.ID) {
		t.Fatal("设置后 HasPassword 应为 true")
	}

	got, token, err := a.LoginWithPassword("phone", u.Phone, "first-password")
	if err != nil {
		t.Fatalf("密码登录失败：%v", err)
	}
	if got.ID != u.ID || token == "" {
		t.Fatalf("登录结果不对：user=%v token=%q", got, token)
	}

	if _, _, err := a.LoginWithPassword("phone", u.Phone, "wrong-password"); err != ErrWrongPassword {
		t.Fatalf("错误密码应返回 ErrWrongPassword，得到 %v", err)
	}
	if _, _, err := a.LoginWithPassword("email", u.Email, "first-password"); err != nil {
		t.Fatalf("邮箱密码登录失败：%v", err)
	}

	// 已有密码：原密码错误必须拒绝，正确才能改
	if err := a.SetPassword(u.ID, "bad-old", "second-password"); err != ErrOldPassword {
		t.Fatalf("原密码错误应返回 ErrOldPassword，得到 %v", err)
	}
	if err := a.SetPassword(u.ID, "first-password", "second-password"); err != nil {
		t.Fatalf("修改密码失败：%v", err)
	}
	if _, _, err := a.LoginWithPassword("phone", u.Phone, "second-password"); err != nil {
		t.Fatalf("新密码登录失败：%v", err)
	}
	if _, _, err := a.LoginWithPassword("phone", u.Phone, "first-password"); err != ErrWrongPassword {
		t.Fatal("旧密码修改后不应还能登录")
	}

	// 密码长度 8..72 个字符
	if err := a.SetPassword(u.ID, "second-password", "short"); err != ErrWeakPassword {
		t.Fatalf("过短密码应返回 ErrWeakPassword，得到 %v", err)
	}
	if err := a.SetPassword(u.ID, "second-password", strings.Repeat("密", 73)); err != ErrWeakPassword {
		t.Fatalf("过长密码应返回 ErrWeakPassword，得到 %v", err)
	}
}

func TestLoginWithPasswordErrors(t *testing.T) {
	a, st, _, _ := newPasswordAuth(t, false)

	// 账号不存在与密码错误同错，避免泄漏账号是否存在
	if _, _, err := a.LoginWithPassword("phone", "13999999999", "whatever-pass"); err != ErrWrongPassword {
		t.Fatalf("不存在的账号应返回 ErrWrongPassword，得到 %v", err)
	}

	noPw := &domain.User{Phone: "13700137000", Name: "无密码", Plan: "free", Quota: 10, Role: "user", Status: "active"}
	if err := st.CreateUser(noPw); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.LoginWithPassword("phone", noPw.Phone, "whatever-pass"); err != ErrPasswordNotSet {
		t.Fatalf("未设置密码应返回 ErrPasswordNotSet，得到 %v", err)
	}

	disabled := &domain.User{Phone: "13600136000", Name: "停用", Plan: "free", Quota: 10, Role: "user", Status: "disabled"}
	if err := st.CreateUser(disabled); err != nil {
		t.Fatal(err)
	}
	if err := a.SetPassword(disabled.ID, "", "disabled-pass"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.LoginWithPassword("phone", disabled.Phone, "disabled-pass"); err != ErrDisabled {
		t.Fatalf("停用账号应返回 ErrDisabled，得到 %v", err)
	}
}

func TestResetPasswordFlow(t *testing.T) {
	a, _, u, _ := newPasswordAuth(t, false)

	code, err := a.SendCode(context.Background(), u.Phone)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ResetPassword("phone", u.Phone, code, "reset-password"); err != nil {
		t.Fatalf("重置密码失败：%v", err)
	}
	if _, _, err := a.LoginWithPassword("phone", u.Phone, "reset-password"); err != nil {
		t.Fatalf("重置后新密码应能登录：%v", err)
	}
	// 验证码一次性
	if err := a.ResetPassword("phone", u.Phone, code, "another-password"); err != ErrInvalidCode {
		t.Fatalf("用过的验证码应失效，得到 %v", err)
	}

	// 弱密码先于验证码校验，不消耗验证码
	code2, err := a.SendCode(context.Background(), "13900139000")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ResetPassword("phone", "13900139000", code2, "short"); err != ErrWeakPassword {
		t.Fatalf("弱密码应返回 ErrWeakPassword，得到 %v", err)
	}
	// 账号不存在
	if err := a.ResetPassword("phone", "13900139000", code2, "valid-enough"); err != ErrAccountNotFound {
		t.Fatalf("不存在的账号应返回 ErrAccountNotFound，得到 %v", err)
	}

	// 验证码错误（该用户当前没有有效验证码）
	if err := a.ResetPassword("phone", u.Phone, "000000", "valid-enough"); err != ErrInvalidCode {
		t.Fatalf("错误验证码应返回 ErrInvalidCode，得到 %v", err)
	}
}

func TestCredentialsFileSecurityAndPersistence(t *testing.T) {
	a, st, u, path := newPasswordAuth(t, false)
	const password = "plaintext-must-not-appear"
	if err := a.SetPassword(u.ID, "", password); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("凭据文件应存在：%v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("凭据文件权限应为 0600，得到 %o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), password) {
		t.Fatal("凭据文件不应包含明文密码")
	}
	var saved map[string]string
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("凭据文件应为 JSON：%v", err)
	}
	if !strings.HasPrefix(saved[u.ID], pbkdf2Prefix+"$") {
		t.Fatalf("凭据文件里应是散列，得到 %q", saved[u.ID])
	}

	// 重新加载：密码仍然有效
	a2 := NewAuth(st, "test-secret", LogSender{}, false)
	if err := a2.SetCredentialsPath(path); err != nil {
		t.Fatal(err)
	}
	if !a2.HasPassword(u.ID) {
		t.Fatal("重新加载后应识别已有密码")
	}
	if _, _, err := a2.LoginWithPassword("phone", u.Phone, password); err != nil {
		t.Fatalf("重新加载后密码登录应成功：%v", err)
	}

	// 文件损坏：返回错误，不 panic，也不把坏文件当成凭据
	bad := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(bad, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a3 := NewAuth(st, "test-secret", LogSender{}, false)
	if err := a3.SetCredentialsPath(bad); err == nil {
		t.Fatal("损坏的凭据文件应返回错误")
	}
	if a3.HasPassword(u.ID) {
		t.Fatal("损坏文件不应加载出任何密码")
	}
	if _, _, err := a3.LoginWithPassword("phone", u.Phone, password); err != ErrPasswordNotSet {
		t.Fatalf("凭据不可用时应按未设置密码处理，得到 %v", err)
	}
}
