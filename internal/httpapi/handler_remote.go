package httpapi

import (
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
	})
}
