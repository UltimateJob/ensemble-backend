// Package agent 实现工作层：Run 生命周期、工具调用循环、三档权限与审批。
//
// 分层（见 docs/agent-executor.md）：
//
//	会话层（service.Orchestrator）负责「角色在群里说话」
//	工作层（本包）负责「角色真的动手」：Run → ToolCall → Approval → Diff
//	运行时层（internal/workspace）负责「本地文件与命令」
//
// 权限必须在服务端强制执行，不能写进提示词——模型可以被说服，代码不行。
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/workspace"
)

// Permission 一个 Agent 的执行权限。
type Permission string

const (
	// PermissionReadonly 只读：能看，不能改，不能跑命令。
	PermissionReadonly Permission = "readonly"
	// PermissionApproval 写工作区自动（有 diff），执行命令每次需批准。
	PermissionApproval Permission = "approval"
	// PermissionFull 完全访问：文件工具受工作区约束，命令等同当前系统用户权限。
	PermissionFull Permission = "full"
	// PermissionAudit 复审：能读、能跑命令（验证测试），但不能写文件。
	// 专给「跑完复审一遍」那一轮用——它必须能真的执行 go test，
	// 又绝不能顺手改代码（否则复审就变成了第二次施工）。
	PermissionAudit Permission = "audit"
)

// ValidPermission 校验权限字符串。
func ValidPermission(p Permission) bool {
	switch p {
	case PermissionReadonly, PermissionApproval, PermissionFull, PermissionAudit:
		return true
	}
	return false
}

// Status Run 状态。
type Status string

const (
	StatusRunning   Status = "running"
	StatusWaiting   Status = "waiting_approval"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusAborted   Status = "aborted"
)

// 默认预算。数值不是随手写的：20 步够一次「读→改→测→修」，再多就该拆任务。
const (
	DefaultMaxSteps    = 20
	MaxAllowedSteps    = 60
	DefaultRunTimeout  = 10 * time.Minute
	ApprovalWait       = 10 * time.Minute
	MaxToolOutputBytes = 8 << 10 // 回填给模型的结果上限
	// maxParallelRuns 同一工作区允许同时跑的任务数
	maxParallelRuns = 3
	// maxReviewRounds 复审最多几轮（防止修复-复审无限循环）
	maxReviewRounds = 2
)

var (
	ErrRunNotFound      = errors.New("agent: run not found")
	ErrRunBusy          = errors.New("agent: workspace already has a running run")
	ErrPermissionDenied = errors.New("agent: permission denied")
	ErrNoWorkspace      = errors.New("agent: no workspace bound")
)

// ToolCall 一次工具调用记录。
type ToolCall struct {
	ID         string    `json:"id"`
	RunID      string    `json:"runId"`
	AgentKey   string    `json:"agentKey"`
	Tool       string    `json:"tool"`
	Args       string    `json:"args"`
	Risk       string    `json:"risk"`
	Status     string    `json:"status"` // ok | error | denied | pending_approval
	Output     string    `json:"output,omitempty"`
	ExitCode   int       `json:"exitCode,omitempty"`
	Diff       string    `json:"diff,omitempty"`
	ApprovalID string    `json:"approvalId,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
	DurationMS int64     `json:"durationMs"`
}

// Approval 一次待批准的调用。
type Approval struct {
	ID         string    `json:"id"`
	RunID      string    `json:"runId"`
	ToolCallID string    `json:"toolCallId"`
	AgentKey   string    `json:"agentKey"`
	Tool       string    `json:"tool"`
	Risk       string    `json:"risk"`
	Summary    string    `json:"summary"`
	Command    string    `json:"command,omitempty"`
	Diff       string    `json:"diff,omitempty"`
	Status     string    `json:"status"`          // pending | approved | rejected | expired
	Scope      string    `json:"scope,omitempty"` // once | run | session
	DecidedBy  string    `json:"decidedBy,omitempty"`
	DecidedAt  time.Time `json:"decidedAt,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`

	decided chan bool
	once    sync.Once
}

// Run 一次任务执行。
type Run struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspaceId"`
	ConversationID string     `json:"conversationId"`
	UserID         string     `json:"userId"`
	OperatorKey    string     `json:"operatorKey"`
	Permission     Permission `json:"permission"`
	// Scope 写入边界（相对路径/glob）。空表示不限。
	Scope Scope `json:"scope,omitempty"`
	// AutoReview 成功后自动起一轮只读复审
	AutoReview bool `json:"autoReview,omitempty"`
	// ReviewRunID 指向自动复审的 Run
	ReviewRunID string `json:"reviewRunId,omitempty"`
	// ParentRunID 复审轮指回主 Run
	ParentRunID string `json:"parentRunId,omitempty"`
	// Chain 整条链的汇总（主 Run + 复审 + …）
	Chain *Chain `json:"chain,omitempty"`
	// Role 主 Run 为空；复审 "review"；修复 "fix"
	Role string `json:"role,omitempty"`
	// ReviewRound 第几轮复审（从 1 开始）
	ReviewRound int `json:"reviewRound,omitempty"`
	// AutoFix 复审发现 issues 时自动起修复轮
	AutoFix    bool                 `json:"autoFix,omitempty"`
	Task       string               `json:"task"`
	Model      string               `json:"model,omitempty"`
	Status     Status               `json:"status"`
	Step       int                  `json:"step"`
	MaxSteps   int                  `json:"maxSteps"`
	TokensIn   int                  `json:"tokensIn"`
	TokensOut  int                  `json:"tokensOut"`
	CostUSD    float64              `json:"costUsd"`
	StartedAt  time.Time            `json:"startedAt"`
	EndedAt    time.Time            `json:"endedAt"`
	SnapshotID string               `json:"snapshotId,omitempty"`
	Diffs      []workspace.FileDiff `json:"diffs,omitempty"`
	DiffTotal  int                  `json:"diffTotal"`
	ToolCalls  []*ToolCall          `json:"toolCalls,omitempty"`
	// approved 本次 Run 内已批准的同类命令（scope=run），避免每条命令都打扰用户
	approved map[string]bool
	Summary  string  `json:"summary,omitempty"`
	Error    string  `json:"error,omitempty"`
	Events   []Event `json:"-"`

	cancel context.CancelFunc
}

// Event 运行事件（SSE 推送 + 刷新后重放）。
type Event struct {
	Seq  int       `json:"seq"`
	Type string    `json:"type"`
	At   time.Time `json:"at"`
	Data any       `json:"data,omitempty"`
}

// 事件类型。
const (
	EventRunStart  = "run.start"
	EventStepStart = "step.start"
	EventAssistant = "assistant.message"
	EventToolStart = "tool.start"
	EventToolEnd   = "tool.end"
	EventApproval  = "approval.required"
	EventDiff      = "diff"
	EventRunEnd    = "run.end"
	EventError     = "error"
)

// Workspace 绑定到某个会话的工作区。
type Workspace struct {
	ID             string    `json:"id"`
	UserID         string    `json:"userId"`
	ConversationID string    `json:"conversationId"`
	Root           string    `json:"root"`
	Label          string    `json:"label,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	ws             *workspace.Workspace
}

// UsageRecorder 在 Run 结束时上报用量。
//
// 执行器的 token 是真金白银，之前完全没进账——报告里发现的问题。
// 用回调而不是直接依赖 store，避免工作层反向依赖存储层。
type UsageRecorder func(userID string, tokensIn, tokensOut int)

// Manager 持有工作区、运行中的 Run 与审批队列。
type Manager struct {
	mu         sync.Mutex
	workspaces map[string]*Workspace
	runs       map[string]*Run
	approvals  map[string]*Approval
	snapshots  map[string]*workspace.Snapshot
	subs       map[string][]chan Event
	// 同一工作区允许并行 Run，但 scope 不能重叠（写入冲突的根因）
	active map[string][]*Run
	// writers 记录「哪个 Run 最近写了哪个文件」，用于文件级互斥。
	// scope 只能挡住「明显不该并行」的组合；两个 Run 都合法但会改同一文件时，
	// 靠这张表让后来的那个先失败、重读、再改，而不是静默覆盖。
	writers map[string]string
	seq     int
	// usage 可选的用量上报
	usage UsageRecorder
	// model 主模型引用（复审 Run 复用同一个网关）
	model Model
	// statePath 状态落盘路径；空表示纯内存
	statePath string
}

// NewManager 创建管理器。
func NewManager() *Manager {
	return &Manager{
		workspaces: make(map[string]*Workspace),
		runs:       make(map[string]*Run),
		approvals:  make(map[string]*Approval),
		snapshots:  make(map[string]*workspace.Snapshot),
		subs:       make(map[string][]chan Event),
		active:     make(map[string][]*Run),
		writers:    make(map[string]string),
	}
}

func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b)
}

// SetModel 注入模型（复审轮复用）。
func (m *Manager) SetModel(md Model) {
	m.mu.Lock()
	m.model = md
	m.mu.Unlock()
}

// SetUsageRecorder 注入用量上报。
func (m *Manager) SetUsageRecorder(fn UsageRecorder) {
	m.mu.Lock()
	m.usage = fn
	m.mu.Unlock()
}

// BindWorkspace 绑定一个本机目录。
func (m *Manager) BindWorkspace(userID, convID, root, label string) (*Workspace, error) {
	ws, err := workspace.Open(root)
	if err != nil {
		return nil, err
	}
	w := &Workspace{
		ID:             newID("wsp"),
		UserID:         userID,
		ConversationID: convID,
		Root:           ws.Root,
		Label:          label,
		CreatedAt:      time.Now(),
		ws:             ws,
	}
	m.mu.Lock()
	m.workspaces[w.ID] = w
	m.mu.Unlock()
	m.SaveAsync()
	return w, nil
}

// Workspace 查找工作区（只允许属主）。
func (m *Manager) Workspace(id, userID string) (*Workspace, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workspaces[id]
	if !ok || w.UserID != userID {
		return nil, false
	}
	return w, true
}

// ListWorkspaces 列出用户的工作区。
func (m *Manager) ListWorkspaces(userID string) []*Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Workspace, 0)
	for _, w := range m.workspaces {
		if w.UserID == userID {
			out = append(out, w)
		}
	}
	return out
}

// UnbindWorkspace 解绑。
func (m *Manager) UnbindWorkspace(id, userID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workspaces[id]
	if !ok || w.UserID != userID {
		return false
	}
	if len(m.active[id]) > 0 {
		return false
	}
	delete(m.workspaces, id)
	return true
}

// fs 暴露底层工作区（同包内使用）。
func (w *Workspace) fs() *workspace.Workspace { return w.ws }

// FS 暴露底层文件系统工具（HTTP 层浏览文件用）。
func (w *Workspace) FS() *workspace.Workspace { return w.ws }

// GetRun 查询运行。
func (m *Manager) GetRun(id string) (*Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	return r, ok
}

// ListRuns 列出某工作区的运行（最新在前）。
func (m *Manager) ListRuns(workspaceID string, limit int) []*Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Run
	for _, r := range m.runs {
		if r.WorkspaceID == workspaceID {
			out = append(out, r)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Subscribe 订阅事件流。返回取消函数。
func (m *Manager) Subscribe(runID string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	m.mu.Lock()
	if r, ok := m.runs[runID]; ok {
		// 先重放已有事件，刷新页面后不丢上下文
		for _, e := range r.Events {
			select {
			case ch <- e:
			default:
			}
		}
	}
	m.subs[runID] = append(m.subs[runID], ch)
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		list := m.subs[runID]
		for i, c := range list {
			if c == ch {
				m.subs[runID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		close(ch)
	}
}

func (m *Manager) emit(r *Run, typ string, data any) {
	m.mu.Lock()
	m.seq++
	e := Event{Seq: m.seq, Type: typ, At: time.Now(), Data: data}
	r.Events = append(r.Events, e)
	subs := append([]chan Event(nil), m.subs[r.ID]...)
	m.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default: // 订阅者慢就丢，事件仍在 r.Events 里可重放
		}
	}
}

// Decide 批准或拒绝一次调用。
//
// scope 只对批准有意义：once（本次）、run（本 Run 同类命令）、session（本会话同类命令）。
func (m *Manager) Decide(approvalID, userID string, approve bool, scope string) error {
	m.mu.Lock()
	a, ok := m.approvals[approvalID]
	if !ok {
		m.mu.Unlock()
		return ErrRunNotFound
	}
	r, ok := m.runs[a.RunID]
	if !ok || r.UserID != userID {
		m.mu.Unlock()
		return ErrRunNotFound
	}
	if a.Status != "pending" {
		m.mu.Unlock()
		return fmt.Errorf("agent: approval already %s", a.Status)
	}
	if approve {
		a.Status = "approved"
		if scope == "" {
			scope = "once"
		}
		a.Scope = scope
		if scope == "run" {
			if r.approved == nil {
				r.approved = map[string]bool{}
			}
			r.approved[a.Risk] = true
		}
	} else {
		a.Status = "rejected"
	}
	a.DecidedBy = userID
	a.DecidedAt = time.Now()
	a.once.Do(func() { close(a.decided) })
	m.mu.Unlock()
	return nil
}

// Abort 取消一次运行。
func (m *Manager) Abort(runID, userID string) error {
	m.mu.Lock()
	r, ok := m.runs[runID]
	if !ok || r.UserID != userID {
		m.mu.Unlock()
		return ErrRunNotFound
	}
	cancel := r.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// permissionFor 决定一次调用是否放行、是否需要审批。
//
// 这是唯一的权限判定入口：HTTP 层、runner、审批回调都必须走这里，
// 避免出现「某条路径忘了检查」。
func permissionFor(p Permission, tool string, risk string) (allowed bool, needApproval bool, reason string) {
	readOnlyTool := tool == "read" || tool == "list" || tool == "glob" || tool == "grep"
	switch p {
	case PermissionReadonly:
		if readOnlyTool {
			return true, false, ""
		}
		return false, false, "只读权限不能写文件或执行命令"
	case PermissionApproval:
		if readOnlyTool {
			return true, false, ""
		}
		if isWriteTool(tool) {
			return true, false, "" // 写工作区自动放行，但会产生 diff/snapshot
		}
		if tool == "run" && risk == workspace.RiskRead {
			return true, false, ""
		}
		return true, true, "该命令需要批准"
	case PermissionFull:
		if risk == workspace.RiskCritical {
			return false, false, "命中不可逆命令黑名单"
		}
		return true, false, ""
	case PermissionAudit:
		if risk == workspace.RiskCritical {
			return false, false, "命中不可逆命令黑名单"
		}
		if isWriteTool(tool) {
			return false, false, "复审轮不能改文件"
		}
		return true, false, ""
	}
	return false, false, "未知权限"
}

// Revert 把工作区恢复到该 Run 开始前的快照。
//
// 只允许 Run 的属主操作；快照没留存时返回错误，而不是假装成功。
func (m *Manager) Revert(runID, userID string) (int, error) {
	m.mu.Lock()
	r, ok := m.runs[runID]
	if !ok || r.UserID != userID {
		m.mu.Unlock()
		return 0, ErrRunNotFound
	}
	snap := m.snapshots[runID]
	w := m.workspaces[r.WorkspaceID]
	busy := len(m.active[r.WorkspaceID]) > 0
	m.mu.Unlock()
	if busy {
		// 并行运行期间回滚会连带回滚别人的改动，宁可不做。
		return 0, fmt.Errorf("agent: 工作区还有任务在跑，等它们结束再回滚")
	}
	if snap == nil || w == nil {
		return 0, fmt.Errorf("agent: snapshot unavailable for run %s", runID)
	}
	changed, err := w.fs().Revert(snap)
	if err != nil {
		return changed, err
	}
	diffs, total, derr := w.fs().Diff(snap, 200)
	m.mu.Lock()
	if derr == nil {
		r.Diffs = diffs
		r.DiffTotal = total
	}
	m.mu.Unlock()
	return changed, nil
}

// pruneSnapshots 只保留最近 keep 个快照，避免临时目录无限增长。
func (m *Manager) pruneSnapshots(keep int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, snap := range m.snapshots {
		if r, ok := m.runs[id]; ok && r.Status == StatusRunning {
			continue
		}
		snap.Cleanup()
		delete(m.snapshots, id)
	}
}

// conflictLocked 判断新 Run 与工作区内已活动的 Run 是否冲突。
//
// 规则：两个 Run 的 scope 必须完全可以区分（不相交），才允许并行。
// 空 scope 表示「整个工作区」，与任何 Run 都冲突。
// 调用方必须持有 m.mu。
func conflictLocked(active []*Run, scope []string) *Run {
	for _, r := range active {
		if scopesOverlap(r.Scope, scope) {
			return r
		}
	}
	return nil
}

// scopesOverlap 判断两组写入边界是否有交集。
func scopesOverlap(a, b []string) bool {
	// 任一方为空（不限）都视为全覆盖
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		x = strings.TrimSpace(x)
		if x == "" {
			return true
		}
		for _, y := range b {
			y = strings.TrimSpace(y)
			if y == "" {
				return true
			}
			if scopeTokensOverlap(x, y) {
				return true
			}
		}
	}
	return false
}

// scopeTokensOverlap 粗略判断两个 scope 词是否有路径交集。
//
// 只做「保守」判断：拿不准就当作重叠（拒绝并行），
// 宁可串行慢一点，也不要让两个 agent 同时改同一个文件。
func scopeTokensOverlap(x, y string) bool {
	px, dirX := strings.TrimSuffix(x, "/"), strings.HasSuffix(x, "/")
	py, dirY := strings.TrimSuffix(y, "/"), strings.HasSuffix(y, "/")

	// 两边都是目录前缀：看是否互为前缀
	if dirX && dirY {
		return px == py || strings.HasPrefix(px, py+"/") || strings.HasPrefix(py, px+"/")
	}
	// 目录对 glob / 文件名：用目录做前缀判断
	if dirX != dirY {
		dir, other := px, py
		if dirY {
			dir, other = py, px
		}
		if other == "*" || other == "**" {
			return true
		}
		// 目录下的文件可能被 glob 命中
		if strings.HasSuffix(other, "/*") || strings.HasSuffix(other, "/**") {
			return strings.HasPrefix(other, dir+"/") || strings.HasPrefix(dir, other)
		}
		// 纯文件名（*.go）：与目录不相交，除非目录本身就在该文件名模式内（无法判断）
		if !strings.Contains(other, "/") {
			return false
		}
		return strings.HasPrefix(other, dir+"/") || strings.HasPrefix(dir, other)
	}
	// 两个都是文件名/glob：完全相同或都含 * 时当作重叠
	if px == py {
		return true
	}
	if strings.Contains(px, "*") || strings.Contains(py, "*") {
		return true
	}
	return false
}

// claimFile 尝试占用一个文件的写权（短锁，写完立刻 release）。
//
// 返回空字符串表示可以写；否则返回占用者（另一个 Run 的 ID）。
// 它防的是「两个 Run 同时写同一个文件」的撕裂，不防「先后覆盖」——
// 后者由模型自己的 read-before-edit 习惯和 scope 兜。
func (m *Manager) claimFile(path, runID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner, ok := m.writers[path]; ok && owner != runID {
		return owner
	}
	m.writers[path] = runID
	return ""
}

// releaseFile 释放单个文件（短锁）。
//
// 长锁会活锁：A 持有到 Run 结束，B 每步都撞墙。
// 写入本身是原子的（temp+rename），只需要盖住「并发写」这一段。
func (m *Manager) releaseFile(path, runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner, ok := m.writers[path]; ok && owner == runID {
		delete(m.writers, path)
	}
}

// releaseFiles 在 Run 结束时清掉它占用的文件。
func (m *Manager) releaseFiles(runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for p, owner := range m.writers {
		if owner == runID {
			delete(m.writers, p)
		}
	}
}

// isWriteTool 判断某个工具是否会写工作区。
func isWriteTool(tool string) bool {
	switch tool {
	case "write", "edit", "docx_create", "xlsx_create":
		return true
	}
	return false
}

// toolOutputs 收集本次 Run 所有工具调用的输出，用于核对文档里的数字来源。
func (r *Run) toolOutputs() []string {
	out := make([]string, 0, len(r.ToolCalls))
	for _, tc := range r.ToolCalls {
		if tc.Output != "" {
			out = append(out, tc.Output)
		}
	}
	return out
}

// SaveAsync 异步落盘，不阻塞调用方。
func (m *Manager) SaveAsync() {
	if m.statePath == "" {
		return
	}
	go func() { _ = m.Save() }()
}

// Chain 一条任务链的合计账。
//
// 用户问的是「这次一共花了多少」，不是「主 Run 花了多少」。
// 复审、修复都是这条链的一部分，必须算进去。
type Chain struct {
	Runs      int     `json:"runs"`
	TokensIn  int     `json:"tokensIn"`
	TokensOut int     `json:"tokensOut"`
	Seconds   float64 `json:"seconds"`
	ToolCalls int     `json:"toolCalls"`
	Steps     int     `json:"steps"`
	DiffFiles int     `json:"diffFiles"`
}

// rollupChainLocked 把整条链的用量汇总到根 Run 上。调用方持锁。
//
// 按 ParentRunID 遍历所有子 Run，而不是顺着 ReviewRunID 单线走：
// 第二轮复审会覆盖 ReviewRunID，只走单线会漏掉第一轮复审和修复轮。
func (m *Manager) rollupChainLocked(root *Run) {
	if root == nil {
		return
	}
	chain := &Chain{}
	seen := map[string]bool{}
	var walk func(r *Run)
	walk = func(r *Run) {
		if r == nil || seen[r.ID] {
			return
		}
		seen[r.ID] = true
		chain.Runs++
		chain.TokensIn += r.TokensIn
		chain.TokensOut += r.TokensOut
		chain.ToolCalls += len(r.ToolCalls)
		chain.Steps += r.Step
		chain.DiffFiles += len(r.Diffs)
		if !r.StartedAt.IsZero() {
			end := r.EndedAt
			if end.IsZero() {
				end = time.Now()
			}
			if d := end.Sub(r.StartedAt).Seconds(); d > 0 {
				chain.Seconds += d
			}
		}
		for _, x := range m.runs {
			if x.ParentRunID == r.ID {
				walk(x)
			}
		}
	}
	walk(root)
	root.Chain = chain
}

// chainRootLocked 沿 ParentRunID 找到链的起点。调用方持锁。
func (m *Manager) chainRootLocked(r *Run) *Run {
	cur := r
	for i := 0; i < 8 && cur != nil && cur.ParentRunID != ""; i++ {
		next, ok := m.runs[cur.ParentRunID]
		if !ok {
			break
		}
		cur = next
	}
	return cur
}

// extractVerdict 从复审结论里取结论标记。
func extractVerdict(summary string) string {
	up := strings.ToUpper(summary)
	switch {
	case strings.Contains(up, "VERDICT: ISSUES"):
		return "issues"
	case strings.Contains(up, "VERDICT: CLEAN"):
		return "clean"
	}
	return ""
}

// ChainSummary 只读地取链汇总（给 handler 用）。
func (m *Manager) ChainSummary(id string) *Chain {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[id]; ok {
		return r.Chain
	}
	return nil
}
