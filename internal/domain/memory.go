package domain

import "time"

// MemoryScope 决定一条记忆能被谁读到。
//
//   - conversation：只属于某个群，绝不跨群。
//   - agent_user：属于「角色 × 用户」，可以跨群；只允许写角色自己和用户本人的内容。
type MemoryScope string

const (
	ScopeConversation MemoryScope = "conversation"
	ScopeAgentUser    MemoryScope = "agent_user"
)

// MemoryKind 记忆类型。关系也用一条高权重的 relation 记忆表示，
// 避免为了首版再引入一套独立的 relationship 表。
type MemoryKind string

const (
	KindEpisode    MemoryKind = "episode"     // 一段经历
	KindFact       MemoryKind = "fact"        // 用户事实/偏好
	KindPromise    MemoryKind = "promise"     // 承诺/待办
	KindOpenThread MemoryKind = "open_thread" // 没聊完的话题
	KindRelation   MemoryKind = "relation"    // 角色与用户的关系/态度
)

// Memory 一条长期记忆。原始消息仍然是事实源；这里只存检索用的索引。
type Memory struct {
	ID                   string      `json:"id"`
	Scope                MemoryScope `json:"scope"`
	ConversationID       string      `json:"conversationId,omitempty"` // conversation 作用域必填
	CharacterID          string      `json:"characterId,omitempty"`    // agent_user 作用域必填
	CharacterName        string      `json:"characterName,omitempty"`
	UserID               string      `json:"userId,omitempty"`
	Kind                 MemoryKind  `json:"kind"`
	Text                 string      `json:"text"`
	Quotes               []string    `json:"quotes,omitempty"`
	OccurredAt           time.Time   `json:"occurredAt"`
	CreatedAt            time.Time   `json:"createdAt"`
	Salience             int         `json:"salience"` // 0-100
	Emotion              int         `json:"emotion"`  // -5..5
	SourceMessageIDs     []string    `json:"sourceMessageIds,omitempty"`
	SourceConversationID string      `json:"sourceConversationId,omitempty"`
}

// ConversationMemoryMeta 群记忆的处理游标。
// CoveredUpTo 之后的原始消息才是「还没整理过」的。
type ConversationMemoryMeta struct {
	ConversationID string    `json:"conversationId"`
	CoveredUpTo    string    `json:"coveredUpTo,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// MemorySnapshot 是给「他们记得」面板用的只读视图。
type MemorySnapshot struct {
	Conversation []*Memory       `json:"conversation"`
	Agents       []AgentMemories `json:"agents"`
}

// AgentMemories 一个角色对当前用户的记忆。
type AgentMemories struct {
	CharacterID   string    `json:"characterId"`
	CharacterName string    `json:"characterName"`
	Relation      *Memory   `json:"relation,omitempty"`
	Memories      []*Memory `json:"memories"`
}
