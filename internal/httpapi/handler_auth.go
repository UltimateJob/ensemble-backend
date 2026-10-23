package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

type authReq struct {
	Kind     string `json:"kind"`   // phone | email
	Target   string `json:"target"` // 手机号或邮箱
	Code     string `json:"code"`
	Name     string `json:"name"`
	Mode     string `json:"mode"`     // code（默认）| password
	Password string `json:"password"` // mode=password 时使用
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
	req.Mode = strings.TrimSpace(req.Mode)
	if req.Mode == "" {
		req.Mode = "code"
	}

	if req.Mode == "password" {
		// 密码登录同样发生在鉴权之前，只能按 IP 限流，防暴力猜密码。
		if !s.allow("ip:" + ClientIP(r)) {
			tooManyRequests(w)
			return
		}
		u, token, err := s.auth.LoginWithPassword(req.Kind, req.Target, req.Password)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrPasswordNotSet):
				fail(w, 400, "password_not_set", err.Error())
			case errors.Is(err, service.ErrWrongPassword):
				fail(w, 401, "invalid_credentials", err.Error())
			case errors.Is(err, service.ErrDisabled):
				fail(w, 403, "disabled", err.Error())
			default:
				serverErr(w, err.Error())
			}
			return
		}
		s.appendLoginEvent(r, u, req.Target)
		ok(w, map[string]interface{}{"token": token, "user": u})
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
	s.appendLoginEvent(r, u, req.Target)
	ok(w, map[string]interface{}{"token": token, "user": u})
}

// handleResetPassword 验证码找回密码。公开接口，按 IP 限流。
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.allow("ip:" + ClientIP(r)) {
		tooManyRequests(w)
		return
	}
	var req struct {
		Kind        string `json:"kind"`
		Target      string `json:"target"`
		Code        string `json:"code"`
		NewPassword string `json:"newPassword"`
	}
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

	if err := s.auth.ResetPassword(req.Kind, req.Target, req.Code, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, service.ErrWeakPassword):
			fail(w, 400, "weak_password", err.Error())
		case errors.Is(err, service.ErrInvalidCode):
			fail(w, 400, "invalid_code", err.Error())
		case errors.Is(err, service.ErrAccountNotFound):
			notFound(w, err.Error())
		default:
			serverErr(w, err.Error())
		}
		return
	}
	ok(w, map[string]bool{"reset": true})
}

// appendLoginEvent 记录登录事件；失败不影响登录结果。
func (s *Server) appendLoginEvent(r *http.Request, u *domain.User, target string) {
	_ = s.store.AppendEvent(&domain.Event{
		UserID: u.ID, Kind: "login", Target: target,
		IP: ClientIP(r), UA: r.UserAgent(),
	})
}
