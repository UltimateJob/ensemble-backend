package domain

import "time"

// TeamRun 一次团队任务。区别于单条 Run：
// 它包含规划、多个并行 Run、确定性验证和返工，是一个有阶段的工作流。
type TeamRun struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
	UserID         string `json:"userId"`
	Title          string `json:"title,omitempty"`
	Task           string `json:"task"`
	Status         string `json:"status"` // planning | building | verifying | repairing | reviewing | done | failed | cancelled
	Phase          string `json:"phase,omitempty"`

	MessageID string `json:"messageId,omitempty"`

	Plan        *TeamPlan        `json:"plan,omitempty"`
	Assignments []TeamAssignment `json:"assignments,omitempty"`
	Phases      []TeamPhase      `json:"phases,omitempty"`
	Checks      []TeamCheck      `json:"checks,omitempty"`
	Issues      []string         `json:"issues,omitempty"`
	Review      *TeamReview      `json:"review,omitempty"`

	Score     int       `json:"score"`
	Repairs   int       `json:"repairs"`
	Error     string    `json:"error,omitempty"`
	Warning   string    `json:"warning,omitempty"`
	TokensIn  int       `json:"tokensIn"`
	TokensOut int       `json:"tokensOut"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
}

// TeamPlan 规划阶段的产出。由模型通过 submit_plan 工具提交，
// 服务端校验后才执行——不是从自然语言里猜。
type TeamPlan struct {
	Objective    string           `json:"objective"`
	Acceptance   []string         `json:"acceptance,omitempty"`
	Stack        []string         `json:"stack,omitempty"`
	Contracts    string           `json:"contracts,omitempty"`
	Assignments  []TeamAssignment `json:"assignments"`
	Verification []TeamCommand    `json:"verification,omitempty"`
	ReviewerKey  string           `json:"reviewerKey,omitempty"`
	Notes        string           `json:"notes,omitempty"`
}

// TeamAssignment 一条分工：谁做、做什么、只许改哪里。
type TeamAssignment struct {
	AgentKey  string   `json:"agentKey"`
	AgentName string   `json:"agentName,omitempty"`
	Task      string   `json:"task"`
	Scope     []string `json:"scope,omitempty"`
}

// TeamCommand 一条可验证的命令。argv 不经 shell。
type TeamCommand struct {
	Name string   `json:"name"`
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd,omitempty"`
}

// TeamPhase 一个阶段的记录。前端据此渲染阶段条和对应的执行气泡。
type TeamPhase struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // running | done | failed
	RunIDs    []string  `json:"runIds,omitempty"`
	Note      string    `json:"note,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
}

// TeamCheck 一条确定性验证结果。
type TeamCheck struct {
	Name     string   `json:"name"`
	Argv     []string `json:"argv,omitempty"`
	Cwd      string   `json:"cwd,omitempty"`
	Passed   bool     `json:"passed"`
	ExitCode int      `json:"exitCode"`
	Output   string   `json:"output,omitempty"`
	Ms       int64    `json:"ms"`
	Skipped  bool     `json:"skipped,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// TeamReview 验收 Agent 的结论。
type TeamReview struct {
	AgentKey string   `json:"agentKey"`
	RunID    string   `json:"runId,omitempty"`
	Verdict  string   `json:"verdict,omitempty"` // pass | issues | error
	Summary  string   `json:"summary,omitempty"`
	Issues   []string `json:"issues,omitempty"`
	// Blockers 是阻塞验收的问题，只有它们会触发返工。
	// Nits 是不影响验收的改进建议，展示但不返工——否则验收人的个人偏好
	// 会让已经通过确定性验证的交付再烧一轮 token（基准里真实发生过）。
	Blockers []string `json:"blockers,omitempty"`
	Nits     []string `json:"nits,omitempty"`
}
