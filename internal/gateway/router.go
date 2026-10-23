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
// 否则按请求类型分池——任务请求只在 TaskOnly 模型里挑，聊天请求
// 跳过 TaskOnly（聊天默认只走对话模型，任务专用渠道不掺和）。
// 对应池为空时回退全部启用模型：标错池不该让请求直接失败。
func (t *ModelTable) Candidates(prefer string, task bool) ([]domain.ModelInfo, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if prefer != "" {
		if m, ok := t.rows[prefer]; ok && m.Enabled {
			return []domain.ModelInfo{m}, true
		}
	}
	var out, other []domain.ModelInfo
	for _, m := range t.rows {
		if !m.Enabled {
			continue
		}
		if m.TaskOnly == task {
			out = append(out, m)
		} else {
			other = append(other, m)
		}
	}
	if len(out) == 0 {
		out = other
	}
	// 按排名排序：rank 小优先；未设置（0）排在有排名的之后，
	// 同 rank 内按 key 稳定排序，再由 Weight 做加权轮询。
	sortByRank(out)
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// sortByRank 按 rank 升序排列；rank=0 视为「未指定」，排在有 rank 的后面。
func sortByRank(list []domain.ModelInfo) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if (a.Rank == 0) != (b.Rank == 0) {
			return a.Rank != 0
		}
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		return a.Key < b.Key
	})
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
	// 用户原始点名的模型要在改写前记下来：改写后是上游模型名，
	// 拿它去找备选渠道只会找到同一个渠道。
	prefer := req.Model
	model, providerName, ok := g.pickModel(req)
	if !ok {
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

		// 故障转移：同一条请求在某个渠道失败时，换一个渠道再试，
		// 而不是在同一个已经出问题的上游上重试。
		// 这是「保底渠道」真正的实现位置：渠道池里有健康的上游就不该失败。
		tried := map[string]bool{}
		curProvider := providerName
		curModel := model
		var lastErr error
		for attempt := 0; attempt <= g.retries; attempt++ {
			if !g.registry.Allow(curProvider, g.threshold, g.openFor) {
				lastErr = errors.New("provider circuit open: " + curProvider)
				// 熔断不代表没有别的渠道：换一个继续。
				if next, nm, ok := g.failoverModel(prefer, req.Task, tried); ok {
					tried[curProvider] = true
					curProvider, curModel = next, nm
					continue
				}
				break
			}
			req.Model = curModel.Upstream
			if req.Model == "" {
				req.Model = curModel.Key
			}
			model = curModel
			// 在途计数：每次尝试进入 +1，结束 -1（选路依据）。
			// 用闭包保证「每条退出路径都只减一次」——之前成功分支
			// 直接 return，计数只加不减，跑满并发上限后所有渠道都被
			// 判定为饱和，整个网关会开始返回 no provider。
			g.registry.BusyAdd(curProvider, 1)
			busyReleased := false
			release := func() {
				if !busyReleased {
					busyReleased = true
					g.registry.BusyAdd(curProvider, -1)
				}
			}

			start := time.Now()
			stream, err := g.callProvider(ctx, curProvider, req)
			if err != nil {
				release()
				g.registry.Report(curProvider, false, time.Since(start), err.Error(), g.threshold, g.openFor)
				lastErr = err
				logx.Req(ctx).Warn("provider call failed", "provider", curProvider, "attempt", attempt, "err", err.Error())
				// 换渠道：同一上游已经明确失败，重试它只会再失败一次。
				if next, nm, ok := g.failoverModel(prefer, req.Task, tried); ok {
					tried[curProvider] = true
					curProvider, curModel = next, nm
				}
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
					g.registry.Report(curProvider, true, time.Since(start), "", g.threshold, g.openFor)
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
					release()
					return
				}
				if chunk.Done {
					release()
					ms := time.Since(start).Milliseconds()
					g.latSum.Add(ms)
					g.latCount.Add(1)
					g.latency.record(ms)
					return
				}
			}

			release()
			if !broken {
				ms := time.Since(start).Milliseconds()
				g.latSum.Add(ms)
				g.latCount.Add(1)
				g.latency.record(ms)
				return
			}
			g.registry.Report(curProvider, false, time.Since(start), lastErr.Error(), g.threshold, g.openFor)
			if next, nm, ok := g.failoverModel(prefer, req.Task, tried); ok {
				tried[curProvider] = true
				curProvider, curModel = next, nm
			}

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

// failoverModel 在当前渠道失败后挑下一个可用渠道。
//
// 与 pickModel 的区别：它排除本次已经试过的 provider，并且优先选
// 在途最少、未熔断的渠道。没有别的渠道时返回 false，调用方如实报错。
func (g *Gateway) failoverModel(prefer string, task bool, tried map[string]bool) (string, domain.ModelInfo, bool) {
	cands, ok := g.models.Candidates(prefer, task)
	if !ok {
		return "", domain.ModelInfo{}, false
	}
	// 用户点名了某个模型时 Candidates 只返回它。点名的渠道挂了仍然
	// 应该走保底：把整个启用池作为后备候选。用户的选择表达的是偏好，
	// 不是「宁可失败也不换」。
	if prefer != "" && len(cands) == 1 {
		if pool, ok := g.models.Candidates("", task); ok && len(pool) > 1 {
			cands = pool
		}
	}
	var best *domain.ModelInfo
	var bestBusy int64 = -1
	for i := range cands {
		m := cands[i]
		p := m.Provider
		if p == "" || tried[p] {
			continue
		}
		if _, exists := g.registry.Get(p); !exists {
			continue
		}
		if g.registry.State(p) == "open" {
			continue
		}
		if b := g.registry.Busy(p); bestBusy < 0 || b < bestBusy {
			bestBusy = b
			best = &m
		}
	}
	if best == nil {
		return "", domain.ModelInfo{}, false
	}
	return best.Provider, *best, true
}

// pickModel 选路策略（按优先级依次判定）：
//
//  1. 排名：候选按 Rank 升序，只有排名最靠前的那一档「有容量」时才用下一档。
//     这样官方渠道排第 1 就永远先走官方，中转只做保底。
//  2. 档内轮询：同排名的渠道按 Weight 加权轮询，保证流量按比例分配。
//  3. 动态负载：档内优先选在途最少的；当整档都达到并发上限
//     （capacity = max(1, weight)）时，自动溢出到下一档——
//     高并发时不会把请求全堆在一个渠道上等排队。
//  4. 熔断渠道不接新流量；全部熔断时退回全量候选，让 Allow 给出明确错误。
func (g *Gateway) pickModel(req domain.ChatRequest) (domain.ModelInfo, string, bool) {
	cands, ok := g.models.Candidates(req.Model, req.Task)
	if !ok {
		return domain.ModelInfo{}, "", false
	}
	prov := func(m domain.ModelInfo) string {
		if req.Provider != "" {
			return req.Provider
		}
		return m.Provider
	}
	// 会话/直通指定了模型：尊重指定，只校验 provider 已注册。
	// 熔断放行交给后面的 Allow——用户点名要走哪家是他的决定；
	// 真挂了会由 Stream 里的 failoverModel 换到保底。
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
		if g.registry.State(p) == "open" {
			continue
		}
		eligible = append(eligible, m)
	}
	if len(eligible) == 0 {
		// 全部熔断时退回全量候选：让 Allow 报出明确的 circuit open，
		// 比这里静默 no_provider 更可诊断。
		eligible = cands
	}
	// 按排名分档，从最好的一档开始；整档饱和才溢出到下一档。
	for len(eligible) > 0 {
		rank := eligible[0].Rank
		tier := make([]domain.ModelInfo, 0, len(eligible))
		rest := make([]domain.ModelInfo, 0, len(eligible))
		for _, m := range eligible {
			if m.Rank == rank {
				tier = append(tier, m)
			} else {
				rest = append(rest, m)
			}
		}
		if pick, ok := pickInTier(tier, g, prov); ok {
			return pick, prov(pick), true
		}
		eligible = rest
	}
	return domain.ModelInfo{}, "", false
}

// defaultChannelConcurrency 单个渠道的默认并发上限。
// 超过它才认为这一档饱和、可以溢出到下一档；同档内仍然按最闲分配。
const defaultChannelConcurrency = 8

// pickInTier 在一档同排名的渠道里选一个。
//
// 先过滤掉达到并发上限的渠道：档内还有空位就选最闲的，
// 同等空闲再按权重轮询；整档都满返回 false，让调用方溢出到下一档。
func pickInTier(tier []domain.ModelInfo, g *Gateway, prov func(domain.ModelInfo) string) (domain.ModelInfo, bool) {
	if len(tier) == 0 {
		return domain.ModelInfo{}, false
	}
	var free []domain.ModelInfo
	minBusy := int64(-1)
	for _, m := range tier {
		b := g.registry.Busy(prov(m))
		// 并发上限是「渠道能同时扛多少路流」，不是权重。
		// 权重只决定同档内的流量比例；用权重当上限会让 weight=1 的
		// 渠道在第二路请求上就被判定饱和，把流量错误地挤到低优先档。
		cap := int64(m.Concurrency)
		if cap <= 0 {
			cap = defaultChannelConcurrency
		}
		if b < cap {
			free = append(free, m)
		}
		if minBusy < 0 || b < minBusy {
			minBusy = b
		}
	}
	if len(free) == 0 {
		return domain.ModelInfo{}, false
	}
	var idle []domain.ModelInfo
	for _, m := range free {
		if g.registry.Busy(prov(m)) == minBusy {
			idle = append(idle, m)
		}
	}
	if len(idle) == 0 {
		idle = free
	}
	m := g.models.PickAmong(idle)
	if m.Key == "" {
		m = idle[0]
	}
	return m, true
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
