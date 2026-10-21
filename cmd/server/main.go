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
	"syscall"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/httpapi"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
	"github.com/UltimateJob/ensemble-backend/internal/service"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

const version = "0.1.0"

func main() {
	cfg := config.Load()
	log := logx.L()

	// ---- 存储 ----
	st := store.NewMemory()

	// ---- 上游注册 ----
	reg := gateway.NewRegistry()
	ds := gateway.NewDeepSeek(cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey, cfg.DeepSeekModel, cfg.UpstreamTimeout)
	reg.Add(ds)

	models := []domain.ModelInfo{
		{Key: "deepseek-chat", Provider: "deepseek", Upstream: "deepseek-chat", Enabled: true, Weight: 10, CostPer1K: 0.001, Note: "通用对话"},
		{Key: "deepseek-reasoner", Provider: "deepseek", Upstream: "deepseek-reasoner", Enabled: false, Weight: 1, CostPer1K: 0.002, Note: "推理模型"},
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
	auth := service.NewAuth(st, cfg.JWTSecret)
	chat := service.NewChat(st, gw)
	orch := service.NewOrchestrator(st, gw)
	shared := &service.Shared{
		Limiter: gateway.NewLimiter(cfg.RatePerMinute, cfg.Burst),
		Started: time.Now(),
		Version: version,
	}
	admin := service.NewAdmin(st, gw, shared)

	// ---- HTTP ----
	srv := httpapi.NewServer(cfg, st, auth, chat, admin, gw, orch)
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
