package httpapi

import (
	"net/http"
	"sort"
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
//
// 环境变量提供的渠道（Source=env）显示为「服务器配置」：
// 可以编辑（保存后变成管理台覆盖，重启不会被环境变量改回去），
// 但不能删除——删了下次启动又回来，状态对不上。

type upstreamReq struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// APIKey 为空表示沿用原 Key；首次创建必须填。
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	Enabled *bool  `json:"enabled"`
	// Kind chat | image。空视为 chat。
	Kind string `json:"kind"`
}

// upstreamView 管理台可见的上游。不含密钥，只说明配没配。
type upstreamView struct {
	Name     string `json:"name"`
	BaseURL  string `json:"baseUrl,omitempty"`
	Model    string `json:"model,omitempty"`
	HasKey   bool   `json:"hasKey"`
	KeyHint  string `json:"keyHint,omitempty"`
	Managed  bool   `json:"managed"`  // 存于 data/upstreams.json，可删
	Reserved bool   `json:"reserved"` // 由 EXTRA_PROVIDERS / DEEPSEEK_* 提供
	Enabled  bool   `json:"enabled"`
	State    string `json:"state"`
	Kind     string `json:"kind,omitempty"`
}

func (s *Server) handleAdminUpstreams(w http.ResponseWriter, r *http.Request) {
	if s.upstreams == nil {
		fail(w, 501, "upstreams_unavailable", "渠道表未初始化")
		return
	}
	if r.Method == http.MethodGet {
		ok(w, s.upstreamViews())
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

	// 名字是渠道主键，也是模型表里的 provider：编辑时不允许改名，
	// 否则引用它的模型会指向一个不存在的渠道。
	old := s.findUpstream(req.Name)
	if old == nil {
		// 新增：首次必须给 Key，否则注册上去只会 401。
		if strings.TrimSpace(req.APIKey) == "" {
			badRequest(w, "首次创建必须填写 API Key")
			return
		}
	}
	source := "admin"
	if old != nil {
		// 环境变量渠道编辑后仍标记为 env：它依然不能被删除，
		// 但改动会覆盖环境变量（种子只在文件里没有同名渠道时写入）。
		source = ""
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" && old != nil {
		kind = old.Kind // 编辑时没传就沿用
	}
	if kind != "" && kind != "chat" && kind != "image" {
		badRequest(w, "kind 只能是 chat 或 image")
		return
	}
	rec, err := s.upstreams.Upsert(store.UpstreamRecord{
		Name: req.Name, BaseURL: req.BaseURL, Key: strings.TrimSpace(req.APIKey),
		Model: req.Model, Enabled: enabled, Source: source, Kind: kind,
	})
	if err != nil {
		fail(w, 400, "upstream_save_failed", err.Error())
		return
	}
	s.registerUpstream(rec)
	// 新渠道如果没有对应的模型行，永远不会接到流量。
	// 默认模型填了就自动建一条（低权重保底），管理员之后可以再调。
	if rec.Enabled && rec.Model != "" {
		s.ensureModelForUpstream(rec)
	}
	s.admin.Audit("admin", "upstream_upsert", rec.Name, rec.BaseURL, ClientIP(r))
	ok(w, s.upstreamViews())
}

func (s *Server) handleAdminUpstreamDelete(w http.ResponseWriter, r *http.Request) {
	if s.upstreams == nil {
		fail(w, 501, "upstreams_unavailable", "渠道表未初始化")
		return
	}
	name := r.PathValue("name")
	// 服务器配置的渠道删不掉：下次启动环境变量又把它注册回来，
	// 管理台显示的状态会和服务实际行为对不上。
	if u := s.findUpstream(name); u != nil && u.Reserved {
		badRequest(w, "该渠道由服务器环境变量提供，不能删除；可以编辑覆盖或停用")
		return
	}
	// 自砖守卫：删掉一个仍有启用模型的渠道，那些模型会路由到不存在的 provider。
	var used []string
	for _, m := range s.admin.Models() {
		if m.Provider == name && m.Enabled {
			used = append(used, m.Key)
		}
	}
	if len(used) > 0 {
		badRequest(w, "还有启用的模型在使用这个上游："+strings.Join(used, "、")+"。先停用或删除这些模型。")
		return
	}
	if !s.upstreams.Delete(name) {
		notFound(w, "渠道不存在")
		return
	}
	if s.gw != nil {
		s.gw.Registry().Remove(name)
	}
	s.admin.Audit("admin", "upstream_delete", name, "", ClientIP(r))
	ok(w, s.upstreamViews())
}

// upstreamViews 把渠道表与网关注册表合成管理台需要的形状。
func (s *Server) upstreamViews() []upstreamView {
	regs := map[string]bool{}
	if s.gw != nil {
		for _, n := range s.gw.Registry().Names() {
			regs[n] = true
		}
	}
	rows := s.upstreams.List()
	out := make([]upstreamView, 0, len(rows))
	for _, r := range rows {
		// 环境变量种子在启动时直接注册进网关；管理台改过/新增的也在。
		// 不在注册表里的（例如停用）状态给「未注册」，不伪装成正常。
		state := "未注册"
		if regs[r.Name] && s.gw != nil {
			state = s.gw.Registry().State(r.Name)
		}
		out = append(out, upstreamView{
			Name: r.Name, BaseURL: r.BaseURL, Model: r.Model,
			HasKey: r.KeyHint != "", KeyHint: r.KeyHint,
			Managed: r.Source != "env", Reserved: r.Source == "env",
			Enabled: r.Enabled, State: state, Kind: r.Kind,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Server) findUpstream(name string) *upstreamView {
	views := s.upstreamViews()
	for i := range views {
		if views[i].Name == name {
			return &views[i]
		}
	}
	return nil
}

// ensureModelForUpstream 给新渠道补一条模型行（已存在则不动）。
func (s *Server) ensureModelForUpstream(rec store.UpstreamRecord) {
	for _, m := range s.admin.Models() {
		if m.Provider == rec.Name {
			return
		}
	}
	// 排名取当前最大值 +1、权重压低：新加的渠道天然是保底，
	// 不会抢掉已有首选；管理员之后可以再调排名/权重。
	maxRank := 0
	for _, m := range s.admin.Models() {
		if m.Rank > maxRank {
			maxRank = m.Rank
		}
	}
	key := rec.Name + "-" + rec.Model
	s.admin.UpsertModel(domain.ModelInfo{
		Key: key, Provider: rec.Name, Upstream: rec.Model,
		Enabled: true, Rank: maxRank + 1, Weight: 1,
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
		s.gw.Registry().Remove(rec.Name)
		return
	}
	key, _ := s.upstreams.Key(rec.Name)
	if key == "" {
		// 没 Key 的渠道注册上去只会 401，不如不注册。
		return
	}
	p := gateway.NewDeepSeek(rec.BaseURL, key, rec.Model, s.cfg.UpstreamTimeout).WithName(rec.Name)
	s.gw.Registry().Add(p)
}
