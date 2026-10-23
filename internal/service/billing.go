package service

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// UsageAgg 一段用量聚合。成本统一按人民币计。
type UsageAgg struct {
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	CostCNY          float64 `json:"cost"`
}

func (a UsageAgg) TotalTokens() int64 { return a.PromptTokens + a.CompletionTokens }

func (a *UsageAgg) add(prompt, completion int64, cost float64) {
	a.Requests++
	a.PromptTokens += prompt
	a.CompletionTokens += completion
	a.CostCNY += cost
}

// UserUsage 单个用户的累计用量与本月用量。
type UserUsage struct {
	UserID string `json:"userId"`
	UsageAgg
	// MonthKey / DayKey 用于跨月、跨日自动清零，不进接口。
	MonthKey string    `json:"-"`
	DayKey   string    `json:"-"`
	Month    UsageAgg  `json:"month"`
	Today    UsageAgg  `json:"today"`
	LastAt   time.Time `json:"lastAt"`
}

// ModelUsage 单个模型的累计用量与本月用量。
type ModelUsage struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
	UsageAgg
	MonthKey string   `json:"-"`
	Month    UsageAgg `json:"month"`
}

// UsageRecord 一条可追溯的调用记录，只在内存里保留最近若干条。
type UsageRecord struct {
	At               time.Time `json:"at"`
	UserID           string    `json:"userId,omitempty"`
	Model            string    `json:"model"`
	Provider         string    `json:"provider,omitempty"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	CostCNY          float64   `json:"cost"`
}

// DayUsage 按自然日聚合的成本，用于趋势图。
type DayUsage struct {
	Date string `json:"date"`
	UsageAgg
}

// BillingStats 成本页需要的聚合结果。
type BillingStats struct {
	Since  time.Time
	Totals UsageAgg
	Today  UsageAgg
	Month  UsageAgg
	Days   []DayUsage
	Models []ModelUsage
	Users  []UserUsage
	Recent []UsageRecord
}

// BillingStatus 给系统页展示的轻量状态。
type BillingStatus struct {
	Since     time.Time `json:"since"`
	Currency  string    `json:"currency"`
	Requests  int64     `json:"requests"`
	CostCNY   float64   `json:"cost"`
	Recent    int       `json:"recent"`
	LastSaved string    `json:"lastSaved,omitempty"`
}

// Billing 上游成本账本。
//
// 说明：这里统计的是「我们向上游付了多少钱」，不是向用户收的钱。
// 用户收入由套餐价格汇总得到，见 Admin.Commerce。
// 数据按自然日/月聚合，最近 300 条明细留在内存；落盘到
// data/billing.json，重启后保留累计值。
type Billing struct {
	mu        sync.RWMutex
	path      string
	startedAt time.Time
	totals    UsageAgg
	months    map[string]*UsageAgg
	days      map[string]*UsageAgg
	users     map[string]*UserUsage
	models    map[string]*ModelUsage
	recent    []UsageRecord
	dirty     bool
	lastSaved time.Time

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
}

type billingSnapshot struct {
	Version   int                    `json:"version"`
	StartedAt time.Time              `json:"startedAt"`
	Totals    UsageAgg               `json:"totals"`
	Months    map[string]*UsageAgg   `json:"months,omitempty"`
	Days      map[string]*UsageAgg   `json:"days,omitempty"`
	Users     map[string]*UserUsage  `json:"users,omitempty"`
	Models    map[string]*ModelUsage `json:"models,omitempty"`
	Recent    []UsageRecord          `json:"recent,omitempty"`
}

// NewBilling 打开成本账本。文件损坏时隔离坏文件并按空账本启动——
// 成本是运营数据，不该像用户数据那样阻断服务启动。
func NewBilling(path string) (*Billing, error) {
	b := &Billing{
		path:      path,
		startedAt: time.Now(),
		months:    make(map[string]*UsageAgg),
		days:      make(map[string]*UsageAgg),
		users:     make(map[string]*UserUsage),
		models:    make(map[string]*ModelUsage),
		stop:      make(chan struct{}),
	}
	if path == "" {
		return b, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		var snap billingSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			quarantine := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
			_ = os.Rename(path, quarantine)
			logx.L().Warn("成本账本损坏，已隔离并按空账本启动",
				"file", path, "隔离为", quarantine, "err", err.Error())
		} else {
			b.restore(snap)
		}
	}
	return b, nil
}

func (b *Billing) restore(snap billingSnapshot) {
	if !snap.StartedAt.IsZero() {
		b.startedAt = snap.StartedAt
	}
	b.totals = snap.Totals
	if snap.Months != nil {
		b.months = snap.Months
	}
	if snap.Days != nil {
		b.days = snap.Days
	}
	if snap.Users != nil {
		b.users = snap.Users
	}
	if snap.Models != nil {
		b.models = snap.Models
	}
	b.recent = snap.Recent
	if len(b.recent) > 300 {
		b.recent = b.recent[len(b.recent)-300:]
	}
}

// Start 启动后台落盘。path 为空时是纯内存账本。
func (b *Billing) Start() {
	b.startOnce.Do(func() {
		if b.path == "" {
			return
		}
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-b.stop:
					return
				case <-t.C:
					if err := b.Flush(); err != nil {
						logx.L().Warn("成本账本落盘失败", "err", err.Error())
					}
				}
			}
		}()
	})
}

// Close 停止后台落盘并做最后一次写入。
func (b *Billing) Close() error {
	b.stopOnce.Do(func() { close(b.stop) })
	return b.Flush()
}

// Record 记录一次上游调用的用量。由 Gateway 在收到 usage 时调用。
func (b *Billing) Record(ev domain.UsageEvent) {
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	prompt := maxInt(ev.PromptTokens, 0)
	completion := maxInt(ev.CompletionTokens, 0)
	if prompt == 0 && completion == 0 {
		completion = maxInt(ev.TotalTokens, 0)
	}
	cost := ev.CostCNY
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		cost = 0
	}
	userID := ev.UserID
	if userID == "" {
		// 后台任务没有登录用户时也要计入总成本，但不计入某个用户。
		userID = "system"
	}
	monthKey := at.Format("2006-01")
	dayKey := at.Format("2006-01-02")

	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensureLocked()
	b.totals.add(int64(prompt), int64(completion), cost)
	m := b.months[monthKey]
	if m == nil {
		m = &UsageAgg{}
		b.months[monthKey] = m
	}
	m.add(int64(prompt), int64(completion), cost)
	d := b.days[dayKey]
	if d == nil {
		d = &UsageAgg{}
		b.days[dayKey] = d
	}
	d.add(int64(prompt), int64(completion), cost)

	u := b.users[userID]
	if u == nil {
		u = &UserUsage{UserID: userID}
		b.users[userID] = u
	}
	if u.MonthKey != monthKey {
		u.MonthKey = monthKey
		u.Month = UsageAgg{}
	}
	if u.DayKey != dayKey {
		u.DayKey = dayKey
		u.Today = UsageAgg{}
	}
	u.UsageAgg.add(int64(prompt), int64(completion), cost)
	u.Month.add(int64(prompt), int64(completion), cost)
	u.Today.add(int64(prompt), int64(completion), cost)
	u.LastAt = at

	modelKey := ev.Model
	if modelKey == "" {
		modelKey = "unknown"
	}
	mu := b.models[modelKey]
	if mu == nil {
		mu = &ModelUsage{Model: modelKey}
		b.models[modelKey] = mu
	}
	mu.Provider = ev.Provider
	if mu.MonthKey != monthKey {
		mu.MonthKey = monthKey
		mu.Month = UsageAgg{}
	}
	mu.UsageAgg.add(int64(prompt), int64(completion), cost)
	mu.Month.add(int64(prompt), int64(completion), cost)

	b.recent = append(b.recent, UsageRecord{
		At: at, UserID: userID, Model: modelKey, Provider: ev.Provider,
		PromptTokens: prompt, CompletionTokens: completion, CostCNY: cost,
	})
	if len(b.recent) > 300 {
		b.recent = b.recent[len(b.recent)-300:]
	}
	b.pruneLocked(at)
	b.dirty = true
}

// Stats 返回成本页聚合。days 控制趋势图天数（7–90）。
func (b *Billing) Stats(days int) BillingStats {
	if days < 7 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	now := time.Now()
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := BillingStats{
		Since:  b.startedAt,
		Totals: b.totals,
		Today:  b.dayLocked(now),
		Month:  b.monthLocked(now),
	}
	out.Days = make([]DayUsage, 0, days)
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		key := d.Format("2006-01-02")
		agg := UsageAgg{}
		if v := b.days[key]; v != nil {
			agg = *v
		}
		out.Days = append(out.Days, DayUsage{Date: key, UsageAgg: agg})
	}
	out.Models = make([]ModelUsage, 0, len(b.models))
	for _, v := range b.models {
		out.Models = append(out.Models, *v)
	}
	sort.Slice(out.Models, func(i, j int) bool {
		if out.Models[i].Month.CostCNY != out.Models[j].Month.CostCNY {
			return out.Models[i].Month.CostCNY > out.Models[j].Month.CostCNY
		}
		return out.Models[i].CostCNY > out.Models[j].CostCNY
	})
	out.Users = make([]UserUsage, 0, len(b.users))
	for _, v := range b.users {
		out.Users = append(out.Users, *v)
	}
	sort.Slice(out.Users, func(i, j int) bool {
		if out.Users[i].Month.CostCNY != out.Users[j].Month.CostCNY {
			return out.Users[i].Month.CostCNY > out.Users[j].Month.CostCNY
		}
		return out.Users[i].CostCNY > out.Users[j].CostCNY
	})
	start := 0
	if len(b.recent) > 100 {
		start = len(b.recent) - 100
	}
	out.Recent = make([]UsageRecord, 0, len(b.recent)-start)
	for i := len(b.recent) - 1; i >= start; i-- {
		out.Recent = append(out.Recent, b.recent[i])
	}
	return out
}

// User 取单个用户的累计与本月用量。
func (b *Billing) User(id string) UserUsage {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if v := b.users[id]; v != nil {
		return *v
	}
	return UserUsage{UserID: id}
}

// Status 系统页用的轻量摘要。
func (b *Billing) Status() BillingStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	st := BillingStatus{
		Since: b.startedAt, Currency: "CNY",
		Requests: b.totals.Requests, CostCNY: b.totals.CostCNY,
		Recent: len(b.recent),
	}
	if !b.lastSaved.IsZero() {
		st.LastSaved = b.lastSaved.Format(time.RFC3339)
	}
	return st
}

func (b *Billing) dayLocked(t time.Time) UsageAgg {
	if v := b.days[t.Format("2006-01-02")]; v != nil {
		return *v
	}
	return UsageAgg{}
}

func (b *Billing) monthLocked(t time.Time) UsageAgg {
	if v := b.months[t.Format("2006-01")]; v != nil {
		return *v
	}
	return UsageAgg{}
}

func (b *Billing) ensureLocked() {
	if b.months == nil {
		b.months = make(map[string]*UsageAgg)
	}
	if b.days == nil {
		b.days = make(map[string]*UsageAgg)
	}
	if b.users == nil {
		b.users = make(map[string]*UserUsage)
	}
	if b.models == nil {
		b.models = make(map[string]*ModelUsage)
	}
}

// pruneLocked 保留 90 天明细、24 个月汇总。
func (b *Billing) pruneLocked(at time.Time) {
	dayCut := at.AddDate(0, 0, -90).Format("2006-01-02")
	for k := range b.days {
		if k < dayCut {
			delete(b.days, k)
		}
	}
	monthCut := at.AddDate(0, -24, 0).Format("2006-01")
	for k := range b.months {
		if k < monthCut {
			delete(b.months, k)
		}
	}
}

// Flush 原子写入成本账本。
func (b *Billing) Flush() error {
	if b.path == "" {
		return nil
	}
	b.mu.Lock()
	if !b.dirty {
		b.mu.Unlock()
		return nil
	}
	snap := billingSnapshot{
		Version:   1,
		StartedAt: b.startedAt,
		Totals:    b.totals,
		Months:    make(map[string]*UsageAgg, len(b.months)),
		Days:      make(map[string]*UsageAgg, len(b.days)),
		Users:     make(map[string]*UserUsage, len(b.users)),
		Models:    make(map[string]*ModelUsage, len(b.models)),
		Recent:    append([]UsageRecord(nil), b.recent...),
	}
	for k, v := range b.months {
		cp := *v
		snap.Months[k] = &cp
	}
	for k, v := range b.days {
		cp := *v
		snap.Days[k] = &cp
	}
	for k, v := range b.users {
		cp := *v
		snap.Users[k] = &cp
	}
	for k, v := range b.models {
		cp := *v
		snap.Models[k] = &cp
	}
	b.dirty = false
	b.mu.Unlock()

	data, err := json.Marshal(snap)
	if err != nil {
		b.setDirty()
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		b.setDirty()
		return err
	}
	if err := os.Rename(tmp, b.path); err != nil {
		b.setDirty()
		return err
	}
	b.mu.Lock()
	b.lastSaved = time.Now()
	b.mu.Unlock()
	return nil
}

func (b *Billing) setDirty() {
	b.mu.Lock()
	b.dirty = true
	b.mu.Unlock()
}

func maxInt(v, min int) int {
	if v < min {
		return min
	}
	return v
}
