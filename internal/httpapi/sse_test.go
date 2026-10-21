package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// readSSE 把 SSE 响应读成事件列表。
func readSSE(t *testing.T, body []byte) []service.Event {
	t.Helper()
	var out []service.Event
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev service.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out
}

func TestOrchestrateStream(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")

	// 建群
	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "茶话会", "studio": "anime",
	})
	id := out["data"].(map[string]interface{})["id"].(string)

	// 用户发言 → 角色自主响应
	rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+id+"/orchestrate", token, map[string]interface{}{
		"content": "今天好累啊",
	})
	if rec.Code != 200 {
		t.Fatalf("编排接口失败：%d %s", rec.Code, rec.Body.String())
	}

	events := readSSE(t, rec.Body.Bytes())
	if len(events) == 0 {
		t.Fatal("没有收到任何 SSE 事件")
	}

	// 必须有一条 done 收尾
	last := events[len(events)-1]
	if last.Type != "done" {
		t.Fatalf("最后一个事件应是 done，得到 %s", last.Type)
	}

	// 至少有一个角色开口（mock 上游不会 PASS）
	var starts, deltas, ends int
	for _, e := range events {
		switch e.Type {
		case "start":
			starts++
		case "delta":
			deltas++
		case "end":
			ends++
		}
	}
	if starts == 0 {
		t.Fatal("这一轮没有人开口")
	}
	if deltas == 0 {
		t.Fatal("没有收到任何增量文本")
	}
	if ends != starts {
		t.Fatalf("start 与 end 数量不匹配：%d vs %d", starts, ends)
	}

	// 不应超过场景上限
	sc, _ := service.FindScenario("anime")
	if starts > sc.Round {
		t.Fatalf("发言人数 %d 超过场景上限 %d", starts, sc.Round)
	}

	// 消息应已落库（用户 1 条 + 角色若干）
	_, out = doJSON(t, h, "GET", "/v1/conversations/"+id+"/messages", token, nil)
	msgs := out["data"].([]interface{})
	if len(msgs) < 2 {
		t.Fatalf("落库消息不足：%d 条", len(msgs))
	}
}

func TestOrchestrateRejectsEmpty(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")
	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "空消息", "studio": "companion",
	})
	id := out["data"].(map[string]interface{})["id"].(string)

	rec, _ := doJSON(t, h, "POST", "/v1/conversations/"+id+"/orchestrate", token, map[string]interface{}{
		"content": "",
	})
	if rec.Code != 400 {
		t.Fatalf("空消息应 400，得到 %d", rec.Code)
	}
}

func TestKickoffStream(t *testing.T) {
	_, h := newTestServer(t)
	token := login(t, h, "13800000000")
	_, out := doJSON(t, h, "POST", "/v1/conversations", token, map[string]interface{}{
		"title": "冷场", "studio": "companion",
	})
	id := out["data"].(map[string]interface{})["id"].(string)

	req := httptest.NewRequest("POST", "/v1/conversations/"+id+"/kickoff", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("kickoff 失败：%d", rec.Code)
	}
	events := readSSE(t, rec.Body.Bytes())
	if len(events) == 0 || events[len(events)-1].Type != "done" {
		t.Fatal("kickoff 应以 done 结束")
	}
	var starts int
	for _, e := range events {
		if e.Type == "start" {
			starts++
		}
	}
	if starts == 0 {
		t.Fatal("kickoff 至少要有人起头，否则整轮冷场")
	}
}
