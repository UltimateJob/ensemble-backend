package service

import (
	"sort"
	"sync"
	"time"
)

// SlowEntry 一次「慢」的记录。
//
// 平均延迟和 P95 只告诉你「整体快不快」，
// 但排查问题时需要知道「谁慢、慢在哪、当时在干什么」。
type SlowEntry struct {
	Time      time.Time `json:"time"`
	ConvID    string    `json:"convId"`
	ConvTitle string    `json:"convTitle"`
	Agent     string    `json:"agent"`
	LatencyMS int64     `json:"latencyMs"`
	Tokens    int       `json:"tokens"`
	Model     string    `json:"model,omitempty"`
	Err       string    `json:"err,omitempty"`
}

// SlowLog 固定容量的环形缓冲。只保留最近 N 条，不落盘——
// 它的用途是「刚出问题时看一眼」，不是长期审计。
type SlowLog struct {
	mu        sync.Mutex
	buf       []SlowEntry
	capacity  int
	next      int
	filled    bool
	threshold int64 // 毫秒，超过才记
	recorded  int64 // 累计条数（含已滚出的）
}

func NewSlowLog(capacity int, thresholdMS int64) *SlowLog {
	if capacity <= 0 {
		capacity = 200
	}
	if thresholdMS <= 0 {
		thresholdMS = 10000
	}
	return &SlowLog{
		buf:       make([]SlowEntry, capacity),
		capacity:  capacity,
		threshold: thresholdMS,
	}
}

// MaybeRecord 超过阈值才记。返回是否记录。
func (l *SlowLog) MaybeRecord(e SlowEntry) bool {
	if l == nil {
		return false
	}
	if e.LatencyMS < l.threshold && e.Err == "" {
		return false
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	l.mu.Lock()
	l.buf[l.next] = e
	l.next++
	if l.next >= l.capacity {
		l.next = 0
		l.filled = true
	}
	l.recorded++
	l.mu.Unlock()
	return true
}

// List 最近 n 条，按时间倒序。
func (l *SlowLog) List(n int) []SlowEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	items := make([]SlowEntry, 0, l.capacity)
	size := l.next
	if l.filled {
		size = l.capacity
	}
	for i := 0; i < size; i++ {
		items = append(items, l.buf[i])
	}
	l.mu.Unlock()

	sort.Slice(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
	if n > 0 && len(items) > n {
		items = items[:n]
	}
	return items
}

// Stats 给运维台用。
func (l *SlowLog) Stats() (total int64, threshold int64) {
	if l == nil {
		return 0, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.recorded, l.threshold
}
