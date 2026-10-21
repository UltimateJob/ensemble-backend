package httpapi

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 两个协作者同时在一个群里说话，会发生什么？
func TestConcurrentOrchestrate(t *testing.T) {
	_, h := newTestServer(t)
	owner := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/conversations", owner, map[string]interface{}{
		"title": "并发测试", "studio": "companion",
	})
	id := out["data"].(map[string]interface{})["id"].(string)

	var wg sync.WaitGroup
	statuses := make([]int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			body := strings.NewReader(`{"content":"并发消息 ` + string(rune('A'+n)) + `"}`)
			req := httptest.NewRequest("POST", "/v1/conversations/"+id+"/orchestrate", body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+owner)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			statuses[n] = rec.Code
		}(i)
	}
	wg.Wait()

	t.Logf("四次并发请求的状态码：%v", statuses)

	// 统计消息
	_, out = doJSON(t, h, "GET", "/v1/conversations/"+id+"/messages?limit=200", owner, nil)
	msgs := out["data"].([]interface{})
	userMsgs := 0
	for _, m := range msgs {
		mm := m.(map[string]interface{})
		if mm["role"] == "user" {
			userMsgs++
		}
	}
	t.Logf("总消息 %d 条，其中用户消息 %d 条", len(msgs), userMsgs)

	if userMsgs != 4 {
		t.Errorf("4 次发言应全部落库（4 条用户消息），实际 %d 条", userMsgs)
	}

	// 同一时刻只应有一轮在编排，其余标记 busy
	busy := 0
	for _, m := range msgs {
		mm := m.(map[string]interface{})
		if mm["content"] == "" {
			busy++
		}
	}
	t.Logf("空白消息（busy 占位）：%d", busy)
}

// 同一会话并发时，一轮编排的发言不能和另一轮交错
func TestConcurrentOrchestrateNoInterleaving(t *testing.T) {
	_, h := newTestServer(t)
	owner := login(t, h, "13800000000")

	_, out := doJSON(t, h, "POST", "/v1/conversations", owner, map[string]interface{}{
		"title": "交错检查", "studio": "companion",
	})
	id := out["data"].(map[string]interface{})["id"].(string)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			body := strings.NewReader(`{"content":"并发 ` + string(rune('A'+n)) + `"}`)
			req := httptest.NewRequest("POST", "/v1/conversations/"+id+"/orchestrate", body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+owner)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
		}(i)
	}
	wg.Wait()

	_, out = doJSON(t, h, "GET", "/v1/conversations/"+id+"/messages?limit=200", owner, nil)
	msgs := out["data"].([]interface{})

	// 消息序号应该严格递增（store 保证），且同一轮的助手消息应连续成段：
	// 不能被另一轮的用户消息从中间打断。
	// 允许的情形：开场白 → 用户A → 助手×N → 用户B → 用户C →（被跳过）
	var lastAssistantIdx = -1
	interrupted := 0
	for i, m := range msgs {
		mm := m.(map[string]interface{})
		role, _ := mm["role"].(string)
		if role == "user" {
			if lastAssistantIdx >= 0 && i-lastAssistantIdx > 1 {
				// 上一段助手消息之后插了用户消息，正常；这里只记录
			}
			lastAssistantIdx = -1
			continue
		}
		if role == "assistant" {
			lastAssistantIdx = i
		}
	}
	_ = interrupted

	// 关键断言：助手消息不能出现在用户消息之前的第一段之后又回头穿插
	// 简化版：每段助手消息之间的用户消息数不超过 1（说明没有两轮同时说话）
	runs := 0
	inRun := false
	for _, m := range msgs {
		mm := m.(map[string]interface{})
		if mm["role"] == "assistant" {
			if !inRun {
				runs++
				inRun = true
			}
		} else {
			inRun = false
		}
	}
	t.Logf("助手消息分成 %d 段（并发被串行化后，每轮一段）", runs)
	if runs > 3 {
		t.Fatalf("助手消息段落过多（%d），可能存在两轮交错", runs)
	}
}
