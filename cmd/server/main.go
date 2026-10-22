// 合奏后端入口。
//
// 启动：go run ./cmd/server
// 管理页：http://localhost:8080/admin/
// 默认管理员令牌：admin-dev-token（可用 ADMIN_TOKEN 覆盖）
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
const version = "1.1.1"

func main() {
	cfg := config.Load()
	log := logx.L()

	// ---- 存储 ----
	dataDir := cfg.DataDir
	fs, err := store.NewFileStore(dataDir)
	if err != nil {
		log.Error("存储初始化失败", "err", err.Error())
		os.Exit(1)
	}
	st := store.Store(fs)

	// ---- 上游注册 ----
	reg := gateway.NewRegistry()
	ds := gateway.NewDeepSeek(cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.UpstreamTimeout)
	reg.Add(ds)

	models := []domain.ModelInfo{
		{Key: "deepseek-chat", Provider: "deepseek", Upstream: "deepseek-chat", Enabled: true, Weight: 10, CostPer1K: 0.001, Note: "通用对话"},
		{Key: "deepseek-reasoner", Provider: "deepseek", Upstream: "deepseek-reasoner", Enabled: false, Weight: 1, CostPer1K: 0.002, Note: "推理模型"},
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

	if !cfg.UpstreamConfigured() {
		if !cfg.AllowMock {
			log.Error("未配置 DEEPSEEK_API_KEY，且 ALLOW_MOCK=false，无法启动")
			os.Exit(1)
		}
		log.Warn("未配置 DEEPSEEK_API_KEY，启用 mock 上游（输出为固定演示文本）")
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

	table := gateway.NewModelTable(models)
	gw := gateway.New(reg, table, cfg.MaxRetries, cfg.BreakerError, cfg.BreakerOpen)

	// ---- 业务 ----
	sender := service.NewSender(cfg.SmsProvider, cfg.SmsSign, cfg.SmsTemplate, cfg.SmsKeyID, cfg.SmsSecret)
	if cfg.SmsProvider != "log" && cfg.SmsProvider != "" {
		log.Warn("短信通道已配置但未接入真实 SDK，发送会失败", "provider", cfg.SmsProvider)
	}
	if cfg.DevBypass {
		log.Warn("DEV_BYPASS 已开启：演示账号免验证码，生产环境请设 DEV_BYPASS=false")
	}
	auth := service.NewAuth(st, cfg.JWTSecret, sender, cfg.DevBypass)
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
	if cfg.SearchEnabled && cfg.UpstreamConfigured() {
		searcher = service.NewSearchClient(cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey, cfg.UpstreamTimeout)
	}
	drafter := service.NewDrafter(gw, searcher)
	shared := &service.Shared{
		Limiter: gateway.NewLimiter(cfg.RatePerMinute, cfg.Burst),
		Started: time.Now(),
		Version: version,
	}
	admin := service.NewAdmin(st, gw, shared)

	// ---- HTTP ----
	// 工作层：本地工作区、Run、审批、回滚。
	agentMgr := agent.NewManager()
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
	srv := httpapi.NewServer(cfg, st, auth, chat, admin, gw, orch, drafter, memSvc, shared.Limiter)
	agentMgr.SetModel(gw)
	// 工作区与最近 100 个 Run 落盘：重启后不用重新绑目录
	stateFile := filepath.Join(dataDir, "agent-state.json")
	if err := agentMgr.Load(stateFile); err != nil {
		log.Warn("加载工作区状态失败，从空状态开始", "err", err.Error())
	}
	agentMgr.SetPersistence(stateFile)
	srv.SetAgentManager(agentMgr)
	httpServer := &http.Server{
		Addr:         cfg.Addr,
		Handler:      srv.Handler(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	// ---- 优雅关闭 ----
	done := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		log.Info("收到退出信号，开始优雅关闭")
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Error("关闭超时", "err", err.Error())
		}
		if err := fs.Close(); err != nil {
			log.Error("落盘失败", "err", err.Error())
		}
		close(done)
	}()

	log.Info("合奏后端已启动",
		"addr", cfg.Addr,
		"version", version,
		"admin", fmt.Sprintf("http://localhost%s/admin/", cfg.Addr),
		"mock", !cfg.UpstreamConfigured(),
	)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("启动失败", "err", err.Error())
		os.Exit(1)
	}
	<-done
	log.Info("已退出")
}
