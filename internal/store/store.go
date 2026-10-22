// Package store 定义存储接口与内存实现。
// 生产接入 PostgreSQL 只需实现同一接口，上层不动。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// Store 所有持久化能力的集合。
type Store interface {
	// 用户
	CreateUser(u *domain.User) error
	GetUser(id string) (*domain.User, bool)
	GetUserByPhone(phone string) (*domain.User, bool)
	GetUserByEmail(email string) (*domain.User, bool)
	ListUsers() []*domain.User
	UpdateUser(u *domain.User) error
	// IncrementUsed 原子增加已用额度。
	// 「读-改-写」在并发下会丢更新，所以单独开一个方法。
	IncrementUsed(userID string, delta int) error
	// TouchLastSeen 只更新最后活跃时间，不碰其他字段。
	TouchLastSeen(userID string) error

	// 会话
	CreateConv(c *domain.Conversation) error
	GetConv(id string) (*domain.Conversation, bool)
	ListConvs(userID string) []*domain.Conversation
	UpdateConv(c *domain.Conversation) error
	DeleteConv(id string) error

	// 消息
	AppendMessage(m *domain.Message) error
	ListMessages(convID string, limit int) []*domain.Message
	// ListMessagesPage 游标分页：取 beforeID 之前的 limit 条。
	// beforeID 为空表示取最新的一页。返回结果按时间正序，以及是否还有更早的。
	ListMessagesPage(convID, beforeID string, limit int) (msgs []*domain.Message, hasMore bool)

	// 角色库
	CreateAgent(a *domain.SavedAgent) error
	ListAgents(userID string) []*domain.SavedAgent
	GetAgent(id string) (*domain.SavedAgent, bool)
	UpdateAgent(a *domain.SavedAgent) error
	DeleteAgent(userID, id string) error

	// 工作室模板
	CreateStudio(s *domain.SavedStudio) error
	ListStudios(userID string) []*domain.SavedStudio
	GetStudio(id string) (*domain.SavedStudio, bool)
	DeleteStudio(userID, id string) error

	// 审计
	AppendAudit(a *domain.AuditLog) error
	ListAudit(limit int) []*domain.AuditLog

	// 记录（互动/事件）
	AppendEvent(e *domain.Event) error
	ListEvents(kind string, limit int) []*domain.Event
}

// Memory 进程内实现，带读写锁。重启即清空，用于首版与本地演示。
type Memory struct {
	mu       sync.RWMutex
	users    map[string]*domain.User
	convs    map[string]*domain.Conversation
	agents   map[string]*domain.SavedAgent
	studios  map[string]*domain.SavedStudio
	msgs     []*domain.Message
	msgCount map[string]int // convID → 该会话消息条数。msgs 的派生索引，load/append/delete 时同步维护，让计数是 O(1)
	audits   []*domain.AuditLog
	events   []*domain.Event
}

func NewMemory() *Memory {
	return &Memory{
		users:    make(map[string]*domain.User),
		convs:    make(map[string]*domain.Conversation),
		agents:   make(map[string]*domain.SavedAgent),
		studios:  make(map[string]*domain.SavedStudio),
		msgCount: make(map[string]int),
	}
}

// ErrUserNotFound 用户不存在。
var ErrUserNotFound = errors.New("用户不存在")

// timeNow 单独抽出来方便测试替换。
var timeNow = time.Now

// NewID 生成短 ID。
func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func (m *Memory) CreateUser(u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u.ID == "" {
		u.ID = NewID("u")
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	cp := *u
	m.users[u.ID] = &cp
	return nil
}

func (m *Memory) GetUser(id string) (*domain.User, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, false
	}
	cp := *u
	return &cp, true
}

func (m *Memory) GetUserByPhone(phone string) (*domain.User, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Phone == phone && phone != "" {
			cp := *u
			return &cp, true
		}
	}
	return nil, false
}

func (m *Memory) GetUserByEmail(email string) (*domain.User, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Email == email && email != "" {
			cp := *u
			return &cp, true
		}
	}
	return nil, false
}

func (m *Memory) ListUsers() []*domain.User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.User, 0, len(m.users))
	for _, u := range m.users {
		cp := *u
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// IncrementUsed 在锁内只改 Used，不与整对象覆盖冲突。
func (m *Memory) IncrementUsed(userID string, delta int) error {
	if delta == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrUserNotFound
	}
	u.Used += delta
	if u.Used < 0 {
		u.Used = 0
	}
	return nil
}

// TouchLastSeen 只动一个字段。
func (m *Memory) TouchLastSeen(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrUserNotFound
	}
	u.LastSeen = timeNow()
	return nil
}

// UpdateUser 保存用户资料。
// Used 与 LastSeen 不让调用方覆盖——这两个走原子方法，
// 否则 PATCH /me 这类「拿旧副本整体写回」会把并发扣掉的额度冲回去。
func (m *Memory) UpdateUser(u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.users[u.ID]
	if !ok {
		cp := *u
		m.users[u.ID] = &cp
		return nil
	}
	cp := *u
	cp.Used = cur.Used
	cp.LastSeen = cur.LastSeen
	m.users[u.ID] = &cp
	return nil
}

func (m *Memory) CreateConv(c *domain.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == "" {
		c.ID = NewID("c")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	c.UpdatedAt = c.CreatedAt
	cp := *c
	m.convs[c.ID] = &cp
	return nil
}

func (m *Memory) GetConv(id string) (*domain.Conversation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.convs[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

func (m *Memory) ListConvs(userID string) []*domain.Conversation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.Conversation, 0)
	for _, c := range m.convs {
		if userID == "" || c.CanAccess(userID) {
			cp := *c
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (m *Memory) UpdateConv(c *domain.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.UpdatedAt = time.Now()
	cp := *c
	m.convs[c.ID] = &cp
	return nil
}

func (m *Memory) DeleteConv(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.convs, id)
	// 原地过滤是安全的：写指针 j 始终 <= 读指针 i，只会覆盖已经读过的
	// 元素，不会碰到还没遍历到的——省一次大切片的内存分配。
	filtered := m.msgs[:0]
	for _, msg := range m.msgs {
		if msg.ConversationID != id {
			filtered = append(filtered, msg)
		}
	}
	m.msgs = filtered
	delete(m.msgCount, id)
	return nil
}

func (m *Memory) AppendMessage(msg *domain.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.ID == "" {
		msg.ID = NewID("m")
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	cp := *msg
	m.msgs = append(m.msgs, &cp)
	m.msgCount[msg.ConversationID]++
	return nil
}

// CountMessages 返回会话当前的消息条数，O(1)。
// 计数在 load/append/delete 时与 msgs 同步维护，
// 比为了拿个数去 ListMessages(convID, 0) 扫全量便宜得多。
func (m *Memory) CountMessages(convID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.msgCount[convID]
}

// ListMessagesPage 游标分页实现。
func (m *Memory) ListMessagesPage(convID, beforeID string, limit int) ([]*domain.Message, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 先收集这个会话的全部消息（按插入顺序）
	var all []*domain.Message
	for _, msg := range m.msgs {
		if msg.ConversationID == convID {
			all = append(all, msg)
		}
	}

	end := len(all)
	if beforeID != "" {
		for i, msg := range all {
			if msg.ID == beforeID {
				end = i
				break
			}
		}
	}
	if end < 0 {
		end = 0
	}

	if limit <= 0 {
		limit = 30
	}
	start := end - limit
	if start < 0 {
		start = 0
	}

	out := make([]*domain.Message, 0, end-start)
	for _, msg := range all[start:end] {
		cp := *msg
		out = append(out, &cp)
	}
	return out, start > 0
}

func (m *Memory) ListMessages(convID string, limit int) []*domain.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Message
	for _, msg := range m.msgs {
		if msg.ConversationID == convID {
			cp := *msg
			out = append(out, &cp)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

/* ---------------- 角色库 ---------------- */

func (m *Memory) CreateAgent(a *domain.SavedAgent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = NewID("ag")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	cp := *a
	m.agents[a.ID] = &cp
	return nil
}

func (m *Memory) ListAgents(userID string) []*domain.SavedAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.SavedAgent, 0)
	for _, a := range m.agents {
		if userID == "" || a.UserID == userID {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Memory) GetAgent(id string) (*domain.SavedAgent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[id]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

func (m *Memory) UpdateAgent(a *domain.SavedAgent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *a
	m.agents[a.ID] = &cp
	return nil
}

func (m *Memory) DeleteAgent(userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok || a.UserID != userID {
		return ErrUserNotFound
	}
	delete(m.agents, id)
	return nil
}

/* ---------------- 工作室模板 ---------------- */

func (m *Memory) CreateStudio(st *domain.SavedStudio) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st.ID == "" {
		st.ID = NewID("ws")
	}
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now()
	}
	cp := *st
	m.studios[st.ID] = &cp
	return nil
}

func (m *Memory) ListStudios(userID string) []*domain.SavedStudio {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.SavedStudio, 0)
	for _, st := range m.studios {
		if userID == "" || st.UserID == userID {
			cp := *st
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Memory) GetStudio(id string) (*domain.SavedStudio, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.studios[id]
	if !ok {
		return nil, false
	}
	cp := *st
	return &cp, true
}

func (m *Memory) DeleteStudio(userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.studios[id]
	if !ok || st.UserID != userID {
		return ErrUserNotFound
	}
	delete(m.studios, id)
	return nil
}

func (m *Memory) AppendAudit(a *domain.AuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = NewID("a")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	cp := *a
	m.audits = append(m.audits, &cp)
	if len(m.audits) > 5000 {
		m.audits = m.audits[len(m.audits)-5000:]
	}
	return nil
}

func (m *Memory) ListAudit(limit int) []*domain.AuditLog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.audits)
	start := 0
	if limit > 0 && n > limit {
		start = n - limit
	}
	out := make([]*domain.AuditLog, 0, n-start)
	for i := n - 1; i >= start; i-- {
		cp := *m.audits[i]
		out = append(out, &cp)
	}
	return out
}

func (m *Memory) AppendEvent(e *domain.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == "" {
		e.ID = NewID("e")
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	cp := *e
	m.events = append(m.events, &cp)
	if len(m.events) > 10000 {
		m.events = m.events[len(m.events)-10000:]
	}
	return nil
}

func (m *Memory) ListEvents(kind string, limit int) []*domain.Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Event
	for i := len(m.events) - 1; i >= 0; i-- {
		e := m.events[i]
		if kind == "" || e.Kind == kind {
			cp := *e
			out = append(out, &cp)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}
