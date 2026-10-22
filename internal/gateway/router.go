package gateway

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// Gateway 对外唯一入口，负责选路、重试、统计。
type Gateway struct {
	registry *Registry
	models   *ModelTable

	threshold int
	openFor   time.Duration
	retries   int

	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	inFlight atomic.Int64
	tokens   atomic.Int64
	latSum   atomic.Int64
	latCount atomic.Int64
	usageMu  sync.RWMutex
	modelUse map[string]int64

	latency *latWindow
}

func New(reg *Registry, models *ModelTable, retries, threshold int, openFor time.Duration) *Gateway {
	return &Gateway{
		registry:  reg,
		models:    models,
		retries:   retries,
		threshold: threshold,
		openFor:   openFor,
		started:   time.Now(),
		modelUse:  make(map[string]int64),
		latency:   newLatWindow(512),
	}
}

// ModelTable 模型注册表，管理员可增删改。
type ModelTable struct {
	mu   sync.RWMutex
	rows map[string]domain.ModelInfo
	// round-robin 游标
	cursor uint64
	// onChange 变更回调（main 用来把模型表落到 data/models.json）。
	// 在锁外调用，回调里可以安全地读 List()。
	onChange func()
}

func NewModelTable(seed []domain.ModelInfo) *ModelTable {
	t := &ModelTable{rows: make(map[string]domain.ModelInfo, len(seed))}
	for _, m := range seed {
		t.rows[m.Key] = m
	}
	return t
}

// SetOnChange 注册变更回调。必须在服务开始接请求前调用一次。
func (t *ModelTable) SetOnChange(fn func()) {
	t.mu.Lock()
	t.onChange = fn
	t.mu.Unlock()
}

func (t *ModelTable) List() []domain.ModelInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]domain.ModelInfo, 0, len(t.rows))
	for _, m := range t.rows {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (t *ModelTable) Get(key string) (domain.ModelInfo, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m, ok := t.rows[key]
	return m, ok
}

func (t *ModelTable) Upsert(m domain.ModelInfo) {
	t.mu.Lock()
	if m.Weight <= 0 {
		m.Weight = 1
	}
	t.rows[m.Key] = m
	onChange := t.onChange
	t.mu.Unlock()
	if onChange != nil {
		onChange()
	}
}

func (t *ModelTable) Delete(key string) bool {
	t.mu.Lock()
	if _, ok := t.rows[key]; !ok {
		t.mu.Unlock()
		return false
	}
	delete(t.rows, key)
	onChange := t.onChange
	t.mu.Unlock()
	if onChange != nil {
		onChange()
	}
	return true
}

// PickAmong 在给定候选里按权重轮询选一个（与 Pick 共用游标）。
// 「同样空闲的渠道之间」用它保持加权公平。
func (t *ModelTable) PickAmong(cands []domain.ModelInfo) domain.ModelInfo {
	if len(cands) == 0 {
		return domain.ModelInfo{}
	}
	if len(cands) == 1 {
		return cands[0]
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	total := 0
	for _, m := range cands {
		total += m.Weight
	}
	if total <= 0 {
		return cands[0]
	}
	t.cursor++
	idx := int(t.cursor % uint64(total))
	acc := 0
	for _, m := range cands {
		acc += m.Weight
		if idx < acc {
			return m
		}
	}
	return cands[len(cands)-1]
}

// Candidates 返回候选模型：指定了 prefer 且存在且启用 → 只返回它；
// 否则返回全部启用模型（按 key 排序，选路确定性）。选哪个由网关按
// 渠道空闲度决定——模型表只管「有哪些可选」。
func (t *ModelTable) Candidates(prefer string) ([]domain.ModelInfo, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if prefer != "" {
		if m, ok := t.rows[prefer]; ok && m.Enabled {
			return []domain.ModelInfo{m}, true
		}
	}
	out := make([]domain.ModelInfo, 0, len(t.rows))
	for _, m := range t.rows {
		if m.Enabled {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, true
}

// Pick 按权重与启用状态挑一个模型。空模型名时用默认。
func (t *ModelTable) Pick(prefer string) (domain.ModelInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if prefer != "" {
		if m, ok := t.rows[prefer]; ok && m.Enabled {
			return m, true
		}
	}

	var candidates []domain.ModelInfo
	total := 0
	for _, m := range t.rows {
		if m.Enabled {
			candidates = append(candidates, m)
			total += m.Weight
		}
	}
	if len(candidates) == 0 {
		return domain.ModelInfo{}, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key < candidates[j].Key })

	t.cursor++
	idx := int(t.cursor % uint64(total))
	acc := 0
	for _, m := range candidates {
		acc += m.Weight
		if idx < acc {
			return m, true
		}
	}
	return candidates[0], true
}

// Stream 执行一次网关调用。返回的 channel 已合并重试与降级。
func (g *Gateway) Stream(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	g.requests.Add(1)
	g.inFlight.Add(1)

	// 选路：会话指定了模型就用它；否则在启用模型里按「渠道空闲度」挑——
	// 先剔除已熔断的渠道，再选在途请求最少的，同样空闲时按权重轮询。
	// 这样四个渠道（官方/中转们）谁空闲走谁，慢的渠道自然少接新请求。
	model, providerName, ok := g.pickModel(req)
	if !ok {
		g.inFlight.Add(-1)
		g.errors.Add(1)
		return nil, ErrNoProvider
	}
	// 在途计数：进入 +1，流结束 -1（选路依据）
	g.registry.BusyAdd(providerName, 1)

	req.Model = model.Upstream
	if req.Model == "" {
		req.Model = model.Key
	}

	out := make(chan domain.Chunk, 32)

	go func() {
		defer g.inFlight.Add(-1)
		defer g.registry.BusyAdd(providerName, -1)
		defer close(out)

		var lastErr error
		for attempt := 0; attempt <= g.retries; attempt++ {
			if !g.registry.Allow(providerName, g.threshold, g.openFor) {
				lastErr = errors.New("provider circuit open: " + providerName)
				break
			}

			start := time.Now()
			stream, err := g.callProvider(ctx, providerName, req)
			if err != nil {
				g.registry.Report(providerName, false, time.Since(start), err.Error(), g.threshold, g.openFor)
				lastErr = err
				logx.Req(ctx).Warn("provider call failed", "provider", providerName, "attempt", attempt, "err", err.Error())
				continue
			}

			// 首块即成功，后续错误由下游处理。
			// forwarded 记录是否已把正文转发给调用方：
			// 一旦转发过半个回答，就不能再整单重试——
			// 第二次回答会接在同一条消息后面，落库变成「半截+完整再来一遍」。
			first := true
			forwarded := false
			broken := false
			for chunk := range stream {
				if chunk.Err != nil {
					lastErr = chunk.Err
					broken = true
					break
				}
				if first {
					g.registry.Report(providerName, true, time.Since(start), "", g.threshold, g.openFor)
					first = false
				}
				if chunk.Usage != nil {
					g.tokens.Add(int64(chunk.Usage.TotalTokens))
					g.usageMu.Lock()
					g.modelUse[model.Key] += int64(chunk.Usage.TotalTokens)
					g.usageMu.Unlock()
				}
				select {
				case out <- chunk:
					if chunk.Content != "" {
						forwarded = true
					}
				case <-ctx.Done():
					return
				}
				if chunk.Done {
					ms := time.Since(start).Milliseconds()
					g.latSum.Add(ms)
					g.latCount.Add(1)
					g.latency.record(ms)
					return
				}
			}

			if !broken {
				ms := time.Since(start).Milliseconds()
				g.latSum.Add(ms)
				g.latCount.Add(1)
				g.latency.record(ms)
				return
			}
			g.registry.Report(providerName, false, time.Since(start), lastErr.Error(), g.threshold, g.openFor)

			// 调用方已收到部分正文，撤不回来：中途断流只能以错误收场，
			// 不再重试。只有一行都没转发出去（对端还没吐过字）时，
			// 重试才是无副作用的。
			if forwarded {
				break
			}
		}

		g.errors.Add(1)
		out <- domain.Chunk{Done: true, Err: lastErr}
	}()

	return out, nil
}

// pickModel 选路：显式指定优先；否则剔除熔断渠道后选在途最少的，
// 同闲按权重轮询。返回选中的模型、provider 名。
func (g *Gateway) pickModel(req domain.ChatRequest) (domain.ModelInfo, string, bool) {
	cands, ok := g.models.Candidates(req.Model)
	if !ok {
		return domain.ModelInfo{}, "", false
	}
	prov := func(m domain.ModelInfo) string {
		if req.Provider != "" {
			return req.Provider
		}
		return m.Provider
	}
	// 会话/直通指定了模型：尊重指定，只校验 provider 已注册，
	// 熔断放行交给后面的 Allow——用户点名要走哪家是他的决定。
	if req.Model != "" && len(cands) == 1 {
		m := cands[0]
		p := prov(m)
		if _, exists := g.registry.Get(p); !exists {
			return domain.ModelInfo{}, "", false
		}
		return m, p, true
	}
	var eligible []domain.ModelInfo
	for _, m := range cands {
		p := prov(m)
		if _, exists := g.registry.Get(p); !exists {
			continue
		}
		// 已熔断的渠道这轮不接新流量（半开探测由 Allow 负责）
		if g.registry.State(p) == "open" {
			continue
		}
		eligible = append(eligible, m)
	}
	// 全部熔断时退回全量候选：让 Allow 报出明确的 circuit open，
	// 比这里静默 no_provider 更可诊断。
	if len(eligible) == 0 {
		eligible = cands
	}
	var minBusy int64 = -1
	for _, m := range eligible {
		if b := g.registry.Busy(prov(m)); minBusy < 0 || b < minBusy {
			minBusy = b
		}
	}
	var idle []domain.ModelInfo
	for _, m := range eligible {
		if g.registry.Busy(prov(m)) == minBusy {
			idle = append(idle, m)
		}
	}
	m := g.models.PickAmong(idle)
	if m.Key == "" {
		return domain.ModelInfo{}, "", false
	}
	return m, prov(m), true
}

func (g *Gateway) callProvider(ctx context.Context, name string, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	p, ok := g.registry.Get(name)
	if !ok {
		return nil, ErrNoProvider
	}
	return p.Chat(ctx, req)
}

// Metrics 汇总指标。
func (g *Gateway) Metrics() domain.Metrics {
	var avg float64
	if n := g.latCount.Load(); n > 0 {
		avg = float64(g.latSum.Load()) / float64(n)
	}
	g.usageMu.RLock()
	usage := make(map[string]int64, len(g.modelUse))
	for k, v := range g.modelUse {
		usage[k] = v
	}
	g.usageMu.RUnlock()

	return domain.Metrics{
		Uptime:       int64(time.Since(g.started).Seconds()),
		Requests:     g.requests.Load(),
		Errors:       g.errors.Load(),
		InFlight:     g.inFlight.Load(),
		TokensUsed:   g.tokens.Load(),
		AvgLatencyMS: avg,
		P95LatencyMS: g.latency.percentile(0.95),
		Providers:    g.registry.Stats(),
		ModelUsage:   usage,
	}
}

// Registry 暴露给管理接口。
func (g *Gateway) Registry() *Registry { return g.registry }

// Models 暴露给管理接口。
func (g *Gateway) Models() *ModelTable { return g.models }
