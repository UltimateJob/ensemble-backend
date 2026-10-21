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
	}
}

// ModelTable 模型注册表，管理员可增删改。
type ModelTable struct {
	mu   sync.RWMutex
	rows map[string]domain.ModelInfo
	// round-robin 游标
	cursor uint64
}

func NewModelTable(seed []domain.ModelInfo) *ModelTable {
	t := &ModelTable{rows: make(map[string]domain.ModelInfo)}
	for _, m := range seed {
		t.rows[m.Key] = m
	}
	return t
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
	defer t.mu.Unlock()
	if m.Weight <= 0 {
		m.Weight = 1
	}
	t.rows[m.Key] = m
}

func (t *ModelTable) Delete(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.rows[key]; !ok {
		return false
	}
	delete(t.rows, key)
	return true
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

	model, ok := g.models.Pick(req.Model)
	if !ok {
		g.inFlight.Add(-1)
		g.errors.Add(1)
		return nil, ErrNoProvider
	}

	providerName := req.Provider
	if providerName == "" {
		providerName = model.Provider
	}
	if _, exists := g.registry.Get(providerName); !exists {
		g.inFlight.Add(-1)
		g.errors.Add(1)
		return nil, ErrNoProvider
	}

	req.Model = model.Upstream
	if req.Model == "" {
		req.Model = model.Key
	}

	out := make(chan domain.Chunk, 32)

	go func() {
		defer g.inFlight.Add(-1)
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

			// 首块即成功，后续错误由下游处理
			first := true
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
				case <-ctx.Done():
					return
				}
				if chunk.Done {
					g.latSum.Add(time.Since(start).Milliseconds())
					g.latCount.Add(1)
					return
				}
			}

			if !broken {
				g.latSum.Add(time.Since(start).Milliseconds())
				g.latCount.Add(1)
				return
			}
			g.registry.Report(providerName, false, time.Since(start), lastErr.Error(), g.threshold, g.openFor)
		}

		g.errors.Add(1)
		out <- domain.Chunk{Done: true, Err: lastErr}
	}()

	return out, nil
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
		P95LatencyMS: avg * 1.8, // 首版用均值估算，接入直方图后替换
		Providers:    g.registry.Stats(),
		ModelUsage:   usage,
	}
}

// Registry 暴露给管理接口。
func (g *Gateway) Registry() *Registry { return g.registry }

// Models 暴露给管理接口。
func (g *Gateway) Models() *ModelTable { return g.models }
