package httpapi

import (
	"path/filepath"
	"strings"
	"testing"
)

func errorCode(out map[string]interface{}) string {
	errObj, _ := out["error"].(map[string]interface{})
	code, _ := errObj["code"].(string)
	return code
}

func TestPasswordEndpoints(t *testing.T) {
	srv, h := newTestServer(t)
	if err := srv.auth.SetCredentialsPath(filepath.Join(t.TempDir(), "credentials.json")); err != nil {
		t.Fatal(err)
	}
	token := login(t, h, "13800000000")

	// 初始没有密码
	rec, out := doJSON(t, h, "GET", "/v1/me", token, nil)
	if rec.Code != 200 {
		t.Fatalf("GET /v1/me 失败：%d", rec.Code)
	}
	data := out["data"].(map[string]interface{})
	if data["hasPassword"] != false {
		t.Fatalf("初始 hasPassword 应为 false，得到 %v", data["hasPassword"])
	}

	// 弱密码
	rec, out = doJSON(t, h, "POST", "/v1/me/password", token, map[string]string{"newPassword": "short"})
	if rec.Code != 400 || errorCode(out) != "weak_password" {
		t.Fatalf("弱密码应返回 400 weak_password，得到 %d %v", rec.Code, out)
	}

	// 首次设置不需要原密码
	rec, out = doJSON(t, h, "POST", "/v1/me/password", token, map[string]string{"newPassword": "first-password"})
	if rec.Code != 200 || out["data"].(map[string]interface{})["changed"] != true {
		t.Fatalf("设置密码失败：%d %v", rec.Code, out)
	}
	_, out = doJSON(t, h, "GET", "/v1/me", token, nil)
	if out["data"].(map[string]interface{})["hasPassword"] != true {
		t.Fatal("设置后 hasPassword 应为 true")
	}

	// 密码登录成功
	rec, out = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13800000000", "mode": "password", "password": "first-password",
	})
	if rec.Code != 200 {
		t.Fatalf("密码登录失败：%d %v", rec.Code, out)
	}
	loginData := out["data"].(map[string]interface{})
	if loginData["token"] == "" || loginData["token"] == nil {
		t.Fatal("密码登录应返回 token")
	}
	user := loginData["user"].(map[string]interface{})
	if _, leaked := user["password"]; leaked {
		t.Fatal("用户对象不应包含密码字段")
	}

	// 密码错误：401 invalid_credentials
	rec, out = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13800000000", "mode": "password", "password": "wrong-password",
	})
	if rec.Code != 401 || errorCode(out) != "invalid_credentials" {
		t.Fatalf("错误密码应返回 401 invalid_credentials，得到 %d %v", rec.Code, out)
	}

	// 修改密码：原密码错/对
	rec, out = doJSON(t, h, "POST", "/v1/me/password", token, map[string]string{
		"oldPassword": "wrong-old", "newPassword": "second-password",
	})
	if rec.Code != 400 || errorCode(out) != "old_password_incorrect" {
		t.Fatalf("原密码错误应返回 400 old_password_incorrect，得到 %d %v", rec.Code, out)
	}
	rec, _ = doJSON(t, h, "POST", "/v1/me/password", token, map[string]string{
		"oldPassword": "first-password", "newPassword": "second-password",
	})
	if rec.Code != 200 {
		t.Fatalf("正确原密码修改应成功：%d", rec.Code)
	}
	rec, _ = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13800000000", "mode": "password", "password": "second-password",
	})
	if rec.Code != 200 {
		t.Fatalf("修改后的新密码应能登录：%d", rec.Code)
	}

	// 未登录不能改密码
	rec, _ = doJSON(t, h, "POST", "/v1/me/password", "", map[string]string{"newPassword": "whatever-pass"})
	if rec.Code != 401 {
		t.Fatalf("未登录改密码应返回 401，得到 %d", rec.Code)
	}
}

func TestResetPasswordEndpoint(t *testing.T) {
	srv, h := newTestServer(t)
	if err := srv.auth.SetCredentialsPath(filepath.Join(t.TempDir(), "credentials.json")); err != nil {
		t.Fatal(err)
	}
	// 先建号（验证码登录在 bypass 下免码），重置密码要求账号已存在
	_ = login(t, h, "13800000000")

	// 弱密码
	rec, out := doJSON(t, h, "POST", "/v1/auth/reset-password", "", map[string]string{
		"kind": "phone", "target": "13800000000", "code": "000000", "newPassword": "short",
	})
	if rec.Code != 400 || errorCode(out) != "weak_password" {
		t.Fatalf("弱密码应返回 400 weak_password，得到 %d %v", rec.Code, out)
	}

	// 演示账号在 bypass 下免验证码
	rec, out = doJSON(t, h, "POST", "/v1/auth/reset-password", "", map[string]string{
		"kind": "phone", "target": "13800000000", "code": "000000", "newPassword": "reset-password",
	})
	if rec.Code != 200 || out["data"].(map[string]interface{})["reset"] != true {
		t.Fatalf("重置密码失败：%d %v", rec.Code, out)
	}
	rec, _ = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13800000000", "mode": "password", "password": "reset-password",
	})
	if rec.Code != 200 {
		t.Fatalf("重置后的密码应能登录：%d", rec.Code)
	}

	// 不存在的账号：先发码，再重置 → 404
	rec, out = doJSON(t, h, "POST", "/v1/auth/send-code", "", map[string]string{
		"kind": "phone", "target": "13700137000",
	})
	if rec.Code != 200 {
		t.Fatalf("发验证码失败：%d %v", rec.Code, out)
	}
	code := out["data"].(map[string]interface{})["devCode"].(string)
	rec, out = doJSON(t, h, "POST", "/v1/auth/reset-password", "", map[string]string{
		"kind": "phone", "target": "13700137000", "code": code, "newPassword": "valid-password",
	})
	if rec.Code != 404 {
		t.Fatalf("不存在的账号应返回 404，得到 %d %v", rec.Code, out)
	}

	// 验证码错误
	rec, out = doJSON(t, h, "POST", "/v1/auth/reset-password", "", map[string]string{
		"kind": "phone", "target": "13700137000", "code": "000000", "newPassword": "valid-password",
	})
	if rec.Code != 400 || errorCode(out) != "invalid_code" {
		t.Fatalf("错误验证码应返回 400 invalid_code，得到 %d %v", rec.Code, out)
	}
}

func TestUpdateMeValidation(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	rec, _ := doJSON(t, h, "PATCH", "/v1/me", token, map[string]string{
		"name": strings.Repeat("字", 25),
	})
	if rec.Code != 400 {
		t.Fatalf("超过 24 字的昵称应返回 400，得到 %d", rec.Code)
	}
	rec, _ = doJSON(t, h, "PATCH", "/v1/me", token, map[string]string{"avatar": "#GGGGGG"})
	if rec.Code != 400 {
		t.Fatalf("非法颜色头像应返回 400，得到 %d", rec.Code)
	}
	rec, _ = doJSON(t, h, "PATCH", "/v1/me", token, map[string]string{"name": "  "})
	if rec.Code != 400 {
		t.Fatalf("空昵称应返回 400，得到 %d", rec.Code)
	}

	rec, out := doJSON(t, h, "PATCH", "/v1/me", token, map[string]string{
		"name": "  新名字  ", "avatar": "#FF7A45",
	})
	if rec.Code != 200 {
		t.Fatalf("更新资料失败：%d %v", rec.Code, out)
	}
	updated := out["data"].(map[string]interface{})
	if updated["name"] != "新名字" || updated["avatar"] != "#FF7A45" {
		t.Fatalf("资料未按预期更新：%v", updated)
	}

	// emoji 头像
	rec, out = doJSON(t, h, "PATCH", "/v1/me", token, map[string]string{"avatar": "🐱"})
	if rec.Code != 200 || out["data"].(map[string]interface{})["avatar"] != "🐱" {
		t.Fatalf("emoji 头像应被接受：%d %v", rec.Code, out)
	}
}
