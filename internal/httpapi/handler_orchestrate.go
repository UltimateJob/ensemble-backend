package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleOrchestrate 多 Agent 群聊：SSE 推送每个角色的发言。
func (s *Server) handleOrchestrate(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil || req.Content == "" {
		badRequest(w, "消息不能为空")
		return
	}

	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}

	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(ev service.Event) bool {
		b, err := json.Marshal(ev)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		if canFlush {
			flusher.Flush()
		}
		return true
	}

	s.orchestrator.Run(r.Context(), conv, u, req.Content, func(ev service.Event) {
		send(ev)
	})

	_ = s.store.AppendEvent(&domain.Event{
		UserID: u.ID, Kind: "chat", Target: conv.ID,
		Meta: map[string]string{"studio": conv.Studio},
	})
}

// handleKickoff 让群里的角色自己聊起来（没有用户消息也能触发）。
func (s *Server) handleKickoff(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	var req struct {
		Topic string `json:"topic"`
	}
	_ = decode(r, &req)

	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}

	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(ev service.Event) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return
		}
		if canFlush {
			flusher.Flush()
		}
	}
	s.orchestrator.Kickoff(r.Context(), conv, u, req.Topic, send)
}

// handleDraftAgent 用模型生成一个角色设定。
func (s *Server) handleDraftAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Keywords string `json:"keywords"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	agent, err := s.drafter.Draft(r.Context(), req.Name, req.Keywords)
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	// Agent.Prompt 在场景列表里不对外，这里单独返回
	ok(w, map[string]string{
		"key":    agent.Key,
		"name":   agent.Name,
		"title":  agent.Title,
		"emoji":  agent.Emoji,
		"color":  agent.Color,
		"prompt": agent.Prompt,
	})
}

// handleScenarios 列出所有内置场景。
func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	ok(w, service.Scenarios)
}
