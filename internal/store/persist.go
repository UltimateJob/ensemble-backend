package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// FileStore 在 Memory 之上做分片落盘。
//
// 目录结构：
//
//	data/
//	  users.json          用户
//	  conversations.json  会话
//	  audits.json         审计
//	  events.json         行为事件
//	  messages/
//	    <convID>.json     每个会话的消息单独一个文件
//
// 相比「全部塞进一个 JSON」：
//   - 写一条消息只重写这个会话的文件，不是整库
//   - 会话再多也不会让单次写入变慢
//   - 出问题只影响一个会话
type FileStore struct {
	*Memory
	dir string

	mu           sync.Mutex
	dirtyUsers   bool
	dirtyAgents  bool
	dirtyStudios bool
	dirtyConvs   bool
	dirtyAudits  bool
	dirtyEvents  bool
	dirtyMsgs    map[string]bool // convID → 需要重写

	dirty chan struct{}
	stop  chan struct{}
	once  sync.Once

	lock *lockFile
}

const (
	fileUsers   = "users.json"
	fileAgents  = "agents.json"
	fileStudios = "studios.json"
	fileConvs   = "conversations.json"
	fileAudits  = "audits.json"
	fileEvents  = "events.json"
	dirMsgs     = "messages"
)

// legacyFile 早期版本把全部状态放在一个文件里。
const legacyFile = "ensemble.json"

// NewFileStore 打开或新建一个分片存储。dir 是数据目录。
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, dirMsgs), 0o755); err != nil {
		return nil, err
	}
	// 先抢锁再读数据：两个实例同时读到一半再写，会互相覆盖
	lk, err := acquireLock(filepath.Join(dir, ".lock"))
	if err != nil {
		return nil, err
	}

	fs := &FileStore{
		Memory:    NewMemory(),
		dir:       dir,
		dirtyMsgs: make(map[string]bool),
		dirty:     make(chan struct{}, 1),
		stop:      make(chan struct{}),
		lock:      lk,
	}
	if err := fs.load(); err != nil {
		logx.L().Warn("读取历史数据时出错，从空库继续", "err", err.Error())
	}
	go fs.loop()
	return fs, nil
}

/* ---------------- 加载 ---------------- */

func (fs *FileStore) load() error {
	// 兼容旧版单文件格式
	legacy := filepath.Join(fs.dir, legacyFile)
	if _, err := os.Stat(legacy); err == nil {
		if err := fs.loadLegacy(legacy); err != nil {
			return err
		}
		logx.L().Info("已从旧版单文件迁移", "file", legacy)
		_ = os.Rename(legacy, legacy+".migrated")

		// 迁移进来的数据全部标脏，立刻写成新格式
		fs.mu.Lock()
		fs.dirtyUsers, fs.dirtyConvs, fs.dirtyAudits, fs.dirtyEvents = true, true, true, true
		fs.Memory.mu.RLock()
		for _, m := range fs.Memory.msgs {
			fs.dirtyMsgs[m.ConversationID] = true
		}
		fs.Memory.mu.RUnlock()
		fs.mu.Unlock()
		return fs.Flush()
	}

	var users []*domain.User
	if err := readJSON(filepath.Join(fs.dir, fileUsers), &users); err != nil {
		return err
	}
	var convs []*domain.Conversation
	if err := readJSON(filepath.Join(fs.dir, fileConvs), &convs); err != nil {
		return err
	}
	var agents []*domain.SavedAgent
	if err := readJSON(filepath.Join(fs.dir, fileAgents), &agents); err != nil {
		return err
	}
	var studios []*domain.SavedStudio
	if err := readJSON(filepath.Join(fs.dir, fileStudios), &studios); err != nil {
		return err
	}
	var audits []*domain.AuditLog
	if err := readJSON(filepath.Join(fs.dir, fileAudits), &audits); err != nil {
		return err
	}
	var events []*domain.Event
	if err := readJSON(filepath.Join(fs.dir, fileEvents), &events); err != nil {
		return err
	}

	fs.Memory.mu.Lock()
	for _, u := range users {
		fs.Memory.users[u.ID] = u
	}
	for _, c := range convs {
		fs.Memory.convs[c.ID] = c
	}
	for _, a := range agents {
		fs.Memory.agents[a.ID] = a
	}
	for _, st := range studios {
		fs.Memory.studios[st.ID] = st
	}
	fs.Memory.audits = audits
	fs.Memory.events = events
	fs.Memory.mu.Unlock()

	// 逐个会话读消息
	entries, err := os.ReadDir(filepath.Join(fs.dir, dirMsgs))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var msgs []*domain.Message
		if err := readJSON(filepath.Join(fs.dir, dirMsgs, e.Name()), &msgs); err != nil {
			logx.L().Warn("会话消息文件损坏，已跳过", "file", e.Name(), "err", err.Error())
			continue
		}
		fs.Memory.mu.Lock()
		fs.Memory.msgs = append(fs.Memory.msgs, msgs...)
		fs.Memory.mu.Unlock()
		total += len(msgs)
	}

	logx.L().Info("已加载历史数据",
		"users", len(users), "convs", len(convs), "msgs", total)
	return nil
}

func (fs *FileStore) loadLegacy(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	fs.Memory.Load(s)
	return nil
}

func readJSON(path string, v interface{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

/* ---------------- 写入 ---------------- */

func (fs *FileStore) mark(what string) {
	fs.mu.Lock()
	switch what {
	case "users":
		fs.dirtyUsers = true
	case "agents":
		fs.dirtyAgents = true
	case "studios":
		fs.dirtyStudios = true
	case "convs":
		fs.dirtyConvs = true
	case "audits":
		fs.dirtyAudits = true
	case "events":
		fs.dirtyEvents = true
	default: // 视为 convID
		fs.dirtyMsgs[what] = true
	}
	fs.mu.Unlock()

	select {
	case fs.dirty <- struct{}{}:
	default:
	}
}

func (fs *FileStore) loop() {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-fs.stop:
			return
		case <-t.C:
			select {
			case <-fs.dirty:
				if err := fs.Flush(); err != nil {
					logx.L().Error("落盘失败", "err", err.Error())
				}
			default:
			}
		}
	}
}

// Flush 把脏的部分写盘。原子替换，避免半截文件。
func (fs *FileStore) Flush() error {
	fs.mu.Lock()
	dUsers, dConvs, dAudits, dEvents := fs.dirtyUsers, fs.dirtyConvs, fs.dirtyAudits, fs.dirtyEvents
	dAgents, dStudios := fs.dirtyAgents, fs.dirtyStudios
	msgs := make([]string, 0, len(fs.dirtyMsgs))
	for id := range fs.dirtyMsgs {
		msgs = append(msgs, id)
	}
	fs.dirtyUsers, fs.dirtyConvs, fs.dirtyAudits, fs.dirtyEvents = false, false, false, false
	fs.dirtyAgents, fs.dirtyStudios = false, false
	fs.dirtyMsgs = make(map[string]bool)
	fs.mu.Unlock()

	sort.Strings(msgs)

	if dStudios {
		fs.Memory.mu.RLock()
		list := make([]*domain.SavedStudio, 0, len(fs.Memory.studios))
		for _, st := range fs.Memory.studios {
			list = append(list, st)
		}
		fs.Memory.mu.RUnlock()
		if err := writeJSON(filepath.Join(fs.dir, fileStudios), list); err != nil {
			return err
		}
	}
	if dAgents {
		fs.Memory.mu.RLock()
		list := make([]*domain.SavedAgent, 0, len(fs.Memory.agents))
		for _, a := range fs.Memory.agents {
			list = append(list, a)
		}
		fs.Memory.mu.RUnlock()
		if err := writeJSON(filepath.Join(fs.dir, fileAgents), list); err != nil {
			return err
		}
	}
	if dUsers {
		fs.Memory.mu.RLock()
		list := make([]*domain.User, 0, len(fs.Memory.users))
		for _, u := range fs.Memory.users {
			list = append(list, u)
		}
		fs.Memory.mu.RUnlock()
		if err := writeJSON(filepath.Join(fs.dir, fileUsers), list); err != nil {
			return err
		}
	}
	if dConvs {
		fs.Memory.mu.RLock()
		list := make([]*domain.Conversation, 0, len(fs.Memory.convs))
		for _, c := range fs.Memory.convs {
			list = append(list, c)
		}
		fs.Memory.mu.RUnlock()
		if err := writeJSON(filepath.Join(fs.dir, fileConvs), list); err != nil {
			return err
		}
	}
	if dAudits {
		fs.Memory.mu.RLock()
		list := append([]*domain.AuditLog(nil), fs.Memory.audits...)
		fs.Memory.mu.RUnlock()
		if err := writeJSON(filepath.Join(fs.dir, fileAudits), list); err != nil {
			return err
		}
	}
	if dEvents {
		fs.Memory.mu.RLock()
		list := append([]*domain.Event(nil), fs.Memory.events...)
		fs.Memory.mu.RUnlock()
		if err := writeJSON(filepath.Join(fs.dir, fileEvents), list); err != nil {
			return err
		}
	}
	for _, convID := range msgs {
		fs.Memory.mu.RLock()
		var list []*domain.Message
		for _, m := range fs.Memory.msgs {
			if m.ConversationID == convID {
				list = append(list, m)
			}
		}
		fs.Memory.mu.RUnlock()
		if err := writeJSON(filepath.Join(fs.dir, dirMsgs, safeName(convID)+".json"), list); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// safeName 防止 convID 里出现路径分隔符。
func safeName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

// Close 停止后台协程、做最后一次落盘，然后释放目录锁。
func (fs *FileStore) Close() error {
	fs.once.Do(func() { close(fs.stop) })
	err := fs.Flush()
	if fs.lock != nil {
		if lerr := fs.lock.release(); lerr != nil && err == nil {
			err = lerr
		}
		fs.lock = nil
	}
	return err
}

/* ---------------- 写操作打标记 ---------------- */

func (fs *FileStore) CreateUser(u *domain.User) error {
	err := fs.Memory.CreateUser(u)
	fs.mark("users")
	return err
}

func (fs *FileStore) UpdateUser(u *domain.User) error {
	err := fs.Memory.UpdateUser(u)
	fs.mark("users")
	return err
}

func (fs *FileStore) IncrementUsed(userID string, delta int) error {
	err := fs.Memory.IncrementUsed(userID, delta)
	fs.mark("users")
	return err
}

func (fs *FileStore) TouchLastSeen(userID string) error {
	err := fs.Memory.TouchLastSeen(userID)
	fs.mark("users")
	return err
}

func (fs *FileStore) CreateConv(c *domain.Conversation) error {
	err := fs.Memory.CreateConv(c)
	fs.mark("convs")
	return err
}

func (fs *FileStore) UpdateConv(c *domain.Conversation) error {
	err := fs.Memory.UpdateConv(c)
	fs.mark("convs")
	return err
}

func (fs *FileStore) DeleteConv(id string) error {
	err := fs.Memory.DeleteConv(id)
	fs.mark("convs")
	fs.mark(id) // 消息文件也重写（变成空）
	return err
}

func (fs *FileStore) AppendMessage(m *domain.Message) error {
	err := fs.Memory.AppendMessage(m)
	fs.mark(m.ConversationID)
	return err
}

func (fs *FileStore) CreateAgent(a *domain.SavedAgent) error {
	err := fs.Memory.CreateAgent(a)
	fs.mark("agents")
	return err
}

func (fs *FileStore) UpdateAgent(a *domain.SavedAgent) error {
	err := fs.Memory.UpdateAgent(a)
	fs.mark("agents")
	return err
}

func (fs *FileStore) DeleteAgent(userID, id string) error {
	err := fs.Memory.DeleteAgent(userID, id)
	fs.mark("agents")
	return err
}

func (fs *FileStore) CreateStudio(st *domain.SavedStudio) error {
	err := fs.Memory.CreateStudio(st)
	fs.mark("studios")
	return err
}

func (fs *FileStore) DeleteStudio(userID, id string) error {
	err := fs.Memory.DeleteStudio(userID, id)
	fs.mark("studios")
	return err
}

func (fs *FileStore) AppendAudit(a *domain.AuditLog) error {
	err := fs.Memory.AppendAudit(a)
	fs.mark("audits")
	return err
}

func (fs *FileStore) AppendEvent(e *domain.Event) error {
	err := fs.Memory.AppendEvent(e)
	fs.mark("events")
	return err
}

/* ---------------- 旧格式兼容 ---------------- */

// Snapshot 早期单文件格式的结构，仅用于迁移。
type Snapshot struct {
	Version int                    `json:"version"`
	SavedAt time.Time              `json:"savedAt"`
	Users   []*domain.User         `json:"users"`
	Convs   []*domain.Conversation `json:"conversations"`
	Msgs    []*domain.Message      `json:"messages"`
	Audits  []*domain.AuditLog     `json:"audits"`
	Events  []*domain.Event        `json:"events"`
}

// Load 用快照覆盖内存状态（迁移用）。
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
