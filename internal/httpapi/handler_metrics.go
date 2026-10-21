package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// handleMetrics 以 Prometheus 文本格式暴露指标。
//
// 不引入 prometheus client 库——这个格式简单到手写就行，
// 而零依赖是这个项目的硬约束。
//
// 采集器期望的是：
//
//	# HELP <name> <说明>
//	# TYPE <name> <counter|gauge|histogram 的简单形式>
//	<name>{label="v"} <数值>
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ov := s.admin.OverviewOf()
	m := ov.Metrics

	var b strings.Builder

	write := func(name, help, typ string, lines []string) {
		fmt.Fprintf(&b, "# HELP %s %s\n", name, help)
		fmt.Fprintf(&b, "# TYPE %s %s\n", name, typ)
		for _, l := range lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}

	write("ensemble_uptime_seconds", "进程运行时长", "gauge",
		[]string{fmt.Sprintf("ensemble_uptime_seconds %d", m.Uptime)})

	write("ensemble_http_requests_total", "累计处理的 HTTP 请求数", "counter",
		[]string{fmt.Sprintf("ensemble_http_requests_total %d", m.Requests)})

	write("ensemble_http_errors_total", "累计返回 5xx 的请求数", "counter",
		[]string{fmt.Sprintf("ensemble_http_errors_total %d", m.Errors)})

	write("ensemble_http_in_flight", "当前处理中的请求数", "gauge",
		[]string{fmt.Sprintf("ensemble_http_in_flight %d", m.InFlight)})

	write("ensemble_llm_tokens_total", "累计消耗的上游 token", "counter",
		[]string{fmt.Sprintf("ensemble_llm_tokens_total %d", m.TokensUsed)})

	write("ensemble_llm_latency_ms", "上游调用延迟", "gauge", []string{
		fmt.Sprintf("ensemble_llm_latency_ms{quantile=\"avg\"} %.2f", m.AvgLatencyMS),
		fmt.Sprintf("ensemble_llm_latency_ms{quantile=\"0.95\"} %.2f", m.P95LatencyMS),
	})

	// 上游健康
	var prov []string
	for _, p := range m.Providers {
		prov = append(prov, fmt.Sprintf(
			`ensemble_provider_up{provider=%q} %d`, p.Name, boolToInt(p.State != "open")))
		prov = append(prov, fmt.Sprintf(
			`ensemble_provider_success_total{provider=%q} %d`, p.Name, p.Success))
		prov = append(prov, fmt.Sprintf(
			`ensemble_provider_failure_total{provider=%q} %d`, p.Name, p.Failure))
		prov = append(prov, fmt.Sprintf(
			`ensemble_provider_latency_ms{provider=%q} %.2f`, p.Name, p.LatencyMS))
	}
	if len(prov) > 0 {
		write("ensemble_provider_up", "上游是否可用（1=可用，0=熔断中）", "gauge", prov)
	}

	// 模型用量
	var mu []string
	keys := make([]string, 0, len(m.ModelUsage))
	for k := range m.ModelUsage {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		mu = append(mu, fmt.Sprintf(`ensemble_model_requests_total{model=%q} %d`, k, m.ModelUsage[k]))
	}
	if len(mu) > 0 {
		write("ensemble_model_requests_total", "各模型的请求数", "counter", mu)
	}

	// 业务量
	write("ensemble_users", "注册用户数", "gauge",
		[]string{fmt.Sprintf("ensemble_users %d", ov.Users)})
	write("ensemble_users_active_24h", "24 小时内有活动的用户数", "gauge",
		[]string{fmt.Sprintf("ensemble_users_active_24h %d", ov.ActiveUsers)})
	write("ensemble_conversations", "会话数", "gauge",
		[]string{fmt.Sprintf("ensemble_conversations %d", ov.Conversations)})
	write("ensemble_messages", "消息总数", "gauge",
		[]string{fmt.Sprintf("ensemble_messages %d", ov.Messages)})

	// 慢请求与审计（自上次重启以来）
	if s.orchestrator != nil && s.orchestrator.Slow() != nil {
		total, threshold := s.orchestrator.Slow().Stats()
		write("ensemble_slow_requests_total", "超过阈值的慢请求数（重启清零）", "counter",
			[]string{fmt.Sprintf("ensemble_slow_requests_total %d", total)})
		write("ensemble_slow_threshold_ms", "慢请求判定阈值", "gauge",
			[]string{fmt.Sprintf("ensemble_slow_threshold_ms %d", threshold)})
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// handlePublicMetrics 给 Prometheus 抓取用。
// 需要 admin 令牌——业务指标（用户数、会话数）不该公开。
func (s *Server) handlePublicMetrics(w http.ResponseWriter, r *http.Request) {
	tok := r.Header.Get("X-Admin-Token")
	if tok == "" {
		tok = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if tok != s.cfg.AdminToken {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.handleMetrics(w, r)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
