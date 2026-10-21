package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

func (s *Server) handleListConvs(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	ok(w, s.chat.List(u.ID))
}

func (s *Server) handleCreateConv(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	var req struct {
		Title   string             `json:"title"`
		Studio  string             `json:"studio"`
		Model   string             `json:"model"`
		Members []string           `json:"members"`
		Agents  []domain.ConvAgent `json:"agents"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	conv, err := s.chat.Create(u.ID, req.Title, req.Studio, req.Model, req.Members, req.Agents)
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	created(w, conv)
}

func (s *Server) handleGetConv(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	ok(w, conv)
}

func (s *Server) handleDeleteConv(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	if _, err := s.chat.Get(u.ID, r.PathValue("id")); err != nil {
		notFound(w, "会话不存在")
		return
	}
	_ = s.store.DeleteConv(r.PathValue("id"))
	_ = s.store.AppendEvent(&domain.Event{UserID: u.ID, Kind: "conv_delete", Target: r.PathValue("id")})
	ok(w, map[string]string{"deleted": "true"})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	msgs, err := s.chat.Messages(u.ID, r.PathValue("id"), queryInt(r, "limit", 100))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	ok(w, msgs)
}

// handleSendMessage 建立 SSE 流，把模型输出逐段推给客户端。
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	u, okUser := UserFrom(r.Context())
	if !okUser {
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

	convID := r.PathValue("id")
	stream, conv, err := s.chat.Send(r.Context(), u, convID, req.Content)
	if err != nil {
		switch err {
		case service.ErrQuotaExceeded:
			fail(w, 402, "quota_exceeded", err.Error())
		case service.ErrConvNotFound:
			notFound(w, "会话不存在")
		case service.ErrForbidden:
			forbidden(w)
		default:
			serverErr(w, err.Error())
		}
		return
	}

	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	start := time.Now()
	var full string
	var tokens int

	send := func(payload interface{}) bool {
		b, err := json.Marshal(payload)
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

	for chunk := range stream {
		if chunk.Err != nil {
			send(map[string]interface{}{"error": chunk.Err.Error(), "done": true})
			break
		}
		if chunk.Content != "" {
			full += chunk.Content
			if !send(map[string]interface{}{"delta": chunk.Content}) {
				return
			}
		}
		if chunk.Usage != nil {
			tokens = chunk.Usage.CompletionTokens
		}
		if chunk.Done {
			break
		}
	}

	latency := int(time.Since(start).Milliseconds())
	s.chat.SaveAssistant(convID, conv.Model, full, tokens, latency)
	_ = s.store.AppendEvent(&domain.Event{
		UserID: u.ID, Kind: "chat", Target: convID,
		Meta: map[string]string{"tokens": fmt.Sprint(tokens), "ms": fmt.Sprint(latency)},
	})

	send(map[string]interface{}{
		"done":    true,
		"tokens":  tokens,
		"latency": latency,
	})
}
