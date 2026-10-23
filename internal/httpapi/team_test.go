package httpapi

import (
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
)

func errCodeOf(body map[string]interface{}) string {
	e, _ := body["error"].(map[string]interface{})
	if e == nil {
		return ""
	}
	s, _ := e["code"].(string)
	return s
}

func createdID(t *testing.T, body map[string]interface{}) string {
	t.Helper()
	data, _ := body["data"].(map[string]interface{})
	if data == nil {
		t.Fatalf("响应缺少 data：%v", body)
	}
	if id, _ := data["id"].(string); id != "" {
		return id
	}
	t.Fatalf("响应缺少 id：%v", data)
	return ""
}

// 团队接口要求：已绑定工作区、群主、至少两个角色。
func TestStartTeamValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetTeamManager(agent.NewTeamManager())
	srv.SetAgentManager(agent.NewManager())
	// 路由在 Handler() 里按已注入的依赖注册；注入后重建一次。
	h := srv.Handler()

	token := login(t, h, "13800000000")

	rec, body := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "团队测试", "studio": "code", "members": []string{"arch", "backend"},
	})
	if rec.Code != 201 {
		t.Fatalf("create conv: %d %v", rec.Code, body)
	}
	convID := createdID(t, body)

	rec, body = doJSON(t, h, "POST", "/v1/conversations/"+convID+"/team", token, map[string]interface{}{
		"task": "做点事",
	})
	if rec.Code != 409 {
		t.Fatalf("未绑工作区应 409，得到 %d %v", rec.Code, body)
	}
	if code := errCodeOf(body); code != "workspace_required" {
		t.Fatalf("error code=%s", code)
	}

	rec, body = doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "单聊", "studio": "code", "members": []string{"backend"},
	})
	if rec.Code != 201 {
		t.Fatalf("create solo: %d %v", rec.Code, body)
	}
	soloID := createdID(t, body)

	rec, body = doJSON(t, h, "POST", "/v1/conversations/"+soloID+"/team", token, map[string]interface{}{
		"task": "做点事",
	})
	if rec.Code != 409 {
		t.Fatalf("单聊未绑工作区应 409，得到 %d %v", rec.Code, body)
	}
}

func TestTeamEndpointsRequireAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetTeamManager(agent.NewTeamManager())
	srv.SetAgentManager(agent.NewManager())
	h := srv.Handler()

	rec, _ := doJSON(t, h, "GET", "/v1/teams/nope", "", nil)
	if rec.Code != 401 {
		t.Fatalf("未登录应 401，得到 %d", rec.Code)
	}
}
