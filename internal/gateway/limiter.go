package gateway

import (
	"sync"
	"time"
)

// Limiter 每用户令牌桶，防止单个账号打满上游。
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // 每秒补充
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewLimiter(perMinute, burst int) *Limiter {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = perMinute
	}
	l := &Limiter{
		buckets: make(map[string]*bucket),
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
	}
	go l.gc()
	return l
}

// Allow 消耗一个令牌。
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	now := time.Now()
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gc 清理长时间不活跃的桶，防止内存增长。
func (l *Limiter) gc() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		l.mu.Lock()
		cut := time.Now().Add(-30 * time.Minute)
		for k, b := range l.buckets {
			if b.last.Before(cut) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
