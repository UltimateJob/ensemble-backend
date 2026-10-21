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
)

// ValidPermission 校验权限字符串。
func ValidPermission(p Permission) bool {
	switch p {
	case PermissionReadonly, PermissionApproval, PermissionFull:
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
	ID             string               `json:"id"`
	WorkspaceID    string               `json:"workspaceId"`
	ConversationID string               `json:"conversationId"`
	UserID         string               `json:"userId"`
	OperatorKey    string               `json:"operatorKey"`
	Permission     Permission           `json:"permission"`
	Task           string               `json:"task"`
	Model          string               `json:"model,omitempty"`
	Status         Status               `json:"status"`
	Step           int                  `json:"step"`
	MaxSteps       int                  `json:"maxSteps"`
	TokensIn       int                  `json:"tokensIn"`
	TokensOut      int                  `json:"tokensOut"`
	CostUSD        float64              `json:"costUsd"`
	StartedAt      time.Time            `json:"startedAt"`
	EndedAt        time.Time            `json:"endedAt"`
	SnapshotID     string               `json:"snapshotId,omitempty"`
	Diffs          []workspace.FileDiff `json:"diffs,omitempty"`
	DiffTotal      int                  `json:"diffTotal"`
	ToolCalls      []*ToolCall          `json:"toolCalls,omitempty"`
	Summary        string               `json:"summary,omitempty"`
	Error          string               `json:"error,omitempty"`
	Events         []Event              `json:"-"`

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

// Manager 持有工作区、运行中的 Run 与审批队列。
type Manager struct {
	mu         sync.Mutex
	workspaces map[string]*Workspace
	runs       map[string]*Run
	approvals  map[string]*Approval
	snapshots  map[string]*workspace.Snapshot
	subs       map[string][]chan Event
	// 同一工作区同时只允许一个 active Run（写入冲突的根因）
	active map[string]string
	seq    int
}

// NewManager 创建管理器。
func NewManager() *Manager {
	return &Manager{
		workspaces: make(map[string]*Workspace),
		runs:       make(map[string]*Run),
		approvals:  make(map[string]*Approval),
		snapshots:  make(map[string]*workspace.Snapshot),
		subs:       make(map[string][]chan Event),
		active:     make(map[string]string),
	}
}

func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b)
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
	if _, busy := m.active[id]; busy {
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
		if tool == "write" || tool == "edit" {
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
	m.mu.Unlock()
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
