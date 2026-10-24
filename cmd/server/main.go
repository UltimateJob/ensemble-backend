// 纸团后端入口。
//
// 启动：go run ./cmd/server
// 管理页：http://localhost:8080/admin/
// 默认管理员令牌：admin-dev-token（可用 ADMIN_TOKEN 覆盖）
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/httpapi"
	"github.com/UltimateJob/ensemble-backend/internal/memory"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
	"github.com/UltimateJob/ensemble-backend/internal/service"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// 1.0.0 意味着「可以承诺 API 稳定、可以对外」。
// 现在的功能是完整的，但运营基础设施还缺（计费、账号找回、短信通道、
// 多实例）。所以标 RC——候选版本，功能冻结，等基础设施补齐。
const version = "1.3.1"

// portOf 从监听地址里取端口：":8080" 和 "127.0.0.1:8080" 都返回 "8080"。
func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 && i+1 < len(addr) {
		return addr[i+1:]
	}
	return addr
}

// watchParent 盯着父进程，父进程消失就退出。
//
// 桌面端是「壳 + 内置后端」两个进程。壳被强杀（崩溃、Force Quit、
// pkill -9）时 exec 的子进程不会跟着死，会变成孤儿占着端口，下次启动
// 只能换端口。由后端自己轮询 getppid 是最可靠的做法：壳一没，它就退出。
func watchParent(stop func()) {
	parent := os.Getppid()
	for {
		time.Sleep(2 * time.Second)
		if os.Getppid() != parent {
			logx.L().Info("父进程已退出，后端随之关闭", "pid", os.Getpid())
			stop()
			os.Exit(0)
		}
	}
}

// seedDemoDir 找演示数据目录。
//
// 三个位置，按优先级：
//  1. 可执行文件旁边（.app 包里是 Contents/Resources/seed/demo）
//  2. 可执行文件上一级的 Resources
//  3. 当前工作目录下的 seed/demo（go run / 本地开发）
//
// 都没有就返回空串——裁剪过的部署不带演示数据是正常的。
func seedDemoDir() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "seed", "demo"),
			filepath.Join(dir, "..", "Resources", "seed", "demo"),
		)
	}
	candidates = append(candidates, filepath.Join("seed", "demo"))
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}

func main() {
	cfg := config.Load()
	log := logx.L()

	// ---- 存储 ----
	// 连接器凭据的加密密钥必须在打开存储**之前**注入：
	// NewFileStore 加载 connectors.json 时要解密，晚一步就会用兜底
	// 密钥去解，结果是所有连接器都变成「解密失败」。
	dataDir := cfg.DataDir
	if err := store.SetSecretKey(cfg.JWTSecret); err != nil {
		log.Error("连接器凭据加密初始化失败", "err", err.Error())
		os.Exit(1)
	}
	fs, err := store.NewFileStore(dataDir)
	if err != nil {
		log.Error("存储初始化失败", "err", err.Error())
		os.Exit(1)
	}
	// 首次启动装一份演示内容：产品打开就该是有东西的，
	// 而不是让用户先去读 README 找 `make demo`。
	if seedDir := seedDemoDir(); seedDir != "" {
		if err := fs.SeedDemo(seedDir); err != nil {
			log.Warn("装载演示内容失败（不影响使用）", "err", err.Error())
		}
	}
	st := store.Store(fs)

	// 套餐与成本账本。套餐损坏拒绝启动（影响权益），成本账本损坏
	// 由 NewBilling 隔离坏文件后继续，不阻断对话服务。
	plans, err := service.NewPlanStore(filepath.Join(dataDir, "plans.json"))
	if err != nil {
		log.Error("套餐配置加载失败，拒绝启动", "err", err.Error())
		os.Exit(1)
	}
	billing, err := service.NewBilling(filepath.Join(dataDir, "billing.json"))
	if err != nil {
		log.Error("成本账本初始化失败，拒绝启动", "err", err.Error())
		os.Exit(1)
	}
	billing.Start()

	// 运营开关与用户钱包。设置坏文件回退默认值，钱包坏文件隔离后从空账本起，
	// 两者都不阻断对话服务；真正的资金流水每次变更同步落盘。
	settings, err := service.OpenSettings(filepath.Join(dataDir, "settings.json"))
	if err != nil {
		log.Error("运营设置初始化失败", "err", err.Error())
		os.Exit(1)
	}
	wallet, err := service.OpenWallet(filepath.Join(dataDir, "wallet.json"))
	if err != nil {
		log.Error("钱包账本初始化失败", "err", err.Error())
		os.Exit(1)
	}

	// ---- 上游注册 ----
	reg := gateway.NewRegistry()
	ds := gateway.NewDeepSeek(cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.UpstreamTimeout)
	reg.Add(ds)
	// OpenAI 兼容中转池：每个中转一个名字，注册表/熔断/管理台都按名字区分。
	for _, ep := range cfg.ExtraProviders {
		reg.Add(gateway.NewDeepSeek(ep.BaseURL, ep.APIKey, ep.Model, cfg.UpstreamTimeout).WithName(ep.Name))
		log.Info("已注册中转上游", "name", ep.Name, "base", ep.BaseURL)
	}

	// ---- 可编辑的上游渠道 ----
	// 环境变量里的渠道作为初始种子写入渠道表（只在文件里没有同名渠道时），
	// 之后全部在管理台维护。渠道表是启动时的最终事实：管理台改过的地址/
	// Key/启停状态要覆盖环境变量，否则重启后停用的渠道会「复活」。
	upstreams, uerr := store.NewUpstreams(filepath.Join(dataDir, "upstreams.json"))
	if uerr != nil {
		log.Warn("加载上游渠道表失败，管理台渠道编辑暂不可用", "err", uerr.Error())
		upstreams = nil
	} else {
		seedUpstreams(upstreams, cfg)
		for _, rec := range upstreams.List() {
			if !rec.Enabled {
				reg.Remove(rec.Name)
				continue
			}
			key, _ := upstreams.Key(rec.Name)
			if key == "" {
				// 没 Key 的渠道注册上去只会 401，不如不注册。
				reg.Remove(rec.Name)
				continue
			}
			reg.Add(gateway.NewDeepSeek(rec.BaseURL, key, rec.Model, cfg.UpstreamTimeout).WithName(rec.Name))
		}
		log.Info("已从渠道表同步上游", "数量", len(upstreams.List()))
	}
	// routableUpstreams 渠道表里真正能接流量的数量。管理台加过渠道后，
	// 即使没有环境变量也不该再降级到 mock。
	routableUpstreams := 0
	if upstreams != nil {
		for _, rec := range upstreams.List() {
			if rec.Enabled && rec.KeyHint != "" {
				routableUpstreams++
			}
		}
	}

	// 渠道额度查询：DeepSeek 余额、Command Code / OpenDesign / GLM 的 5h、7d 窗口。
	// 配置单独落 data/quota.json（0600），不触碰上游密钥。
	quotaStore, err := gateway.OpenQuotaStore(filepath.Join(dataDir, "quota.json"))
	if err != nil {
		log.Warn("额度配置加载失败，从空配置开始", "err", err.Error())
	}
	quotaSvc := gateway.NewQuotaService(reg, quotaStore)

	models := []domain.ModelInfo{
		// Rank 决定路由优先级：官方渠道排最前，中转作为保底。
		{Key: "deepseek-chat", Provider: "deepseek", Upstream: "deepseek-chat", Enabled: true, Rank: 1, Weight: 10, CostPer1K: 0.001, Note: "通用对话（官方，首选）"},
		{Key: "deepseek-reasoner", Provider: "deepseek", Upstream: "deepseek-reasoner", Enabled: false, Rank: 1, Weight: 1, CostPer1K: 0.002, Note: "推理模型"},
	}

	// 生产自检：不安全的默认值直接拒绝启动。
	// 宁可起不来，也不要带着 DEV_BYPASS=true 和默认运维令牌对外跑。
	if problems := cfg.ProdProblems(); len(problems) > 0 {
		log.Error("生产模式自检未通过，拒绝启动", "问题数", len(problems))
		for i, p := range problems {
			log.Error(fmt.Sprintf("  %d. %s", i+1, p))
		}
		os.Exit(1)
	}
	if cfg.Prod {
		log.Info("生产模式：自检通过")
	}

	// 桌面端：模型调用转给线上后端。
	// Key 只存在服务器上，客户端不持有——这是「客户端不自带 Key」的前提。
	// 线上后端本身就是一个可路由的上游，所以要计入 routableUpstreams，
	// 否则配置了 REMOTE_BASE 还会被当成「没有上游」而降级到 mock。
	var remote *gateway.Remote
	if base := strings.TrimSpace(cfg.RemoteBase); base != "" {
		remote = gateway.NewRemote(base, "", cfg.DeepSeekModel, cfg.UpstreamTimeout)
		reg.Add(remote)
		models = []domain.ModelInfo{
			{Key: "teamwise-chat", Provider: remote.Name(), Upstream: cfg.DeepSeekModel, Enabled: true, Weight: 10, Note: "线上"},
		}
		routableUpstreams++
		log.Info("模型调用指向线上后端", "base", base,
			"说明", "登录后由前端注入令牌；未登录时聊天不可用")
	}

	// 渠道表里已有可用渠道时不再降级 mock：管理台加过渠道就不该被演示台词顶掉。
	if routableUpstreams == 0 {
		if !cfg.AllowMock {
			log.Error("未配置任何上游（DEEPSEEK_API_KEY / REMOTE_BASE / EXTRA_PROVIDERS），且 ALLOW_MOCK=false，无法启动")
			os.Exit(1)
		}
		log.Warn("未配置上游，启用 mock 上游（输出为固定演示文本）")
		reg.Add(&gateway.Mock{})
		models = append(models, domain.ModelInfo{
			Key: "mock-chat", Provider: "mock", Upstream: "mock", Enabled: true, Weight: 5, Note: "本地演示上游",
		})
		for i := range models {
			if models[i].Key == "deepseek-chat" {
				models[i].Enabled = false
			}
		}
	}

	// 模型表持久化：管理台加的模型（中转池这类）重启不能丢。
	// data/models.json 存在时整体取代内置种子——管理台的修改就是事实。
	modelsFile := filepath.Join(dataDir, "models.json")
	if b, err := os.ReadFile(modelsFile); err == nil && len(b) > 0 {
		var saved []domain.ModelInfo
		if err := json.Unmarshal(b, &saved); err == nil && len(saved) > 0 {
			models = saved
			log.Info("已加载模型表", "来源", modelsFile, "条数", len(saved))
		}
	}
	table := gateway.NewModelTable(models)
	table.SetOnChange(func() {
		b, err := json.Marshal(table.List())
		if err != nil {
			return
		}
		tmp := modelsFile + ".tmp"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			log.Warn("模型表落盘失败", "err", err.Error())
			return
		}
		_ = os.Rename(tmp, modelsFile)
	})
	gw := gateway.New(reg, table, cfg.MaxRetries, cfg.BreakerError, cfg.BreakerOpen)
	// 成本归因：每次拿到上游 usage 就记一笔；开启「自动扣费」时
	// 同一笔成本再从用户钱包扣一次。默认只记账不扣钱。
	gw.SetUsageObserver(func(ev domain.UsageEvent) {
		billing.Record(ev)
		if ev.UserID != "" && settings.Get().AutoDeduct {
			wallet.Spend(ev.UserID, ev.CostCNY, ev.Model)
		}
	})

	// ---- 业务 ----
	sender := service.NewSender(cfg.SmsProvider, cfg.SmsSign, cfg.SmsTemplate, cfg.SmsKeyID, cfg.SmsSecret)
	if cfg.SmsProvider != "log" && cfg.SmsProvider != "" {
		log.Warn("短信通道已配置但未接入真实 SDK，发送会失败", "provider", cfg.SmsProvider)
	}
	if cfg.DevBypass {
		log.Warn("DEV_BYPASS 已开启：演示账号免验证码，生产环境请设 DEV_BYPASS=false")
	}
	auth := service.NewAuth(st, cfg.JWTSecret, sender, cfg.DevBypass)
	// 密码凭据单独落盘，不塞进用户对象。文件损坏不拒绝启动：
	// 记一条告警，密码登录按「未设置密码」处理，账号本身还能用验证码进。
	if err := auth.SetCredentialsPath(filepath.Join(dataDir, "credentials.json")); err != nil {
		log.Warn("加载密码凭据失败，密码登录暂不可用", "err", err.Error())
	}
	// 注册策略由运营设置驱动：关闭注册后，未注册的手机号/邮箱不能自动建号。
	auth.SetSignupPolicy(func() service.SignupPolicy {
		s := settings.Get()
		quota := s.TrialQuota
		if quota <= 0 {
			if p, ok := plans.Get(s.DefaultPlan); ok {
				quota = p.MonthlyQuota
			}
		}
		return service.SignupPolicy{Open: s.RegistrationOpen, Plan: s.DefaultPlan, Quota: quota}
	})
	chat := service.NewChat(st, gw)
	slow := service.NewSlowLog(200, int64(cfg.SlowThresholdMS))

	var memStore memory.Store
	if ms, err := memory.NewFileStore(filepath.Join(dataDir, "memories.json")); err != nil {
		log.Warn("记忆存储初始化失败，先以无记忆模式运行", "err", err.Error())
	} else {
		memStore = ms
	}
	memSvc := service.NewMemoryService(memStore, gw)
	orch := service.NewOrchestrator(st, gw, slow, memSvc)
	var searcher *service.SearchClient
	// Search API 走官方 Key 和官方地址——中转池未必代理这个端点，
	// 不能因为配了中转就误以为联网搜索可用。
	if cfg.SearchEnabled && strings.TrimSpace(cfg.DeepSeekAPIKey) != "" {
		searcher = service.NewSearchClient(cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey, cfg.UpstreamTimeout)
	}
	drafter := service.NewDrafter(gw, searcher)
	shared := &service.Shared{
		Limiter: gateway.NewLimiter(cfg.RatePerMinute, cfg.Burst),
		Started: time.Now(),
		Version: version,
	}
	admin := service.NewAdmin(st, gw, shared)
	admin.SetCommercial(plans, billing)
	admin.SetControl(settings, wallet)

	// ---- HTTP ----
	// 工作层：本地工作区、Run、审批、回滚。
	agentMgr := agent.NewManager()
	// Run 结束后把结果沉淀成一条群消息（kind=run），刷新后仍能看到。
	// 返回的消息 id 会随 run.end 事件下发，前端用它避免重复渲染。
	agentMgr.SetRunEndObserver(func(r *agent.Run) string {
		return chat.AppendRunMessage(r)
	})
	// 执行器的 token 按「每 10k token 折 1 次额度」计入用量，最少 1 次。
	agentMgr.SetUsageRecorder(func(userID string, in, out int) {
		credits := (in + out + 9999) / 10000
		if credits < 1 {
			credits = 1
		}
		if err := st.IncrementUsed(userID, credits); err != nil {
			log.Warn("记录执行用量失败", "err", err.Error())
		}
	})
	// 团队任务：规划、并行执行、验证、返工由后端编排。
	// 结束时落一条团队消息；run 级消息仍由上面的 observer 负责。
	teamMgr := agent.NewTeamManager()
	teamMgr.SetObserver(func(t *domain.TeamRun) {
		if t == nil {
			return
		}
		switch t.Status {
		case "done", "partial", "failed", "cancelled":
			chat.AppendTeamMessage(t)
		}
	})
	srv := httpapi.NewServer(cfg, st, auth, chat, admin, gw, orch, drafter, memSvc, shared.Limiter)
	srv.SetTeamManager(teamMgr)
	srv.SetQuotas(quotaSvc)

	// 上游渠道表在启动早期已加载并同步到注册表（见上方），
	// 这里只把同一份表注入 HTTP 层，供管理台增删改。
	if upstreams != nil {
		srv.SetUpstreams(upstreams)
		srv.SetProviderDeleteGuard(func(name string) string {
			for _, m := range admin.Models() {
				if m.Provider == name && m.Enabled {
					return "渠道「" + name + "」还有启用的模型，先停用或删除模型"
				}
			}
			return ""
		})
	}

	// ---- 手机端 ----
	// 加密密钥已在上方存储初始化前注入（加载时要解密）。
	devices := service.NewDevices(st, cfg.JWTSecret)
	goals := service.NewGoals(st)
	conns := service.NewConnectors(st)
	library := service.NewLibrary(st)
	activity := service.NewActivity(agentMgr)
	official := service.NewOfficial(st)
	srv.SetRemoteServices(devices, goals, conns, library, activity, official)
	orch.SetActivity(activity)
	orch.SetLibrary(library)

	agentMgr.SetModel(gw)
	// 连接器动作注入 Agent：配了 GitHub/Notion/飞书，执行器才看得见那些工具。
	// 没配就是空列表，模型不会幻想出 github_search_code 这类工具。
	agentMgr.SetConnectors(conns)
	// 工作区与最近 100 个 Run 落盘：重启后不用重新绑目录
	stateFile := filepath.Join(dataDir, "agent-state.json")
	if err := agentMgr.Load(stateFile); err != nil {
		log.Warn("加载工作区状态失败，从空状态开始", "err", err.Error())
	}
	agentMgr.SetPersistence(stateFile)
	srv.SetAgentManager(agentMgr)
	// 线上 provider 交给 HTTP 层，用于接收前端注入的登录令牌。
	if remote != nil {
		srv.SetRemote(remote)
	}
	httpServer := &http.Server{
		Addr:         cfg.Addr,
		Handler:      srv.Handler(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	// ---- 优雅关闭 ----
	done := make(chan struct{})
	shutdown := func() {
		log.Info("收到退出信号，开始优雅关闭")
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Error("关闭超时", "err", err.Error())
		}
		if err := fs.Close(); err != nil {
			log.Error("落盘失败", "err", err.Error())
		}
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		shutdown()
		close(done)
	}()

	// 自带后端的桌面壳被强杀时，子进程会变成孤儿，占着端口不放。
	// 让后端自己盯着父进程：壳没了就退出。只在被显式要求时启用，
	// systemd 里的线上服务不该因为父进程变化而自杀。
	//
	// 参数与环境变量都认：macOS 的 Process 在某些情况下会过滤掉
	// 自定义环境变量，命令行参数没有这个风险。
	watchParentOn := os.Getenv("EXIT_WITH_PARENT") == "1"
	for _, a := range os.Args[1:] {
		if a == "--exit-with-parent" {
			watchParentOn = true
		}
	}
	if watchParentOn {
		go watchParent(shutdown)
	}

	log.Info("纸团后端已启动",
		"addr", cfg.Addr,
		"version", version,
		// cfg.Addr 可能是 ":8080"（默认）或 "127.0.0.1:8080"（线上 systemd）。
		// 只取端口，避免拼成 localhost127.0.0.1:8080。
		"admin", "http://localhost:"+portOf(cfg.Addr)+"/admin/",
		"mock", routableUpstreams == 0,
	)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("启动失败", "err", err.Error())
		os.Exit(1)
	}
	<-done
	log.Info("已退出")
}

// seedUpstreams 把环境变量里的渠道补进渠道表。
//
// 只在文件里没有同名渠道时写入：管理台改过的 Key 不能被下次启动的
// 环境变量覆盖回去（否则「热改 Key」重启就丢）。
func seedUpstreams(u *store.Upstreams, cfg config.Config) {
	existing := map[string]bool{}
	for _, r := range u.List() {
		existing[r.Name] = true
	}
	if !existing["deepseek"] && cfg.DeepSeekAPIKey != "" {
		_, _ = u.Upsert(store.UpstreamRecord{
			Name: "deepseek", BaseURL: cfg.DeepSeekBaseURL, Key: cfg.DeepSeekAPIKey,
			Model: cfg.DeepSeekModel, Source: "env", Enabled: true,
		})
	}
	for _, ep := range cfg.ExtraProviders {
		if existing[ep.Name] {
			continue
		}
		_, _ = u.Upsert(store.UpstreamRecord{
			Name: ep.Name, BaseURL: ep.BaseURL, Key: ep.APIKey,
			Model: ep.Model, Source: "env", Enabled: true,
		})
	}
}
