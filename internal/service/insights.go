package service

import (
	"sort"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// SeriesPoint 一分钟粒度的运行指标。
//
// 累计计数器只能回答「到现在为止」，运维排障需要的是
// 「刚才那一分钟发生了什么」。Requests/Errors/Tokens 都是区间增量，
// 延迟是该区间的平均值（按请求数加权），P95 取区间峰值。
type SeriesPoint struct {
	TS           int64   `json:"ts"` // 毫秒时间戳
	Requests     int64   `json:"requests"`
	Errors       int64   `json:"errors"`
	Tokens       int64   `json:"tokens"`
	InFlight     int64   `json:"inFlight"`
	ActiveUsers  int     `json:"activeUsers"`
	AvgLatencyMS float64 `json:"avgLatencyMs"`
	P95LatencyMS float64 `json:"p95LatencyMs"`
}

const (
	seriesRetention = 24 * time.Hour
	seriesInterval  = time.Minute
)

// Insights 在进程内保留最近 24 小时的指标序列。
// 每分钟采样一次，数据量固定（约 1440 个点），不落盘：
// 它的用途是趋势和告警，不是长期计费审计。
type Insights struct {
	mu   sync.Mutex
	gw   *gateway.Gateway
	st   store.Store
	pts  []SeriesPoint
	last domain.Metrics

	lastLatSum   int64
	lastLatCount int64
	seeded       bool
	started      bool
}

func NewInsights(gw *gateway.Gateway, st store.Store) *Insights {
	return &Insights{gw: gw, st: st}
}

// Start 启动后台采样。重复调用无副作用。
func (in *Insights) Start(interval time.Duration) {
	if in == nil || in.gw == nil {
		return
	}
	if interval <= 0 {
		interval = seriesInterval
	}
	in.mu.Lock()
	if in.started {
		in.mu.Unlock()
		return
	}
	in.started = true
	in.mu.Unlock()

	// 先立刻采一次作为基线，之后按间隔记增量。
	in.sampleOnce()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			in.sampleOnce()
		}
	}()
}

func (in *Insights) sampleOnce() {
	m := in.gw.Metrics()
	sum, count := in.gw.LatencyTotals()
	now := time.Now()

	in.mu.Lock()
	defer in.mu.Unlock()

	if !in.seeded {
		in.last = m
		in.lastLatSum, in.lastLatCount = sum, count
		in.seeded = true
		return
	}

	// 计数器只在重启时归零；出现负数说明进程刚重启，按当前值重算。
	delta := func(cur, prev int64) int64 {
		if cur < prev {
			return cur
		}
		return cur - prev
	}
	avg := m.AvgLatencyMS
	if ds, dc := sum-in.lastLatSum, count-in.lastLatCount; ds > 0 && dc > 0 {
		avg = float64(ds) / float64(dc)
	}

	active := 0
	if in.st != nil {
		cut := now.Add(-24 * time.Hour)
		for _, u := range in.st.ListUsers() {
			if u.LastSeen.After(cut) {
				active++
			}
		}
	}

	p := SeriesPoint{
		TS:           now.UnixMilli(),
		Requests:     delta(m.Requests, in.last.Requests),
		Errors:       delta(m.Errors, in.last.Errors),
		Tokens:       delta(m.TokensUsed, in.last.TokensUsed),
		InFlight:     m.InFlight,
		ActiveUsers:  active,
		AvgLatencyMS: avg,
		P95LatencyMS: m.P95LatencyMS,
	}
	in.last = m
	in.lastLatSum, in.lastLatCount = sum, count
	in.pts = append(in.pts, p)

	// 丢掉超过保留期的点。切片有序，剪掉开头一段即可。
	cut := now.Add(-seriesRetention).UnixMilli()
	i := 0
	for i < len(in.pts) && in.pts[i].TS < cut {
		i++
	}
	if i > 0 {
		in.pts = append(in.pts[:0], in.pts[i:]...)
	}
}

// Series 返回最近 hours 小时的指标，聚合成不超过 maxPoints 个点。
// 24 小时默认 5 分钟一个桶，1 小时保持 1 分钟粒度。
func (in *Insights) Series(hours, maxPoints int) []SeriesPoint {
	if in == nil {
		return []SeriesPoint{}
	}
	if hours <= 0 {
		hours = 24
	}
	if hours > 24 {
		hours = 24
	}
	if maxPoints <= 0 {
		maxPoints = 240
	}
	from := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()

	in.mu.Lock()
	pts := make([]SeriesPoint, len(in.pts))
	copy(pts, in.pts)
	in.mu.Unlock()

	return aggregateSeries(pts, from, time.Duration(hours)*time.Hour, maxPoints)
}

// aggregateSeries 把有序的分钟点合并成桶。计数器求和，延迟按请求数加权，
// P95 取桶内最大值（峰值比平均值更值得被看见）。
func aggregateSeries(points []SeriesPoint, from int64, span time.Duration, maxPoints int) []SeriesPoint {
	if maxPoints <= 0 {
		maxPoints = 240
	}
	// 时间戳统一用毫秒。span 是 Duration（纳秒），换算后再分桶。
	spanMS := span.Milliseconds()
	bucket := int64(time.Minute / time.Millisecond)
	if spanMS > 0 {
		if want := spanMS / int64(maxPoints); want > bucket {
			// 向上取整到整分钟，避免 5 分钟图表出现 4:37 这种刻度。
			bucket = (want + int64(time.Minute/time.Millisecond) - 1) /
				int64(time.Minute/time.Millisecond) * int64(time.Minute/time.Millisecond)
		}
	}

	type acc struct {
		p      SeriesPoint
		latSum float64
		latN   int
		latReq int64
		latAvg float64
	}
	buckets := make(map[int64]*acc)
	var keys []int64
	for _, pt := range points {
		if pt.TS < from {
			continue
		}
		key := pt.TS / bucket * bucket
		a, exists := buckets[key]
		if !exists {
			a = &acc{p: SeriesPoint{TS: key}}
			buckets[key] = a
			keys = append(keys, key)
		}
		a.p.Requests += pt.Requests
		a.p.Errors += pt.Errors
		a.p.Tokens += pt.Tokens
		a.p.InFlight = pt.InFlight
		a.p.ActiveUsers = pt.ActiveUsers
		if pt.P95LatencyMS > a.p.P95LatencyMS {
			a.p.P95LatencyMS = pt.P95LatencyMS
		}
		if pt.Requests > 0 {
			a.latAvg += pt.AvgLatencyMS * float64(pt.Requests)
			a.latReq += pt.Requests
		} else {
			a.latSum += pt.AvgLatencyMS
			a.latN++
		}
	}

	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]SeriesPoint, 0, len(keys))
	for _, k := range keys {
		a := buckets[k]
		if a.latReq > 0 {
			a.p.AvgLatencyMS = a.latAvg / float64(a.latReq)
		} else if a.latN > 0 {
			a.p.AvgLatencyMS = a.latSum / float64(a.latN)
		}
		out = append(out, a.p)
	}
	return out
}

// Summary 给管理台的一次性摘要：最近一个采样点 + 保留窗口。
type InsightsSummary struct {
	Latest        *SeriesPoint `json:"latest,omitempty"`
	IntervalSec   int          `json:"intervalSec"`
	RetentionHour int          `json:"retentionHours"`
}

func (in *Insights) Summary() InsightsSummary {
	out := InsightsSummary{IntervalSec: int(seriesInterval.Seconds()), RetentionHour: int(seriesRetention.Hours())}
	if in == nil {
		return out
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if len(in.pts) > 0 {
		p := in.pts[len(in.pts)-1]
		out.Latest = &p
	}
	return out
}
