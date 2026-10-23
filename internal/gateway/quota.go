package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// QuotaConfig 渠道额度查询的额外配置。
//
// 大多数渠道用上游自己的 API Key 就能查额度；OpenDesign 的账号接口
// 需要 Cloud 控制台的 Control Key（不是推理 Key）和工作区 ID，所以
// 单独存一份，不混进上游配置，避免把控制面凭据发给模型请求。
type QuotaConfig struct {
	Provider    string `json:"provider"`
	Kind        string `json:"kind,omitempty"` // auto | deepseek | commandcode | opendesign | glm | off
	ControlKey  string `json:"controlKey,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
}

// QuotaConfigInfo 管理台可见的配置，密钥只返回是否已配置。
type QuotaConfigInfo struct {
	Provider      string `json:"provider"`
	Kind          string `json:"kind"`
	WorkspaceID   string `json:"workspaceId,omitempty"`
	HasControlKey bool   `json:"hasControlKey"`
}

type quotaSnapshot struct {
	Version int                    `json:"version"`
	Items   map[string]QuotaConfig `json:"items"`
}

// QuotaStore 保存额度查询配置（data/quota.json，0600）。
type QuotaStore struct {
	mu    sync.Mutex
	path  string
	items map[string]QuotaConfig
}

// OpenQuotaStore 读取额度配置。文件损坏时隔离坏文件并按空配置启动：
// 额度展示是辅助信息，不应该阻断聊天服务。
func OpenQuotaStore(path string) (*QuotaStore, error) {
	s := &QuotaStore{path: path, items: make(map[string]QuotaConfig)}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return s, nil
	}
	var snap quotaSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		quarantine := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		_ = os.Rename(path, quarantine)
		logx.L().Warn("额度配置损坏，已隔离并按空配置启动",
			"file", path, "隔离为", quarantine, "err", err.Error())
		return s, nil
	}
	for name, cfg := range snap.Items {
		cfg.Provider = normalizeQuotaProvider(name)
		if err := validateQuotaConfig(cfg); err != nil {
			logx.L().Warn("忽略非法的额度配置", "provider", name, "err", err.Error())
			continue
		}
		s.items[cfg.Provider] = cfg
	}
	return s, nil
}

// quotaNameRe 渠道名会出现在 URL、JSON 与配置文件名里，
// 限制成容易处理的字符，避免空格、斜杠把接口路径弄乱。
var quotaNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func normalizeQuotaProvider(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func validateQuotaConfig(cfg QuotaConfig) error {
	if !quotaNameRe.MatchString(cfg.Provider) {
		return errors.New("渠道名称不合法")
	}
	switch cfg.Kind {
	case "", "auto", "deepseek", "commandcode", "opendesign", "glm", "off":
	default:
		return errors.New("不支持的额度查询类型")
	}
	if len(cfg.ControlKey) > 512 {
		return errors.New("Control Key 过长")
	}
	if len(cfg.WorkspaceID) > 128 {
		return errors.New("工作区 ID 过长")
	}
	return nil
}

// Get 取某个渠道的额度配置。
func (s *QuotaStore) Get(name string) (QuotaConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.items[normalizeQuotaProvider(name)]
	return cfg, ok
}

// Upsert 保存配置。ControlKey 留空表示沿用原值，避免每次改工作区都要重贴密钥。
func (s *QuotaStore) Upsert(cfg QuotaConfig) error {
	cfg.Provider = normalizeQuotaProvider(cfg.Provider)
	cfg.Kind = strings.ToLower(strings.TrimSpace(cfg.Kind))
	if cfg.Kind == "" {
		cfg.Kind = "auto"
	}
	cfg.ControlKey = strings.TrimSpace(cfg.ControlKey)
	cfg.WorkspaceID = strings.TrimSpace(cfg.WorkspaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.items[cfg.Provider]
	if exists && cfg.ControlKey == "" {
		cfg.ControlKey = old.ControlKey
	}
	if err := validateQuotaConfig(cfg); err != nil {
		return err
	}
	s.items[cfg.Provider] = cfg
	if err := s.saveLocked(); err != nil {
		if exists {
			s.items[cfg.Provider] = old
		} else {
			delete(s.items, cfg.Provider)
		}
		return err
	}
	return nil
}

// Delete 删除配置，回到自动识别。
func (s *QuotaStore) Delete(name string) error {
	name = normalizeQuotaProvider(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.items[name]
	if !exists {
		return nil
	}
	delete(s.items, name)
	if err := s.saveLocked(); err != nil {
		s.items[name] = old
		return err
	}
	return nil
}

// Snapshot 管理台可见的配置，按渠道名排序。
func (s *QuotaStore) Snapshot() []QuotaConfigInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]QuotaConfigInfo, 0, len(s.items))
	for _, cfg := range s.items {
		out = append(out, QuotaConfigInfo{
			Provider:      cfg.Provider,
			Kind:          cfg.Kind,
			WorkspaceID:   cfg.WorkspaceID,
			HasControlKey: cfg.ControlKey != "",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

func (s *QuotaStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	snap := quotaSnapshot{Version: 1, Items: make(map[string]QuotaConfig, len(s.items))}
	for k, v := range s.items {
		snap.Items[k] = v
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// QuotaService 查询各渠道额度。结果缓存 45 秒，避免管理台轮询打爆上游。
type QuotaService struct {
	reg    *Registry
	store  *QuotaStore
	client *http.Client

	mu    sync.Mutex
	cache map[string]quotaCache
	ttl   time.Duration
	now   func() time.Time
}

type quotaCache struct {
	q  domain.ChannelQuota
	at time.Time
}

// NewQuotaService 创建额度查询服务。
func NewQuotaService(reg *Registry, store *QuotaStore) *QuotaService {
	if store == nil {
		store = &QuotaStore{items: make(map[string]QuotaConfig)}
	}
	return &QuotaService{
		reg:   reg,
		store: store,
		client: &http.Client{
			Timeout: 12 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     60 * time.Second,
				ForceAttemptHTTP2:   true,
			},
		},
		cache: make(map[string]quotaCache),
		ttl:   45 * time.Second,
		now:   time.Now,
	}
}

// Configs 返回可编辑的额度配置。
func (s *QuotaService) Configs() []QuotaConfigInfo { return s.store.Snapshot() }

// SaveConfig 保存配置并清掉缓存。
func (s *QuotaService) SaveConfig(cfg QuotaConfig) error {
	if err := s.store.Upsert(cfg); err != nil {
		return err
	}
	s.Invalidate(cfg.Provider)
	return nil
}

// DeleteConfig 删除配置并清掉缓存。
func (s *QuotaService) DeleteConfig(name string) error {
	if err := s.store.Delete(name); err != nil {
		return err
	}
	s.Invalidate(name)
	return nil
}

// Invalidate 让某个渠道的缓存立即失效。
func (s *QuotaService) Invalidate(name string) {
	s.mu.Lock()
	delete(s.cache, normalizeQuotaProvider(name))
	s.mu.Unlock()
}

// Snapshot 查询所有支持额度展示的渠道。refresh 为真时跳过缓存。
func (s *QuotaService) Snapshot(ctx context.Context, refresh bool) []domain.ChannelQuota {
	names := s.reg.Names()
	out := make([]domain.ChannelQuota, 0, len(names))
	type result struct {
		i int
		q domain.ChannelQuota
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res []result
	)
	for _, name := range names {
		p, ok := s.reg.Get(name)
		if !ok {
			continue
		}
		if _, ok := p.(*DeepSeek); !ok {
			continue
		}
		cfg, _ := s.store.Get(name)
		if detectQuotaKind(name, p.(*DeepSeek).base, cfg.Kind) == "" {
			continue
		}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			q := s.query(ctx, name, refresh)
			mu.Lock()
			res = append(res, result{q: q})
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	for _, r := range res {
		out = append(out, r.q)
	}
	sort.Slice(out, func(i, j int) bool {
		oi, oj := quotaKindOrder(out[i].Kind), quotaKindOrder(out[j].Kind)
		if oi != oj {
			return oi < oj
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func quotaKindOrder(kind string) int {
	switch kind {
	case "deepseek":
		return 0
	case "commandcode":
		return 1
	case "opendesign":
		return 2
	case "glm":
		return 3
	default:
		return 9
	}
}

func (s *QuotaService) query(ctx context.Context, name string, refresh bool) domain.ChannelQuota {
	name = normalizeQuotaProvider(name)
	now := s.now()
	if !refresh {
		s.mu.Lock()
		if c, ok := s.cache[name]; ok && now.Sub(c.at) < s.ttl {
			q := c.q
			s.mu.Unlock()
			return q
		}
		s.mu.Unlock()
	}
	q := s.fetch(ctx, name)
	s.mu.Lock()
	s.cache[name] = quotaCache{q: q, at: now}
	s.mu.Unlock()
	return q
}

func (s *QuotaService) fetch(ctx context.Context, name string) domain.ChannelQuota {
	q := domain.ChannelQuota{Provider: name, FetchedAt: s.now(), Configured: true}
	p, ok := s.reg.Get(name)
	if !ok {
		q.Err = "渠道不存在"
		return q
	}
	d, ok := p.(*DeepSeek)
	if !ok {
		q.Err = "该渠道不支持额度查询"
		return q
	}
	cfg, _ := s.store.Get(name)
	kind := detectQuotaKind(name, d.base, cfg.Kind)
	q.Kind = kind
	if kind == "off" || kind == "" {
		q.Err = "该渠道未启用额度查询"
		return q
	}
	if d.key == "" && !(kind == "opendesign" && cfg.ControlKey != "") {
		q.Configured = false
		q.Err = "未配置 API Key，无法查询额度"
		return q
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	switch kind {
	case "deepseek":
		return s.fetchDeepSeek(ctx, d, q)
	case "commandcode":
		return s.fetchCommandCode(ctx, d, q)
	case "opendesign":
		return s.fetchOpenDesign(ctx, d, cfg, q)
	case "glm":
		return s.fetchGLM(ctx, d, q)
	default:
		q.Err = "该渠道不支持额度查询"
		return q
	}
}

// detectQuotaKind 根据渠道名、Base URL 和人工配置判断查询方式。
// 未识别的渠道（例如 d1api）返回空字符串，页面不展示。
func detectQuotaKind(name, base, override string) string {
	override = strings.ToLower(strings.TrimSpace(override))
	if override != "" && override != "auto" {
		return override
	}
	s := strings.ToLower(name + " " + base)
	switch {
	case strings.Contains(s, "commandcode"):
		return "commandcode"
	case strings.Contains(s, "open-design"), strings.Contains(s, "opendesign"):
		return "opendesign"
	case strings.Contains(s, "bigmodel"), strings.Contains(s, "z.ai"),
		strings.Contains(s, "zhipu"), strings.Contains(s, "glm"):
		return "glm"
	case strings.Contains(s, "deepseek"):
		return "deepseek"
	default:
		return ""
	}
}

func (s *QuotaService) get(ctx context.Context, endpoint string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, readErr := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return b, resp.StatusCode, fmt.Errorf("HTTP %d：%s", resp.StatusCode, msg)
	}
	return b, resp.StatusCode, nil
}

func apiOrigin(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(base, "/")
	}
	return u.Scheme + "://" + u.Host
}

func resetFromMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func addQuotaWindow(q *domain.ChannelQuota, label, unit string, used, limit float64, reset time.Time) {
	if limit <= 0 {
		return
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	pct := used / limit * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	q.Windows = append(q.Windows, domain.QuotaWindow{
		Label: label, Unit: unit, Used: used, Limit: limit,
		Remaining: remaining, Percent: pct, ResetAt: reset,
	})
}

// ---------- DeepSeek 官方余额 ----------

type dsBalanceResp struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}

func (s *QuotaService) fetchDeepSeek(ctx context.Context, d *DeepSeek, q domain.ChannelQuota) domain.ChannelQuota {
	base := strings.TrimSuffix(d.base, "/v1")
	endpoint := base + "/user/balance"
	body, _, err := s.get(ctx, endpoint, map[string]string{"Authorization": "Bearer " + d.key})
	if err != nil {
		q.Err = "查询 DeepSeek 余额失败：" + err.Error()
		return q
	}
	return parseDeepSeekBalance(body, q)
}

func parseDeepSeekBalance(body []byte, q domain.ChannelQuota) domain.ChannelQuota {
	var r dsBalanceResp
	if err := json.Unmarshal(body, &r); err != nil {
		q.Err = "DeepSeek 余额返回格式无法解析"
		return q
	}
	if len(r.BalanceInfos) == 0 {
		q.Err = "DeepSeek 账户没有余额信息"
		return q
	}
	pick := r.BalanceInfos[0]
	for _, b := range r.BalanceInfos {
		if strings.EqualFold(b.Currency, "CNY") {
			pick = b
			break
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(pick.TotalBalance), 64)
	if err != nil {
		q.Err = "DeepSeek 余额字段无法解析"
		return q
	}
	q.Currency = strings.ToUpper(pick.Currency)
	q.Remaining = &v
	q.Total = &v
	q.Source = "DeepSeek /user/balance"
	if !r.IsAvailable {
		q.Err = "DeepSeek 账户当前不可用"
	}
	return q
}

// ---------- Command Code 5h / 7d ----------

type ccCreditsResp struct {
	Credits struct {
		MonthlyCredits   float64 `json:"monthlyCredits"`
		PurchasedCredits float64 `json:"purchasedCredits"`
		FreeCredits      float64 `json:"freeCredits"`
		PlanID           string  `json:"planId"`
	} `json:"credits"`
	WindowLimits struct {
		Limited  bool      `json:"limited"`
		FiveHour *ccWindow `json:"fiveHour"`
		Weekly   *ccWindow `json:"weekly"`
	} `json:"windowLimits"`
}

type ccWindow struct {
	Used     float64 `json:"used"`
	Cap      float64 `json:"cap"`
	Exceeded bool    `json:"exceeded"`
	ResetAt  int64   `json:"resetAt"`
}

func (s *QuotaService) fetchCommandCode(ctx context.Context, d *DeepSeek, q domain.ChannelQuota) domain.ChannelQuota {
	origin := apiOrigin(d.base)
	headers := map[string]string{"Authorization": "Bearer " + d.key}
	body, _, err := s.get(ctx, origin+"/alpha/billing/credits", headers)
	if err != nil {
		q.Err = "查询 Command Code 额度失败：" + err.Error()
		return q
	}
	q = parseCommandCodeCredits(body, q)
	if q.Err != "" {
		return q
	}
	// 套餐信息是辅助字段，查不到不影响额度展示。
	if b, _, err := s.get(ctx, origin+"/alpha/billing/subscriptions", headers); err == nil {
		var sub struct {
			Data struct {
				PlanID string `json:"planId"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &sub) == nil && sub.Data.PlanID != "" {
			q.Plan = commandCodePlanName(sub.Data.PlanID)
		}
	}
	return q
}

func parseCommandCodeCredits(body []byte, q domain.ChannelQuota) domain.ChannelQuota {
	var r ccCreditsResp
	if err := json.Unmarshal(body, &r); err != nil {
		q.Err = "Command Code 额度返回格式无法解析"
		return q
	}
	total := r.Credits.MonthlyCredits + r.Credits.PurchasedCredits + r.Credits.FreeCredits
	q.Currency = "USD"
	q.Remaining = &total
	if r.Credits.PlanID != "" {
		q.Plan = commandCodePlanName(r.Credits.PlanID)
	}
	if w := r.WindowLimits.FiveHour; w != nil {
		addQuotaWindow(&q, "5 小时", "USD", w.Used, w.Cap, resetFromMillis(w.ResetAt))
	}
	if w := r.WindowLimits.Weekly; w != nil {
		addQuotaWindow(&q, "7 天", "USD", w.Used, w.Cap, resetFromMillis(w.ResetAt))
	}
	q.Source = "Command Code /alpha/billing/credits"
	if len(q.Windows) == 0 {
		q.Err = "Command Code 没有返回 5 小时 / 7 天窗口"
	}
	return q
}

func commandCodePlanName(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	switch s {
	case "individual-go", "individual-go-v1":
		return "Go"
	case "individual-goat":
		return "GOAT"
	case "individual-pro", "individual-pro-v1":
		return "Pro"
	case "individual-provider":
		return "Provider"
	case "individual-max":
		return "Max"
	case "individual-ultra":
		return "Ultra"
	case "teams-pro":
		return "Teams Pro"
	}
	s = strings.TrimPrefix(s, "individual-")
	s = strings.ReplaceAll(s, "-", " ")
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---------- OpenDesign 5h / 7d + 钱包 ----------

type odUsageWindow struct {
	PolicyID         string `json:"policyId"`
	DurationSeconds  int64  `json:"durationSeconds"`
	UsedCredits      string `json:"usedCredits"`
	RemainingCredits string `json:"remainingCredits"`
	LimitCredits     string `json:"limitCredits"`
	ResetsAt         string `json:"resetsAt"`
}

type odPreflight struct {
	WorkspaceID       string          `json:"workspaceId"`
	WorkspaceMemberID string          `json:"workspaceMemberId"`
	Eligible          *bool           `json:"eligible"`
	Windows           []odUsageWindow `json:"windows"`
	BalanceUSD        string          `json:"balanceUsd"`
	CodingPlan        *odPreflight    `json:"codingPlan"`
}

func (s *QuotaService) fetchOpenDesign(ctx context.Context, d *DeepSeek, cfg QuotaConfig, q domain.ChannelQuota) domain.ChannelQuota {
	workspace := strings.TrimSpace(cfg.WorkspaceID)
	if workspace == "" {
		q.Configured = false
		q.Err = "需要 OpenDesign Cloud 的 Control Key 和工作区 ID"
		return q
	}
	key := strings.TrimSpace(cfg.ControlKey)
	if key == "" {
		key = d.key
	}
	headers := map[string]string{
		"Authorization":       "Bearer " + key,
		"x-vela-workspace-id": workspace,
	}
	prefix := "https://amr-api.open-design.ai/api/v1/workspaces/" + url.PathEscape(workspace)
	body, status, err := s.get(ctx, prefix+"/billing/preflight", headers)
	if err != nil {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			q.Err = "OpenDesign Control Key 无效，或没有该工作区权限"
		} else {
			q.Err = "查询 OpenDesign 套餐额度失败：" + err.Error()
		}
		// 套餐端点失败时仍尝试钱包余额。
	}
	if err == nil {
		q = parseOpenDesignPreflight(body, workspace, q)
	}
	if b, _, werr := s.get(ctx, prefix+"/wallet/balance", headers); werr == nil {
		var wallet struct {
			BalanceUSD  string `json:"balanceUsd"`
			WorkspaceID string `json:"workspaceId"`
		}
		if json.Unmarshal(b, &wallet) == nil && (wallet.WorkspaceID == "" || wallet.WorkspaceID == workspace) {
			if v, perr := strconv.ParseFloat(strings.TrimSpace(wallet.BalanceUSD), 64); perr == nil {
				q.Currency = "USD"
				q.Remaining = &v
				if q.Source == "" {
					q.Source = "OpenDesign /wallet/balance"
				}
			}
		}
	}
	if q.Err == "" && len(q.Windows) == 0 && q.Remaining == nil {
		q.Err = "OpenDesign 没有返回额度信息"
	}
	return q
}

func parseOpenDesignPreflight(body []byte, workspace string, q domain.ChannelQuota) domain.ChannelQuota {
	var r odPreflight
	if err := json.Unmarshal(body, &r); err != nil {
		q.Err = "OpenDesign 额度返回格式无法解析"
		return q
	}
	if r.CodingPlan != nil {
		if r.WorkspaceID == "" {
			r.WorkspaceID = r.CodingPlan.WorkspaceID
		}
		if r.Eligible == nil {
			r.Eligible = r.CodingPlan.Eligible
		}
		if len(r.Windows) == 0 {
			r.Windows = r.CodingPlan.Windows
		}
		if r.BalanceUSD == "" {
			r.BalanceUSD = r.CodingPlan.BalanceUSD
		}
	}
	if r.WorkspaceID != "" && r.WorkspaceID != workspace {
		q.Err = "OpenDesign 返回了其他工作区的额度"
		return q
	}
	if r.Eligible != nil && !*r.Eligible && len(r.Windows) == 0 {
		q.Err = "OpenDesign 当前工作区没有生效的套餐"
		return q
	}
	for _, w := range r.Windows {
		used, err1 := strconv.ParseFloat(strings.TrimSpace(w.UsedCredits), 64)
		limit, err2 := strconv.ParseFloat(strings.TrimSpace(w.LimitCredits), 64)
		if err1 != nil || err2 != nil || limit <= 0 {
			continue
		}
		label := ""
		switch w.DurationSeconds {
		case 5 * 60 * 60:
			label = "5 小时"
		case 7 * 24 * 60 * 60:
			label = "7 天"
		default:
			label = fmt.Sprintf("%d 秒窗口", w.DurationSeconds)
		}
		var reset time.Time
		if w.ResetsAt != "" {
			if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
				reset = t
			}
		}
		addQuotaWindow(&q, label, "USD", used, limit, reset)
	}
	if r.BalanceUSD != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(r.BalanceUSD), 64); err == nil {
			q.Remaining = &v
		}
	}
	q.Currency = "USD"
	q.Source = "OpenDesign /billing/preflight"
	return q
}

// ---------- GLM / Z.ai 5h / 7d ----------

type glmQuotaResp struct {
	Code    int    `json:"code"`
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
	Data    struct {
		Level  string `json:"level"`
		Limits []struct {
			Type          string  `json:"type"`
			Unit          int     `json:"unit"`
			Percentage    float64 `json:"percentage"`
			NextResetTime int64   `json:"nextResetTime"`
		} `json:"limits"`
	} `json:"data"`
}

func (s *QuotaService) fetchGLM(ctx context.Context, d *DeepSeek, q domain.ChannelQuota) domain.ChannelQuota {
	host := strings.ToLower(apiOrigin(d.base))
	endpoint := "https://api.z.ai/api/monitor/usage/quota/limit"
	if strings.Contains(host, "bigmodel.cn") {
		endpoint = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"
	}
	// GLM 的额度接口使用原始 Key，不加 Bearer。
	body, _, err := s.get(ctx, endpoint, map[string]string{
		"Authorization":   d.key,
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
	})
	if err != nil {
		q.Err = "查询 GLM 额度失败：" + err.Error()
		return q
	}
	return parseGLMQuota(body, q)
}

func parseGLMQuota(body []byte, q domain.ChannelQuota) domain.ChannelQuota {
	var r glmQuotaResp
	if err := json.Unmarshal(body, &r); err != nil {
		q.Err = "GLM 额度返回格式无法解析"
		return q
	}
	if r.Code != 0 && r.Code != 200 {
		msg := strings.TrimSpace(r.Msg)
		if msg == "" {
			msg = fmt.Sprintf("错误码 %d", r.Code)
		}
		q.Err = "GLM 拒绝了额度查询：" + msg
		return q
	}
	if !r.Success && r.Code == 0 {
		q.Err = "GLM 额度查询未成功"
		return q
	}
	q.Plan = r.Data.Level
	q.Currency = "%"
	seen := make(map[string]bool)
	for _, l := range r.Data.Limits {
		if strings.EqualFold(l.Type, "TIME_LIMIT") {
			continue // 联网搜索等附加额度，不是 5h/7d 模型窗口
		}
		label := "5 小时"
		if l.Unit == 6 {
			label = "7 天"
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		addQuotaWindow(&q, label, "%", l.Percentage, 100, resetFromMillis(l.NextResetTime))
	}
	q.Source = "GLM /api/monitor/usage/quota/limit"
	if len(q.Windows) == 0 {
		q.Err = "GLM 没有返回 5 小时 / 7 天窗口"
	}
	return q
}
