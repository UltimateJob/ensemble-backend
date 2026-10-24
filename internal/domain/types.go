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

	// Preferences 界面偏好（通知、默认视图等）。键值都是字符串，
	// 避免为每个开关加一个字段；空值不落盘。
	Preferences map[string]string `json:"preferences,omitempty"`
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
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Title  string `json:"title"`
	Studio string `json:"studio,omitempty"` // 工作室模板：bid | code | research | anime | companion | custom
	// Tagline 一句话简介。新建团队时可以填，显示在房间标题下方。
	Tagline string   `json:"tagline,omitempty"`
	Model   string   `json:"model"`
	Members []string `json:"members"` // 场景内置角色的 key

	// Agents 自定义角色。为空时用场景预设，非空时覆盖。
	// 这是「自己造角色拉进群」的落地方式。
	Agents []ConvAgent `json:"agents,omitempty"`

	// Collaborators 可以一起看和发言的用户 ID。
	// 创建者（UserID）永远有权限，这里存的是被邀请的人。
	Collaborators []string `json:"collaborators,omitempty"`

	// Workspace 绑定的本地工作区。nil 表示还没绑（代码工作室会提示绑定）。
	Workspace *ConvWorkspace `json:"workspace,omitempty"`
	// DefaultPermission 群默认执行权限：ask | approval | full。
	DefaultPermission string `json:"defaultPermission,omitempty"`
	// AgentPermissions 按角色覆盖的权限，key 是 agent key。
	AgentPermissions map[string]string `json:"agentPermissions,omitempty"`

	// Summary 早期对话的压缩摘要。
	//
	// 群聊一长，早期消息就会被字符预算挤掉，角色开始「失忆」——
	// 忘了之前定过什么方案、谁负责哪块。摘要代替直接丢弃：
	// 把挤出去的那部分压成几句事实，附在上下文最前面。
	//
	// SummaryUpTo 是已被摘要覆盖的最后一条消息 ID。用它做游标，
	// 每轮只压缩「新挤出去的部分」，不重复总结。
	Summary     string `json:"summary,omitempty"`
	SummaryUpTo string `json:"summaryUpTo,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ConvWorkspace 会话绑定的工作区。当前只支持 local；
// cloud 是预留值，服务端会明确拒绝，而不是静默忽略。
type ConvWorkspace struct {
	ID      string    `json:"id,omitempty"`
	Kind    string    `json:"kind"` // local | cloud
	Root    string    `json:"root"`
	Label   string    `json:"label,omitempty"`
	BoundAt time.Time `json:"boundAt,omitempty"`
}

// SavedAgent 用户角色库里的角色。可以反复拉进不同的群。
// 创建群时会被复制成 ConvAgent（快照），之后改库不影响已有群。
type SavedAgent struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Emoji  string `json:"emoji,omitempty"`
	Color  string `json:"color,omitempty"`
	Prompt string `json:"prompt"`
	Source string `json:"source,omitempty"` // draft（AI 生成）| manual（手写）
	// Upgraded 升级角色：发言走任务池模型（deepseek-v4.1-flash 这类），
	// 处理文件、写代码等高难度任务能力更强。默认不升级。
	Upgraded bool `json:"upgraded,omitempty"`
	// Capabilities 创建时授予的能力，如 "image"（可调用生图）。
	Capabilities []string  `json:"capabilities,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
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
	// Upgraded 升级角色：进群时从角色库快照带过来，群主可改。
	// 升级后发言走任务池模型（deepseek-v4.1-flash 这类）。
	Upgraded bool `json:"upgraded,omitempty"`
	// Permission 该角色的执行权限：readonly / approval / full。
	// 空值表示未设置，由工作层按场景默认（approval）。
	Permission string `json:"permission,omitempty"`
	// Scope 该角色允许写的路径（如 "web/" 或 "*.go"）。空表示不限。
	Scope []string `json:"scope,omitempty"`
	// Capabilities 这个角色被显式授予的能力，如 "image"。
	// 生图不是「配了渠道就人人能用」——只有带 image 能力的角色才拿得到
	// image_generate 工具，否则一个写文案的角色也能悄悄花掉生图额度。
	Capabilities []string `json:"capabilities,omitempty"`
}

// CapImage 生图能力。
const CapImage = "image"

// HasCapability 判断角色是否被授予某项能力。
func (a *ConvAgent) HasCapability(cap string) bool {
	for _, c := range a.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
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
	// 以下是执行器落群消息用的可选字段。旧数据没有，解码为零值。
	Attachments []ArtifactRef `json:"attachments,omitempty"`
	Kind        string        `json:"kind,omitempty"`     // text | run | run.failed | team | system | capability | task
	Ref         string        `json:"ref,omitempty"`      // run id / team id
	Mentions    []string      `json:"mentions,omitempty"` // 被 @ 的 agent key
	// Capability 能力卡的内容（Kind=capability 时有值）。
	// 设计图里它是「标题 + 三条要点」，不再是纯文本。
	Capability *CapabilityCard `json:"capability,omitempty"`
	// TaskCard 任务卡（Kind=task 时有值）。
	TaskCard *MessageTask `json:"taskCard,omitempty"`
	// Badge 名字后的徽章：official（官方）/ host（主持）
	Badge string `json:"badge,omitempty"`
}

// ArtifactRef 一条产物引用：谁、在哪个工作区、哪个文件、多大。
type ArtifactRef struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // file | doc | sheet | note
	Name      string    `json:"name"`
	Path      string    `json:"path,omitempty"` // 工作区相对路径
	Size      int64     `json:"size,omitempty"`
	AgentKey  string    `json:"agentKey,omitempty"`
	AgentName string    `json:"agentName,omitempty"`
	RunID     string    `json:"runId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
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
	// Task 请求来自任务执行器：自动选路只在 TaskOnly 模型里挑
	Task bool `json:"task,omitempty"`
	// Workload 任务性质，决定走哪个模型池。空表示不限制。
	//
	// 实测（同一批任务同时跑 deepseek-chat 与任务池 Flash）：
	// 编程、长文档分析、数据整理三类上两者质量相当，Flash 快 10-20%、
	// 省 15-35% token；创意写作与决策分析各有长短，没有稳定的优劣。
	// 所以分池的依据是「这类任务更需要什么」，不是「谁更强」：
	//   chat    闲聊、问答——要稳定、便宜
	//   code    编程、执行——要工具调用可靠、快
	//   writing 长文产出——要能撑住长上下文
	//   analysis 分析决策——要能处理长输入
	Workload Workload `json:"workload,omitempty"`
	// 路由提示：指定 provider 时优先使用
	Provider string `json:"provider,omitempty"`
	// Tools 非空时开启 tool calling；ToolChoice: auto | none | required
	Tools      []Tool `json:"tools,omitempty"`
	ToolChoice string `json:"toolChoice,omitempty"`
	// ToolChoiceName 指定必须调用的工具名。比 required 更严格：
	// 部分上游会把 required 当成 auto，模型于是写一长段正文而不调工具
	// （基准里规划阶段因此被 5000 token 截断）。
	ToolChoiceName string `json:"toolChoiceName,omitempty"`
	// UserID 发起请求的用户，用于配额与成本归因。只由服务端填充，
	// 不接受客户端 JSON 注入（json:"-"）。
	UserID string `json:"-"`
	// ConversationID 可选，便于后续做会话级成本归因。
	ConversationID string `json:"-"`
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

// ChannelProbe 渠道体检结果：连通性、首字延迟、吐字量/速度、剩余额度。
type ChannelProbe struct {
	OK          bool     `json:"ok"`
	HTTPStatus  int      `json:"httpStatus"`
	FirstByteMS int64    `json:"firstByteMs"` // 首个内容块到达耗时
	TotalMS     int64    `json:"totalMs"`
	Chars       int      `json:"chars"`     // 正文吐字数（含思考增量）
	Tokens      int      `json:"tokens"`    // 输出 token 数：优先 usage，缺失时按增量估算
	TokPerSec   float64  `json:"tokPerSec"` // 首字之后的输出速度
	Model       string   `json:"model"`
	BillingUSD  *float64 `json:"billingUsd,omitempty"` // 剩余额度（美元，new-api 风格；查不到就没有）
	Err         string   `json:"err,omitempty"`
}

// ModelInfo 模型注册信息，管理员可改。
// Workload 任务性质。
type Workload string

const (
	WorkloadChat     Workload = "chat"
	WorkloadCode     Workload = "code"
	WorkloadWriting  Workload = "writing"
	WorkloadAnalysis Workload = "analysis"
)

// Valid 是否是已知的 workload。
func (w Workload) Valid() bool {
	switch w {
	case WorkloadChat, WorkloadCode, WorkloadWriting, WorkloadAnalysis:
		return true
	}
	return false
}

type ModelInfo struct {
	Key      string `json:"key"`      // 唯一标识，如 deepseek-chat
	Provider string `json:"provider"` // deepseek | mock
	// TaskOnly 任务专用模型：给执行器（代码开发这类真实任务）用，
	// 不出现在用户的选择器里，聊天的自动选路也会跳过它。
	TaskOnly bool   `json:"taskOnly,omitempty"`
	Upstream string `json:"upstream"` // 上游模型名
	Enabled  bool   `json:"enabled"`
	// Rank 路由排名，越小越优先。同 rank 按 Weight 加权轮询。
	// 0 表示未设置（排在有明确排名的渠道之后，但仍在池内）。
	Rank int `json:"rank,omitempty"`
	// Concurrency 该渠道允许的同时在途请求数。0 用默认值（8）。
	// 达到上限后新请求会溢出到同档其他渠道或下一档，而不是排队。
	Concurrency int `json:"concurrency,omitempty"`
	// 同排名内的加权轮询权重。
	Weight int `json:"weight"`
	// 成本按人民币计，单位是「每 1M Token」。
	// 输入/输出分开计价，才能算准真实毛利；旧字段 CostPer1K 仅作兼容回退。
	CostPer1K       float64 `json:"costPer1k,omitempty"`
	InputCostPer1M  float64 `json:"inputCostPer1M,omitempty"`
	OutputCostPer1M float64 `json:"outputCostPer1M,omitempty"`
	Note            string  `json:"note,omitempty"`
	// Workloads 这个模型适合哪些任务性质。空表示不限。
	//
	// 这是「偏好」不是「约束」：优先在这些任务上选它，但池里没有
	// 更合适的模型时仍然可以用——配错标签不该让请求直接失败。
	Workloads []Workload `json:"workloads,omitempty"`
}

// UpstreamInfo 管理台展示的一个模型上游。不含密钥，只有「是否已配置」。
type UpstreamInfo struct {
	Name     string `json:"name"`
	BaseURL  string `json:"baseUrl,omitempty"`
	Model    string `json:"model,omitempty"` // 该上游的默认模型（可空）
	HasKey   bool   `json:"hasKey"`
	Managed  bool   `json:"managed"`  // 在管理台可编辑/删除（存于 data/upstreams.json）
	Reserved bool   `json:"reserved"` // 由服务器环境变量 EXTRA_PROVIDERS 提供
	State    string `json:"state"`    // 熔断状态 closed | open | half-open
}

// QuotaWindow 一个滚动额度窗口（例如 5 小时 / 7 天）。
// Used/Limit 为同一单位；Percent 是已用百分比，方便直接画进度条。
type QuotaWindow struct {
	Label     string    `json:"label"` // 5 小时 | 7 天
	Unit      string    `json:"unit"`  // USD | CNY | %
	Used      float64   `json:"used"`
	Limit     float64   `json:"limit"`
	Remaining float64   `json:"remaining"`
	Percent   float64   `json:"percent"`
	ResetAt   time.Time `json:"resetAt,omitempty"`
}

// ChannelQuota 一个上游渠道当前的额度状态。
//
// 不同渠道能拿到的东西不一样：
//   - DeepSeek 官方：账户余额（CNY）
//   - Command Code：月度额度 + 5h/7d 窗口 + 套餐
//   - OpenDesign：钱包余额 + 5h/7d 窗口（需要 Control Key + 工作区 ID）
//   - GLM / Z.ai：5h/7d 窗口百分比
//
// 查询失败时保留 Provider/Kind，Err 里给出可读原因，不影响页面其它渠道。
type ChannelQuota struct {
	Provider   string        `json:"provider"`
	Kind       string        `json:"kind"` // deepseek | commandcode | opendesign | glm
	Configured bool          `json:"configured"`
	Currency   string        `json:"currency,omitempty"` // CNY | USD | %
	Remaining  *float64      `json:"remaining,omitempty"`
	Total      *float64      `json:"total,omitempty"`
	Plan       string        `json:"plan,omitempty"`
	Windows    []QuotaWindow `json:"windows,omitempty"`
	Source     string        `json:"source,omitempty"`
	FetchedAt  time.Time     `json:"fetchedAt"`
	Err        string        `json:"err,omitempty"`
}

// PlanConfig 套餐配置。价格与成本统一按人民币计。
//
// 套餐是「售卖维度」，额度是「用量维度」：PriceMonthly 用来算 MRR，
// MonthlyQuota 是每月可调用次数（沿用现有 User.Quota 的计量方式）。
type PlanConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	PriceMonthly float64  `json:"priceMonthly"`
	MonthlyQuota int      `json:"monthlyQuota"`
	Description  string   `json:"description,omitempty"`
	Features     []string `json:"features,omitempty"`
	Color        string   `json:"color,omitempty"`
	Enabled      bool     `json:"enabled"`
	Sort         int      `json:"sort,omitempty"`

	// ---- 权限 ----
	// AllowedModels 可用模型白名单；空表示全部启用模型都可用。
	// 超出套餐的模型请求会退回白名单内的自动选路，而不是直接报错。
	AllowedModels []string `json:"allowedModels,omitempty"`
	// RatePerMinute 每用户每分钟请求数；0 表示使用服务默认值。
	RatePerMinute int `json:"ratePerMinute,omitempty"`
	// MaxConcurrency 每用户同时在途请求数上限；0 表示不限。
	MaxConcurrency int `json:"maxConcurrency,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UsageEvent 一次上游调用的实际用量，用于成本核算。
// 由网关在拿到上游 usage 后发出，服务层负责聚合与落盘。
type UsageEvent struct {
	UserID           string    `json:"userId,omitempty"`
	Model            string    `json:"model"`
	Provider         string    `json:"provider,omitempty"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	TotalTokens      int       `json:"totalTokens"`
	CostCNY          float64   `json:"cost"`
	At               time.Time `json:"at"`
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

// CapabilityCard 能力介绍卡（设计图：纸团能帮你做什么）。
type CapabilityCard struct {
	Title string           `json:"title"`
	Items []CapabilityItem `json:"items"`
}

// CapabilityItem 能力卡里的一条。
type CapabilityItem struct {
	Title string `json:"title"`
	Desc  string `json:"desc"`
}

// MessageTask 消息里的任务卡。
type MessageTask struct {
	Title      string `json:"title"`
	Status     string `json:"status"` // done | doing | blocked
	StatusText string `json:"statusText,omitempty"`
	// File 任务产出的文件（设计图里任务卡内嵌一个文件行）。
	File *ArtifactRef `json:"file,omitempty"`
}
