package httpapi

import (
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
)

// maintenanceMiddleware 维护模式：只拦用户侧 /v1 接口，
// 管理台、健康检查、静态页面照常可用——否则管理员没法关掉维护。
func (s *Server) maintenanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		st := s.admin.Settings()
		if !st.MaintenanceMode {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Retry-After", "120")
		fail(w, http.StatusServiceUnavailable, "maintenance", st.MaintenanceMessage)
	})
}

// planOf 取用户当前套餐；套餐被删时返回零值。
func (s *Server) planOf(u *domain.User) (domain.PlanConfig, bool) {
	if u == nil {
		return domain.PlanConfig{}, false
	}
	return s.admin.Plan(u.Plan)
}

// allowPlan 套餐级每分钟限流。0 表示沿用服务默认限流（已在 authMiddleware 里判过）。
func (s *Server) allowPlan(u *domain.User) bool {
	p, ok := s.planOf(u)
	if !ok || p.RatePerMinute <= 0 {
		return true
	}
	s.limitMu.Lock()
	l := s.planLimiters[p.ID]
	if l == nil {
		burst := p.RatePerMinute
		if burst < 1 {
			burst = 1
		}
		l = gateway.NewLimiter(p.RatePerMinute, burst)
		s.planLimiters[p.ID] = l
	}
	s.limitMu.Unlock()
	return l.Allow(u.ID)
}

// enterConcurrency 套餐级并发上限。进入返回 false 表示已经占满。
func (s *Server) enterConcurrency(u *domain.User) bool {
	p, ok := s.planOf(u)
	if !ok || p.MaxConcurrency <= 0 {
		return true
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	if s.userInflight[u.ID] >= p.MaxConcurrency {
		return false
	}
	s.userInflight[u.ID]++
	return true
}

func (s *Server) exitConcurrency(u *domain.User) {
	p, ok := s.planOf(u)
	if !ok || p.MaxConcurrency <= 0 {
		return
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	if n := s.userInflight[u.ID]; n <= 1 {
		delete(s.userInflight, u.ID)
	} else {
		s.userInflight[u.ID] = n - 1
	}
}

// consumesQuota 判断这个请求是否可能产生上游调用。
// 余额不足拦截只针对这些请求，浏览会话、看模型列表不受影响。
func consumesQuota(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	p := r.URL.Path
	switch {
	case p == "/v1/chat/completions",
		p == "/v1/agents/draft",
		p == "/v1/agents/polish":
		return true
	case strings.HasSuffix(p, "/messages"),
		strings.HasSuffix(p, "/orchestrate"),
		strings.HasSuffix(p, "/kickoff"),
		strings.HasSuffix(p, "/runs"),
		strings.HasSuffix(p, "/collab"):
		return true
	}
	return false
}

// balanceBlocked 余额不足拦截。默认关闭；开启后余额 <= 0 的用户不能
// 发起消耗额度的请求，避免欠费继续扩大。
func (s *Server) balanceBlocked(u *domain.User, r *http.Request) bool {
	if u == nil || s.admin.Wallet() == nil || !consumesQuota(r) {
		return false
	}
	if !s.admin.Settings().EnforceBalance {
		return false
	}
	return s.admin.Wallet().Balance(u.ID) <= 0
}

// allowedModels 用户当前套餐的模型白名单；空表示不限制。
func (s *Server) allowedModels(u *domain.User) []string {
	if p, ok := s.planOf(u); ok {
		return p.AllowedModels
	}
	return nil
}

// planSummary 给用户侧 /v1/me 的套餐摘要，不暴露成本与内部字段。
func (s *Server) planSummary(u *domain.User) map[string]interface{} {
	p, ok := s.planOf(u)
	if !ok {
		return map[string]interface{}{"id": u.Plan}
	}
	return map[string]interface{}{
		"id": p.ID, "name": p.Name, "priceMonthly": p.PriceMonthly,
		"monthlyQuota": p.MonthlyQuota, "features": p.Features,
		"allowedModels": p.AllowedModels,
		"ratePerMinute": p.RatePerMinute, "maxConcurrency": p.MaxConcurrency,
	}
}

// userWallet 钱包摘要，用户侧只给余额和最近流水/订单，
// 不暴露其它用户与兑换码池。
func (s *Server) userWallet(u *domain.User) map[string]interface{} {
	w := s.admin.Wallet()
	acct := w.Account(u.ID)
	return map[string]interface{}{
		"balanceCny": acct.BalanceCNY,
		"totalTopup": acct.TotalTopup,
		"totalSpent": acct.TotalSpent,
		"updatedAt":  acct.UpdatedAt,
		"txns":       w.Txns(u.ID, 50),
		"orders":     w.Orders(u.ID, 50),
		"currency":   "CNY",
	}
}
