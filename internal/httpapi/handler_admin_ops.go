package httpapi

import (
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// messageCount 会话消息数。存储实现了 O(1) 计数索引就直接用，
// 否则退回扫描（旧实现 / 测试替身）。
func (s *Server) messageCount(convID string) int {
	if c, ok := s.store.(interface{ CountMessages(string) int }); ok {
		return c.CountMessages(convID)
	}
	return len(s.store.ListMessages(convID, 0))
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// handleAdminUserDetail 单个用户的全景：资料、会话、最近行为。
// 列表页只给摘要，真正排障时再按需拉详情。
func (s *Server) handleAdminUserDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, found := s.store.GetUser(id)
	if !found {
		notFound(w, "用户不存在")
		return
	}

	type convRow struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Studio    string    `json:"studio,omitempty"`
		Workspace string    `json:"workspace,omitempty"`
		Owned     bool      `json:"owned"`
		Busy      bool      `json:"busy"`
		Messages  int       `json:"messages"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	convs := s.store.ListConvs(id)
	rows := make([]convRow, 0, len(convs))
	msgTotal := 0
	for _, c := range convs {
		n := s.messageCount(c.ID)
		msgTotal += n
		busy := false
		if s.orchestrator != nil {
			busy = s.orchestrator.Busy(c.ID)
		}
		rows = append(rows, convRow{
			ID: c.ID, Title: c.Title, Studio: c.Studio,
			Owned: c.UserID == id, Busy: busy, Messages: n, UpdatedAt: c.UpdatedAt,
		})
	}
	if len(rows) > 50 {
		rows = rows[:50]
	}

	events := s.admin.Events("", 0)
	recent := make([]interface{}, 0, 20)
	for _, e := range events {
		if e.UserID != id {
			continue
		}
		recent = append(recent, e)
		if len(recent) >= 20 {
			break
		}
	}

	ok(w, map[string]interface{}{
		"user":          u,
		"conversations": rows,
		"events":        recent,
		"usage": map[string]int{
			"conversations": len(convs),
			"messages":      msgTotal,
			"remaining":     u.Remaining(),
		},
		"billing": s.admin.UserBilling(id),
		"wallet":  s.admin.Wallet().Account(id),
		"risk":    firstRisk(s, id),
	})
}

// handleAdminUserPlan 修改套餐档位。
func (s *Server) handleAdminUserPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plan       string `json:"plan"`
		ApplyQuota *bool  `json:"applyQuota"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	id := r.PathValue("id")
	if _, found := s.store.GetUser(id); !found {
		notFound(w, "用户不存在")
		return
	}
	applyQuota := true
	if req.ApplyQuota != nil {
		applyQuota = *req.ApplyQuota
	}
	u, ok2 := s.admin.SetUserPlanWithQuota(id, strings.TrimSpace(req.Plan), applyQuota)
	if !ok2 {
		badRequest(w, "套餐不存在，请先在「套餐配置」里创建")
		return
	}
	detail := u.Plan
	if applyQuota {
		detail += " · quota=" + strconv.Itoa(u.Quota)
	}
	s.admin.Audit("admin", "user_plan", id, detail, ClientIP(r))
	ok(w, u)
}

// handleAdminLogs 管理操作审计，支持搜索、筛选和分页。
func (s *Server) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	actor := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("actor")))
	page := clampInt(queryInt(r, "page", 1), 1, 1<<20)
	size := clampInt(queryInt(r, "pageSize", 50), 1, 200)

	all := s.admin.Logs(0)
	actions := map[string]bool{}
	items := make([]interface{}, 0, len(all))
	for _, a := range all {
		actions[a.Action] = true
		if action != "" && a.Action != action {
			continue
		}
		if actor != "" && !strings.Contains(strings.ToLower(a.Actor), actor) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(strings.Join([]string{a.Actor, a.Action, a.Target, a.Detail, a.IP}, " "))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		items = append(items, a)
	}
	actionList := make([]string, 0, len(actions))
	for a := range actions {
		actionList = append(actionList, a)
	}
	sort.Strings(actionList)

	total := len(items)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	ok(w, map[string]interface{}{
		"items":    items[start:end],
		"total":    total,
		"page":     page,
		"pageSize": size,
		"actions":  actionList,
	})
}

// handleAdminEvents 用户行为，同样分页。
func (s *Server) handleAdminEvents(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	page := clampInt(queryInt(r, "page", 1), 1, 1<<20)
	size := clampInt(queryInt(r, "pageSize", 50), 1, 200)

	all := s.admin.Events("", 0)
	kinds := map[string]bool{}
	items := make([]interface{}, 0, len(all))
	for _, e := range all {
		kinds[e.Kind] = true
		if kind != "" && e.Kind != kind {
			continue
		}
		if user != "" && e.UserID != user {
			continue
		}
		if q != "" {
			meta := ""
			for k, v := range e.Meta {
				meta += " " + k + " " + v
			}
			hay := strings.ToLower(e.UserID + " " + e.Kind + " " + e.Target + " " + e.IP + " " + meta)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		items = append(items, e)
	}
	kindList := make([]string, 0, len(kinds))
	for k := range kinds {
		kindList = append(kindList, k)
	}
	sort.Strings(kindList)

	total := len(items)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	ok(w, map[string]interface{}{
		"items":    items[start:end],
		"total":    total,
		"page":     page,
		"pageSize": size,
		"kinds":    kindList,
	})
}

// handleAdminSeries 最近 hours 小时的指标序列。
func (s *Server) handleAdminSeries(w http.ResponseWriter, r *http.Request) {
	hours := clampInt(queryInt(r, "hours", 24), 1, 24)
	points := clampInt(queryInt(r, "points", 240), 12, 1000)
	ins := s.admin.Insights()
	if ins == nil {
		ok(w, map[string]interface{}{"points": []string{}, "hours": hours})
		return
	}
	ok(w, map[string]interface{}{
		"points":  ins.Series(hours, points),
		"hours":   hours,
		"summary": ins.Summary(),
	})
}

// handleAdminSystem 系统页面：运行环境、落盘健康、安全配置摘要。
// 只返回非敏感配置；密钥、JWT、短信凭据一律不出现在响应里。
func (s *Server) handleAdminSystem(w http.ResponseWriter, r *http.Request) {
	ov := s.admin.OverviewOf()
	m := ov.Metrics

	persist := store.PersistHealth{OK: true}
	if ph, has := s.store.(interface{ PersistHealth() store.PersistHealth }); has {
		persist = ph.PersistHealth()
	}

	openProviders := 0
	for _, p := range m.Providers {
		if p.State == "open" {
			openProviders++
		}
	}
	models := s.admin.Models()
	enabledModels := 0
	for _, mod := range models {
		if mod.Enabled {
			enabledModels++
		}
	}
	managedUpstreams := 0
	if s.upstreams != nil {
		managedUpstreams = len(s.upstreams.List())
	}
	planCount := len(s.admin.Plans())
	billingStatus := s.admin.BillingStatus()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	ready := len(m.Providers)-openProviders > 0 && persist.OK

	ok(w, map[string]interface{}{
		"version":   ov.Version,
		"startedAt": ov.StartedAt,
		"uptime":    m.Uptime,
		"ready":     ready,
		"persist":   persist,
		"counts": map[string]int{
			"users":         ov.Users,
			"activeUsers":   ov.ActiveUsers,
			"conversations": ov.Conversations,
			"messages":      ov.Messages,
			"models":        len(models),
			"modelsEnabled": enabledModels,
			"providers":     len(m.Providers),
			"providersOpen": openProviders,
			"upstreams":     managedUpstreams,
			"upstreamsEnv":  len(s.cfg.ExtraProviders),
			"plans":         planCount,
		},
		"gateway": map[string]interface{}{
			"requests":     m.Requests,
			"errors":       m.Errors,
			"inFlight":     m.InFlight,
			"tokensUsed":   m.TokensUsed,
			"avgLatencyMs": m.AvgLatencyMS,
			"p95LatencyMs": m.P95LatencyMS,
		},
		"config": map[string]interface{}{
			"dataDir":           s.cfg.DataDir,
			"prod":              s.cfg.Prod,
			"allowMock":         s.cfg.AllowMock,
			"searchEnabled":     s.cfg.SearchEnabled,
			"ratePerMinute":     s.cfg.RatePerMinute,
			"rateBurst":         s.cfg.Burst,
			"maxRetries":        s.cfg.MaxRetries,
			"breakerErrors":     s.cfg.BreakerError,
			"breakerOpenMs":     s.cfg.BreakerOpen.Milliseconds(),
			"upstreamTimeoutMs": s.cfg.UpstreamTimeout.Milliseconds(),
			"slowThresholdMs":   s.cfg.SlowThresholdMS,
			"adminTokenDefault": s.cfg.AdminToken == "admin-dev-token",
		},
		"runtime": map[string]interface{}{
			"goVersion":   runtime.Version(),
			"os":          runtime.GOOS,
			"arch":        runtime.GOARCH,
			"cpus":        runtime.NumCPU(),
			"goroutines":  runtime.NumGoroutine(),
			"memAllocMB":  float64(ms.Alloc) / (1 << 20),
			"memSysMB":    float64(ms.Sys) / (1 << 20),
			"gcPauseLast": ms.PauseNs[(ms.NumGC+255)%256] / 1e6,
		},
		"billing": billingStatus,
	})
}
