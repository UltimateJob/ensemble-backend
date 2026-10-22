// Package domain 定义领域模型。所有层共享，不依赖任何外部包。
package domain

import (
	"encoding/json"
	"time"
)

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

// SavedAgent 用户角色库里的角色。可以反复拉进不同的群。
// 创建群时会被复制成 ConvAgent（快照），之后改库不影响已有群。
type SavedAgent struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	Emoji     string    `json:"emoji,omitempty"`
	Color     string    `json:"color,omitempty"`
	Prompt    string    `json:"prompt"`
	Source    string    `json:"source,omitempty"` // draft（AI 生成）| manual（手写）
	CreatedAt time.Time `json:"createdAt"`
}

// SavedStudio 用户自己保存的工作室模板。
// 内置场景是官方给的，这个是用户调的——一组角色 + 一句开场，可以反复开群。
type SavedStudio struct {
	ID        string      `json:"id"`
	UserID    string      `json:"userId"`
	Name      string      `json:"name"`
	Tagline   string      `json:"tagline"`
	Accent    string      `json:"accent"`
	Icon      string      `json:"icon"`
	Opening   string      `json:"opening,omitempty"`
	Agents    []ConvAgent `json:"agents"`
	CreatedAt time.Time   `json:"createdAt"`
}

// ConvAgent 会话内的自定义角色，带完整设定。
type ConvAgent struct {
	Key string `json:"key"`
	// CharacterID 是跨群记忆的身份。同一个角色被复制到不同群时保持不变；
	// 旧数据没有这个字段，读取时按名字派生。
	CharacterID string `json:"characterId,omitempty"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Emoji       string `json:"emoji,omitempty"`
	Color       string `json:"color,omitempty"`
	Prompt      string `json:"prompt"`
	// Permission 该角色的执行权限：readonly / approval / full。
	// 空值表示未设置，由工作层按场景默认（approval）。
	Permission string `json:"permission,omitempty"`
	// Scope 该角色允许写的路径（如 "web/" 或 "*.go"）。空表示不限。
	Scope []string `json:"scope,omitempty"`
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
//
// 支持 OpenAI 兼容的 tool calling：assistant 消息可携带 ToolCalls，
// role=tool 的消息用 ToolCallID 关联到被回答的那次调用。
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// Tool 暴露给模型的一个工具（JSON Schema 描述入参）。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall 一次工具调用。Arguments 是模型产出的 JSON 字符串，原样保留。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCallDelta 流式工具调用增量。Index 标识同一次调用。
type ToolCallDelta struct {
	Index     int    `json:"index"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
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
	// Tools 非空时开启 tool calling；ToolChoice: auto | none | required
	Tools      []Tool `json:"tools,omitempty"`
	ToolChoice string `json:"toolChoice,omitempty"`
}

// Chunk 流式返回的一段。
type Chunk struct {
	Content string `json:"content"`
	Done    bool   `json:"done"`
	// ToolCall 非空表示这一段是工具调用增量
	ToolCall *ToolCallDelta `json:"toolCall,omitempty"`
	// FinishReason 上游结束原因：stop | tool_calls | length
	FinishReason string `json:"finishReason,omitempty"`
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

// ChannelProbe 渠道体检结果：连通性、首字延迟、吐字量、剩余额度。
type ChannelProbe struct {
	OK          bool     `json:"ok"`
	HTTPStatus  int      `json:"httpStatus"`
	FirstByteMS int64    `json:"firstByteMs"` // 首个内容块到达耗时
	TotalMS     int64    `json:"totalMs"`
	Chars       int      `json:"chars"` // 正文吐字数（不含思考）
	Model       string   `json:"model"`
	BillingUSD  *float64 `json:"billingUsd,omitempty"` // 剩余额度（美元，new-api 风格；查不到就没有）
	Err         string   `json:"err,omitempty"`
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
	// InFlight 此刻正在这个渠道上执行的请求数——「路由到空闲渠道」依据的就是它
	InFlight int64  `json:"inFlight"`
	LastErr  string `json:"lastErr,omitempty"`
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
