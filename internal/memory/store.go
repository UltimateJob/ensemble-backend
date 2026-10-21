// Package memory 保存长期记忆。
//
// 首版用单文件 JSON：记忆量远小于消息，一条记忆 < 1KB，
// 1000 条约 1MB，单文件足够；接口保持可替换（未来换 SQLite/Postgres）。
//
// 作用域只有两种：
//   - conversation：群内记忆，只按 conversation_id 读取，绝不跨群。
//   - agent_user：角色 × 用户，跨群读取，但写入时必须只包含角色自己与用户本人的内容。
package memory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

const maxPerBucket = 200

// ErrNotFound 记忆不存在（目前用于清空时返回统计）。
var ErrNotFound = errors.New("没有找到记忆")

type conversationBucket struct {
	Meta     domain.ConversationMemoryMeta `json:"meta"`
	Memories []*domain.Memory              `json:"memories"`
}

type agentBucket struct {
	CharacterID   string           `json:"characterId"`
	CharacterName string           `json:"characterName"`
	UserID        string           `json:"userId"`
	Memories      []*domain.Memory `json:"memories"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

type fileData struct {
	Conversations map[string]*conversationBucket `json:"conversations"`
	Agents        map[string]*agentBucket        `json:"agents"`
}

// Store 是记忆存储接口。上层只依赖这个接口。
type Store interface {
	AddConversation(convID string, mems []*domain.Memory) error
	AddAgent(characterID, characterName, userID string, mems []*domain.Memory) error
	ListConversation(convID string) []*domain.Memory
	ListAgent(characterID, userID string) []*domain.Memory
	CoveredUpTo(convID string) string
	SetCovered(convID, messageID string) error
	ClearConversation(convID string) int
	ClearAgent(characterID, userID string) int
	AgentNames(userID string) map[string]string
}

// FileStore 单文件记忆存储。写操作同步落盘，量小、简单、不会丢。
type FileStore struct {
	mu   sync.RWMutex
	path string
	data fileData
}

// NewFileStore 打开或创建记忆文件。path 是完整文件路径。
func NewFileStore(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	fs := &FileStore{
		path: path,
		data: fileData{
			Conversations: map[string]*conversationBucket{},
			Agents:        map[string]*agentBucket{},
		},
	}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &fs.data); err != nil {
			return nil, err
		}
	}
	if fs.data.Conversations == nil {
		fs.data.Conversations = map[string]*conversationBucket{}
	}
	if fs.data.Agents == nil {
		fs.data.Agents = map[string]*agentBucket{}
	}
	return fs, nil
}

func agentKey(characterID, userID string) string {
	return characterID + "|" + userID
}

func (fs *FileStore) saveLocked() error {
	b, err := json.MarshalIndent(fs.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := fs.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, fs.path)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "mem_" + hex.EncodeToString(b)
}

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), "")
}

// add 去重 + 截断，返回实际新增/更新的条数。
func add(dst *[]*domain.Memory, mems []*domain.Memory, now time.Time) int {
	n := 0
	for _, m := range mems {
		if m == nil || strings.TrimSpace(m.Text) == "" {
			continue
		}
		if m.ID == "" {
			m.ID = newID()
		}
		if m.CreatedAt.IsZero() {
			m.CreatedAt = now
		}
		if m.OccurredAt.IsZero() {
			m.OccurredAt = now
		}
		if m.Salience < 0 {
			m.Salience = 0
		}
		if m.Salience > 100 {
			m.Salience = 100
		}
		key := string(m.Kind) + "|" + normalize(m.Text)
		merged := false
		for _, old := range *dst {
			if string(old.Kind)+"|"+normalize(old.Text) == key {
				// 同一件事再次被提到：提高权重，合并来源，不重复存。
				if m.Salience > old.Salience {
					old.Salience = m.Salience
				}
				if m.Emotion != 0 {
					old.Emotion = m.Emotion
				}
				old.CreatedAt = now
				old.Quotes = mergeStrings(old.Quotes, m.Quotes)
				old.SourceMessageIDs = mergeStrings(old.SourceMessageIDs, m.SourceMessageIDs)
				merged = true
				n++
				break
			}
		}
		if merged {
			continue
		}
		*dst = append(*dst, m)
		n++
	}
	if len(*dst) > maxPerBucket {
		sort.SliceStable(*dst, func(i, j int) bool {
			if (*dst)[i].Salience == (*dst)[j].Salience {
				return (*dst)[i].CreatedAt.After((*dst)[j].CreatedAt)
			}
			return (*dst)[i].Salience > (*dst)[j].Salience
		})
		*dst = (*dst)[:maxPerBucket]
	}
	return n
}

func mergeStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (fs *FileStore) AddConversation(convID string, mems []*domain.Memory) error {
	if convID == "" || len(mems) == 0 {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	b := fs.data.Conversations[convID]
	if b == nil {
		b = &conversationBucket{Meta: domain.ConversationMemoryMeta{ConversationID: convID}}
		fs.data.Conversations[convID] = b
	}
	now := time.Now()
	for _, m := range mems {
		m.Scope = domain.ScopeConversation
		m.ConversationID = convID
	}
	add(&b.Memories, mems, now)
	b.Meta.UpdatedAt = now
	return fs.saveLocked()
}

func (fs *FileStore) AddAgent(characterID, characterName, userID string, mems []*domain.Memory) error {
	if characterID == "" || userID == "" || len(mems) == 0 {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	key := agentKey(characterID, userID)
	b := fs.data.Agents[key]
	if b == nil {
		b = &agentBucket{CharacterID: characterID, CharacterName: characterName, UserID: userID}
		fs.data.Agents[key] = b
	}
	if characterName != "" {
		b.CharacterName = characterName
	}
	// 关系是慢变量，只保留最新一条，避免每轮整理都新增一条。
	hasRelation := false
	for _, m := range mems {
		if m.Kind == domain.KindRelation {
			hasRelation = true
			break
		}
	}
	if hasRelation {
		kept := b.Memories[:0]
		for _, old := range b.Memories {
			if old.Kind != domain.KindRelation {
				kept = append(kept, old)
			}
		}
		b.Memories = kept
	}
	now := time.Now()
	for _, m := range mems {
		m.Scope = domain.ScopeAgentUser
		m.CharacterID = characterID
		m.CharacterName = b.CharacterName
		m.UserID = userID
	}
	add(&b.Memories, mems, now)
	b.UpdatedAt = now
	return fs.saveLocked()
}

func (fs *FileStore) ListConversation(convID string) []*domain.Memory {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	b := fs.data.Conversations[convID]
	if b == nil {
		return nil
	}
	out := make([]*domain.Memory, len(b.Memories))
	copy(out, b.Memories)
	return out
}

func (fs *FileStore) ListAgent(characterID, userID string) []*domain.Memory {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	b := fs.data.Agents[agentKey(characterID, userID)]
	if b == nil {
		return nil
	}
	out := make([]*domain.Memory, len(b.Memories))
	copy(out, b.Memories)
	return out
}

func (fs *FileStore) CoveredUpTo(convID string) string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if b := fs.data.Conversations[convID]; b != nil {
		return b.Meta.CoveredUpTo
	}
	return ""
}

func (fs *FileStore) SetCovered(convID, messageID string) error {
	if convID == "" || messageID == "" {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	b := fs.data.Conversations[convID]
	if b == nil {
		b = &conversationBucket{Meta: domain.ConversationMemoryMeta{ConversationID: convID}}
		fs.data.Conversations[convID] = b
	}
	b.Meta.CoveredUpTo = messageID
	b.Meta.UpdatedAt = time.Now()
	return fs.saveLocked()
}

// ClearConversation 清空群内记忆，但保留处理游标：
// 清空之后不会把旧消息重新学一遍。
func (fs *FileStore) ClearConversation(convID string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	b := fs.data.Conversations[convID]
	if b == nil {
		return 0
	}
	n := len(b.Memories)
	b.Memories = nil
	b.Meta.UpdatedAt = time.Now()
	_ = fs.saveLocked()
	return n
}

// ClearAgent 清空某个角色对当前用户的全部记忆（跨群）。
func (fs *FileStore) ClearAgent(characterID, userID string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	key := agentKey(characterID, userID)
	b := fs.data.Agents[key]
	if b == nil {
		return 0
	}
	n := len(b.Memories)
	b.Memories = nil
	b.UpdatedAt = time.Now()
	_ = fs.saveLocked()
	return n
}

// AgentNames 返回该用户下所有角色 ID → 名字的映射，供前端展示。
func (fs *FileStore) AgentNames(userID string) map[string]string {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := map[string]string{}
	for _, b := range fs.data.Agents {
		if b.UserID == userID && b.CharacterName != "" {
			out[b.CharacterID] = b.CharacterName
		}
	}
	return out
}
