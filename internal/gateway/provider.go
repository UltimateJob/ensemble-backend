// Package gateway 是模型网关：统一入口、选路、熔断、重试、计费统计。
//
// 设计要点：
//   - Provider 是接口，DeepSeek 是首版实现，新增厂商只需实现 Chat。
//   - Router 负责在多个 Provider 之间选路，并维护熔断状态。
//   - 上层不感知具体厂商，只面向 domain.ChatRequest。
package gateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// Provider 一个模型上游。
type Provider interface {
	// Name 唯一名称，如 "deepseek"。
	Name() string
	// Chat 发起对话。返回的 channel 由调用方消费，错误时 channel 会带 Err 关闭。
	Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error)
}

// ErrNoProvider 没有可用上游。
var ErrNoProvider = errors.New("gateway: no healthy provider")

// Registry 保存所有上游及其健康状态。
type Registry struct {
	mu        sync.RWMutex
	providers map[string]*entry
}

type entry struct {
	p Provider

	mu       sync.Mutex
	state    string // closed | open | half-open
	fails    int
	openTill time.Time

	success  atomic.Int64
	failure  atomic.Int64
	latSum   atomic.Int64 // 累计毫秒
	latCount atomic.Int64
	lastErr  atomic.Value // string
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]*entry)}
}

func (r *Registry) Add(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = &entry{p: p, state: "closed"}
}

func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.providers[name]
	if !ok {
		return nil, false
	}
	return e.p, true
}

// Allow 熔断判定：closed 放行；open 到期转 half-open 放行一个；half-open 放行试探。
func (r *Registry) Allow(name string, threshold int, openFor time.Duration) bool {
	r.mu.RLock()
	e, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	switch e.state {
	case "closed":
		return true
	case "open":
		if time.Now().After(e.openTill) {
			e.state = "half-open"
			return true
		}
		return false
	default: // half-open：只放一个
		return true
	}
}

// Report 上报一次调用结果。
func (r *Registry) Report(name string, ok bool, d time.Duration, errMsg string, threshold int, openFor time.Duration) {
	r.mu.RLock()
	e, exists := r.providers[name]
	r.mu.RUnlock()
	if !exists {
		return
	}

	if ok {
		e.success.Add(1)
	} else {
		e.failure.Add(1)
		if errMsg != "" {
			e.lastErr.Store(errMsg)
		}
	}
	if d > 0 {
		e.latSum.Add(d.Milliseconds())
		e.latCount.Add(1)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if ok {
		e.fails = 0
		e.state = "closed"
		return
	}
	e.fails++
	if e.fails >= threshold {
		e.state = "open"
		e.openTill = time.Now().Add(openFor)
	}
}

// Stats 返回各上游状态快照。
func (r *Registry) Stats() []domain.ProviderStat {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.ProviderStat, 0, len(r.providers))
	for name, e := range r.providers {
		e.mu.Lock()
		st := e.state
		e.mu.Unlock()

		var avg float64
		if n := e.latCount.Load(); n > 0 {
			avg = float64(e.latSum.Load()) / float64(n)
		}
		lastErr, _ := e.lastErr.Load().(string)
		out = append(out, domain.ProviderStat{
			Name:      name,
			State:     st,
			Success:   e.success.Load(),
			Failure:   e.failure.Load(),
			LatencyMS: avg,
			LastErr:   lastErr,
		})
	}
	return out
}

// Reset 手动恢复某个上游（管理员操作）。
func (r *Registry) Reset(name string) bool {
	r.mu.RLock()
	e, ok := r.providers[name]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	e.mu.Lock()
	e.fails = 0
	e.state = "closed"
	e.mu.Unlock()
	return true
}

// Names 列出所有上游名。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for n := range r.providers {
		out = append(out, n)
	}
	return out
}
