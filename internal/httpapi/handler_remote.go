package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/gateway"
)

// SetRemote 注入线上后端 provider。未配置 REMOTE_BASE 时为 nil，
// 相关路由不注册。
func (s *Server) SetRemote(r *gateway.Remote) { s.remote = r }

// handleRemoteToken 接收前端登录线上后拿到的令牌。
//
// 为什么要走这个端点：桌面端是自带后端的 .app，模型调用由本地后端
// 转发给线上。但令牌是前端在线上登录时拿到的，本地后端不知道。
// 前端登录成功后调这里把令牌交过来，之后本地后端就能替它发请求。
//
// 安全边界：只允许本机调用。远程用户能改本地后端的令牌等于能冒充他，
// 而这个端点本身不做鉴权（它注入的就是鉴权材料）。
func (s *Server) handleRemoteToken(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		forbidden(w)
		return
	}
	if s.remote == nil {
		notFound(w, "未配置线上后端")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	token := strings.TrimSpace(req.Token)
	// 空令牌表示登出：清掉，后续模型调用会明确失败而不是偷偷用旧令牌。
	s.remote.SetToken(token)
	ok(w, map[string]any{"set": token != "", "provider": s.remote.Name()})
}

// handleRemoteStatus 给前端查「本地后端连的是哪个线上环境、有没有令牌」。
func (s *Server) handleRemoteStatus(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		ok(w, map[string]any{"enabled": false})
		return
	}
	ok(w, map[string]any{
		"enabled":  true,
		"provider": s.remote.Name(),
		"loggedIn": s.remote.Token() != "",
	})
}

/* ------------------------------------------------------------
   鉴权代理：账号体系在线上，本地只做镜像
   ------------------------------------------------------------ */

// remoteAuth 转发 /v1/auth/* 到线上。返回线上的 data 与 HTTP 状态。
//
// 不直接让前端跨域打线上：桌面端的页面由本地后端提供，跨域要处理
// CORS 与 cookie；转发一次就能保持同源，也顺便把错误文案统一掉。
func (s *Server) remoteAuth(ctx context.Context, path string, body any) (map[string]any, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	url := s.remote.Base() + "/v1/auth/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var out struct {
		OK    bool           `json:"ok"`
		Data  map[string]any `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("线上返回了无法解析的内容")
	}
	if !out.OK {
		msg := out.Error.Message
		if msg == "" {
			msg = "线上服务暂时不可用"
		}
		return nil, resp.StatusCode, errors.New(msg)
	}
	return out.Data, resp.StatusCode, nil
}

// handleSendCodeProxy 发验证码：配了线上就走线上。
func (s *Server) handleSendCodeProxy(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		s.handleSendCode(w, r)
		return
	}
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
	if strings.TrimSpace(req.Target) == "" {
		badRequest(w, "请填写手机号或邮箱")
		return
	}
	data, status, err := s.remoteAuth(r.Context(), "send-code", req)
	if err != nil {
		fail(w, status, "remote_unavailable", err.Error())
		return
	}
	ok(w, data)
}

// handleLoginProxy 登录：线上验证码校验通过后，本地签发一份镜像会话。
//
// 为什么要两份令牌：线上令牌用于调模型（由前端交给 /v1/remote/token），
// 本地令牌用于本地业务（工作区、执行记录）。职责分开，谁也不越界。
func (s *Server) handleLoginProxy(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		s.handleLogin(w, r)
		return
	}
	var req authReq
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Target = strings.TrimSpace(req.Target)
	if req.Target == "" {
		badRequest(w, "请填写手机号或邮箱")
		return
	}

	body := map[string]any{
		"kind": req.Kind, "target": req.Target, "name": req.Name,
	}
	switch req.Mode {
	case "password":
		body["mode"] = "password"
		body["password"] = req.Password
	default:
		body["code"] = req.Code
	}
	data, status, err := s.remoteAuth(r.Context(), "login", body)
	if err != nil {
		fail(w, status, "remote_auth", err.Error())
		return
	}

	// 线上登录成功：本地建一份镜像用户并签发本地会话。
	kind := req.Kind
	if kind == "" {
		kind = "phone"
	}
	u, token, err := s.auth.LoginTrusted(kind, req.Target, req.Name)
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	ok(w, map[string]any{
		"user":  u,
		"token": token,
		// 线上令牌交给前端：模型调用要用它，本地后端不保存
		"remoteToken": data["token"],
	})
}
