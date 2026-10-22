package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
	"github.com/UltimateJob/ensemble-backend/internal/service"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// Server 聚合依赖并注册路由。
type Server struct {
	cfg          config.Config
	store        store.Store
	auth         *service.Auth
	chat         *service.Chat
	admin        *service.Admin
	gw           *gateway.Gateway
	orchestrator *service.Orchestrator
	drafter      *service.Drafter

	// limiter 每用户/每 IP 令牌桶。main 里创建后注入，中间件与 handler 共用。
	limiter *gateway.Limiter

	// 在线时间节流：每用户 60 秒内只 Touch 一次，见 touchLastSeen。
	seenMu sync.Mutex
	seenAt map[string]time.Time
}

func NewServer(
	cfg config.Config,
	st store.Store,
	auth *service.Auth,
	chat *service.Chat,
	admin *service.Admin,
	gw *gateway.Gateway,
	orch *service.Orchestrator,
	drafter *service.Drafter,
	limiter *gateway.Limiter,
) *Server {
	return &Server{
		cfg: cfg, store: st, auth: auth, chat: chat, admin: admin,
		gw: gw, orchestrator: orch, drafter: drafter,
		limiter: limiter,
		seenAt:  make(map[string]time.Time),
	}
}

// Handler 构建完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// ---- 健康检查 ----
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"status": "ok"})
	})
	// Prometheus 抓取习惯用 /metrics。要求 admin 令牌，
	// 免得把用户量和业务指标公开出去。
	mux.HandleFunc("GET /metrics", s.handlePublicMetrics)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		m := s.gw.Metrics()
		// 只数 provider 个数不够：注册了但全部熔断（open）时，
		// 网关一样路由不了任何请求，这时不该被当成 ready。
		// closed / half-open 都还能接流量。
		available := 0
		for _, p := range m.Providers {
			if p.State != "open" {
				available++
			}
		}
		ready := available > 0

		// 落盘状态也决定「能不能对外服务」。
		// 写不进磁盘时接口照样返回 200，但数据只在内存里，重启就没了——
		// 这种状态不该被当成 ready。
		persist := map[string]interface{}{"ok": true}
		if ph, has := s.store.(interface{ PersistHealth() store.PersistHealth }); has {
			h := ph.PersistHealth()
			persist = map[string]interface{}{
				"ok": h.OK, "failures": h.Failures,
				"lastError": h.LastError, "lastSuccessAt": h.LastSuccessAt,
			}
			if !h.OK {
				ready = false
			}
		}
		ok(w, map[string]interface{}{
			"ready": ready, "providers": len(m.Providers), "providersReady": available, "persist": persist,
		})
	})

	// ---- 鉴权 ----
	mux.HandleFunc("POST /v1/auth/send-code", s.handleSendCode)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)

	// ---- 需要登录 ----
	userMux := http.NewServeMux()
	userMux.HandleFunc("GET /v1/scenarios", s.handleScenariosAll)
	userMux.HandleFunc("GET /v1/search", s.handleSearch)
	userMux.HandleFunc("GET /v1/characters", s.handleSearchCharacters)
	userMux.HandleFunc("POST /v1/characters/to-agent", s.handleCharacterToAgent)
	userMux.HandleFunc("GET /v1/me", s.handleMe)
	userMux.HandleFunc("PATCH /v1/me", s.handleUpdateMe)
	userMux.HandleFunc("GET /v1/me/events", s.handleMyEvents)
	userMux.HandleFunc("POST /v1/events", s.handleTrack)
	userMux.HandleFunc("POST /v1/agents/draft", s.handleDraftAgent)
	userMux.HandleFunc("GET /v1/agents", s.handleListAgents)
	userMux.HandleFunc("POST /v1/agents", s.handleCreateAgent)
	userMux.HandleFunc("PATCH /v1/agents/{id}", s.handleUpdateAgent)
	userMux.HandleFunc("DELETE /v1/agents/{id}", s.handleDeleteAgent)
	userMux.HandleFunc("POST /v1/agents/{id}/to/{convId}", s.handleAgentToConversation)
	userMux.HandleFunc("GET /v1/studios", s.handleListStudios)
	userMux.HandleFunc("POST /v1/studios", s.handleCreateStudio)
	userMux.HandleFunc("DELETE /v1/studios/{id}", s.handleDeleteStudio)

	userMux.HandleFunc("GET /v1/conversations", s.handleListConvs)
	userMux.HandleFunc("POST /v1/conversations", s.handleCreateConv)
	userMux.HandleFunc("GET /v1/conversations/{id}", s.handleGetConv)
	userMux.HandleFunc("PATCH /v1/conversations/{id}", s.handleUpdateConv)
	userMux.HandleFunc("GET /v1/conversations/{id}/collaborators", s.handleListCollaborators)
	userMux.HandleFunc("POST /v1/conversations/{id}/collaborators", s.handleAddCollaborator)
	userMux.HandleFunc("DELETE /v1/conversations/{id}/collaborators/{uid}", s.handleRemoveCollaborator)
	userMux.HandleFunc("DELETE /v1/conversations/{id}/agents/{key}", s.handleRemoveConvAgent)
	userMux.HandleFunc("POST /v1/conversations/{id}/leave", s.handleLeave)
	userMux.HandleFunc("DELETE /v1/conversations/{id}", s.handleDeleteConv)
	userMux.HandleFunc("GET /v1/conversations/{id}/messages", s.handleListMessages)
	userMux.HandleFunc("POST /v1/conversations/{id}/messages", s.handleSendMessage)
	userMux.HandleFunc("POST /v1/conversations/{id}/orchestrate", s.handleOrchestrate)
	userMux.HandleFunc("POST /v1/conversations/{id}/kickoff", s.handleKickoff)

	authMW := s.authMiddleware()
	for _, p := range []string{
		"/v1/me", "/v1/me/", "/v1/search", "/v1/scenarios", "/v1/characters", "/v1/characters/", "/v1/events", "/v1/agents/draft", "/v1/agents", "/v1/agents/", "/v1/studios", "/v1/studios/",
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
	adminMux.HandleFunc("GET /admin/api/slow", s.handleAdminSlow)
	adminMux.HandleFunc("POST /admin/api/users/{id}/reset-usage", s.handleAdminResetUsage)
	adminMux.HandleFunc("GET /admin/api/conversations", s.handleAdminConversations)
	adminMux.HandleFunc("POST /admin/api/conversations/{id}/unlock", s.handleAdminUnlock)
	adminMux.HandleFunc("GET /admin/api/events", s.handleAdminEvents)
	adminMux.HandleFunc("GET /admin/api/metrics", s.handleMetrics)

	// 登录接口免 token，其余需要
	mux.Handle("/admin/api/login", adminMux)
	mux.Handle("/admin/api/", s.adminAuth(adminMux))

	// ---- 管理页面静态资源 ----
	// 静态目录不能只按 CWD 解析：用 systemd / launchd 部署时
	// 工作目录不一定是仓库根，页面会整页 404 还查不出原因。
	mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(webDir("admin"))))

	// ---- 客户端 ----
	app := http.FileServer(webDir("app"))
	mux.Handle("GET /app/", http.StripPrefix("/app/", app))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	h := Chain(mux,
		Recover,
		Trace,
		CORS,
		Access,
	)

	// 限制请求体大小，放在最外层——任何路径都不能靠一个大 body 撑爆内存。
	// 2MB：消息上限 4000 字约 12KB，留足余量。
	// 实测没有这个限制时，单个 100MB 请求会让内存从 80MB 涨到 331MB。
	return LimitBody(h, 2<<20)
}

// webDir 定位 web/ 下的静态资源子目录。
// 先看可执行文件旁边（部署成系统服务时 CWD 不可控，二进制和 web/ 一起拷贝），
// 再看当前工作目录（go run / 本地开发）。两处都没有时打一条显眼的 Error：
// 不拒启动（API 还能用），但页面全 404 的原因必须能从日志里一眼看到。
func webDir(sub string) http.Dir {
	var tried []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "web", sub)
		tried = append(tried, dir)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return http.Dir(dir)
		}
	}
	dir := filepath.Join("web", sub)
	tried = append(tried, dir)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return http.Dir(dir)
	}
	logx.L().Error("静态资源目录未找到，页面将全部 404（接口不受影响）", "tried", tried)
	return http.Dir(dir)
}
