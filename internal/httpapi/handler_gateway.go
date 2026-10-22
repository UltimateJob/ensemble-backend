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
	// 这个接口之前完全绕过额度：不查剩余、不计数，
	// 拿到 token 的人可以无限免费刷模型。和其它入口一视同仁。
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}

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
		_ = s.store.IncrementUsed(u.ID, 1)
		ok(w, map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": full}},
			},
		})
		return
	}

	// 心跳：等首 token 期间连接可能长时间无字节，
	// 有空闲超时的代理会掐断连接，见 sseWriter。
	sw := startSSE(w)
	defer sw.stop()

	for c := range stream {
		if c.Err != nil {
			b, _ := json.Marshal(map[string]string{"error": c.Err.Error()})
			sw.writeRaw(fmt.Sprintf("data: %s\n\n", b))
			break
		}
		if c.Content != "" {
			b, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{{"delta": map[string]string{"content": c.Content}}},
			})
			// 写失败＝客户端已断开，继续把流读完即可，不需要扣费
			if !sw.writeRaw(fmt.Sprintf("data: %s\n\n", b)) {
				return
			}
		}
		if c.Done {
			sw.writeRaw("data: [DONE]\n\n")
			// 流正常走完才计数；中途出错或断开不扣——
			// 用户没拿到完整回复就不该付费。
			_ = s.store.IncrementUsed(u.ID, 1)
			break
		}
	}
}

// handleUserModels 给普通用户的可选模型列表（建群/换模型的选择器用）。
// 只给 key 和备注，不给成本、上游名这些内部账——那是运维台的事。
func (s *Server) handleUserModels(w http.ResponseWriter, r *http.Request) {
	type userModel struct {
		Key  string `json:"key"`
		Note string `json:"note,omitempty"`
	}
	out := []userModel{}
	for _, m := range s.gw.Models().List() {
		if !m.Enabled {
			continue
		}
		out = append(out, userModel{Key: m.Key, Note: m.Note})
	}
	ok(w, out)
}
