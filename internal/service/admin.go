package service

import (
	"fmt"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// Admin 运维用例：看板、模型、用户、审计。
type Admin struct {
	store    store.Store
	gw       *gateway.Gateway
	shared   *Shared
	insights *Insights
	plans    *PlanStore
	billing  *Billing
	settings *SettingsStore
	wallet   *Wallet
}

// Shared 跨服务的运行时状态，管理页需要读。
type Shared struct {
	Limiter *gateway.Limiter
	Started time.Time
	Version string
}

func NewAdmin(s store.Store, gw *gateway.Gateway, shared *Shared) *Admin {
	plans, _ := NewPlanStore("")
	billing, _ := NewBilling("")
	settings, _ := OpenSettings("")
	wallet, _ := OpenWallet("")
	a := &Admin{
		store: s, gw: gw, shared: shared,
		insights: NewInsights(gw, s),
		plans:    plans,
		billing:  billing,
		settings: settings,
		wallet:   wallet,
	}
	// 指标序列是运维趋势和告警的数据源，随进程常驻采样。
	a.insights.Start(seriesInterval)
	return a
}

// SetCommercial 注入落盘的套餐配置与成本账本。
// 须在开始接请求前调用；测试环境不调用时使用内存默认值。
func (a *Admin) SetCommercial(plans *PlanStore, billing *Billing) {
	if plans != nil {
		a.plans = plans
	}
	if billing != nil {
		a.billing = billing
	}
}

// SetControl 注入运营设置与钱包账本。须在开始接请求前调用。
func (a *Admin) SetControl(settings *SettingsStore, wallet *Wallet) {
	if settings != nil {
		a.settings = settings
	}
	if wallet != nil {
		a.wallet = wallet
	}
}

// Settings 当前运营设置。
func (a *Admin) Settings() Settings {
	if a.settings == nil {
		return DefaultSettings()
	}
	return a.settings.Get()
}

// UpdateSettings 保存运营设置。默认套餐必须真实存在，
// 否则新用户会落在一个没有额度规则的空档上。
func (a *Admin) UpdateSettings(in Settings) (Settings, error) {
	if a.settings == nil {
		return in, fmt.Errorf("设置存储未初始化")
	}
	if in.DefaultPlan != "" {
		if _, ok := a.plans.Get(in.DefaultPlan); !ok {
			return a.Settings(), fmt.Errorf("默认套餐「%s」不存在", in.DefaultPlan)
		}
	}
	return a.settings.Update(in)
}

// Wallet 钱包账本。
func (a *Admin) Wallet() *Wallet {
	if a.wallet == nil {
		a.wallet, _ = OpenWallet("")
	}
	return a.wallet
}

// Insights 指标时间序列。
func (a *Admin) Insights() *Insights { return a.insights }

// Billing 成本账本。
func (a *Admin) Billing() *Billing { return a.billing }

// Plans 套餐配置。
func (a *Admin) Plans() []domain.PlanConfig { return a.plans.List() }

// Plan 取单个套餐。
func (a *Admin) Plan(id string) (domain.PlanConfig, bool) { return a.plans.Get(id) }

// UpsertPlan 新增或更新套餐。
func (a *Admin) UpsertPlan(p domain.PlanConfig) error { return a.plans.Upsert(p) }

// DeletePlan 删除套餐。
func (a *Admin) DeletePlan(id string) error { return a.plans.Delete(id) }

// UsersOnPlan 统计某个套餐下的用户数，删除前做保护。
func (a *Admin) UsersOnPlan(id string) int {
	n := 0
	for _, u := range a.store.ListUsers() {
		if u.Plan == id {
			n++
		}
	}
	return n
}

// Overview 管理页首屏数据。
type Overview struct {
	Metrics       domain.Metrics `json:"metrics"`
	Users         int            `json:"users"`
	ActiveUsers   int            `json:"activeUsers"`
	Conversations int            `json:"conversations"`
	Messages      int            `json:"messages"`
	Models        int            `json:"models"`
	Version       string         `json:"version"`
	StartedAt     time.Time      `json:"startedAt"`
}

func (a *Admin) OverviewOf() Overview {
	users := a.store.ListUsers()
	active := 0
	cut := time.Now().Add(-24 * time.Hour)
	for _, u := range users {
		if u.LastSeen.After(cut) {
			active++
		}
	}
	convs := a.store.ListConvs("")
	msgCount := 0
	// 有 O(1) 计数索引时直接用；没有才退回逐会话扫描。
	// 之前每个会话都 ListMessages 一次，几千个会话、几万条消息时
	// 每 4 秒轮询一次总览会把 CPU 吃满。
	if counter, ok := a.store.(interface{ CountMessages(string) int }); ok {
		for _, c := range convs {
			msgCount += counter.CountMessages(c.ID)
		}
	} else {
		for _, c := range convs {
			msgCount += len(a.store.ListMessages(c.ID, 0))
		}
	}
	return Overview{
		Metrics:       a.gw.Metrics(),
		Users:         len(users),
		ActiveUsers:   active,
		Conversations: len(convs),
		Messages:      msgCount,
		Models:        len(a.gw.Models().List()),
		Version:       a.shared.Version,
		StartedAt:     a.shared.Started,
	}
}

func (a *Admin) Models() []domain.ModelInfo { return a.gw.Models().List() }

func (a *Admin) UpsertModel(m domain.ModelInfo) { a.gw.Models().Upsert(m) }

func (a *Admin) DeleteModel(key string) bool { return a.gw.Models().Delete(key) }

func (a *Admin) Users() []*domain.User { return a.store.ListUsers() }

func (a *Admin) SetUserStatus(id, status string) (*domain.User, bool) {
	u, ok := a.store.GetUser(id)
	if !ok {
		return nil, false
	}
	u.Status = status
	_ = a.store.UpdateUser(u)
	return u, true
}

func (a *Admin) SetUserQuota(id string, quota int) (*domain.User, bool) {
	u, ok := a.store.GetUser(id)
	if !ok {
		return nil, false
	}
	u.Quota = quota
	_ = a.store.UpdateUser(u)
	return u, true
}

// SetUserPlan 修改套餐。只接受已配置的档位，并默认同步套餐额度。
func (a *Admin) SetUserPlan(id, plan string) (*domain.User, bool) {
	return a.SetUserPlanWithQuota(id, plan, true)
}

// SetUserPlanWithQuota 修改套餐。applyQuota 为真时把额度同步成套餐月额度，
// 让「换套餐」和「额度」保持一致；也可以只改标签、保留自定义额度。
func (a *Admin) SetUserPlanWithQuota(id, plan string, applyQuota bool) (*domain.User, bool) {
	p, exists := a.plans.Get(plan)
	if !exists {
		return nil, false
	}
	u, found := a.store.GetUser(id)
	if !found {
		return nil, false
	}
	u.Plan = p.ID
	if applyQuota {
		u.Quota = p.MonthlyQuota
	}
	_ = a.store.UpdateUser(u)
	return u, true
}

func (a *Admin) ResetProvider(name string) bool { return a.gw.Registry().Reset(name) }

func (a *Admin) Audit(actor, action, target, detail, ip string) {
	_ = a.store.AppendAudit(&domain.AuditLog{
		Actor:  actor,
		Action: action,
		Target: target,
		Detail: detail,
		IP:     ip,
	})
}

func (a *Admin) Logs(limit int) []*domain.AuditLog { return a.store.ListAudit(limit) }

func (a *Admin) Events(kind string, limit int) []*domain.Event {
	return a.store.ListEvents(kind, limit)
}
