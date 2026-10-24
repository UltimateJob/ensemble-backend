package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
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

	mem    *service.MemoryService
	agents *agent.Manager
	// remote 线上后端 provider。nil 表示本地直连上游。
	remote *gateway.Remote
	// teams 团队任务编排器。未注入时团队接口不注册。
	teams *agent.TeamManager

	// 手机端相关。devices 为 nil 时远程路由不注册（单机模式不受影响）。
	devices  *service.Devices
	goals    *service.Goals
	conns    *service.Connectors
	library  *service.Library
	activity *service.Activity
	official *service.Official

	// upstreams 可编辑的上游渠道表。为 nil 时渠道接口不注册。
	upstreams *store.Upstreams
	// beforeProviderDelete 删除渠道前的业务校验（main 注入：模型表引用检查）。
	beforeProviderDelete func(name string) string
	// quotas 渠道额度查询（余额 / 5h / 7d 窗口）。
	quotas *gateway.QuotaService

	// 套餐级限流/并发。按套餐 ID 惰性创建令牌桶；
	// 并发计数按用户 ID 累计，进入 +1、离开 -1。
	limitMu      sync.Mutex
	planLimiters map[string]*gateway.Limiter
	userInflight map[string]int
}

// SetQuotas 注入渠道额度查询服务。须在开始接请求前调用。
func (s *Server) SetQuotas(q *gateway.QuotaService) { s.quotas = q }

func NewServer(
	cfg config.Config,
	st store.Store,
	auth *service.Auth,
	chat *service.Chat,
	admin *service.Admin,
	gw *gateway.Gateway,
	orch *service.Orchestrator,
	drafter *service.Drafter,
	mem *service.MemoryService,
	limiter *gateway.Limiter,
) *Server {
	return &Server{
		cfg: cfg, store: st, auth: auth, chat: chat, admin: admin,
		gw: gw, orchestrator: orch, drafter: drafter,
		limiter: limiter,
		seenAt:  make(map[string]time.Time),
		mem:     mem,

		planLimiters: make(map[string]*gateway.Limiter),
		userInflight: make(map[string]int),
	}
}

// SetUpstreams 注入可编辑的上游渠道表。须在开始接请求前调用。
func (s *Server) SetUpstreams(u *store.Upstreams) { s.upstreams = u }

// SetProviderDeleteGuard 注入删除渠道前的引用检查。
func (s *Server) SetProviderDeleteGuard(fn func(name string) string) { s.beforeProviderDelete = fn }

// SetTeamManager 注入团队任务编排器。须在开始接请求前调用。
func (s *Server) SetTeamManager(t *agent.TeamManager) { s.teams = t }

// SetRemoteServices 注入手机端所需的服务。
//
// 用 setter 而不是加构造参数：NewServer 已经是 11 个参数了，
// 再往里塞会波及所有测试的调用点。这些服务只有远程场景才需要。
func (s *Server) SetRemoteServices(
	devices *service.Devices,
	goals *service.Goals,
	conns *service.Connectors,
	library *service.Library,
	activity *service.Activity,
	official *service.Official,
) {
	s.devices = devices
	s.goals = goals
	s.conns = conns
	s.library = library
	s.activity = activity
	s.official = official
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

	// ---- 桌面端：线上令牌注入 ----
	// 这两个端点必须在鉴权之外：它们处理的就是「还没登录」的状态。
	// handleRemoteToken 自己校验来源必须是本机。
	if s.remote != nil {
		mux.HandleFunc("POST /v1/remote/token", s.handleRemoteToken)
		mux.HandleFunc("GET /v1/remote/status", s.handleRemoteStatus)
	}

	// ---- 鉴权 ----
	// 配了线上后端时，登录走线上鉴权、本地只做镜像；否则本地自建账号。
	mux.HandleFunc("POST /v1/auth/send-code", s.handleSendCodeProxy)
	mux.HandleFunc("POST /v1/auth/login", s.handleLoginProxy)
	mux.HandleFunc("POST /v1/auth/reset-password", s.handleResetPassword)

	// ---- 需要登录 ----
	userMux := http.NewServeMux()
	userMux.HandleFunc("GET /v1/scenarios", s.handleScenariosAll)
	userMux.HandleFunc("GET /v1/models", s.handleUserModels)
	userMux.HandleFunc("GET /v1/search", s.handleSearch)
	userMux.HandleFunc("GET /v1/characters", s.handleSearchCharacters)
	userMux.HandleFunc("POST /v1/characters/to-agent", s.handleCharacterToAgent)
	userMux.HandleFunc("GET /v1/me", s.handleMe)
	userMux.HandleFunc("PATCH /v1/me", s.handleUpdateMe)
	userMux.HandleFunc("POST /v1/me/password", s.handleSetPassword)
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
	userMux.HandleFunc("DELETE /v1/conversations/{id}/agents/{key}", s.handleRemoveConvAgent)
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
		userMux.HandleFunc("POST /v1/workspaces/{id}/collab", s.handleCollab)
		userMux.HandleFunc("GET /v1/runs/{id}", s.handleGetRun)
		userMux.HandleFunc("GET /v1/runs/{id}/events", s.handleRunEvents)
		userMux.HandleFunc("POST /v1/runs/{id}/approve", s.handleApprove)
		userMux.HandleFunc("POST /v1/runs/{id}/abort", s.handleAbort)
		userMux.HandleFunc("GET /v1/runs/{id}/diff", s.handleRunDiff)
		userMux.HandleFunc("POST /v1/runs/{id}/revert", s.handleRevert)

		// 会话级工作区：代码工作室绑定本地目录、@ 派活、团队任务。
		userMux.HandleFunc("PUT /v1/conversations/{id}/workspace", s.handleBindConvWorkspace)
		userMux.HandleFunc("DELETE /v1/conversations/{id}/workspace", s.handleUnbindConvWorkspace)
		userMux.HandleFunc("POST /v1/conversations/{id}/execute", s.handleExecute)
		userMux.HandleFunc("GET /v1/conversations/{id}/runs", s.handleConvRuns)
		userMux.HandleFunc("GET /v1/workspaces/{id}/raw", s.handleWorkspaceRaw)
		userMux.HandleFunc("GET /v1/workspaces/{id}/runs", s.handleWorkspaceRuns)
	}
	if s.teams != nil {
		userMux.HandleFunc("POST /v1/conversations/{id}/team", s.handleStartTeam)
		userMux.HandleFunc("GET /v1/conversations/{id}/teams", s.handleListTeams)
		userMux.HandleFunc("GET /v1/teams/{id}", s.handleGetTeam)
		userMux.HandleFunc("GET /v1/teams/{id}/events", s.handleTeamEvents)
		userMux.HandleFunc("POST /v1/teams/{id}/cancel", s.handleCancelTeam)
	}

	// ---- 手机端：目标 / 连接器 / 资料库 / 状态环 ----
	if s.goals != nil {
		userMux.HandleFunc("GET /v1/goals", s.handleListGoals)
		userMux.HandleFunc("POST /v1/goals", s.handleCreateGoal)
		userMux.HandleFunc("PATCH /v1/goals/{id}", s.handleUpdateGoal)
		userMux.HandleFunc("DELETE /v1/goals/{id}", s.handleDeleteGoal)
	}
	if s.conns != nil {
		userMux.HandleFunc("GET /v1/connectors", s.handleListConnectors)
		userMux.HandleFunc("POST /v1/connectors", s.handleCreateConnector)
		userMux.HandleFunc("PATCH /v1/connectors/{id}", s.handleUpdateConnector)
		userMux.HandleFunc("DELETE /v1/connectors/{id}", s.handleDeleteConnector)
		userMux.HandleFunc("POST /v1/connectors/{id}/probe", s.handleProbeConnector)
	}
	if s.library != nil {
		userMux.HandleFunc("GET /v1/library", s.handleListArtifacts)
		userMux.HandleFunc("GET /v1/library/{id}", s.handleGetArtifact)
		userMux.HandleFunc("DELETE /v1/library/{id}", s.handleDeleteArtifact)
	}
	if s.official != nil {
		userMux.HandleFunc("GET /v1/official", s.handleOfficial)
	}
	if s.devices != nil {
		userMux.HandleFunc("POST /v1/devices/pair-code", s.handleIssuePairCode)
		userMux.HandleFunc("GET /v1/devices", s.handleListDevices)
		userMux.HandleFunc("DELETE /v1/devices/{id}", s.handleRevokeDevice)
		userMux.HandleFunc("PATCH /v1/devices/{id}", s.handleDeviceApproval)
		userMux.HandleFunc("GET /v1/conversations/{id}/activity", s.handleActivity)
	}

	// 配对接口免登录：手机首次配对时还没有任何凭证
	mux.HandleFunc("POST /v1/devices/pair", s.handlePair)

	// 钱包与兑换码：用户侧只读余额/流水，兑换码由管理员生成。
	userMux.HandleFunc("GET /v1/wallet", s.handleMyWallet)
	userMux.HandleFunc("POST /v1/redeem", s.handleRedeemCode)

	// authMiddleware 在基础鉴权之上叠了限流与在线时间节流
	authMW := s.authMiddleware()
	for _, p := range []string{
		"/v1/me", "/v1/me/", "/v1/search", "/v1/scenarios", "/v1/models", "/v1/characters", "/v1/characters/", "/v1/events", "/v1/agents/draft", "/v1/agents", "/v1/agents/", "/v1/studios", "/v1/studios/",
		"/v1/conversations", "/v1/conversations/", "/v1/memories",
		"/v1/workspaces", "/v1/workspaces/", "/v1/runs", "/v1/runs/", "/v1/teams", "/v1/teams/",
		"/v1/goals", "/v1/goals/", "/v1/connectors", "/v1/connectors/",
		"/v1/library", "/v1/library/", "/v1/devices", "/v1/devices/", "/v1/official",
		"/v1/wallet", "/v1/redeem",
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
	adminMux.HandleFunc("GET /admin/api/users/{id}", s.handleAdminUserDetail)
	adminMux.HandleFunc("POST /admin/api/users/{id}/plan", s.handleAdminUserPlan)
	adminMux.HandleFunc("POST /admin/api/users/{id}/status", s.handleAdminUserStatus)
	adminMux.HandleFunc("POST /admin/api/users/{id}/quota", s.handleAdminUserQuota)
	adminMux.HandleFunc("POST /admin/api/providers/{name}/reset", s.handleAdminResetProvider)
	adminMux.HandleFunc("POST /admin/api/providers/{name}/check", s.handleAdminProviderCheck)
	adminMux.HandleFunc("GET /admin/api/logs", s.handleAdminLogs)
	adminMux.HandleFunc("GET /admin/api/slow", s.handleAdminSlow)
	adminMux.HandleFunc("POST /admin/api/users/{id}/reset-usage", s.handleAdminResetUsage)
	adminMux.HandleFunc("GET /admin/api/conversations", s.handleAdminConversations)
	adminMux.HandleFunc("POST /admin/api/conversations/{id}/unlock", s.handleAdminUnlock)
	adminMux.HandleFunc("GET /admin/api/events", s.handleAdminEvents)
	adminMux.HandleFunc("GET /admin/api/metrics", s.handleMetrics)
	adminMux.HandleFunc("GET /admin/api/series", s.handleAdminSeries)
	adminMux.HandleFunc("GET /admin/api/system", s.handleAdminSystem)

	// 商业化：套餐、成本、钱包、兑换码、运营设置、风控。
	adminMux.HandleFunc("GET /admin/api/plans", s.handleAdminPlans)
	adminMux.HandleFunc("POST /admin/api/plans", s.handleAdminPlans)
	adminMux.HandleFunc("DELETE /admin/api/plans/{id}", s.handleAdminPlanDelete)
	adminMux.HandleFunc("GET /admin/api/commerce", s.handleAdminCommerce)
	adminMux.HandleFunc("GET /admin/api/ops", s.handleAdminOps)
	adminMux.HandleFunc("POST /admin/api/ops", s.handleAdminOpsSave)
	adminMux.HandleFunc("GET /admin/api/risk", s.handleAdminRisk)
	adminMux.HandleFunc("GET /admin/api/wallet", s.handleAdminWallet)
	adminMux.HandleFunc("GET /admin/api/wallet/{id}", s.handleAdminWalletUser)
	adminMux.HandleFunc("POST /admin/api/wallet/{id}/topup", s.handleAdminWalletTopUp)
	adminMux.HandleFunc("POST /admin/api/wallet/{id}/adjust", s.handleAdminWalletAdjust)
	adminMux.HandleFunc("GET /admin/api/redeem-codes", s.handleAdminRedeemCodes)
	adminMux.HandleFunc("POST /admin/api/redeem-codes", s.handleAdminRedeemCodeCreate)
	adminMux.HandleFunc("POST /admin/api/redeem-codes/{code}/disable", s.handleAdminRedeemCodeDisable)
	adminMux.HandleFunc("DELETE /admin/api/redeem-codes/{code}", s.handleAdminRedeemCodeDelete)
	adminMux.HandleFunc("GET /admin/api/orders", s.handleAdminOrders)

	// 上游渠道与渠道额度。
	adminMux.HandleFunc("GET /admin/api/upstreams", s.handleAdminUpstreams)
	adminMux.HandleFunc("POST /admin/api/upstreams", s.handleAdminUpstreams)
	adminMux.HandleFunc("DELETE /admin/api/upstreams/{name}", s.handleAdminUpstreamDelete)
	adminMux.HandleFunc("GET /admin/api/quotas", s.handleAdminQuotas)
	adminMux.HandleFunc("POST /admin/api/quota-config", s.handleAdminQuotaConfig)
	adminMux.HandleFunc("DELETE /admin/api/quota-config/{name}", s.handleAdminQuotaConfigDelete)

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

	// ---- 手机端 Web 版（Expo 导出产物） ----
	// 有 web/m 目录时挂在 /m/。同源托管的意义：手机浏览器直接打开就能用，
	// 不需要额外的静态服务器，也不会有 CORS 与深链 404 的问题。
	// 没有构建产物时这一条不注册，不影响现有部署。
	if mDir := mobileWebDir(); mDir != "" {
		// SPA fallback：/m/goals、/m/artifact/xxx 这类客户端路由没有物理文件，
		// 直接交给 FileServer 会 404。这里先看文件是否存在，不存在就返回
		// index.html，让前端 router 接管。
		stripped := http.StripPrefix("/m/", http.FileServer(http.Dir(mDir)))
		mux.HandleFunc("GET /m/", func(w http.ResponseWriter, r *http.Request) {
			rel := strings.TrimPrefix(r.URL.Path, "/m/")
			if rel != "" {
				full := filepath.Join(mDir, filepath.Clean("/"+rel))
				if st, err := os.Stat(full); err == nil && !st.IsDir() {
					// 必须用 StripPrefix 后的 handler，否则 FileServer 会拿
					// 完整 URL 路径去拼目录，永远找不到文件。
					stripped.ServeHTTP(w, r)
					return
				}
				// 有扩展名但文件不存在 → 真的 404，不要回 index.html
				if filepath.Ext(rel) != "" {
					http.NotFound(w, r)
					return
				}
			}
			http.ServeFile(w, r, filepath.Join(mDir, "index.html"))
		})
		// Expo 导出的 index.html 里资源路径是绝对路径 /_expo/…，
		// 挂在子目录下会全部 404。把这两个前缀也指向同一目录。
		expo := http.FileServer(http.Dir(mDir))
		mux.Handle("GET /_expo/", expo)
	}

	// ---- 官网（web/site）----
	// 有官网目录时根路径就是落地页，静态资源挂 /site/；
	// 目录不存在（比如裁剪过的部署）退回跳转 /app/，不影响已有用户。
	siteDir := webDir("site")
	if st, err := os.Stat(string(siteDir) + "/index.html"); err == nil && !st.IsDir() {
		mux.Handle("GET /site/", http.StripPrefix("/site/", http.FileServer(siteDir)))
		mux.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, string(siteDir)+"/index.html")
		}))
	} else {
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/app/", http.StatusFound)
		})
	}

	h := Chain(mux,
		Recover,
		Trace,
		CORS,
		Access,
	)

	// 限制请求体大小，放在最外层——任何路径都不能靠一个大 body 撑爆内存。
	// 2MB：消息上限 4000 字约 12KB，留足余量。
	// 实测没有这个限制时，单个 100MB 请求会让内存从 80MB 涨到 331MB。
	return LimitBody(s.maintenanceMiddleware(h), 2<<20)
}

// exeDirFn 返回可执行文件所在目录。抽成变量便于测试覆盖。
var exeDirFn = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// webDir 定位 web/ 下的静态资源子目录。
// 依次尝试三处：
//  1. 可执行文件旁边（部署成系统服务时 CWD 不可控，二进制和 web/ 一起拷贝）；
//  2. macOS .app 包的 Contents/Resources/web（二进制在 Contents/MacOS/）；
//  3. 当前工作目录（go run / 本地开发）。
//
// 都没有时打一条显眼的 Error：不拒启动（API 还能用），
// 但页面全 404 的原因必须能从日志里一眼看到。
func webDir(sub string) http.Dir {
	var tried []string
	if exeDir, err := exeDirFn(); err == nil {
		dir := filepath.Join(exeDir, "web", sub)
		tried = append(tried, dir)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return http.Dir(dir)
		}
		bundleDir := filepath.Join(exeDir, "..", "Resources", "web", sub)
		tried = append(tried, bundleDir)
		if st, err := os.Stat(bundleDir); err == nil && st.IsDir() {
			return http.Dir(bundleDir)
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

// mobileWebDir 返回手机端 Web 构建产物目录；没有就返回空。
//
// 与 webDir 的区别：webDir 找不到会打 ERROR（因为 app/admin 缺了就是故障），
// 而手机端构建产物是可选的——大多数人用 Expo 跑原生 App，不进仓库的
// 构建产物不该在每次启动时刷一条错误日志。
func mobileWebDir() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "web", "m"))
	}
	candidates = append(candidates, filepath.Join("web", "m"))
	for _, dir := range candidates {
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			return dir
		}
	}
	return ""
}
