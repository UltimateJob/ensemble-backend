package store

import (
	"sort"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

/* ---------------- 设备 ---------------- */

func (m *Memory) CreateDevice(d *domain.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.ID == "" {
		d.ID = NewID("d")
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	cp := *d
	m.devices[d.ID] = &cp
	if cp.TokenHash != "" {
		m.deviceByTK[cp.TokenHash] = cp.ID
	}
	return nil
}

func (m *Memory) GetDevice(id string) (*domain.Device, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[id]
	if !ok {
		return nil, false
	}
	cp := *d
	return &cp, true
}

func (m *Memory) GetDeviceByTokenHash(hash string) (*domain.Device, bool) {
	if hash == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.deviceByTK[hash]
	if !ok {
		return nil, false
	}
	d, ok := m.devices[id]
	if !ok {
		return nil, false
	}
	cp := *d
	return &cp, true
}

func (m *Memory) ListDevices(userID string) []*domain.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Device, 0)
	for _, d := range m.devices {
		if d.UserID == userID {
			cp := *d
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Memory) UpdateDevice(d *domain.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.devices[d.ID]
	if !ok {
		return ErrUserNotFound
	}
	// 令牌哈希允许变更（重新配对），索引同步更新
	if cur.TokenHash != "" && cur.TokenHash != d.TokenHash {
		delete(m.deviceByTK, cur.TokenHash)
	}
	if d.TokenHash != "" {
		m.deviceByTK[d.TokenHash] = d.ID
	}
	cp := *d
	m.devices[d.ID] = &cp
	return nil
}

/* ---------------- 目标 ---------------- */

func (m *Memory) CreateGoal(g *domain.Goal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g.ID == "" {
		g.ID = NewID("g")
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now()
	}
	g.UpdatedAt = time.Now()
	if g.ManualProgress == 0 {
		g.ManualProgress = -1 // 默认不手动覆盖
	}
	cp := *g
	m.goals[g.ID] = &cp
	return nil
}

func (m *Memory) GetGoal(id string) (*domain.Goal, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.goals[id]
	if !ok {
		return nil, false
	}
	cp := *g
	return &cp, true
}

func (m *Memory) ListGoals(userID string) []*domain.Goal {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Goal, 0)
	for _, g := range m.goals {
		if g.UserID == userID {
			cp := *g
			out = append(out, &cp)
		}
	}
	// 未完成的排前面，同状态按更新时间倒序
	sort.Slice(out, func(i, j int) bool {
		di, dj := out[i].Status == domain.GoalDone, out[j].Status == domain.GoalDone
		if di != dj {
			return !di
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (m *Memory) UpdateGoal(g *domain.Goal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.goals[g.ID]
	if !ok || cur.UserID != g.UserID {
		return ErrUserNotFound
	}
	g.UpdatedAt = time.Now()
	if g.Status == domain.GoalDone && cur.Status != domain.GoalDone {
		g.CompletedAt = g.UpdatedAt
	}
	if g.Status != domain.GoalDone {
		g.CompletedAt = time.Time{}
	}
	cp := *g
	m.goals[g.ID] = &cp
	return nil
}

func (m *Memory) DeleteGoal(userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.goals[id]
	if !ok || g.UserID != userID {
		return ErrUserNotFound
	}
	delete(m.goals, id)
	return nil
}

/* ---------------- 连接器 ---------------- */

func (m *Memory) CreateConnector(c *domain.Connector) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == "" {
		c.ID = NewID("c")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	c.UpdatedAt = time.Now()
	if c.Status == "" {
		c.Status = domain.ConnStatusUnset
	}
	cp := *c
	m.conns[c.ID] = &cp
	return nil
}

func (m *Memory) GetConnector(id string) (*domain.Connector, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.conns[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

func (m *Memory) ListConnectors(userID string) []*domain.Connector {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Connector, 0)
	for _, c := range m.conns {
		if c.UserID == userID {
			cp := *c
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Memory) UpdateConnector(c *domain.Connector) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.conns[c.ID]
	if !ok || cur.UserID != c.UserID {
		return ErrUserNotFound
	}
	c.UpdatedAt = time.Now()
	cp := *c
	m.conns[c.ID] = &cp
	return nil
}

func (m *Memory) DeleteConnector(userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[id]
	if !ok || c.UserID != userID {
		return ErrUserNotFound
	}
	delete(m.conns, id)
	return nil
}

/* ---------------- 产物 ---------------- */

func (m *Memory) CreateArtifact(a *domain.Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = NewID("art")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	cp := *a
	m.artifacts[a.ID] = &cp
	return nil
}

func (m *Memory) GetArtifact(id string) (*domain.Artifact, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.artifacts[id]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

// ListArtifacts 返回摘要（Content 置空），列表页不需要全文。
func (m *Memory) ListArtifacts(userID, convID string, limit int) []*domain.Artifact {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Artifact, 0)
	for _, a := range m.artifacts {
		if a.UserID != userID {
			continue
		}
		if convID != "" && a.ConversationID != convID {
			continue
		}
		cp := *a
		cp.Content = "" // 列表只给摘要
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *Memory) DeleteArtifact(userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.artifacts[id]
	if !ok || a.UserID != userID {
		return ErrUserNotFound
	}
	delete(m.artifacts, id)
	return nil
}
