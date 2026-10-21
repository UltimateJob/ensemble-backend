package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// Snapshot 可序列化的全量状态。
type Snapshot struct {
	Version int                    `json:"version"`
	SavedAt time.Time              `json:"savedAt"`
	Users   []*domain.User         `json:"users"`
	Convs   []*domain.Conversation `json:"conversations"`
	Msgs    []*domain.Message      `json:"messages"`
	Audits  []*domain.AuditLog     `json:"audits"`
	Events  []*domain.Event        `json:"events"`
}

// Dump 导出内存状态。
func (m *Memory) Dump() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := Snapshot{Version: 1, SavedAt: time.Now()}
	for _, u := range m.users {
		cp := *u
		s.Users = append(s.Users, &cp)
	}
	for _, c := range m.convs {
		cp := *c
		s.Convs = append(s.Convs, &cp)
	}
	for _, msg := range m.msgs {
		cp := *msg
		s.Msgs = append(s.Msgs, &cp)
	}
	for _, a := range m.audits {
		cp := *a
		s.Audits = append(s.Audits, &cp)
	}
	for _, e := range m.events {
		cp := *e
		s.Events = append(s.Events, &cp)
	}
	return s
}

// Load 用快照覆盖内存状态。
func (m *Memory) Load(s Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.users = make(map[string]*domain.User, len(s.Users))
	for _, u := range s.Users {
		m.users[u.ID] = u
	}
	m.convs = make(map[string]*domain.Conversation, len(s.Convs))
	for _, c := range s.Convs {
		m.convs[c.ID] = c
	}
	m.msgs = s.Msgs
	m.audits = s.Audits
	m.events = s.Events
}

// FileStore 在 Memory 之上做异步落盘。
// 写操作标记 dirty，后台每 400ms 合并写一次；进程退出前 Flush。
type FileStore struct {
	*Memory
	path  string
	dirty chan struct{}
	stop  chan struct{}
	once  sync.Once
}

// NewFile 打开（或新建）一个持久化存储。
func NewFile(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	fs := &FileStore{
		Memory: NewMemory(),
		path:   path,
		dirty:  make(chan struct{}, 1),
		stop:   make(chan struct{}),
	}
	if err := fs.load(); err != nil && !os.IsNotExist(err) {
		logx.L().Warn("历史数据读取失败，从空库开始", "err", err.Error())
	}
	go fs.loop()
	return fs, nil
}

func (f *FileStore) load() error {
	b, err := os.ReadFile(f.path)
	if err != nil {
		return err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	f.Memory.Load(s)
	logx.L().Info("已加载历史数据",
		"users", len(s.Users), "convs", len(s.Convs),
		"msgs", len(s.Msgs), "savedAt", s.SavedAt.Format(time.RFC3339))
	return nil
}

func (f *FileStore) markDirty() {
	select {
	case f.dirty <- struct{}{}:
	default:
	}
}

func (f *FileStore) loop() {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			select {
			case <-f.dirty:
				if err := f.Flush(); err != nil {
					logx.L().Error("落盘失败", "err", err.Error())
				}
			default:
			}
		}
	}
}

// Flush 立即写盘。先写临时文件再 rename，避免半截文件。
func (f *FileStore) Flush() error {
	snap := f.Memory.Dump()
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// Close 停止后台协程并做最后一次落盘。
func (f *FileStore) Close() error {
	f.once.Do(func() { close(f.stop) })
	return f.Flush()
}

// ---- 写操作全部打标记 ----

func (f *FileStore) CreateUser(u *domain.User) error {
	err := f.Memory.CreateUser(u)
	f.markDirty()
	return err
}
func (f *FileStore) UpdateUser(u *domain.User) error {
	err := f.Memory.UpdateUser(u)
	f.markDirty()
	return err
}
func (f *FileStore) CreateConv(c *domain.Conversation) error {
	err := f.Memory.CreateConv(c)
	f.markDirty()
	return err
}
func (f *FileStore) UpdateConv(c *domain.Conversation) error {
	err := f.Memory.UpdateConv(c)
	f.markDirty()
	return err
}
func (f *FileStore) DeleteConv(id string) error {
	err := f.Memory.DeleteConv(id)
	f.markDirty()
	return err
}
func (f *FileStore) AppendMessage(m *domain.Message) error {
	err := f.Memory.AppendMessage(m)
	f.markDirty()
	return err
}
func (f *FileStore) AppendAudit(a *domain.AuditLog) error {
	err := f.Memory.AppendAudit(a)
	f.markDirty()
	return err
}
func (f *FileStore) AppendEvent(e *domain.Event) error {
	err := f.Memory.AppendEvent(e)
	f.markDirty()
	return err
}
