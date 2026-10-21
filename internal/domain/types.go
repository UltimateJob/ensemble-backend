// Package domain 定义领域模型。所有层共享，不依赖任何外部包。
package domain

import "time"

// Role 会话角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// User 用户。手机号与邮箱都可作为登录标识，密码按需。
type User struct {
	ID        string    `json:"id"`
	Phone     string    `json:"phone,omitempty"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name"`
	Avatar    string    `json:"avatar,omitempty"`
	Plan      string    `json:"plan"`   // free | pro | team
	Quota     int       `json:"quota"`  // 本月额度（次）
	Used      int       `json:"used"`   // 已用
	Role      string    `json:"role"`   // user | admin
	Status    string    `json:"status"` // active | disabled
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

// CanAccess 判断某个用户能否进入这个会话。
// 创建者与受邀协作者都可以；其他人一律不可见。
func (c *Conversation) CanAccess(userID string) bool {
	if userID == "" {
		return false
	}
	if c.UserID == userID {
		return true
	}
	for _, id := range c.Collaborators {
		if id == userID {
			return true
		}
	}
	return false
}

// IsOwner 是否是创建者。只有创建者能改协作者。
func (c *Conversation) IsOwner(userID string) bool {
	return c.UserID == userID
}

// Remaining 返回剩余额度。
func (u *User) Remaining() int {
	if u.Quota <= u.Used {
		return 0
	}
	return u.Quota - u.Used
}

// Conversation 一个会话，对应前端的一个群聊。
type Conversation struct {
	ID      string   `json:"id"`
	UserID  string   `json:"userId"`
	Title   string   `json:"title"`
	Studio  string   `json:"studio,omitempty"` // 工作室模板：bid | code | research | anime | companion | custom
	Model   string   `json:"model"`
	Members []string `json:"members"` // 场景内置角色的 key

	// Agents 自定义角色。为空时用场景预设，非空时覆盖。
	// 这是「自己造角色拉进群」的落地方式。
	Agents []ConvAgent `json:"agents,omitempty"`

	// Collaborators 可以一起看和发言的用户 ID。
	// 创建者（UserID）永远有权限，这里存的是被邀请的人。
	Collaborators []string `json:"collaborators,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ConvAgent 会话内的自定义角色，带完整设定。
type ConvAgent struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Emoji  string `json:"emoji,omitempty"`
	Color  string `json:"color,omitempty"`
	Prompt string `json:"prompt"`
}

// Message 一条消息。Agent 消息带产物引用。
type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId"`
	Role           Role      `json:"role"`
	Agent          string    `json:"agent,omitempty"` // 工头/查证/撰写/审校/交付
	Content        string    `json:"content"`
	Artifacts      []string  `json:"artifacts,omitempty"`
	TokensIn       int       `json:"tokensIn"`
	TokensOut      int       `json:"tokensOut"`
	Model          string    `json:"model,omitempty"`
	LatencyMS      int       `json:"latencyMs"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ChatMessage 送往模型的消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 网关统一入参。
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"maxTokens,omitempty"`
	Stream      bool          `json:"stream"`
	// 路由提示：指定 provider 时优先使用
	Provider string `json:"provider,omitempty"`
}

// Chunk 流式返回的一段。
type Chunk struct {
	Content string `json:"content"`
	Done    bool   `json:"done"`
	// 最后一个 chunk 携带用量
	Usage *Usage `json:"usage,omitempty"`
	Err   error  `json:"-"`
}

// Usage 用量。
type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

// ModelInfo 模型注册信息，管理员可改。
type ModelInfo struct {
	Key       string  `json:"key"`      // 唯一标识，如 deepseek-chat
	Provider  string  `json:"provider"` // deepseek | mock
	Upstream  string  `json:"upstream"` // 上游模型名
	Enabled   bool    `json:"enabled"`
	Weight    int     `json:"weight"`    // 加权轮询权重
	CostPer1K float64 `json:"costPer1k"` // 参考单价，用于用量统计
	Note      string  `json:"note,omitempty"`
}

// AuditLog 管理员操作审计。
type AuditLog struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail,omitempty"`
	IP        string    `json:"ip,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Metrics 运行时指标快照。
type Metrics struct {
	Uptime       int64            `json:"uptime"` // 秒
	Requests     int64            `json:"requests"`
	Errors       int64            `json:"errors"`
	InFlight     int64            `json:"inFlight"`
	TokensUsed   int64            `json:"tokensUsed"`
	AvgLatencyMS float64          `json:"avgLatencyMs"`
	P95LatencyMS float64          `json:"p95LatencyMs"`
	Providers    []ProviderStat   `json:"providers"`
	ModelUsage   map[string]int64 `json:"modelUsage"`
}

// ProviderStat 单个上游的健康与用量。
type ProviderStat struct {
	Name      string  `json:"name"`
	State     string  `json:"state"` // closed | open | half-open
	Success   int64   `json:"success"`
	Failure   int64   `json:"failure"`
	LatencyMS float64 `json:"latencyMs"`
	LastErr   string  `json:"lastErr,omitempty"`
}

// Event 互动/行为记录。前端埋点与业务事件共用。
type Event struct {
	ID        string            `json:"id"`
	UserID    string            `json:"userId,omitempty"`
	Kind      string            `json:"kind"` // login | chat | approve | reject | share | error
	Target    string            `json:"target,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	IP        string            `json:"ip,omitempty"`
	UA        string            `json:"ua,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}
