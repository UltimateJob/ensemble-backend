package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// handleGatewayChat 网关直连接口，协议对齐 OpenAI /v1/chat/completions。
// 客户端可指定 model，网关按模型表与权重选路。
func (s *Server) handleGatewayChat(w http.ResponseWriter, r *http.Request) {
	var req domain.ChatRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if len(req.Messages) == 0 {
		badRequest(w, "messages 不能为空")
		return
	}

	stream, err := s.gw.Stream(r.Context(), req)
	if err != nil {
		fail(w, 503, "no_provider", err.Error())
		return
	}

	if !req.Stream {
		var full string
		var lastErr string
		for c := range stream {
			if c.Err != nil {
				lastErr = c.Err.Error()
				break
			}
			full += c.Content
		}
		if lastErr != "" {
			fail(w, 502, "upstream_error", lastErr)
			return
		}
		ok(w, map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": full}},
			},
		})
		return
	}

	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	for c := range stream {
		if c.Err != nil {
			b, _ := json.Marshal(map[string]string{"error": c.Err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", b)
			if canFlush {
				flusher.Flush()
			}
			break
		}
		if c.Content != "" {
			b, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{{"delta": map[string]string{"content": c.Content}}},
			})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if c.Done {
			fmt.Fprint(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
			}
			break
		}
	}
}
