package httpapi

import (
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/service"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// Server 聚合依赖并注册路由。
type Server struct {
	cfg   config.Config
	store store.Store
	auth  *service.Auth
	chat  *service.Chat
	admin *service.Admin
	gw    *gateway.Gateway
}

func NewServer(
	cfg config.Config,
	st store.Store,
	auth *service.Auth,
	chat *service.Chat,
	admin *service.Admin,
	gw *gateway.Gateway,
) *Server {
	return &Server{cfg: cfg, store: st, auth: auth, chat: chat, admin: admin, gw: gw}
}

// Handler 构建完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// ---- 健康检查 ----
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		m := s.gw.Metrics()
		ready := len(m.Providers) > 0
		ok(w, map[string]interface{}{"ready": ready, "providers": len(m.Providers)})
	})

	// ---- 鉴权 ----
	mux.HandleFunc("POST /v1/auth/send-code", s.handleSendCode)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)

	// ---- 需要登录 ----
	userMux := http.NewServeMux()
	userMux.HandleFunc("GET /v1/me", s.handleMe)
	userMux.HandleFunc("PATCH /v1/me", s.handleUpdateMe)
	userMux.HandleFunc("GET /v1/me/events", s.handleMyEvents)
	userMux.HandleFunc("POST /v1/events", s.handleTrack)

	userMux.HandleFunc("GET /v1/conversations", s.handleListConvs)
	userMux.HandleFunc("POST /v1/conversations", s.handleCreateConv)
	userMux.HandleFunc("GET /v1/conversations/{id}", s.handleGetConv)
	userMux.HandleFunc("DELETE /v1/conversations/{id}", s.handleDeleteConv)
	userMux.HandleFunc("GET /v1/conversations/{id}/messages", s.handleListMessages)
	userMux.HandleFunc("POST /v1/conversations/{id}/messages", s.handleSendMessage)

	authMW := Auth(s.auth.Verify)
	for _, p := range []string{
		"/v1/me", "/v1/me/", "/v1/events",
		"/v1/conversations", "/v1/conversations/",
	} {
		mux.Handle(p, authMW(userMux))
	}

	// ---- 网关直连（客户端自带 Key 场景可换成 API Key 鉴权） ----
	mux.Handle("POST /v1/chat/completions", authMW(http.HandlerFunc(s.handleGatewayChat)))

	// ---- 管理接口 ----
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("POST /admin/api/login", s.handleAdminLogin)
	adminMux.HandleFunc("GET /admin/api/overview", s.handleAdminOverview)
	adminMux.HandleFunc("GET /admin/api/models", s.handleAdminModels)
	adminMux.HandleFunc("POST /admin/api/models", s.handleAdminModels)
	adminMux.HandleFunc("DELETE /admin/api/models/{key}", s.handleAdminModelDelete)
	adminMux.HandleFunc("GET /admin/api/users", s.handleAdminUsers)
	adminMux.HandleFunc("POST /admin/api/users/{id}/status", s.handleAdminUserStatus)
	adminMux.HandleFunc("POST /admin/api/users/{id}/quota", s.handleAdminUserQuota)
	adminMux.HandleFunc("POST /admin/api/providers/{name}/reset", s.handleAdminResetProvider)
	adminMux.HandleFunc("GET /admin/api/logs", s.handleAdminLogs)
	adminMux.HandleFunc("GET /admin/api/events", s.handleAdminEvents)

	// 登录接口免 token，其余需要
	mux.Handle("/admin/api/login", adminMux)
	mux.Handle("/admin/api/", s.adminAuth(adminMux))

	// ---- 管理页面静态资源 ----
	mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir("web/admin"))))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})

	return Chain(mux,
		Recover,
		Trace,
		CORS,
		Access,
	)
}
