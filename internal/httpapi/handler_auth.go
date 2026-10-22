package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

type authReq struct {
	Kind   string `json:"kind"`   // phone | email
	Target string `json:"target"` // 手机号或邮箱
	Code   string `json:"code"`
	Name   string `json:"name"`
}

func (s *Server) handleSendCode(w http.ResponseWriter, r *http.Request) {
	// 发验证码发生在登录之前，没有 userID 可用，只能按 IP 限。
	// 不限的话，一个脚本就能借这个接口刷短信（真通道按条计费）。
	if !s.allow("ip:" + ClientIP(r)) {
		tooManyRequests(w)
		return
	}
	var req struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Target = strings.TrimSpace(req.Target)
	if req.Target == "" {
		badRequest(w, "请填写手机号或邮箱")
		return
	}
	code, err := s.auth.SendCode(r.Context(), req.Target)
	if err != nil {
		if errors.Is(err, service.ErrCodeTooFrequent) {
			// 同一目标 60 秒内的重复发码是客户端问题，不是服务端故障
			tooManyRequests(w)
			return
		}
		serverErr(w, err.Error())
		return
	}
	resp := map[string]string{"sent": "true", "channel": s.cfg.SmsProvider}
	// 只有内置 log 通道才把验证码回给前端
	if code != "" {
		resp["devCode"] = code
	}
	ok(w, resp)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authReq
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	if req.Kind == "" {
		req.Kind = "phone"
	}
	req.Target = strings.TrimSpace(req.Target)
	if req.Target == "" {
		badRequest(w, "请填写手机号或邮箱")
		return
	}

	u, token, err := s.auth.Login(req.Kind, req.Target, req.Code, req.Name)
	if err != nil {
		switch err {
		case service.ErrInvalidCode:
			fail(w, 400, "invalid_code", err.Error())
		case service.ErrDisabled:
			fail(w, 403, "disabled", err.Error())
		default:
			serverErr(w, err.Error())
		}
		return
	}
	_ = s.store.AppendEvent(&domain.Event{
		UserID: u.ID, Kind: "login", Target: req.Target,
		IP: ClientIP(r), UA: r.UserAgent(),
	})
	ok(w, map[string]interface{}{"token": token, "user": u})
}
