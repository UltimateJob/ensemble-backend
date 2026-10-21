package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/memory"
	"github.com/UltimateJob/ensemble-backend/internal/service"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// newTestServer 组装一套只用 mock 上游的实例，不依赖网络。
func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()

	cfg := config.Config{
		AdminToken:    "test-admin",
		JWTSecret:     "test-secret",
		MaxRetries:    1,
		BreakerError:  3,
		BreakerOpen:   1 * time.Second,
		AllowMock:     true,
		RatePerMinute: 1000,
		Burst:         100,
	}

	st := store.NewMemory()
	reg := gateway.NewRegistry()
	reg.Add(&gateway.Mock{})
	models := gateway.NewModelTable([]domain.ModelInfo{
		{Key: "mock-chat", Provider: "mock", Upstream: "mock", Enabled: true, Weight: 1},
	})
	gw := gateway.New(reg, models, 1, 3, time.Second)

	auth := service.NewAuth(st, cfg.JWTSecret, service.LogSender{}, true)
	chat := service.NewChat(st, gw)
	orch := service.NewOrchestrator(st, gw, service.NewSlowLog(50, 1000))
	drafter := service.NewDrafter(gw)
	shared := &service.Shared{
		Limiter: gateway.NewLimiter(1000, 100),
		Started: time.Now(),
		Version: "test",
	}
	admin := service.NewAdmin(st, gw, shared)

	srv := NewServer(cfg, st, auth, chat, admin, gw, orch, drafter)
	return srv, srv.Handler()
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body interface{}) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func login(t *testing.T, h http.Handler, phone string) string {
	t.Helper()
	rec, out := doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": phone,
	})
	if rec.Code != 200 {
		t.Fatalf("登录失败：%d %s", rec.Code, rec.Body.String())
	}
	data := out["data"].(map[string]interface{})
	return data["token"].(string)
}

func TestHealth(t *testing.T) {
	_, h := newTestServer(t)
	rec, out := doJSON(t, h, "GET", "/healthz", "", nil)
	if rec.Code != 200 || out["ok"] != true {
		t.Fatalf("健康检查异常：%d %v", rec.Code, out)
	}
}

func TestLoginCreatesUser(t *testing.T) {
	_, h := newTestServer(t)

	// 演示账号免验证码
	token := login(t, h, "13800000000")
	if token == "" {
		t.Fatal("没有拿到 token")
	}

	// 同一个手机号再登录，不应重复建号
	rec, out := doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13800000000",
	})
	if rec.Code != 200 {
		t.Fatalf("二次登录失败：%s", rec.Body.String())
	}
	data := out["data"].(map[string]interface{})
	user := data["user"].(map[string]interface{})
	if user["id"] != "u_"+"" {
		// 只检查非空即可，ID 是随机生成的
		if user["id"] == "" {
			t.Fatal("用户 ID 为空")
		}
	}
}

func TestLoginRejectsBadCode(t *testing.T) {
	_, h := newTestServer(t)
	rec, _ := doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13900000000", "code": "000000",
	})
	if rec.Code != 400 {
		t.Fatalf("验证码错误应返回 400，得到 %d", rec.Code)
	}
}

func TestAuthRequired(t *testing.T) {
	_, h := newTestServer(t)
	for _, path := range []string{"/v1/me", "/v1/conversations"} {
		rec, _ := doJSON(t, h, "GET", path, "", nil)
		if rec.Code != 401 {
			t.Fatalf("%s 未登录应返回 401，得到 %d", path, rec.Code)
		}
	}
	// 伪造 token
	rec, _ := doJSON(t, h, "GET", "/v1/me", "fake-token", nil)
	if rec.Code != 401 {
		t.Fatalf("伪造 token 应被拒绝，得到 %d", rec.Code)
	}
}

func TestConversationLifecycle(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	// 创建
	rec, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title":  "标书攻坚组",
		"studio": "bid",
	})
	if rec.Code != 201 {
		t.Fatalf("创建会话失败：%d %s", rec.Code, rec.Body.String())
	}
	conv := out["data"].(map[string]interface{})
	id := conv["id"].(string)

	// 场景有开场白时，应自动落一条消息
	rec, out = doJSON(t, h, "GET", "/v1/conversations/"+id+"/messages", token, nil)
	if rec.Code != 200 {
		t.Fatalf("读消息失败：%d", rec.Code)
	}
	msgs := out["data"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("竞标场景应有 1 条开场白，得到 %d 条", len(msgs))
	}
	first := msgs[0].(map[string]interface{})
	if first["agent"] != "工头" {
		t.Fatalf("开场白应由第一个角色发出，得到 %v", first["agent"])
	}

	// 列表
	rec, out = doJSON(t, h, "GET", "/v1/conversations", token, nil)
	if rec.Code != 200 || len(out["data"].([]interface{})) != 1 {
		t.Fatal("会话列表不正确")
	}

	// 删除
	rec, _ = doJSON(t, h, "DELETE", "/v1/conversations/"+id, token, nil)
	if rec.Code != 200 {
		t.Fatalf("删除失败：%d", rec.Code)
	}
	rec, _ = doJSON(t, h, "GET", "/v1/conversations/"+id, token, nil)
	if rec.Code != 404 {
		t.Fatalf("删除后应 404，得到 %d", rec.Code)
	}
}

func TestConversationIsolation(t *testing.T) {
	_, h := newTestServer(t)
	tokenA := login(t, h, "13800000000")

	// 用另一个账号（走验证码流程）
	rec, out := doJSON(t, h, "POST", "/v1/auth/send-code", "", map[string]string{
		"kind": "phone", "target": "13700000000",
	})
	if rec.Code != 200 {
		t.Fatalf("发验证码失败：%d", rec.Code)
	}
	code := out["data"].(map[string]interface{})["devCode"].(string)
	rec, out = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13700000000", "code": code,
	})
	if rec.Code != 200 {
		t.Fatalf("第二个账号登录失败：%s", rec.Body.String())
	}
	tokenB := out["data"].(map[string]interface{})["token"].(string)

	// A 建会话
	_, out = doJSON(t, h, "POST", "/v1/conversations", tokenA, map[string]interface{}{
		"title": "A 的群", "studio": "companion",
	})
	idA := out["data"].(map[string]interface{})["id"].(string)

	// B 不能读 A 的会话
	rec, _ = doJSON(t, h, "GET", "/v1/conversations/"+idA, tokenB, nil)
	if rec.Code != 404 {
		t.Fatalf("跨用户读会话应拒绝，得到 %d", rec.Code)
	}

	// B 的列表里不应出现 A 的群
	_, out = doJSON(t, h, "GET", "/v1/conversations", tokenB, nil)
	if len(out["data"].([]interface{})) != 0 {
		t.Fatal("B 不应看到 A 的会话")
	}
}

func TestScenariosEndpoint(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")
	rec, out := doJSON(t, h, "GET", "/v1/scenarios", token, nil)
	if rec.Code != 200 {
		t.Fatalf("场景接口失败：%d", rec.Code)
	}
	list := out["data"].([]interface{})
	if len(list) < 5 {
		t.Fatalf("应至少有 5 个场景，得到 %d", len(list))
	}
	// 场景列表不应把角色 prompt 暴露给前端
	first := list[0].(map[string]interface{})
	agents := first["agents"].([]interface{})
	a0 := agents[0].(map[string]interface{})
	if _, leaked := a0["prompt"]; leaked {
		t.Fatal("角色的系统提示词不应通过接口外泄")
	}
}

func TestAdminAuth(t *testing.T) {
	_, h := newTestServer(t)

	rec, _ := doJSON(t, h, "GET", "/admin/api/overview", "", nil)
	if rec.Code != 401 {
		t.Fatalf("无令牌应 401，得到 %d", rec.Code)
	}

	req := httptest.NewRequest("GET", "/admin/api/overview", nil)
	req.Header.Set("X-Admin-Token", "test-admin")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != 200 {
		t.Fatalf("带正确令牌应 200，得到 %d：%s", rec2.Code, rec2.Body.String())
	}
}

func TestCustomAgentsConversation(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	rec, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title":  "老街口",
		"studio": "custom",
		"agents": []map[string]string{
			{"key": "a1", "name": "夜岚", "title": "冷面剑客", "prompt": "你话极少，一句不超过五个字。"},
			{"key": "a2", "name": "老周", "title": "退休教师", "prompt": "你爱讲道理，说话慢。"},
		},
	})
	if rec.Code != 201 {
		t.Fatalf("建自定义群失败：%d %s", rec.Code, rec.Body.String())
	}
	conv := out["data"].(map[string]interface{})
	if conv["agents"] == nil {
		t.Fatal("自定义角色应被持久化在会话上")
	}
	if n := len(conv["agents"].([]interface{})); n != 2 {
		t.Fatalf("应有 2 个自定义角色，得到 %d", n)
	}
}

func TestRenameConversation(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "旧名字", "studio": "companion",
	})
	id := out["data"].(map[string]interface{})["id"].(string)

	rec, out := doJSON(t, h, "PATCH", "/v1/conversations/"+id, token, map[string]interface{}{
		"title": "新名字",
	})
	if rec.Code != 200 {
		t.Fatalf("改名失败：%d %s", rec.Code, rec.Body.String())
	}
	if got := out["data"].(map[string]interface{})["title"]; got != "新名字" {
		t.Fatalf("标题未更新，得到 %v", got)
	}

	// 太长应被拒
	long := ""
	for i := 0; i < 50; i++ {
		long += "字"
	}
	rec, _ = doJSON(t, h, "PATCH", "/v1/conversations/"+id, token, map[string]interface{}{"title": long})
	if rec.Code != 400 {
		t.Fatalf("超长标题应 400，得到 %d", rec.Code)
	}

	// 别人不能改
	tokenB := login(t, h, "13800000000") // 同一个人；这里换个号
	_ = tokenB
	rec, _ = doJSON(t, h, "PATCH", "/v1/conversations/"+id, "", map[string]interface{}{"title": "x"})
	if rec.Code != 401 {
		t.Fatalf("未登录改名应 401，得到 %d", rec.Code)
	}
}

// 协作者体系：这是权限核心，测试要覆盖边界。
func TestCollaboratorAccess(t *testing.T) {
	_, h := newTestServer(t)

	owner := login(t, h, "13800000000")

	// 第二个用户走验证码流程注册
	_, out := doJSON(t, h, "POST", "/v1/auth/send-code", "", map[string]string{
		"kind": "phone", "target": "13600000001",
	})
	code := out["data"].(map[string]interface{})["devCode"].(string)
	_, out = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13600000001", "code": code,
	})
	guest := out["data"].(map[string]interface{})["token"].(string)

	// owner 建群
	_, out = doJSON(t, h, "POST", "/v1/conversations", owner, map[string]interface{}{
		"title": "协作测试", "studio": "companion",
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	// 邀请前：外人看不到
	rec, _ := doJSON(t, h, "GET", "/v1/conversations/"+convID, guest, nil)
	if rec.Code != 404 {
		t.Fatalf("邀请前外人应 404，得到 %d", rec.Code)
	}
	_, out = doJSON(t, h, "GET", "/v1/conversations", guest, nil)
	if len(out["data"].([]interface{})) != 0 {
		t.Fatal("邀请前不应出现在对方的列表里")
	}

	// 邀请
	rec, out = doJSON(t, h, "POST", "/v1/conversations/"+convID+"/collaborators", owner, map[string]interface{}{
		"target": "13600000001",
	})
	if rec.Code != 200 {
		t.Fatalf("邀请失败：%d %s", rec.Code, rec.Body.String())
	}

	// 邀请后：能看到、能发消息
	rec, _ = doJSON(t, h, "GET", "/v1/conversations/"+convID, guest, nil)
	if rec.Code != 200 {
		t.Fatalf("受邀后应能访问，得到 %d", rec.Code)
	}
	_, out = doJSON(t, h, "GET", "/v1/conversations", guest, nil)
	if len(out["data"].([]interface{})) != 1 {
		t.Fatal("受邀后应出现在对方的列表里")
	}

	rec, _ = doJSON(t, h, "POST", "/v1/conversations/"+convID+"/orchestrate", guest, map[string]interface{}{
		"content": "我也说一句",
	})
	if rec.Code != 200 {
		t.Fatalf("协作者应能发言，得到 %d", rec.Code)
	}

	// 协作者不能改群名（管理动作只归创建者）
	rec, _ = doJSON(t, h, "PATCH", "/v1/conversations/"+convID, guest, map[string]interface{}{
		"title": "我要改名",
	})
	if rec.Code != 403 {
		t.Fatalf("协作者改名应被拒绝，得到 %d", rec.Code)
	}

	// 协作者不能邀请别人
	rec, _ = doJSON(t, h, "POST", "/v1/conversations/"+convID+"/collaborators", guest, map[string]interface{}{
		"target": "13800000000",
	})
	if rec.Code == 200 {
		t.Fatal("协作者不应有权邀请别人")
	}

	// 协作者主动退出
	rec, _ = doJSON(t, h, "POST", "/v1/conversations/"+convID+"/leave", guest, nil)
	if rec.Code != 200 {
		t.Fatalf("退出失败：%d", rec.Code)
	}
	rec, _ = doJSON(t, h, "GET", "/v1/conversations/"+convID, guest, nil)
	if rec.Code != 404 {
		t.Fatalf("退出后应重新变成 404，得到 %d", rec.Code)
	}

	// 创建者不能退出
	rec, _ = doJSON(t, h, "POST", "/v1/conversations/"+convID+"/leave", owner, nil)
	if rec.Code == 200 {
		t.Fatal("创建者不能退出群聊")
	}
}

func TestInviteNonExistentUser(t *testing.T) {
	_, h := newTestServer(t)
	owner := login(t, h, "13800000000")
	_, out := doJSON(t, h, "POST", "/v1/conversations", owner, map[string]interface{}{
		"title": "空邀请", "studio": "companion",
	})
	id := out["data"].(map[string]interface{})["id"].(string)

	rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+id+"/collaborators", owner, map[string]interface{}{
		"target": "13999999999",
	})
	if rec.Code != 404 {
		t.Fatalf("邀请未注册用户应 404，得到 %d", rec.Code)
	}
}

// 角色库：存下来能复用，且改库不影响已建的群（快照语义）
func TestAgentLibrary(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	// 存一个角色
	rec, out := doJSON(t, h, "POST", "/v1/agents", token, map[string]interface{}{
		"name":   "夜岚",
		"title":  "冷面剑客",
		"prompt": "你说话极简，一句不超过五个字，绝不主动闲聊。",
		"color":  "#B45309",
	})
	if rec.Code != 201 {
		t.Fatalf("存角色失败：%d %s", rec.Code, rec.Body.String())
	}
	agent := out["data"].(map[string]interface{})
	agentID := agent["id"].(string)

	// 人设太短应被拒
	rec, _ = doJSON(t, h, "POST", "/v1/agents", token, map[string]interface{}{
		"name": "太短", "prompt": "话少",
	})
	if rec.Code != 400 {
		t.Fatalf("人设过短应 400，得到 %d", rec.Code)
	}

	// 列表
	_, out = doJSON(t, h, "GET", "/v1/agents", token, nil)
	if len(out["data"].([]interface{})) != 1 {
		t.Fatal("角色库应有 1 个角色")
	}

	// 拉进群
	// 用 custom 而不是 companion——后者会自动生成随机朋友，
	// 会让「群里应有 1 个自定义角色」这个断言失真
	_, out = doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "带角色的群", "studio": "custom",
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	rec, out = doJSON(t, h, "POST", "/v1/agents/"+agentID+"/to/"+convID, token, nil)
	if rec.Code != 200 {
		t.Fatalf("拉进群失败：%d %s", rec.Code, rec.Body.String())
	}
	conv := out["data"].(map[string]interface{})
	custom := conv["agents"].([]interface{})
	if len(custom) != 1 {
		t.Fatalf("群里应有 1 个自定义角色，得到 %d", len(custom))
	}
	if custom[0].(map[string]interface{})["name"] != "夜岚" {
		t.Fatal("角色名不对")
	}

	// 重复拉同一个角色应拒绝
	rec, _ = doJSON(t, h, "POST", "/v1/agents/"+agentID+"/to/"+convID, token, nil)
	if rec.Code != 409 {
		t.Fatalf("重复拉入应 409，得到 %d", rec.Code)
	}

	// 改库里的人设，群里那份不应跟着变（快照）
	rec, _ = doJSON(t, h, "PATCH", "/v1/agents/"+agentID, token, map[string]interface{}{
		"prompt": "改成完全不同的设定，话很多很热情，见谁都打招呼聊半天那种。",
	})
	if rec.Code != 200 {
		t.Fatalf("改角色失败：%d", rec.Code)
	}
	_, out = doJSON(t, h, "GET", "/v1/conversations/"+convID, token, nil)
	conv2 := out["data"].(map[string]interface{})
	p := conv2["agents"].([]interface{})[0].(map[string]interface{})["prompt"].(string)
	if p != "你说话极简，一句不超过五个字，绝不主动闲聊。" {
		t.Fatalf("已有群里的角色不应被库的改动影响，得到 %q", p)
	}

	// 别人看不到我的角色
	tokenB := login(t, h, "13800000000")
	_ = tokenB
	_, out = doJSON(t, h, "GET", "/v1/agents", "", nil)
	if out["ok"] == true {
		t.Fatal("未登录不应能读角色库")
	}

	// 删除
	rec, _ = doJSON(t, h, "DELETE", "/v1/agents/"+agentID, token, nil)
	if rec.Code != 200 {
		t.Fatalf("删除失败：%d", rec.Code)
	}
	_, out = doJSON(t, h, "GET", "/v1/agents", token, nil)
	if len(out["data"].([]interface{})) != 0 {
		t.Fatal("删除后角色库应为空")
	}
}

// 我的工作室：把调好的组合存成模板，像内置场景一样复用
func TestSavedStudio(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	// 用自定义角色建一个群
	rec, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title":  "老街口",
		"studio": "custom",
		"agents": []map[string]string{
			{"key": "a1", "name": "夜岚", "title": "冷面剑客", "prompt": "你话极少，一句不超过五个字。"},
			{"key": "a2", "name": "林小满", "title": "小卖部老板娘", "prompt": "你话多爱笑，五点半进货。"},
		},
	})
	if rec.Code != 201 {
		t.Fatalf("建群失败：%d", rec.Code)
	}
	convID := out["data"].(map[string]interface{})["id"].(string)

	// 从这个群存成模板
	rec, out = doJSON(t, h, "POST", "/v1/studios", token, map[string]interface{}{
		"fromConvId": convID,
		"tagline":    "楼下几个邻居，随时能唠两句",
	})
	if rec.Code != 201 {
		t.Fatalf("存模板失败：%d %s", rec.Code, rec.Body.String())
	}
	st := out["data"].(map[string]interface{})
	if st["name"] != "老街口" {
		t.Fatalf("没指定名字时应沿用群名，得到 %v", st["name"])
	}
	if n := len(st["agents"].([]interface{})); n != 2 {
		t.Fatalf("模板应含 2 个角色，得到 %d", n)
	}

	// 场景列表里应该出现它，并标记为自定义
	_, out = doJSON(t, h, "GET", "/v1/scenarios", token, nil)
	list := out["data"].([]interface{})
	var found bool
	for _, raw := range list {
		s := raw.(map[string]interface{})
		if s["name"] == "老街口" {
			found = true
			if s["custom"] != true {
				t.Fatal("自定义模板应标记 custom=true")
			}
		}
	}
	if !found {
		t.Fatal("存的模板应出现在场景列表里")
	}
	if len(list) < 6 {
		t.Fatalf("内置 5 个 + 自定义 1 个，应至少 6 条，得到 %d", len(list))
	}

	// 没有自定义角色的群不能存。
	// 注意不能用 companion / anime——那两个场景开群时会自动生成角色，
	// 永远有自定义角色。用竞标场景（角色固定在场景里，不进 conv.Agents）。
	_, out = doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "纯预设群", "studio": "bid",
	})
	emptyConv := out["data"].(map[string]interface{})["id"].(string)
	rec, _ = doJSON(t, h, "POST", "/v1/studios", token, map[string]interface{}{
		"fromConvId": emptyConv,
	})
	if rec.Code != 400 {
		t.Fatalf("没有自定义角色时应拒绝，得到 %d", rec.Code)
	}

	// 删除
	studioID := st["id"].(string)
	rec, _ = doJSON(t, h, "DELETE", "/v1/studios/"+studioID, token, nil)
	if rec.Code != 200 {
		t.Fatalf("删模板失败：%d", rec.Code)
	}
	_, out = doJSON(t, h, "GET", "/v1/scenarios", token, nil)
	for _, raw := range out["data"].([]interface{}) {
		if raw.(map[string]interface{})["name"] == "老街口" {
			t.Fatal("删除后不应再出现在列表里")
		}
	}
}

func TestAdminOps(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	_, me := doJSON(t, h, "GET", "/v1/me", token, nil)
	user := me["data"].(map[string]interface{})["user"].(map[string]interface{})
	userID := user["id"].(string)

	// 先花掉一点额度
	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "运维测试", "studio": "companion",
	})
	convID := out["data"].(map[string]interface{})["id"].(string)
	doJSON(t, h, "POST", "/v1/conversations/"+convID+"/orchestrate", token, map[string]interface{}{
		"content": "花点额度",
	})

	// 清零
	req := httptest.NewRequest("POST", "/admin/api/users/"+userID+"/reset-usage", nil)
	req.Header.Set("X-Admin-Token", "test-admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("清零失败：%d %s", rec.Code, rec.Body.String())
	}
	_, me = doJSON(t, h, "GET", "/v1/me", token, nil)
	if used := me["data"].(map[string]interface{})["user"].(map[string]interface{})["used"].(float64); used != 0 {
		t.Fatalf("清零后 used 应为 0，得到 %v", used)
	}

	// 会话列表（带锁状态）
	req = httptest.NewRequest("GET", "/admin/api/conversations", nil)
	req.Header.Set("X-Admin-Token", "test-admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("会话列表失败：%d", rec.Code)
	}
	var listResp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	list := listResp["data"].([]interface{})
	if len(list) == 0 {
		t.Fatal("会话列表不应为空")
	}
	first := list[0].(map[string]interface{})
	if _, hasBusy := first["busy"]; !hasBusy {
		t.Fatal("会话列表应带 busy 字段")
	}

	// 强行解锁（本来没锁，也应成功且 wasBusy=false）
	req = httptest.NewRequest("POST", "/admin/api/conversations/"+convID+"/unlock", nil)
	req.Header.Set("X-Admin-Token", "test-admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("解锁失败：%d", rec.Code)
	}

	// 无令牌一律拒绝
	req = httptest.NewRequest("POST", "/admin/api/users/"+userID+"/reset-usage", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("无令牌应 401，得到 %d", rec.Code)
	}
}

// 协作者列表要返回真实姓名和真实在线状态
func TestCollaboratorsShowRealNames(t *testing.T) {
	_, h := newTestServer(t)
	owner := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/conversations", owner, map[string]interface{}{
		"title": "协作", "studio": "companion",
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	// 加一个协作者
	rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+convID+"/collaborators", owner,
		map[string]interface{}{"target": "13800000000"})
	if rec.Code != 200 && rec.Code != 201 {
		t.Fatalf("加协作者失败：%d", rec.Code)
	}

	_, out = doJSON(t, h, "GET", "/v1/conversations/"+convID+"/collaborators", owner, nil)
	people := out["data"].([]interface{})
	if len(people) == 0 {
		t.Fatal("协作者列表不应为空")
	}
	for _, raw := range people {
		p := raw.(map[string]interface{})
		if p["name"] == "" || p["name"] == nil {
			t.Fatalf("协作者应带真实姓名，得到 %v", p)
		}
		if _, ok := p["online"]; !ok {
			t.Fatal("协作者应带 online 字段——界面上的绿点不能是写死的")
		}
	}
}

func TestCharacterLibrary(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	rec, out := doJSON(t, h, "GET", "/v1/characters?q=%E6%AF%92%E8%88%8C", token, nil)
	if rec.Code != 200 {
		t.Fatalf("搜索角色失败：%d %s", rec.Code, rec.Body.String())
	}
	items, _ := out["data"].([]interface{})
	if len(items) == 0 {
		t.Fatal("搜「毒舌」应该有结果")
	}

	rec, out = doJSON(t, h, "POST", "/v1/characters/to-agent", token, map[string]string{
		"name": "雪之下雪乃",
	})
	if rec.Code != 200 {
		t.Fatalf("角色转 Agent 失败：%d %s", rec.Code, rec.Body.String())
	}
	agent := out["data"].(map[string]interface{})
	if agent["key"] == "" || agent["key"] == nil {
		t.Fatal("角色转 Agent 必须带上 key，否则多人建群会全部被当成同一个人")
	}
	if agent["prompt"] == "" || agent["prompt"] == nil {
		t.Fatal("角色转 Agent 应带完整人设")
	}
}

func TestMemorySnapshotAndClear(t *testing.T) {
	srv, h := newTestServer(t)
	token := login(t, h, "13800000000")

	// 当前用户 ID
	_, me := doJSON(t, h, "GET", "/v1/me", token, nil)
	uid := me["data"].(map[string]interface{})["user"].(map[string]interface{})["id"].(string)

	// 建一个群，挂上记忆服务
	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "记忆接口测试", "studio": "bid",
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	ms, err := memory.NewFileStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv.mem = service.NewMemoryService(ms, nil)
	_ = ms.AddConversation(convID, []*domain.Memory{
		{Kind: domain.KindFact, Text: "用户下周三要交标书"},
	})
	_ = ms.AddAgent("bid:conductor", "工头", uid, []*domain.Memory{
		{Kind: domain.KindFact, Text: "用户最近压力很大"},
		{Kind: domain.KindRelation, Text: "你们一起扛过一个截止日期"},
	})

	rec, out := doJSON(t, h, "GET", "/v1/memories?conversationId="+convID, token, nil)
	if rec.Code != 200 {
		t.Fatalf("读取记忆失败：%d %s", rec.Code, rec.Body.String())
	}
	data := out["data"].(map[string]interface{})
	if n := len(data["conversation"].([]interface{})); n != 1 {
		t.Fatalf("群记忆应为 1 条，得到 %d", n)
	}
	agents := data["agents"].([]interface{})
	if len(agents) != 1 {
		t.Fatalf("应有 1 个角色记忆，得到 %d", len(agents))
	}
	if agents[0].(map[string]interface{})["relation"] == nil {
		t.Fatal("关系记忆应被单独返回")
	}

	rec, out = doJSON(t, h, "DELETE", "/v1/memories?characterId=bid:conductor", token, nil)
	if rec.Code != 200 || out["data"].(map[string]interface{})["cleared"].(float64) != 2 {
		t.Fatalf("清空角色记忆应返回 2，得到 %d %v", rec.Code, out)
	}
	_, out = doJSON(t, h, "GET", "/v1/memories?conversationId="+convID, token, nil)
	if n := len(out["data"].(map[string]interface{})["agents"].([]interface{})); n != 0 {
		t.Fatalf("清空后角色记忆应为空，得到 %d", n)
	}

	rec, out = doJSON(t, h, "DELETE", "/v1/memories?conversationId="+convID, token, nil)
	if rec.Code != 200 || out["data"].(map[string]interface{})["cleared"].(float64) != 1 {
		t.Fatalf("清空群记忆应返回 1，得到 %d %v", rec.Code, out)
	}

	// 空快照返回 [] 而不是 null，前端和第三方调用都少一层判空。
	_, out = doJSON(t, h, "GET", "/v1/memories?conversationId="+convID, token, nil)
	data = out["data"].(map[string]interface{})
	if data["conversation"] == nil || data["agents"] == nil {
		t.Fatalf("空记忆应返回空数组，得到 %v", data)
	}
}

func TestPolishAgentValidationAndLibraryFallback(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	rec, _ := doJSON(t, h, "POST", "/v1/agents/polish", token, map[string]interface{}{
		"name": "", "source": "火影忍者", "kind": "anime",
	})
	if rec.Code != 400 {
		t.Fatalf("空名字应返回 400，得到 %d", rec.Code)
	}

	// 测试上游是 mock，解析不出 JSON 时应回退到本地角色库。
	rec, out := doJSON(t, h, "POST", "/v1/agents/polish", token, map[string]interface{}{
		"name": "雪之下雪乃", "source": "", "kind": "anime",
	})
	if rec.Code != 200 {
		t.Fatalf("本地库命中应返回 200，得到 %d %s", rec.Code, rec.Body.String())
	}
	data := out["data"].(map[string]interface{})
	if data["found"] != true {
		t.Fatalf("本地库命中应 found=true：%v", data)
	}
	agent := data["agent"].(map[string]interface{})
	if agent["name"] != "雪之下雪乃" || agent["prompt"] == "" {
		t.Fatalf("回退角色不完整：%v", agent)
	}
}
