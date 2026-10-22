package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/service"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// newTightServer 用一个很小的令牌桶建服务，专测限流路径。
func newTightServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		AdminToken: "test-admin", JWTSecret: "test-secret",
		MaxRetries: 1, BreakerError: 3, BreakerOpen: time.Second, AllowMock: true,
	}
	st := store.NewMemory()
	reg := gateway.NewRegistry()
	reg.Add(&gateway.Mock{})
	models := gateway.NewModelTable([]domain.ModelInfo{
		{Key: "mock-chat", Provider: "mock", Upstream: "mock", Enabled: true, Weight: 1},
	})
	gw := gateway.New(reg, models, 1, 3, time.Second)
	auth := service.NewAuth(st, cfg.JWTSecret, service.LogSender{}, true)
	limiter := gateway.NewLimiter(1, 2) // 每分钟 1 个、突发 2 个：第三次必被拒
	admin := service.NewAdmin(st, gw, &service.Shared{Limiter: limiter, Version: "test"})
	srv := NewServer(cfg, st, auth, service.NewChat(st, gw), admin, gw,
		service.NewOrchestrator(st, gw, service.NewSlowLog(50, 1000)), service.NewDrafter(gw), limiter)
	return srv.Handler()
}

// 认证后的每个请求都要过限流：令牌桶空了应回 429。
func TestAuthedRequestRateLimited(t *testing.T) {
	h := newTightServer(t)
	token := login(t, h, "13800000000")

	// 突发 2 个：前两次 200
	for i := 0; i < 2; i++ {
		rec, _ := doJSON(t, h, "GET", "/v1/me", token, nil)
		if rec.Code != 200 {
			t.Fatalf("前两次请求应放行，第 %d 次得到 %d：%s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec, out := doJSON(t, h, "GET", "/v1/me", token, nil)
	if rec.Code != 429 {
		t.Fatalf("桶空后应 429，得到 %d", rec.Code)
	}
	e := out["error"].(map[string]interface{})
	if e["code"] != "rate_limited" {
		t.Fatalf("错误码应为 rate_limited，得到 %v", e["code"])
	}
}

// send-code 在登录之前，按 IP 限流。
// 每次用不同目标号：service 层还有「同一目标 60 秒一次」的限制，
// 这里只测 IP 维度的令牌桶。
func TestSendCodeRateLimitedPerIP(t *testing.T) {
	h := newTightServer(t)
	for i := 0; i < 2; i++ {
		rec, _ := doJSON(t, h, "POST", "/v1/auth/send-code", "", map[string]string{
			"kind": "phone", "target": "1350000000" + strconv.Itoa(i),
		})
		if rec.Code != 200 {
			t.Fatalf("前两次发码应放行，得到 %d：%s", rec.Code, rec.Body.String())
		}
	}
	rec, _ := doJSON(t, h, "POST", "/v1/auth/send-code", "", map[string]string{
		"kind": "phone", "target": "13500000009",
	})
	if rec.Code != 429 {
		t.Fatalf("同 IP 第三次发码应 429，得到 %d", rec.Code)
	}
}

// adminQuota 通过运维台把某个用户的额度设成指定值（测试夹具）。
func adminQuota(t *testing.T, h http.Handler, userID string, quota int) {
	t.Helper()
	rec := doAdmin(t, h, "POST", "/admin/api/users/"+userID+"/quota",
		`{"quota":`+strconv.Itoa(quota)+`}`)
	if rec.Code != 200 {
		t.Fatalf("设置额度失败：%d %s", rec.Code, rec.Body.String())
	}
}

func myUserID(t *testing.T, h http.Handler, token string) string {
	t.Helper()
	_, out := doJSON(t, h, "GET", "/v1/me", token, nil)
	return out["data"].(map[string]interface{})["user"].(map[string]interface{})["id"].(string)
}

// doAdmin 带 X-Admin-Token 的请求（测试夹具）。
func doAdmin(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("X-Admin-Token", "test-admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// /v1/chat/completions 不再绕过额度：超额 402，成功后计数。
func TestGatewayChatQuota(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")
	uid := myUserID(t, h, token)

	adminQuota(t, h, uid, 0)
	rec, _ := doJSON(t, h, "POST", "/v1/chat/completions", token, map[string]interface{}{
		"messages": []map[string]string{{"role": "user", "content": "你好"}},
	})
	if rec.Code != 402 {
		t.Fatalf("额度耗尽应 402，得到 %d：%s", rec.Code, rec.Body.String())
	}

	adminQuota(t, h, uid, 5)
	rec, _ = doJSON(t, h, "POST", "/v1/chat/completions", token, map[string]interface{}{
		"messages": []map[string]string{{"role": "user", "content": "你好"}},
	})
	if rec.Code != 200 {
		t.Fatalf("有额度应 200，得到 %d：%s", rec.Code, rec.Body.String())
	}
	_, out := doJSON(t, h, "GET", "/v1/me", token, nil)
	used := out["data"].(map[string]interface{})["user"].(map[string]interface{})["used"].(float64)
	if used != 1 {
		t.Fatalf("一次成功调用后 used 应为 1，得到 %v", used)
	}
}

// 流式直通调用同样要计数。
func TestGatewayChatStreamCharged(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	rec, _ := doJSON(t, h, "POST", "/v1/chat/completions", token, map[string]interface{}{
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": "你好"}},
	})
	if rec.Code != 200 {
		t.Fatalf("流式调用失败：%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatal("流式响应应以 [DONE] 结束")
	}
	_, out := doJSON(t, h, "GET", "/v1/me", token, nil)
	used := out["data"].(map[string]interface{})["user"].(map[string]interface{})["used"].(float64)
	if used != 1 {
		t.Fatalf("流式正常结束后 used 应为 1，得到 %v", used)
	}
}

// 协作者能看群、能说话，但不能解散别人的群。
func TestCollaboratorCannotDeleteConv(t *testing.T) {
	_, h := newTestServer(t)
	owner := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/auth/send-code", "", map[string]string{
		"kind": "phone", "target": "13600000002",
	})
	code := out["data"].(map[string]interface{})["devCode"].(string)
	_, out = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13600000002", "code": code,
	})
	guest := out["data"].(map[string]interface{})["token"].(string)

	_, out = doJSON(t, h, "POST", "/v1/conversations", owner, map[string]interface{}{
		"title": "谁的群", "studio": "companion",
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+convID+"/collaborators", owner,
		map[string]interface{}{"target": "13600000002"})
	if rec.Code != 200 {
		t.Fatalf("邀请失败：%d %s", rec.Code, rec.Body.String())
	}

	rec, _ = doJSON(t, h, "DELETE", "/v1/conversations/"+convID, guest, nil)
	if rec.Code != 403 {
		t.Fatalf("协作者删群应 403，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "只有群主能删除群聊") {
		t.Fatalf("错误文案应说明只有群主能删，得到 %s", rec.Body.String())
	}

	rec, _ = doJSON(t, h, "DELETE", "/v1/conversations/"+convID, owner, nil)
	if rec.Code != 200 {
		t.Fatalf("群主删群应 200，得到 %d", rec.Code)
	}
}

// 把角色移出群：owner only、按 Key 匹配、连带清 Members、不存在 404。
func TestRemoveConvAgent(t *testing.T) {
	_, h := newTestServer(t)
	owner := login(t, h, "13800000000")

	rec, out := doJSON(t, h, "POST", "/v1/conversations", owner, map[string]interface{}{
		"title":   "移出角色",
		"studio":  "custom",
		"members": []string{"a1", "a2"},
		"agents": []map[string]string{
			{"key": "a1", "name": "夜岚", "title": "剑客", "prompt": "你话极少，一句不超过五个字。"},
			{"key": "a2", "name": "老周", "title": "教师", "prompt": "你爱讲道理，说话慢条斯理。"},
		},
	})
	if rec.Code != 201 {
		t.Fatalf("建群失败：%d %s", rec.Code, rec.Body.String())
	}
	convID := out["data"].(map[string]interface{})["id"].(string)

	// 协作者不能移
	_, out = doJSON(t, h, "POST", "/v1/auth/send-code", "", map[string]string{
		"kind": "phone", "target": "13600000003",
	})
	code := out["data"].(map[string]interface{})["devCode"].(string)
	_, out = doJSON(t, h, "POST", "/v1/auth/login", "", map[string]string{
		"kind": "phone", "target": "13600000003", "code": code,
	})
	guest := out["data"].(map[string]interface{})["token"].(string)
	doJSON(t, h, "POST", "/v1/conversations/"+convID+"/collaborators", owner,
		map[string]interface{}{"target": "13600000003"})
	rec, _ = doJSON(t, h, "DELETE", "/v1/conversations/"+convID+"/agents/a1", guest, nil)
	if rec.Code != 403 {
		t.Fatalf("协作者移角色应 403，得到 %d", rec.Code)
	}

	// owner 移出 a1
	rec, out = doJSON(t, h, "DELETE", "/v1/conversations/"+convID+"/agents/a1", owner, nil)
	if rec.Code != 200 {
		t.Fatalf("移出角色失败：%d %s", rec.Code, rec.Body.String())
	}
	conv := out["data"].(map[string]interface{})
	agents := conv["agents"].([]interface{})
	if len(agents) != 1 || agents[0].(map[string]interface{})["key"] != "a2" {
		t.Fatalf("移出后应只剩 a2，得到 %v", agents)
	}
	for _, m := range conv["members"].([]interface{}) {
		if m == "a1" {
			t.Fatal("Members 里的同名 key 也应被清掉")
		}
	}

	// 再移一次同一个 → 404
	rec, _ = doJSON(t, h, "DELETE", "/v1/conversations/"+convID+"/agents/a1", owner, nil)
	if rec.Code != 404 {
		t.Fatalf("不存在的角色应 404，得到 %d", rec.Code)
	}
}

// PATCH 会话：带 agents 就整组替换，不带就不动；空数组清空。
func TestUpdateConvAgents(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "换角色组", "studio": "custom",
		"agents": []map[string]string{
			{"key": "old", "name": "旧角色", "title": "旧", "prompt": "你是旧角色，说话老派。"},
		},
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	// 只改名：agents 不受影响
	_, out = doJSON(t, h, "PATCH", "/v1/conversations/"+convID, token,
		map[string]interface{}{"title": "只改名"})
	conv := out["data"].(map[string]interface{})
	if n := len(conv["agents"].([]interface{})); n != 1 {
		t.Fatalf("不带 agents 字段时不应动角色，得到 %d 个", n)
	}

	// 整组替换
	rec, out := doJSON(t, h, "PATCH", "/v1/conversations/"+convID, token, map[string]interface{}{
		"agents": []map[string]string{
			{"key": "n1", "name": "新甲", "title": "新", "prompt": "你是新角色甲，爱接话。"},
			{"key": "n2", "name": "新乙", "title": "新", "prompt": "你是新角色乙，爱提问。"},
		},
	})
	if rec.Code != 200 {
		t.Fatalf("替换角色组失败：%d %s", rec.Code, rec.Body.String())
	}
	conv = out["data"].(map[string]interface{})
	agents := conv["agents"].([]interface{})
	if len(agents) != 2 || agents[0].(map[string]interface{})["key"] != "n1" {
		t.Fatalf("角色组应被整组替换，得到 %v", agents)
	}

	// 空数组 = 清空（回到场景预设）。
	// 注意 agents 带 omitempty：清空后响应里不出现这个字段。
	rec, out = doJSON(t, h, "PATCH", "/v1/conversations/"+convID, token,
		map[string]interface{}{"agents": []map[string]string{}})
	if rec.Code != 200 {
		t.Fatalf("清空角色组失败：%d %s", rec.Code, rec.Body.String())
	}
	conv = out["data"].(map[string]interface{})
	if list, has := conv["agents"].([]interface{}); has && len(list) != 0 {
		t.Fatalf("空数组应清空角色组，得到 %d 个", len(list))
	}
}

// kickoff 也要在写 SSE 头之前挡住没额度的人。
func TestKickoffQuotaExceeded(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")
	uid := myUserID(t, h, token)

	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "冷场", "studio": "companion",
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	adminQuota(t, h, uid, 0)
	rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+convID+"/kickoff", token,
		map[string]interface{}{"topic": "随便聊"})
	if rec.Code != 402 {
		t.Fatalf("没额度的 kickoff 应 402，得到 %d：%s", rec.Code, rec.Body.String())
	}
}

// draft-agent 消耗真上游：没额度 402，入参太长 400。
func TestDraftAgentGuards(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")
	uid := myUserID(t, h, token)

	adminQuota(t, h, uid, 0)
	rec, _ := doJSON(t, h, "POST", "/v1/agents/draft", token,
		map[string]string{"name": "测试", "keywords": "测试"})
	if rec.Code != 402 {
		t.Fatalf("没额度的 draft 应 402，得到 %d", rec.Code)
	}

	// 恢复额度后测长度上限
	adminQuota(t, h, uid, 100)
	rec, _ = doJSON(t, h, "POST", "/v1/agents/draft", token, map[string]string{
		"name": strings.Repeat("字", 51), "keywords": "短",
	})
	if rec.Code != 400 {
		t.Fatalf("51 字名字应 400，得到 %d", rec.Code)
	}
	rec, _ = doJSON(t, h, "POST", "/v1/agents/draft", token, map[string]string{
		"name": "正常", "keywords": strings.Repeat("词", 201),
	})
	if rec.Code != 400 {
		t.Fatalf("201 字关键词应 400，得到 %d", rec.Code)
	}
	rec, out := doJSON(t, h, "POST", "/v1/agents/draft", token, map[string]string{
		"name": "正常", "keywords": "温柔,爱笑",
	})
	if rec.Code != 200 {
		t.Fatalf("正常 draft 应 200，得到 %d：%s", rec.Code, rec.Body.String())
	}
	if out["data"].(map[string]interface{})["name"] == "" {
		t.Fatal("draft 应返回生成的角色")
	}
}

// 管理台登录成功后不能再回显令牌。
func TestAdminLoginNoTokenEcho(t *testing.T) {
	_, h := newTestServer(t)

	rec, out := doJSON(t, h, "POST", "/admin/api/login", "",
		map[string]string{"token": "test-admin"})
	if rec.Code != 200 {
		t.Fatalf("正确令牌应 200，得到 %d", rec.Code)
	}
	data := out["data"].(map[string]interface{})
	if v, has := data["adminToken"]; has {
		t.Fatalf("登录响应不应回显令牌，得到 %v", v)
	}
	if data["ok"] != true {
		t.Fatalf("应返回 {ok:true}，得到 %v", data)
	}

	rec, _ = doJSON(t, h, "POST", "/admin/api/login", "",
		map[string]string{"token": "wrong-token"})
	if rec.Code != 401 {
		t.Fatalf("错误令牌应 401，得到 %d", rec.Code)
	}
}

// 模型表自砖守卫：不能删最后一个启用的模型，不能指向不存在的 provider。
func TestAdminModelGuards(t *testing.T) {
	_, h := newTestServer(t)

	// 只有一个启用的 mock-chat：删它 → 400
	rec := doAdmin(t, h, "DELETE", "/admin/api/models/mock-chat", "")
	if rec.Code != 400 {
		t.Fatalf("删最后一个启用模型应 400，得到 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "至少保留一个启用的模型") {
		t.Fatalf("文案应说明保留要求，得到 %s", rec.Body.String())
	}

	// 指向不存在的 provider → 400
	rec = doAdmin(t, h, "POST", "/admin/api/models",
		`{"key":"m2","provider":"no-such-provider","upstream":"x","enabled":true}`)
	if rec.Code != 400 {
		t.Fatalf("未知 provider 应 400，得到 %d：%s", rec.Code, rec.Body.String())
	}

	// 加一个同 provider 的启用模型后，原来的就能删了
	rec = doAdmin(t, h, "POST", "/admin/api/models",
		`{"key":"mock-2","provider":"mock","upstream":"mock","enabled":true,"weight":1}`)
	if rec.Code != 200 {
		t.Fatalf("合法模型应能 upsert，得到 %d：%s", rec.Code, rec.Body.String())
	}
	rec = doAdmin(t, h, "DELETE", "/admin/api/models/mock-chat", "")
	if rec.Code != 200 {
		t.Fatalf("还有别的启用模型时应允许删除，得到 %d：%s", rec.Code, rec.Body.String())
	}
}

// readyz：至少一个 provider 不在熔断 open 状态，且落盘健康。
func TestReadyz(t *testing.T) {
	_, h := newTestServer(t)
	rec, out := doJSON(t, h, "GET", "/readyz", "", nil)
	if rec.Code != 200 {
		t.Fatalf("readyz 应 200，得到 %d", rec.Code)
	}
	data := out["data"].(map[string]interface{})
	if data["ready"] != true {
		t.Fatalf("mock 上游 closed 状态应 ready，得到 %v", data)
	}
	if pr := data["providersReady"].(float64); pr < 1 {
		t.Fatalf("至少应有一个可用 provider，得到 %v", pr)
	}
}

// brokenWriter 模拟客户端中途断开：所有 Write 都失败。
type brokenWriter struct {
	*httptest.ResponseRecorder
}

func (b *brokenWriter) Write(p []byte) (int, error) { return 0, errors.New("broken pipe") }
func (b *brokenWriter) Flush()                      {}

// 客户端断开后，模型已生成的回复也必须落库，不能跟着连接一起丢。
func TestSendMessageSavesDespiteClientGone(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "断线保护", "studio": "custom",
		"agents": []map[string]string{
			{"key": "a1", "name": "夜岚", "title": "剑客", "prompt": "你话极少，一句不超过五个字。"},
		},
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	// 先正常发一条，拿到 mock 的基线行为
	rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+convID+"/messages", token,
		map[string]interface{}{"content": "在吗"})
	if rec.Code != 200 {
		t.Fatalf("正常发送失败：%d %s", rec.Code, rec.Body.String())
	}

	// 再用写必失败的 ResponseWriter 模拟断线
	bw := &brokenWriter{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("POST", "/v1/conversations/"+convID+"/messages",
		strings.NewReader(`{"content":"断线后这条也要落库"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(bw, req)

	// 模型回复应已保存（用户消息 + 两条助手回复）
	_, out = doJSON(t, h, "GET", "/v1/conversations/"+convID+"/messages", token, nil)
	msgs := out["data"].([]interface{})
	var assistants int
	for _, m := range msgs {
		if m.(map[string]interface{})["role"] == "assistant" {
			assistants++
		}
	}
	if assistants != 2 {
		t.Fatalf("断线的回复也应落库：应有 2 条助手消息，得到 %d", assistants)
	}
	last := msgs[len(msgs)-1].(map[string]interface{})
	if last["content"] == "" {
		t.Fatal("落库的助手消息内容不应为空")
	}
}

// legacy 发消息接口的入参校验与 orchestrate 对齐。
func TestSendMessageValidation(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "校验", "studio": "custom",
		"agents": []map[string]string{
			{"key": "a1", "name": "夜岚", "title": "剑客", "prompt": "你话极少，一句不超过五个字。"},
		},
	})
	convID := out["data"].(map[string]interface{})["id"].(string)

	cases := []struct {
		name, content string
		want          int
	}{
		{"空字符串", "", 400},
		{"只有空格", "   ", 400},
		{"超长", strings.Repeat("长", 4001), 400},
		{"正常", "正常消息", 200},
	}
	for _, c := range cases {
		rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+convID+"/messages", token,
			map[string]interface{}{"content": c.content})
		if rec.Code != c.want {
			t.Fatalf("%s：期望 %d，得到 %d", c.name, c.want, rec.Code)
		}
	}
}
