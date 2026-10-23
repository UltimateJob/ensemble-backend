package httpapi

import (
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// 上游渠道的可视化管理。
//
// 渠道原先只能写 EXTRA_PROVIDERS，换 Key 要改环境变量并重启。
// 这里提供管理台接口：增删改 + 启停 + 热注册到网关。
// Key 只写不读——列表接口永远不回传明文，只给末四位。

type upstreamReq struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// APIKey 为空表示沿用原 Key；首次创建必须填。
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	Enabled *bool  `json:"enabled"`
}

func (s *Server) handleAdminUpstreams(w http.ResponseWriter, r *http.Request) {
	if s.upstreams == nil {
		fail(w, 501, "upstreams_unavailable", "渠道表未初始化")
		return
	}
	if r.Method == http.MethodGet {
		ok(w, s.upstreams.List())
		return
	}
	var req upstreamReq
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.Name == "" || req.BaseURL == "" {
		badRequest(w, "name 与 baseUrl 必填")
		return
	}
	// 名字会变成网关的 provider 名，不能带空格或斜杠。
	if strings.ContainsAny(req.Name, " /\\") {
		badRequest(w, "name 不能包含空格或斜杠")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rec, err := s.upstreams.Upsert(store.UpstreamRecord{
		Name: req.Name, BaseURL: req.BaseURL, Key: strings.TrimSpace(req.APIKey),
		Model: req.Model, Enabled: enabled,
	})
	if err != nil {
		fail(w, 400, "upstream_save_failed", err.Error())
		return
	}
	s.registerUpstream(rec)
	// 新渠道如果没有对应的模型行，永远不会接到流量。
	// 默认模型填了就自动建一条（排在有排名渠道之后，作为保底），
	// 管理员之后可以再调排名/权重。
	if rec.Enabled && rec.Model != "" {
		s.ensureModelForUpstream(rec)
	}
	s.admin.Audit("admin", "upstream_upsert", rec.Name, rec.BaseURL, ClientIP(r))
	ok(w, s.upstreams.List())
}

func (s *Server) handleAdminUpstreamDelete(w http.ResponseWriter, r *http.Request) {
	if s.upstreams == nil {
		fail(w, 501, "upstreams_unavailable", "渠道表未初始化")
		return
	}
	name := r.PathValue("name")
	// 自砖守卫：删掉一个仍有启用模型的渠道，那些模型会路由到不存在的 provider。
	if s.beforeProviderDelete != nil {
		if msg := s.beforeProviderDelete(name); msg != "" {
			badRequest(w, msg)
			return
		}
	}
	if !s.upstreams.Delete(name) {
		notFound(w, "渠道不存在")
		return
	}
	s.admin.Audit("admin", "upstream_delete", name, "", ClientIP(r))
	ok(w, s.upstreams.List())
}

// ensureModelForUpstream 给新渠道补一条模型行（已存在则不动）。
func (s *Server) ensureModelForUpstream(rec store.UpstreamRecord) {
	for _, m := range s.admin.Models() {
		if m.Provider == rec.Name {
			return
		}
	}
	// 排名默认取当前最大值 +1：新加的渠道天然是保底，不会抢掉首选。
	maxRank := 0
	for _, m := range s.admin.Models() {
		if m.Rank > maxRank {
			maxRank = m.Rank
		}
	}
	key := rec.Name + "-" + rec.Model
	s.admin.UpsertModel(domain.ModelInfo{
		Key: key, Provider: rec.Name, Upstream: rec.Model,
		Enabled: true, Rank: maxRank + 1, Weight: 10, Concurrency: 8,
		Note: "由渠道 " + rec.Name + " 自动创建",
	})
}

// registerUpstream 把渠道热注册到网关（或更新）。
//
// Registry.Add 是覆盖语义：同名渠道会被替换，熔断状态随之重置——
// 换了 Key 就该重新试一次，沿用旧的熔断状态没有意义。
func (s *Server) registerUpstream(rec store.UpstreamRecord) {
	if s.gw == nil {
		return
	}
	if !rec.Enabled {
		return
	}
	key, _ := s.upstreams.Key(rec.Name)
	if key == "" {
		// 没 Key 的渠道注册上去只会 401，不如不注册。
		return
	}
	p := gateway.NewDeepSeek(rec.BaseURL, key, rec.Model, s.providerTimeout).WithName(rec.Name)
	s.gw.Registry().Add(p)
}
