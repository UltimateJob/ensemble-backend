package domain

import "time"

// Goal 团队目标看板上的一个目标。
//
// 目标与群（Conversation）或工作区 Run 关联：进度不是手填的百分比，
// 而是从关联的事实里派生（见 service.GoalProgress）——手填的数字
// 三天后一定和现实脱节，而看板存在的意义就是它还算数。
type Goal struct {
	ID             string `json:"id"`
	UserID         string `json:"userId"`
	ConversationID string `json:"conversationId,omitempty"`
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	// Status todo | doing | done。用户可手动改，也可以由进度自动推进。
	Status string `json:"status"`
	// Assignees 负责的角色 key（对应会话里的 ConvAgent.Key）。
	Assignees []string `json:"assignees,omitempty"`
	// RunIDs 关联的执行任务，进度与产出从这里取。
	RunIDs []string `json:"runIds,omitempty"`
	// ManualProgress 用户手动设定的进度（0-100）。为 -1 表示不手动覆盖，
	// 用派生进度。用 -1 而不是 0，是因为 0 是合法的手动值。
	ManualProgress int       `json:"manualProgress"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	// CompletedAt 完成时间，用于「本周完成」这类统计。
	CompletedAt time.Time `json:"completedAt"`
}

// Goal 状态常量。
const (
	GoalTodo  = "todo"
	GoalDoing = "doing"
	GoalDone  = "done"
)

// ValidGoalStatus 校验状态取值。
func ValidGoalStatus(s string) bool {
	return s == GoalTodo || s == GoalDoing || s == GoalDone
}

// Connector 一个外部服务连接器（飞书/Notion/GitHub/日历…）。
//
// 第一版只做「凭据存储 + 真调通」：不实现各家完整的 OAuth 授权页，
// 但凭据是真存的、连通性测试是真发请求的——比一个只会显示「已连接」
// 的假开关诚实。
type Connector struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	// Scope team（团队公共）| private（我的私有）
	Scope string `json:"scope"`
	// Kind 服务类型：feishu / notion / github / calendar / webhook
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Enabled 关闭后不会出现在 Agent 的可用工具里。
	Enabled bool `json:"enabled"`
	// Config 非敏感配置（base URL、工作区 ID 等）。可直接返回给客户端。
	Config map[string]string `json:"config,omitempty"`
	// Secrets 敏感凭据（token/app secret）。不通过 API 返回明文，
	// 只在落盘时加密（见 store 层的 sealSecret）。
	Secrets map[string]string `json:"-"`
	// SecretKeys 仅返回「哪些字段已配置」，让界面能显示「已填写」而不泄露值。
	SecretKeys []string `json:"secretKeys,omitempty"`
	// Status ok | error | unset。来自最近一次连通性测试。
	Status    string    `json:"status"`
	LastError string    `json:"lastError,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Connector 状态常量。
const (
	ConnStatusOK    = "ok"
	ConnStatusError = "error"
	ConnStatusUnset = "unset"
)

// Connector 作用域常量。
const (
	ConnScopeTeam    = "team"
	ConnScopePrivate = "private"
)

// Artifact 一个可归档的产出。
//
// 之前产物只是消息正文里的一段文字 + 编排器推的 28 字摘要，
// 没有独立身份：换个群就找不到，也没法按团队/角色检索。
// 资料库需要它是一等实体。
type Artifact struct {
	ID             string `json:"id"`
	UserID         string `json:"userId"`
	ConversationID string `json:"conversationId,omitempty"`
	// MessageID 产出它的那条消息，便于「回到原文」。
	MessageID string `json:"messageId,omitempty"`
	RunID     string `json:"runId,omitempty"`
	// AgentKey / AgentName 谁产出的。
	AgentKey  string `json:"agentKey,omitempty"`
	AgentName string `json:"agentName,omitempty"`
	// Title 产物名（如「技术应答」「接口定义」）。
	Title   string `json:"title"`
	Kind    string `json:"kind"` // doc | code | sheet | chart | note
	Content string `json:"content"`
	// Summary 列表页用的摘要，不返回全文。
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
