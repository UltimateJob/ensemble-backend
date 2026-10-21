package service

import (
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// Admin 运维用例：看板、模型、用户、审计。
type Admin struct {
	store  store.Store
	gw     *gateway.Gateway
	shared *Shared
}

// Shared 跨服务的运行时状态，管理页需要读。
type Shared struct {
	Limiter *gateway.Limiter
	Started time.Time
	Version string
}

func NewAdmin(s store.Store, gw *gateway.Gateway, shared *Shared) *Admin {
	return &Admin{store: s, gw: gw, shared: shared}
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
	for _, c := range convs {
		msgCount += len(a.store.ListMessages(c.ID, 0))
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
