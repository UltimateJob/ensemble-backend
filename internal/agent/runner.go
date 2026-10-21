package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/workspace"
)

// Model 是 runner 需要的模型能力。gateway.Gateway 满足这个接口，
// 测试里可以塞一个脚本化的假模型，不需要真实网络。
type Model interface {
	Stream(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error)
}

// StartRequest 启动一次 Run 的入参。
type StartRequest struct {
	WorkspaceID    string
	ConversationID string
	UserID         string
	OperatorKey    string
	OperatorName   string
	Permission     Permission
	// Scope 该 agent 允许写的路径（空表示不限）
	Scope    []string
	Task     string
	Model    string
	MaxSteps int
	// SystemPrompt 由会话层注入角色人设与协作规则；为空时用默认 operator 提示。
	SystemPrompt string
}

// 工具名常量，避免字符串散落。
const (
	ToolRead  = "read"
	ToolList  = "list"
	ToolGlob  = "glob"
	ToolGrep  = "grep"
	ToolWrite = "write"
	ToolEdit  = "edit"
	ToolRun   = "run"
)

// Tools 返回暴露给模型的工具声明。
//
// 只给「能落地」的工具：聊天里说「我来改」不算执行，Run 里调用写工具才算。
func Tools() []domain.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []domain.Tool{
		{
			Name:        ToolRead,
			Description: "读取工作区内一个文本文件，按行分页。offset 从 0 开始，limit 为行数。",
			Parameters:  raw(`{"type":"object","properties":{"path":{"type":"string","description":"工作区相对路径"},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":0}},"required":["path"]}`),
		},
		{
			Name:        ToolList,
			Description: "列出一个目录下的条目（一层）。path 省略表示工作区根目录。",
			Parameters:  raw(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
		{
			Name:        ToolGlob,
			Description: "按通配符查找文件，如 **/*.go 或 src/*.ts。",
			Parameters:  raw(`{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`),
		},
		{
			Name:        ToolGrep,
			Description: "按正则搜索工作区内的文本文件，返回 path/line/text。",
			Parameters:  raw(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"limit":{"type":"integer"}},"required":["pattern"]}`),
		},
		{
			Name:        ToolWrite,
			Description: "整文件写入（会创建父目录）。覆盖前请先 read 确认内容。",
			Parameters:  raw(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		},
		{
			Name:        ToolEdit,
			Description: "精确文本替换。old 必须在文件中唯一（除非 all=true）。这是改代码的首选方式。",
			Parameters:  raw(`{"type":"object","properties":{"path":{"type":"string"},"edits":{"type":"array","items":{"type":"object","properties":{"old":{"type":"string"},"new":{"type":"string"},"all":{"type":"boolean"}},"required":["old","new"]}}},"required":["path","edits"]}`),
		},
		{
			Name:        ToolRun,
			Description: "在工作区内执行命令。argv 是参数数组（不经过 shell）；必须用管道时设 shell=true。输出上限 256KB，默认超时 120s。",
			Parameters:  raw(`{"type":"object","properties":{"argv":{"type":"array","items":{"type":"string"}},"cwd":{"type":"string"},"timeout_seconds":{"type":"integer"},"shell":{"type":"boolean"}},"required":["argv"]}`),
		},
	}
}

// DefaultSystemPrompt operator 的默认提示。
//
// 关键约束都写在这里，但真正的强制在 permissionFor——提示词只是减少无效尝试。
func DefaultSystemPrompt(operator string) string {
	name := operator
	if name == "" {
		name = "operator"
	}
	return fmt.Sprintf(`你是群里的执行者 %s，负责把任务真正落到工作区里。

工作方式：
1. 先看再改：用 list/glob/grep 摸清结构，用 read 读要改的文件。
2. 改代码优先用 edit（精确替换），整文件重写用 write。
3. 改完必须验证：用 run 跑构建/测试，失败就继续修，不要声称「应该没问题」。
4. 每次调用只做一件事，参数要有明确路径，不要猜。
5. 结束时用一两句话说明：改了什么、验证结果、还剩什么。

边界：
- 所有路径必须是工作区相对路径，不要用绝对路径或 ..
- 不要执行 git push / reset / clean；不要装全局依赖。
- 命令以当前用户权限运行，这是本地运行时，不是沙箱。
- 如果权限不足，如实说明是什么权限被拒，不要绕过。`, name)
}

// Start 异步启动一次 Run。
func (m *Manager) Start(ctx context.Context, model Model, req StartRequest) (*Run, error) {
	if strings.TrimSpace(req.Task) == "" {
		return nil, fmt.Errorf("agent: empty task")
	}
	w, ok := m.Workspace(req.WorkspaceID, req.UserID)
	if !ok {
		return nil, ErrNoWorkspace
	}
	if !ValidPermission(req.Permission) {
		req.Permission = PermissionApproval
	}
	maxSteps := req.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}
	if maxSteps > MaxAllowedSteps {
		maxSteps = MaxAllowedSteps
	}

	m.mu.Lock()
	if busy := conflictLocked(m.active[w.ID], req.Scope); busy != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: scope 与运行中的 %s 重叠", ErrRunBusy, busy.ID)
	}
	// Run 的生命周期不能绑定在 HTTP 请求上：请求一返回 ctx 就取消，
	// 任务会被瞬间 abort。这里用独立的超时 context，Abort 走 cancel。
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DefaultRunTimeout)
	r := &Run{
		ID:             newID("run"),
		WorkspaceID:    w.ID,
		ConversationID: req.ConversationID,
		UserID:         req.UserID,
		OperatorKey:    req.OperatorKey,
		Permission:     req.Permission,
		Scope:          Scope(req.Scope),
		Task:           req.Task,
		Model:          req.Model,
		Status:         StatusRunning,
		MaxSteps:       maxSteps,
		StartedAt:      time.Now(),
		cancel:         cancel,
	}
	m.runs[r.ID] = r
	m.active[w.ID] = append(m.active[w.ID], r)
	m.mu.Unlock()

	go m.loop(runCtx, r, w, model, req)
	return r, nil
}

func (m *Manager) loop(ctx context.Context, r *Run, w *Workspace, model Model, req StartRequest) {
	finish := func(status Status, errMsg string) {
		m.mu.Lock()
		r.Status = status
		r.EndedAt = time.Now()
		r.Error = errMsg
		list := m.active[w.ID]
		for i, x := range list {
			if x.ID == r.ID {
				m.active[w.ID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(m.active[w.ID]) == 0 {
			delete(m.active, w.ID)
		}
		m.mu.Unlock()
		if r.cancel != nil {
			r.cancel()
		}
		m.mu.Lock()
		rec := m.usage
		in, out := r.TokensIn, r.TokensOut
		m.mu.Unlock()
		if rec != nil && (in+out) > 0 {
			rec(r.UserID, in, out)
		}
		m.emit(r, EventRunEnd, map[string]any{
			"status":  status,
			"error":   errMsg,
			"step":    r.Step,
			"tokens":  map[string]int{"in": r.TokensIn, "out": r.TokensOut},
			"diff":    len(r.Diffs),
			"endedAt": r.EndedAt,
		})
		m.finishRun(r)
	}
	defer func() {
		if rec := recover(); rec != nil {
			finish(StatusFailed, fmt.Sprintf("panic: %v", rec))
		}
	}()

	m.emit(r, EventRunStart, map[string]any{
		"runId":       r.ID,
		"workspaceId": r.WorkspaceID,
		"operator":    r.OperatorKey,
		"permission":  r.Permission,
		"root":        w.Root,
		"maxSteps":    r.MaxSteps,
		"note":        "本地运行时：命令以当前用户权限执行，文件工具受工作区约束",
	})

	// 快照在第一步之前做，保证 revert 能回到「动手前」。
	snap, err := w.fs().Snapshot(r.ID)
	if err != nil {
		finish(StatusFailed, "snapshot: "+err.Error())
		return
	}
	m.mu.Lock()
	r.SnapshotID = snap.ID
	m.snapshots[r.ID] = snap
	m.mu.Unlock()

	system := req.SystemPrompt
	if strings.TrimSpace(system) == "" {
		system = DefaultSystemPrompt(req.OperatorName)
	}
	msgs := []domain.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: req.Task},
	}

	for step := 1; step <= r.MaxSteps; step++ {
		select {
		case <-ctx.Done():
			finish(StatusAborted, "用户中止")
			return
		default:
		}

		m.mu.Lock()
		r.Step = step
		m.mu.Unlock()
		m.emit(r, EventStepStart, map[string]any{"step": step, "maxSteps": r.MaxSteps})

		ch, err := model.Stream(ctx, domain.ChatRequest{
			Model:      r.Model,
			Messages:   msgs,
			Tools:      Tools(),
			ToolChoice: "auto",
			Stream:     true,
		})
		if err != nil {
			finish(StatusFailed, "model: "+err.Error())
			return
		}

		content, calls, usage, streamErr := collect(ch)
		if usage != nil {
			m.mu.Lock()
			r.TokensIn += usage.PromptTokens
			r.TokensOut += usage.CompletionTokens
			m.mu.Unlock()
		}
		if streamErr != nil {
			finish(StatusFailed, "stream: "+streamErr.Error())
			return
		}
		if content != "" {
			m.emit(r, EventAssistant, map[string]any{"step": step, "text": content})
		}
		if len(calls) == 0 {
			m.mu.Lock()
			r.Summary = strings.TrimSpace(content)
			m.mu.Unlock()
			break
		}

		// 回填 assistant 的工具调用，再逐条执行。
		msgs = append(msgs, domain.ChatMessage{Role: "assistant", Content: content, ToolCalls: calls})
		for i := range calls {
			call := calls[i]
			if call.ID == "" {
				call.ID = newID("call")
				calls[i].ID = call.ID
			}
			out := m.execute(ctx, r, w, call)
			msgs = append(msgs, domain.ChatMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    out,
			})
		}
	}

	diffs, total, derr := w.fs().Diff(snap, 200)
	if derr == nil && len(r.Scope) > 0 {
		// 并行时快照是全工作区的：只报自己边界内的改动，
		// 否则两个 agent 会把对方的 diff 也算成自己的。
		kept := diffs[:0]
		for _, d := range diffs {
			if r.Scope.Allows(d.Path) {
				kept = append(kept, d)
			}
		}
		diffs = kept
		total = len(kept)
	}
	if derr == nil {
		m.mu.Lock()
		r.Diffs = diffs
		r.DiffTotal = total
		m.mu.Unlock()
		if total > 0 {
			m.emit(r, EventDiff, map[string]any{"files": len(diffs), "total": total})
		}
	}

	if r.Summary == "" {
		r.Summary = fmt.Sprintf("完成 %d 步，改动 %d 个文件", r.Step, total)
	}
	finish(StatusDone, "")
}

// finishRun 收尾：清理快照目录（保留到 revert 之后由调用方触发 DeleteSnapshot）。
func (m *Manager) finishRun(r *Run) {
	// 快照目录留给 revert 使用；Run 记录里只保留 ID。
}

// collect 消费模型流，聚合文本与工具调用。
func collect(ch <-chan domain.Chunk) (string, []domain.ToolCall, *domain.Usage, error) {
	var sb strings.Builder
	var usage *domain.Usage
	byIndex := map[int]*domain.ToolCall{}
	order := []int{}

	for c := range ch {
		if c.Err != nil {
			return sb.String(), buildCalls(byIndex, order), usage, c.Err
		}
		if c.Content != "" {
			sb.WriteString(c.Content)
		}
		if c.ToolCall != nil {
			idx := c.ToolCall.Index
			tc, ok := byIndex[idx]
			if !ok {
				tc = &domain.ToolCall{}
				byIndex[idx] = tc
				order = append(order, idx)
			}
			if c.ToolCall.ID != "" {
				tc.ID = c.ToolCall.ID
			}
			if c.ToolCall.Name != "" {
				tc.Name = c.ToolCall.Name
			}
			tc.Arguments += c.ToolCall.Arguments
		}
		if c.Usage != nil {
			usage = c.Usage
		}
	}
	return sb.String(), buildCalls(byIndex, order), usage, nil
}

func buildCalls(byIndex map[int]*domain.ToolCall, order []int) []domain.ToolCall {
	out := make([]domain.ToolCall, 0, len(order))
	for _, idx := range order {
		tc := byIndex[idx]
		// 参数可能被模型写坏，至少要能解析；解析失败在 execute 里报错给模型。
		if strings.TrimSpace(tc.Arguments) == "" {
			tc.Arguments = "{}"
		}
		out = append(out, *tc)
	}
	return out
}

// execute 执行一次工具调用：权限 → 审批 → 执行 → 记录。
func (m *Manager) execute(ctx context.Context, r *Run, w *Workspace, call domain.ToolCall) string {
	started := time.Now()
	record := &ToolCall{
		ID:        call.ID,
		RunID:     r.ID,
		AgentKey:  r.OperatorKey,
		Tool:      call.Name,
		Args:      clip(call.Arguments, 2000),
		Status:    "ok",
		StartedAt: started,
	}

	risk := toolRisk(call)
	record.Risk = risk
	allowed, needApproval, reason := permissionFor(r.Permission, call.Name, risk)

	m.mu.Lock()
	r.ToolCalls = append(r.ToolCalls, record)
	m.mu.Unlock()
	m.emit(r, EventToolStart, map[string]any{
		"toolCallId": call.ID,
		"tool":       call.Name,
		"args":       clip(call.Arguments, 600),
		"risk":       risk,
	})

	deny := func(msg string) string {
		record.Status = "denied"
		record.Output = msg
		record.EndedAt = time.Now()
		record.DurationMS = time.Since(started).Milliseconds()
		m.emit(r, EventToolEnd, map[string]any{"toolCallId": call.ID, "status": "denied", "output": msg})
		payload, _ := json.Marshal(map[string]any{"error": msg, "denied": true})
		return string(payload)
	}

	if !allowed {
		return deny(reason)
	}

	// 写入边界：只读权限已经在上面挡掉了；这里挡的是「权限够但越界」。
	if call.Name == ToolWrite || call.Name == ToolEdit {
		if p := argPath(call.Arguments); p != "" && !r.Scope.Allows(p) {
			who := r.OperatorKey
			if who == "" {
				who = "该角色"
			}
			return deny("越界：" + who + " 只允许改 " + r.Scope.String() + "，不能改 " + p)
		}
	}

	if needApproval {
		m.mu.Lock()
		auto := r.approved != nil && r.approved[risk]
		m.mu.Unlock()
		if auto {
			needApproval = false
		}
	}
	if needApproval {
		ap := &Approval{
			ID:         newID("apv"),
			RunID:      r.ID,
			ToolCallID: call.ID,
			AgentKey:   r.OperatorKey,
			Tool:       call.Name,
			Risk:       risk,
			Summary:    summarizeCall(call),
			Command:    commandOf(call),
			Status:     "pending",
			CreatedAt:  time.Now(),
			decided:    make(chan bool, 1),
		}
		m.mu.Lock()
		m.approvals[ap.ID] = ap
		r.Status = StatusWaiting
		m.mu.Unlock()
		record.ApprovalID = ap.ID
		record.Status = "pending_approval"
		m.emit(r, EventApproval, ap)

		select {
		case <-ap.decided:
		case <-time.After(ApprovalWait):
			m.mu.Lock()
			if ap.Status == "pending" {
				ap.Status = "expired"
				close(ap.decided)
			}
			m.mu.Unlock()
		case <-ctx.Done():
			return deny("运行已中止")
		}

		m.mu.Lock()
		status := ap.Status
		scope := ap.Scope
		r.Status = StatusRunning
		m.mu.Unlock()
		if status != "approved" {
			return deny("用户" + map[string]string{"rejected": "拒绝了", "expired": "未在时限内批准"}[status] + "该操作")
		}
		_ = scope
	}

	out, diff, exitCode, err := runTool(ctx, w, call)
	record.EndedAt = time.Now()
	record.DurationMS = time.Since(started).Milliseconds()
	record.Diff = clip(diff, 4000)
	record.ExitCode = exitCode
	if err != nil {
		record.Status = "error"
		record.Output = clip(err.Error(), MaxToolOutputBytes)
		m.emit(r, EventToolEnd, map[string]any{"toolCallId": call.ID, "status": "error", "output": record.Output})
		payload, _ := json.Marshal(map[string]any{"error": record.Output})
		return string(payload)
	}
	record.Output = clip(out, MaxToolOutputBytes)
	m.emit(r, EventToolEnd, map[string]any{
		"toolCallId": call.ID,
		"status":     "ok",
		"output":     record.Output,
		"diff":       record.Diff,
		"exitCode":   exitCode,
		"durationMs": record.DurationMS,
	})
	return record.Output
}

// toolRisk 按工具与参数推断风险。
func toolRisk(call domain.ToolCall) string {
	switch call.Name {
	case ToolRun:
		var args struct {
			Argv  []string `json:"argv"`
			Shell bool     `json:"shell"`
		}
		_ = json.Unmarshal([]byte(call.Arguments), &args)
		return workspace.RiskOf(args.Argv, args.Shell)
	case ToolWrite, ToolEdit:
		return workspace.RiskWrite
	default:
		return workspace.RiskRead
	}
}

func commandOf(call domain.ToolCall) string {
	if call.Name != ToolRun {
		return ""
	}
	var args struct {
		Argv []string `json:"argv"`
	}
	_ = json.Unmarshal([]byte(call.Arguments), &args)
	return strings.Join(args.Argv, " ")
}

func summarizeCall(call domain.ToolCall) string {
	switch call.Name {
	case ToolRun:
		return "执行命令：" + commandOf(call)
	case ToolWrite:
		var a struct{ Path string }
		_ = json.Unmarshal([]byte(call.Arguments), &a)
		return "写入文件：" + a.Path
	case ToolEdit:
		var a struct{ Path string }
		_ = json.Unmarshal([]byte(call.Arguments), &a)
		return "修改文件：" + a.Path
	}
	return call.Name + " " + clip(call.Arguments, 160)
}

// runTool 真正干活。返回值：给模型的内容、diff、退出码、错误。
func runTool(ctx context.Context, w *Workspace, call domain.ToolCall) (string, string, int, error) {
	fs := w.fs()
	switch call.Name {
	case ToolRead:
		var a struct {
			Path   string `json:"path"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
			return "", "", 0, fmt.Errorf("bad args: %w", err)
		}
		chunk, err := fs.Read(a.Path, a.Offset, a.Limit)
		if err != nil {
			return "", "", 0, err
		}
		b, _ := json.Marshal(chunk)
		return string(b), "", 0, nil

	case ToolList:
		var a struct{ Path string }
		if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
			return "", "", 0, fmt.Errorf("bad args: %w", err)
		}
		items, err := fs.List(a.Path)
		if err != nil {
			return "", "", 0, err
		}
		b, _ := json.Marshal(map[string]any{"path": a.Path, "entries": items})
		return string(b), "", 0, nil

	case ToolGlob:
		var a struct{ Pattern string }
		if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
			return "", "", 0, fmt.Errorf("bad args: %w", err)
		}
		files, err := fs.Glob(a.Pattern)
		if err != nil {
			return "", "", 0, err
		}
		b, _ := json.Marshal(map[string]any{"pattern": a.Pattern, "files": files, "count": len(files)})
		return string(b), "", 0, nil

	case ToolGrep:
		var a struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
			Limit   int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
			return "", "", 0, fmt.Errorf("bad args: %w", err)
		}
		matches, err := fs.Grep(a.Pattern, a.Path, a.Limit)
		if err != nil {
			return "", "", 0, err
		}
		b, _ := json.Marshal(map[string]any{"matches": matches, "count": len(matches)})
		return string(b), "", 0, nil

	case ToolWrite:
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
			return "", "", 0, fmt.Errorf("bad args: %w", err)
		}
		n, created, err := fs.Write(a.Path, []byte(a.Content))
		if err != nil {
			return "", "", 0, err
		}
		b, _ := json.Marshal(map[string]any{"path": a.Path, "bytes": n, "created": created})
		return string(b), "", 0, nil

	case ToolEdit:
		var a struct {
			Path  string           `json:"path"`
			Edits []workspace.Edit `json:"edits"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
			return "", "", 0, fmt.Errorf("bad args: %w", err)
		}
		res, err := fs.Edit(a.Path, a.Edits)
		if err != nil {
			return "", "", 0, err
		}
		b, _ := json.Marshal(res)
		return string(b), "", 0, nil

	case ToolRun:
		var a struct {
			Argv    []string `json:"argv"`
			Cwd     string   `json:"cwd"`
			Timeout int      `json:"timeout_seconds"`
			Shell   bool     `json:"shell"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
			return "", "", 0, fmt.Errorf("bad args: %w", err)
		}
		if workspace.IsDaemonCommand(a.Argv, a.Shell) {
			return "", "", 0, fmt.Errorf("执行器不负责启动常驻进程（nohup / 后台运行会一直挂着）。" +
				"要验证服务，请改成跑测试（如 go test ./...），或用带 timeout 的前台命令")
		}
		cmd := workspace.Command{Argv: a.Argv, Cwd: a.Cwd, Shell: a.Shell}
		if a.Timeout > 0 {
			cmd.Timeout = time.Duration(a.Timeout) * time.Second
		}
		res, err := fs.Run(ctx, cmd)
		if err != nil {
			return "", "", 0, err
		}
		b, _ := json.Marshal(res)
		return string(b), "", res.ExitCode, nil
	}
	return "", "", 0, fmt.Errorf("unknown tool %q", call.Name)
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "… (truncated)"
}

// argPath 从写类工具的参数里取 path，用于 scope 检查。
func argPath(raw string) string {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return ""
	}
	return a.Path
}
