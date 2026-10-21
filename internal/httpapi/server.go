package httpapi

import (
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
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
	mem          *service.MemoryService
	agents       *agent.Manager
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
	mem ...*service.MemoryService,
) *Server {
	s := &Server{cfg: cfg, store: st, auth: auth, chat: chat, admin: admin, gw: gw, orchestrator: orch, drafter: drafter}
	if len(mem) > 0 {
		s.mem = mem[0]
	}
	return s
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
		ready := len(m.Providers) > 0
		ok(w, map[string]interface{}{"ready": ready, "providers": len(m.Providers)})
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
	userMux.HandleFunc("POST /v1/agents/polish", s.handlePolishAgent)
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
	userMux.HandleFunc("POST /v1/conversations/{id}/leave", s.handleLeave)
	userMux.HandleFunc("DELETE /v1/conversations/{id}", s.handleDeleteConv)
	userMux.HandleFunc("GET /v1/conversations/{id}/messages", s.handleListMessages)
	userMux.HandleFunc("POST /v1/conversations/{id}/messages", s.handleSendMessage)
	userMux.HandleFunc("POST /v1/conversations/{id}/orchestrate", s.handleOrchestrate)
	userMux.HandleFunc("POST /v1/conversations/{id}/kickoff", s.handleKickoff)

	userMux.HandleFunc("GET /v1/memories", s.handleMemorySnapshot)
	userMux.HandleFunc("DELETE /v1/memories", s.handleClearMemory)

	// 工作层：工作区 / 任务 / 审批 / 回滚（需要注入 agent.Manager）
	if s.agents != nil {
		userMux.HandleFunc("GET /v1/workspaces", s.handleListWorkspaces)
		userMux.HandleFunc("POST /v1/workspaces", s.handleBindWorkspace)
		userMux.HandleFunc("DELETE /v1/workspaces/{id}", s.handleUnbindWorkspace)
		userMux.HandleFunc("GET /v1/workspaces/{id}/tree", s.handleWorkspaceTree)
		userMux.HandleFunc("GET /v1/workspaces/{id}/file", s.handleWorkspaceFile)
		userMux.HandleFunc("POST /v1/workspaces/{id}/runs", s.handleStartRun)
		userMux.HandleFunc("GET /v1/runs/{id}", s.handleGetRun)
		userMux.HandleFunc("GET /v1/runs/{id}/events", s.handleRunEvents)
		userMux.HandleFunc("POST /v1/runs/{id}/approve", s.handleApprove)
		userMux.HandleFunc("POST /v1/runs/{id}/abort", s.handleAbort)
		userMux.HandleFunc("GET /v1/runs/{id}/diff", s.handleRunDiff)
		userMux.HandleFunc("POST /v1/runs/{id}/revert", s.handleRevert)
	}

	authMW := Auth(s.auth.Verify)
	for _, p := range []string{
		"/v1/me", "/v1/me/", "/v1/search", "/v1/scenarios", "/v1/characters", "/v1/characters/", "/v1/events", "/v1/agents/draft", "/v1/agents", "/v1/agents/", "/v1/studios", "/v1/studios/",
		"/v1/conversations", "/v1/conversations/", "/v1/memories",
		"/v1/workspaces", "/v1/workspaces/", "/v1/runs", "/v1/runs/",
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
	mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir("web/admin"))))

	// ---- 客户端 ----
	app := http.FileServer(http.Dir("web/app"))
	mux.Handle("GET /app/", http.StripPrefix("/app/", app))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	return Chain(mux,
		Recover,
		Trace,
		CORS,
		Access,
	)
}
