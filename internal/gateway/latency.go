package gateway

import (
	"sort"
	"sync"
)

// latWindow 滑动窗口延迟统计。
//
// 保留最近 capacity 个样本，算分位数时复制一份排序。
// 采样只在每次请求结束时发生一次，成本可忽略；
// 真实分位数比「均值 × 1.8」这种估算法靠谱得多。
type latWindow struct {
	mu       sync.Mutex
	buf      []int64
	capacity int
	next     int
	filled   bool
}

func newLatWindow(capacity int) *latWindow {
	if capacity <= 0 {
		capacity = 512
	}
	return &latWindow{buf: make([]int64, capacity), capacity: capacity}
}

func (w *latWindow) record(ms int64) {
	if ms < 0 {
		return
	}
	w.mu.Lock()
	w.buf[w.next] = ms
	w.next++
	if w.next >= w.capacity {
		w.next = 0
		w.filled = true
	}
	w.mu.Unlock()
}

// snapshot 复制当前样本。
func (w *latWindow) snapshot() []int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := w.next
	if w.filled {
		n = w.capacity
	}
	out := make([]int64, n)
	copy(out, w.buf[:n])
	return out
}

// percentile 返回 p 分位（0~1）。空窗口返回 0。
func (w *latWindow) percentile(p float64) float64 {
	s := w.snapshot()
	if len(s) == 0 {
		return 0
	}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })

	if p <= 0 {
		return float64(s[0])
	}
	if p >= 1 {
		return float64(s[len(s)-1])
	}
	idx := int(float64(len(s)-1) * p)
	return float64(s[idx])
}

// count 当前窗口内的样本数。
func (w *latWindow) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.filled {
		return w.capacity
	}
	return w.next
}
