package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/workspace"
)

// TeamManager 团队任务的编排器。
//
// 为什么放在执行层而不是 HTTP/前端：
// - 规划、分工、验证、返工都是可复用的能力，不该只属于某个页面；
// - 状态要能落盘、能被 SSE 订阅、能在刷新后恢复；
// - 前端只负责展示，自定义群和内置群走完全相同的路径。
type TeamManager struct {
	mu       sync.Mutex
	teams    map[string]*teamRun
	onUpdate func(*domain.TeamRun)
}

type teamRun struct {
	view    *domain.TeamRun
	cancel  context.CancelFunc
	seq     int
	history []teamEvent
	subs    map[int]chan teamEvent
	nextSub int
}

// teamEvent 团队事件。type 与 Run 事件对齐，便于前端复用渲染。
type teamEvent struct {
	Seq  int       `json:"seq"`
	Type string    `json:"type"`
	At   time.Time `json:"at"`
	Data any       `json:"data,omitempty"`
}

const (
	TeamPhaseFailed    = "failed"
	TeamPhasePlanning  = "planning"
	TeamPhaseBuilding  = "building"
	TeamPhaseVerifying = "verifying"
	TeamPhaseRepairing = "repairing"
	TeamPhaseReviewing = "reviewing"
	TeamPhaseDone      = "done"
)

const (
	TeamEventPhase  = "team.phase"
	TeamEventPlan   = "team.plan"
	TeamEventRun    = "team.run"
	TeamEventCheck  = "team.check"
	TeamEventIssue  = "team.issue"
	TeamEventReview = "team.review"
	TeamEventEnd    = "team.end"
)

// maxTeamRepairs 最多返工几轮。多轮返工容易变成「互相改坏」，
// 一轮修不完说明计划本身有问题，应该如实报告而不是无限烧 token。
const maxTeamRepairs = 1

func NewTeamManager() *TeamManager {
	return &TeamManager{teams: make(map[string]*teamRun)}
}

// SetObserver 团队状态变化时回调（落盘、写群消息等）。
func (t *TeamManager) SetObserver(fn func(*domain.TeamRun)) {
	t.mu.Lock()
	t.onUpdate = fn
	t.mu.Unlock()
}

func (t *TeamManager) Get(id, userID string) (*domain.TeamRun, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.teams[id]
	if !ok || tr.view.UserID != userID {
		return nil, false
	}
	return cloneTeam(tr.view), true
}

// List 某会话的团队任务（最新在前）。
func (t *TeamManager) List(convID, userID string, limit int) []*domain.TeamRun {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*domain.TeamRun, 0)
	for _, tr := range t.teams {
		if tr.view.ConversationID != convID || tr.view.UserID != userID {
			continue
		}
		out = append(out, cloneTeam(tr.view))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Cancel 停止一个团队任务，并中止所有在跑的 Run。
func (t *TeamManager) Cancel(id, userID string, agents *Manager) error {
	t.mu.Lock()
	tr, ok := t.teams[id]
	if !ok || tr.view.UserID != userID {
		t.mu.Unlock()
		return errors.New("团队任务不存在")
	}
	cancel := tr.cancel
	runIDs := make([]string, 0)
	for _, p := range tr.view.Phases {
		runIDs = append(runIDs, p.RunIDs...)
	}
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if agents != nil {
		for _, rid := range runIDs {
			_ = agents.Abort(rid, userID)
		}
	}
	return nil
}

// Attach 订阅团队事件：先补历史，再接实时。
func (t *TeamManager) Attach(id, userID string, after int) ([]teamEvent, <-chan teamEvent, func(), bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.teams[id]
	if !ok || tr.view.UserID != userID {
		return nil, nil, nil, false
	}
	history := make([]teamEvent, 0)
	if after < tr.seq {
		// 团队事件量小（几十条），全部保留在内存里足够。
		history = append(history, tr.eventsAfterLocked(after)...)
	}
	ch := make(chan teamEvent, 32)
	tr.nextSub++
	key := tr.nextSub
	tr.subs[key] = ch
	cancel := func() {
		t.mu.Lock()
		if tr, ok := t.teams[id]; ok {
			delete(tr.subs, key)
		}
		t.mu.Unlock()
	}
	return history, ch, cancel, true
}

func (tr *teamRun) eventsAfterLocked(after int) []teamEvent {
	out := make([]teamEvent, 0, len(tr.history))
	for _, ev := range tr.history {
		if ev.Seq > after {
			out = append(out, ev)
		}
	}
	return out
}

// StartTeam 启动一次团队任务。异步执行，调用方拿到的 TeamRun 会持续更新。
func (t *TeamManager) StartTeam(ctx context.Context, agents *Manager, model Model, req TeamRequest) (*domain.TeamRun, error) {
	if strings.TrimSpace(req.Task) == "" {
		return nil, errors.New("任务不能为空")
	}
	if len(req.Agents) == 0 {
		return nil, errors.New("群里没有可执行的角色")
	}
	if agents == nil {
		return nil, errors.New("执行器未初始化")
	}
	w, ok := agents.WorkspaceByConversation(req.ConversationID)
	if !ok {
		return nil, errors.New("先为这个群选择本地工作区")
	}

	view := &domain.TeamRun{
		ID:             newID("team"),
		ConversationID: req.ConversationID,
		UserID:         req.UserID,
		Title:          req.Title,
		Task:           req.Task,
		Status:         TeamPhasePlanning,
		Phase:          TeamPhasePlanning,
		MessageID:      req.MessageID,
		StartedAt:      time.Now(),
	}
	teamCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Minute)
	tr := &teamRun{view: view, cancel: cancel, subs: make(map[int]chan teamEvent)}

	t.mu.Lock()
	t.teams[view.ID] = tr
	t.mu.Unlock()
	t.emit(tr, TeamEventPhase, map[string]any{"phase": TeamPhasePlanning})

	go t.run(teamCtx, tr, agents, model, req, w)
	return cloneTeam(view), nil
}

// TeamRequest 启动团队任务的入参。
type TeamRequest struct {
	ConversationID string
	UserID         string
	Title          string
	Task           string
	MessageID      string
	Model          string
	MaxSteps       int
	// Agents 群里的角色（已经过滤掉不可执行的）。
	Agents []TeamAgent
	// DefaultPermission 群默认权限，用来给执行角色兜底。
	DefaultPermission string
}

// TeamAgent 一个可参与团队的角色的完整信息。
type TeamAgent struct {
	Key        string
	Name       string
	Prompt     string
	Permission Permission
	Scope      []string
	// Role 可选：plan / execute / review。空时按 key 名推断。
	Role string
}

func (t *TeamManager) run(ctx context.Context, tr *teamRun, agents *Manager, model Model, req TeamRequest, w *Workspace) {
	defer tr.cancel()
	finish := func(status, errMsg string) {
		t.mu.Lock()
		tr.view.Status = status
		tr.view.Phase = status
		tr.view.Error = errMsg
		tr.view.EndedAt = time.Now()
		view := cloneTeam(tr.view)
		observer := t.onUpdate
		t.mu.Unlock()
		t.emit(tr, TeamEventEnd, map[string]any{
			"status": status, "error": errMsg, "score": view.Score,
			"repairs": view.Repairs,
		})
		if observer != nil {
			observer(view)
		}
	}

	lead, reviewers, executors := splitTeamAgents(req.Agents)
	if lead == nil {
		finish(TeamPhaseFailed, "群里没有规划角色（需要 role=plan 或名为架构/负责人的角色）")
		return
	}
	if len(executors) == 0 {
		finish(TeamPhaseFailed, "群里没有执行角色（需要可写权限的角色）")
		return
	}

	// ---- 规划 ----
	// 规划前拿到仓库现状：否则模型只能猜验证命令（把 Go 项目写成 pytest）。
	var repoFiles []string
	if w.ws != nil {
		repoFiles, _ = w.ws.Glob("**/*")
	}
	// 拓扑选择：任务本身已经把接口契约写死、且角色分工与目录一一对应时，
	// 不需要再花一次模型调用「设计」——直接按目录拆。架构师的价值在于
	// 决定边界；边界已经由用户给定时，再问一遍只是延迟。
	fastPlan, fastWhy := deterministicPlan(req, executors, repoFiles)
	var planRes *PlanResult
	if fastPlan != nil {
		planRes = &PlanResult{Plan: *fastPlan}
		t.emit(tr, TeamEventIssue, map[string]any{"issues": []string{fastWhy}})
	} else {
		var perr error
		planRes, perr = agents.Plan(ctx, model, PlanRequest{
			UserID:       req.UserID,
			Task:         buildPlanTask(req, repoFiles),
			Model:        req.Model,
			SystemPrompt: lead.Prompt,
			LeadName:     lead.Name,
			AgentKeys:    agentKeys(req.Agents),
			Files:        repoFiles,
		})
		if perr != nil {
			finish(TeamPhaseFailed, "规划失败："+perr.Error())
			return
		}
	}
	plan := planRes.Plan
	plan = normalizePlan(plan, executors, reviewers)
	// 验证命令与 worker 并行生成：分工一确定就开干，不再等第二段规划。
	type verifyOutcome struct {
		cmds      []domain.TeamCommand
		tokensIn  int
		tokensOut int
		err       error
	}
	verifyCh := make(chan verifyOutcome, 1)
	go func() {
		cmds, in, out, verr := agents.PlanDetails(ctx, model, PlanRequest{
			UserID: req.UserID, Task: buildPlanTask(req, repoFiles), Model: req.Model,
			SystemPrompt: lead.Prompt, LeadName: lead.Name,
			AgentKeys: agentKeys(req.Agents), Files: repoFiles,
		}, plan)
		verifyCh <- verifyOutcome{cmds: cmds, tokensIn: in, tokensOut: out, err: verr}
	}()
	// 计划完整性是服务端的责任，不能指望模型一次写全：
	// 任务要求 web/，但分工只写了 backend/，返工时没有任何人有权限补前端。
	// 这里按任务里明确要求的路径做覆盖校验，缺谁就按角色的真实 scope 补谁。
	if added, missing := ensurePlanCoverage(&plan, req.Task, executors); len(added) > 0 || len(missing) > 0 {
		if len(missing) > 0 {
			finish(TeamPhaseFailed, "计划缺少可执行的分工，且没有角色能覆盖："+strings.Join(missing, "、"))
			return
		}
		t.emit(tr, TeamEventIssue, map[string]any{"issues": []string{
			"规划遗漏了部分交付路径，已按角色能力补齐分工：" + strings.Join(added, "、"),
		}})
	}
	assignments := make([]domain.TeamAssignment, 0, len(plan.Assignments))
	for _, a := range plan.Assignments {
		assignments = append(assignments, a)
	}
	t.mu.Lock()
	tr.view.Plan = &plan
	tr.view.Assignments = assignments
	tr.view.TokensIn += planRes.TokensIn
	tr.view.TokensOut += planRes.TokensOut
	tr.view.Phase = TeamPhaseBuilding
	tr.view.Status = TeamPhaseBuilding
	t.mu.Unlock()
	t.emit(tr, TeamEventPlan, map[string]any{"plan": plan, "assignments": assignments})
	t.emit(tr, TeamEventPhase, map[string]any{"phase": TeamPhaseBuilding})

	// ---- 并行执行 ----
	phase := t.beginPhase(tr, "执行")
	type execResult struct {
		a   domain.TeamAssignment
		run *Run
		err error
	}
	results := make([]execResult, len(plan.Assignments))
	var wg sync.WaitGroup
	for i, a := range plan.Assignments {
		wg.Add(1)
		go func(i int, a domain.TeamAssignment) {
			defer wg.Done()
			ta := findTeamAgent(req.Agents, a.AgentKey)
			if ta == nil {
				results[i] = execResult{a: a, err: fmt.Errorf("角色 %s 不在群里", a.AgentKey)}
				return
			}
			scope := a.Scope
			if len(scope) == 0 {
				scope = ta.Scope
			}
			task := buildWorkerTask(req, plan, a, len(plan.Assignments), repoFiles)
			run, err := agents.Start(ctx, model, StartRequest{
				WorkspaceID:    w.ID,
				ConversationID: req.ConversationID,
				UserID:         req.UserID,
				OperatorKey:    ta.Key,
				OperatorName:   ta.Name,
				Permission:     ta.Permission,
				Scope:          scope,
				Task:           task,
				Model:          req.Model,
				MaxSteps:       maxStepsOr(req.MaxSteps, DefaultMaxSteps),
				SystemPrompt:   workerSystemPrompt(ta, req),
				Timeout:        20 * time.Minute,
				// 团队执行者不做端到端验证：起服务/curl 由服务端验证阶段统一做。
				VerifyOnly: true,
			})
			if err != nil {
				results[i] = execResult{a: a, err: err}
				return
			}
			t.appendPhaseRun(tr, phase, run.ID)
			t.emit(tr, TeamEventRun, map[string]any{
				"phase": TeamPhaseBuilding, "agentKey": ta.Key, "agentName": ta.Name,
				"runId": run.ID, "task": a.Task,
			})
			done := waitRunDone(ctx, agents, run.ID)
			results[i] = execResult{a: a, run: done, err: runError(done)}
		}(i, a)
	}
	wg.Wait()
	if ctx.Err() != nil {
		t.endPhase(tr, phase, "cancelled", "")
		finish(TeamPhaseFailed, "团队任务已取消")
		return
	}

	failedWorkers := 0
	for _, r := range results {
		if r.run != nil {
			t.addRunTokens(tr, r.run)
		}
		if r.err != nil {
			failedWorkers++
			t.addIssue(tr, fmt.Sprintf("%s 执行失败：%v", r.a.AgentName, r.err))
		}
	}
	t.endPhase(tr, phase, "done", fmt.Sprintf("%d 个角色完成，%d 个失败", len(results)-failedWorkers, failedWorkers))

	// ---- 确定性验证 ----
	vo := <-verifyCh
	t.mu.Lock()
	tr.view.TokensIn += vo.tokensIn
	tr.view.TokensOut += vo.tokensOut
	t.mu.Unlock()
	if vo.err != nil {
		log.Printf("[plan] verification stage failed: %v", vo.err)
	}
	plan.Verification = vo.cmds
	t.mu.Lock()
	tr.view.Plan = &plan
	tr.view.Phase = TeamPhaseVerifying
	tr.view.Status = TeamPhaseVerifying
	t.mu.Unlock()
	t.emit(tr, TeamEventPhase, map[string]any{"phase": TeamPhaseVerifying})
	checks := t.runChecks(ctx, tr, w, plan.Verification)
	t.mu.Lock()
	tr.view.Checks = checks
	t.mu.Unlock()

	// ---- 验收 + 返工 ----
	//
	// 确定性验证已经能判定「能不能用」时，不再花一次模型调用去重复判断：
	// 服务端跑过的测试/构建/静态检查就是最硬的证据。模型验收只在
	// 检查失败、被跳过、或根本没有检查项时介入——它要判断的是
	// 「测试没覆盖到但用户会遇到的问题」，不是把 go test 再想一遍。
	reviewer := pickReviewer(req.Agents, plan.ReviewerKey, reviewers, executors)
	if reviewer != nil && reviewByChecksOnly(checks) {
		review := &domain.TeamReview{
			AgentKey: "system",
			Verdict:  "pass",
			Summary:  "服务端确定性验证全部通过（" + strings.Join(checkNames(checks), "、") + "），未调用模型验收。",
		}
		t.mu.Lock()
		tr.view.Review = review
		t.mu.Unlock()
		t.emit(tr, TeamEventReview, map[string]any{"review": review})
		reviewer = nil
	}
	repairRound := 0
	for {
		// 如果验证失败全部是「计划级」（cwd 错、命令不存在），
		// 让验收 Agent 再读一遍代码只会重复同一个结论、白烧一轮 token。
		// 直接跳过验收，把计划问题如实报出来。
		if allChecksPlanLevel(checks) {
			t.addIssue(tr, "验证命令本身配置有误，已跳过验收：请检查计划的 cwd 与命令")
			break
		}
		if reviewer != nil {
			review, run := t.runReview(ctx, tr, agents, model, req, w, plan, checks, reviewer)
			if run != nil {
				t.addRunTokens(tr, run)
			}
			t.mu.Lock()
			tr.view.Review = review
			t.mu.Unlock()
			t.emit(tr, TeamEventReview, map[string]any{"review": review})
			if review != nil && review.Verdict == "issues" {
				for _, line := range issuesOf(review) {
					t.addIssue(tr, line)
				}
			}
			if review != nil && review.Verdict == "error" {
				t.addIssue(tr, "验收没有完成："+clip(review.Summary, 300))
			}
		}
		// 确定性检查失败优先返工；验收没发现问题且检查全过就收工。
		// 验收本身失败（error）不算「有问题」：没有可执行的问题清单，
		// 返工只会重复同一轮。它只扣分并在结果里如实标出。
		// 计划级失败（cwd 错、命令不存在）不进入返工：改代码修不了它。
		// 如实标成计划问题，让用户看到该改验证命令，而不是让 agent 白跑一轮。
		var planProblems []string
		codeFailures := make([]domain.TeamCheck, 0, len(checks))
		for _, c := range checks {
			if c.Skipped || c.Passed {
				continue
			}
			if planLevelCheckFailure(c) {
				planProblems = append(planProblems, fmt.Sprintf("验证「%s」配置有误：%s", c.Name, clip(strings.ReplaceAll(c.Output, "\n", " "), 200)))
				continue
			}
			codeFailures = append(codeFailures, c)
		}
		for _, p := range planProblems {
			t.addIssue(tr, p)
		}
		failedChecks := failedCheckNames(codeFailures)
		t.mu.Lock()
		review := tr.view.Review
		t.mu.Unlock()
		reviewBlockers := blockersOf(review)
		if len(failedChecks) == 0 && len(reviewBlockers) == 0 {
			break
		}
		if repairRound >= maxTeamRepairs {
			break
		}
		repairRound++
		t.mu.Lock()
		tr.view.Repairs = repairRound
		tr.view.Phase = TeamPhaseRepairing
		tr.view.Status = TeamPhaseRepairing
		t.mu.Unlock()
		t.emit(tr, TeamEventPhase, map[string]any{"phase": TeamPhaseRepairing, "round": repairRound})

		targets := routeRepair(plan.Assignments, failedChecks, review, executors)
		if len(targets) == 0 {
			break
		}
		issues := append([]string{}, failedChecks...)
		issues = append(issues, reviewBlockers...)
		t.emit(tr, TeamEventIssue, map[string]any{"round": repairRound, "issues": issues, "targets": agentKeysOf(targets)})
		t.runRepairs(ctx, tr, agents, model, req, w, plan, targets, issues)
		if ctx.Err() != nil {
			finish(TeamPhaseFailed, "团队任务已取消")
			return
		}
		// 修完重新验证
		t.mu.Lock()
		tr.view.Phase = TeamPhaseVerifying
		tr.view.Status = TeamPhaseVerifying
		t.mu.Unlock()
		checks = t.runChecks(ctx, tr, w, plan.Verification)
		t.mu.Lock()
		tr.view.Checks = checks
		t.mu.Unlock()
	}

	// ---- 收尾评分 ----
	score := scoreTeam(tr.view, failedWorkers)
	t.mu.Lock()
	tr.view.Score = score
	t.mu.Unlock()

	status := TeamPhaseDone
	skipped := 0
	for _, c := range tr.view.Checks {
		if c.Skipped {
			skipped++
		}
	}
	if skipped > 0 {
		tr.view.Warning = fmt.Sprintf("%d 条验证命令被跳过，交付未完全验证", skipped)
	}
	if len(failedCheckNames(tr.view.Checks)) > 0 {
		status = "partial"
	}
	if tr.view.Review != nil && tr.view.Review.Verdict == "error" {
		status = "partial"
	}
	if failedWorkers > 0 && failedWorkers == len(results) {
		status = TeamPhaseFailed
	}
	// 部分完成要给出可读原因，而不是只有一个状态码。
	if status == "partial" {
		reason := "部分验证未通过"
		if tr.view.Review != nil && tr.view.Review.Verdict == "error" {
			reason = "验收未完成：" + clip(tr.view.Review.Summary, 200)
		}
		finish(status, reason)
		return
	}
	finish(status, "")
}

/* ---------------- 子阶段 ---------------- */

func (t *TeamManager) beginPhase(tr *teamRun, name string) int {
	t.mu.Lock()
	tr.view.Phases = append(tr.view.Phases, domain.TeamPhase{Name: name, Status: "running", StartedAt: time.Now()})
	idx := len(tr.view.Phases) - 1
	t.mu.Unlock()
	return idx
}

func (t *TeamManager) appendPhaseRun(tr *teamRun, idx int, runID string) {
	t.mu.Lock()
	if idx >= 0 && idx < len(tr.view.Phases) {
		tr.view.Phases[idx].RunIDs = append(tr.view.Phases[idx].RunIDs, runID)
	}
	t.mu.Unlock()
}

func (t *TeamManager) endPhase(tr *teamRun, idx int, status, note string) {
	t.mu.Lock()
	if idx >= 0 && idx < len(tr.view.Phases) {
		tr.view.Phases[idx].Status = status
		tr.view.Phases[idx].Note = note
		tr.view.Phases[idx].EndedAt = time.Now()
	}
	phase := tr.view.Phase
	name := ""
	if idx >= 0 && idx < len(tr.view.Phases) {
		name = tr.view.Phases[idx].Name
	}
	t.mu.Unlock()
	t.emit(tr, TeamEventPhase, map[string]any{"phase": phase, "name": name, "status": status, "note": note})
}

func (t *TeamManager) addRunTokens(tr *teamRun, run *Run) {
	if run == nil {
		return
	}
	t.mu.Lock()
	tr.view.TokensIn += run.TokensIn
	tr.view.TokensOut += run.TokensOut
	t.mu.Unlock()
}

func (t *TeamManager) addIssue(tr *teamRun, issue string) {
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return
	}
	t.mu.Lock()
	for _, x := range tr.view.Issues {
		if x == issue {
			t.mu.Unlock()
			return
		}
	}
	tr.view.Issues = append(tr.view.Issues, issue)
	t.mu.Unlock()
	t.emit(tr, TeamEventIssue, map[string]any{"issue": issue})
}

/* ---------------- 验证 ---------------- */

func (t *TeamManager) runChecks(ctx context.Context, tr *teamRun, w *Workspace, cmds []domain.TeamCommand) []domain.TeamCheck {
	out := make([]domain.TeamCheck, 0, len(cmds))
	for _, c := range cmds {
		check := domain.TeamCheck{Name: c.Name, Argv: c.Argv, Cwd: c.Cwd}
		if len(c.Argv) == 0 {
			check.Skipped = true
			check.Reason = "空命令"
			out = append(out, check)
			continue
		}
		// 验证命令不经过审批：它是计划里声明过的只读/构建检查，
		// 但仍受工作区路径约束，并拒绝高危命令。
		// 验证命令来自模型，不能无条件执行：只放行读和构建/测试类，
		// 联网、删除、提权、git push 一律拒绝。它们是「验证」，
		// 不应该有副作用。
		risk := workspace.RiskOf(c.Argv, false)
		switch risk {
		case workspace.RiskCritical, workspace.RiskNetwork, workspace.RiskDelete,
			workspace.RiskPrivilege, workspace.RiskGitPush:
			check.Skipped = true
			check.Reason = "验证命令不允许执行 " + risk + " 类操作"
			out = append(out, check)
			continue
		}
		// 计划里的 cwd 经常写错（go.mod 在 backend/，却写 cwd "."）。
		// 这不是代码问题，返工也修不了；执行前按模块文件自动定位一次。
		cwd := resolveCheckCwd(w, c.Argv, c.Cwd)
		if cwd != c.Cwd {
			t.emit(tr, TeamEventIssue, map[string]any{"issues": []string{
				fmt.Sprintf("验证「%s」的 cwd 从 %q 修正为 %q（按模块文件定位）", c.Name, c.Cwd, cwd),
			}})
		}
		started := time.Now()
		res, err := w.fs().Run(ctx, workspace.Command{
			Argv:     c.Argv,
			Cwd:      cwd,
			Timeout:  5 * time.Minute,
			RunToken: "teamcheck-" + tr.view.ID,
		})
		check.Ms = time.Since(started).Milliseconds()
		if err != nil {
			check.Output = err.Error()
			check.ExitCode = -1
		} else {
			check.ExitCode = res.ExitCode
			check.Output = clip(strings.TrimSpace(res.Stdout+"\n"+res.Stderr), 4000)
			if res.TimedOut {
				check.Output = "验证命令超时\n" + check.Output
			}
		}
		check.Passed = check.ExitCode == 0
		out = append(out, check)
		t.emit(tr, TeamEventCheck, map[string]any{"check": check})
	}
	return out
}

// resolveCheckCwd 修正验证命令的工作目录。
//
// 规划模型常把 cwd 写成 "."，但 go.mod / package.json 可能在子目录里。
// 只做确定性修正，不改命令本身：
//   - go 命令：cwd 没有 go.mod 时，找唯一含 go.mod 的子目录；
//   - npm/pnpm/yarn 命令：同理找 package.json。
//
// 找不到或存在多个候选时保持原样，让验证如实失败，而不是猜。
func resolveCheckCwd(w *Workspace, argv []string, cwd string) string {
	if w == nil || len(argv) == 0 {
		return cwd
	}
	base := strings.TrimSpace(cwd)
	// 已经指向模块根目录就不动。
	if moduleFileExists(w, base, argv) {
		return cwd
	}
	entries, err := w.fs().List(base)
	if err != nil {
		return cwd
	}
	want := "go.mod"
	switch strings.ToLower(filepathBase(argv[0])) {
	case "npm", "pnpm", "yarn":
		want = "package.json"
	case "go":
		want = "go.mod"
	default:
		return cwd
	}
	var hits []string
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		sub := e.Name
		if base != "" {
			sub = strings.TrimSuffix(base, "/") + "/" + e.Name
		}
		if w.fs().Exists(sub + "/" + want) {
			hits = append(hits, sub)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return cwd
}

func moduleFileExists(w *Workspace, dir string, argv []string) bool {
	want := "go.mod"
	switch strings.ToLower(filepathBase(argv[0])) {
	case "npm", "pnpm", "yarn":
		want = "package.json"
	}
	p := want
	if dir != "" {
		p = strings.TrimSuffix(dir, "/") + "/" + want
	}
	return w.fs().Exists(p)
}

func filepathBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// writesArtifactIntoWorkspace 判断验证命令是否把构建产物写进工作区。
// 只认 -o/--output 后面跟相对路径的情况；/dev/null、绝对临时路径放行。
func writesArtifactIntoWorkspace(argv []string) string {
	for i, a := range argv {
		if a != "-o" && a != "--output" && !strings.HasPrefix(a, "-o=") && !strings.HasPrefix(a, "--output=") {
			continue
		}
		var target string
		if strings.Contains(a, "=") {
			target = a[strings.Index(a, "=")+1:]
		} else if i+1 < len(argv) {
			target = argv[i+1]
		}
		if target == "" {
			continue
		}
		if strings.HasPrefix(target, "/") || strings.HasPrefix(target, "..") {
			continue
		}
		return target
	}
	return ""
}

/* ---------------- 验收与返工 ---------------- */

func (t *TeamManager) runReview(ctx context.Context, tr *teamRun, agents *Manager, model Model, req TeamRequest, w *Workspace, plan domain.TeamPlan, checks []domain.TeamCheck, reviewer *TeamAgent) (*domain.TeamReview, *Run) {
	t.mu.Lock()
	tr.view.Phase = TeamPhaseReviewing
	tr.view.Status = TeamPhaseReviewing
	t.mu.Unlock()
	t.emit(tr, TeamEventPhase, map[string]any{"phase": TeamPhaseReviewing})

	// 验收不再额外跑一个「审计 Run」。
	//
	// 之前验收人先跑 8–12 步工具循环读文件，再发一次结构化结论。实测这部分
	// 稳定花 1.5–2 分钟，还经常因为上游流式超时直接失败；失败后只拿到文件名，
	// 验收人会用「占位」这种空话放行。证据其实服务端本来就有：
	// 确定性验证结果 + 工作区文件正文。直接把关键文件给验收人，
	// 一次强制工具调用就能得到结论，时间和可靠性都更好。
	var files []string
	if w.ws != nil {
		files, _ = w.ws.Glob("**/*")
	}
	evidence := collectReviewEvidence(w, files)

	rr, rerr := agents.Review(ctx, model, ReviewRequest{
		UserID:          req.UserID,
		Model:           req.Model,
		SystemPrompt:    reviewer.Prompt,
		Task:            req.Task,
		Plan:            plan,
		Checks:          checks,
		Files:           files,
		Evidence:        evidence,
		AllChecksPassed: noFailedChecks(checks),
	})
	if rr != nil {
		t.mu.Lock()
		tr.view.TokensIn += rr.TokensIn
		tr.view.TokensOut += rr.TokensOut
		t.mu.Unlock()
		issues := append([]string{}, rr.Blockers...)
		if len(issues) == 0 {
			issues = append(issues, rr.Issues...)
		}
		issues = append(issues, rr.Nits...)
		return &domain.TeamReview{
			AgentKey: reviewer.Key,
			Verdict:  rr.Verdict, Summary: clip(rr.Summary, 6000),
			Issues: issues, Blockers: rr.Blockers, Nits: rr.Nits,
		}, nil
	}
	// 结构化验收不可用时如实标 error，不把「没结论」当通过。
	log.Printf("[review] structured review failed: %v", rerr)
	return &domain.TeamReview{
		AgentKey: reviewer.Key, Verdict: "error",
		Summary: "验收没有完成：" + clip(rerr.Error(), 500),
	}, nil
}

func (t *TeamManager) runRepairs(ctx context.Context, tr *teamRun, agents *Manager, model Model, req TeamRequest, w *Workspace, plan domain.TeamPlan, targets []domain.TeamAssignment, issues []string) {
	var wg sync.WaitGroup
	for _, a := range targets {
		wg.Add(1)
		go func(a domain.TeamAssignment) {
			defer wg.Done()
			ta := findTeamAgent(req.Agents, a.AgentKey)
			if ta == nil {
				return
			}
			scope := a.Scope
			if len(scope) == 0 {
				scope = ta.Scope
			}
			task := buildRepairTask(req, plan, a, issues)
			run, err := agents.Start(ctx, model, StartRequest{
				WorkspaceID:    w.ID,
				ConversationID: req.ConversationID,
				UserID:         req.UserID,
				OperatorKey:    ta.Key,
				OperatorName:   ta.Name,
				Permission:     ta.Permission,
				Scope:          scope,
				Task:           task,
				Model:          req.Model,
				MaxSteps:       maxStepsOr(req.MaxSteps, DefaultMaxSteps),
				SystemPrompt:   workerSystemPrompt(ta, req),
				Timeout:        25 * time.Minute,
				VerifyOnly:     true,
			})
			if err != nil {
				t.addIssue(tr, fmt.Sprintf("%s 返工启动失败：%v", ta.Name, err))
				return
			}
			t.emit(tr, TeamEventRun, map[string]any{
				"phase": TeamPhaseRepairing, "agentKey": ta.Key, "agentName": ta.Name,
				"runId": run.ID, "task": task,
			})
			done := waitRunDone(ctx, agents, run.ID)
			if done != nil {
				t.addRunTokens(tr, done)
				if done.Status != StatusDone {
					t.addIssue(tr, fmt.Sprintf("%s 返工失败：%s", ta.Name, done.Error))
				}
			}
		}(a)
	}
	wg.Wait()
}

/* ---------------- 事件 ---------------- */

func (t *TeamManager) emit(tr *teamRun, typ string, data any) {
	t.mu.Lock()
	tr.seq++
	ev := teamEvent{Seq: tr.seq, Type: typ, At: time.Now(), Data: data}
	tr.history = append(tr.history, ev)
	subs := make([]chan teamEvent, 0, len(tr.subs))
	for _, ch := range tr.subs {
		subs = append(subs, ch)
	}
	observer := t.onUpdate
	view := cloneTeam(tr.view)
	t.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// 慢订阅者不阻塞执行；它可以用 after 游标重连。
		}
	}
	if observer != nil && typ != TeamEventEnd {
		observer(view)
	}
}

func cloneTeam(v *domain.TeamRun) *domain.TeamRun {
	if v == nil {
		return nil
	}
	cp := *v
	if v.Plan != nil {
		p := *v.Plan
		p.Assignments = append([]domain.TeamAssignment(nil), v.Plan.Assignments...)
		p.Verification = append([]domain.TeamCommand(nil), v.Plan.Verification...)
		p.Acceptance = append([]string(nil), v.Plan.Acceptance...)
		cp.Plan = &p
	}
	cp.Assignments = append([]domain.TeamAssignment(nil), v.Assignments...)
	cp.Phases = append([]domain.TeamPhase(nil), v.Phases...)
	cp.Checks = append([]domain.TeamCheck(nil), v.Checks...)
	cp.Issues = append([]string(nil), v.Issues...)
	if v.Review != nil {
		r := *v.Review
		cp.Review = &r
	}
	return &cp
}

/* ---------------- 辅助 ---------------- */

func splitTeamAgents(agents []TeamAgent) (lead *TeamAgent, reviewers []*TeamAgent, executors []*TeamAgent) {
	for i := range agents {
		a := &agents[i]
		role := strings.ToLower(strings.TrimSpace(a.Role))
		key := strings.ToLower(a.Key)
		name := strings.ToLower(a.Name)
		switch {
		case role == "plan" || key == "arch" || key == "planner" || key == "lead" ||
			strings.Contains(name, "架构") || strings.Contains(name, "负责"):
			if lead == nil {
				lead = a
			}
		case role == "review" || key == "qa" || key == "reviewer" || key == "auditor" ||
			strings.Contains(name, "测试") || strings.Contains(name, "验收") || strings.Contains(name, "审校"):
			reviewers = append(reviewers, a)
		case IsWritablePermission(a.Permission):
			executors = append(executors, a)
		}
	}
	// 没有显式规划角色时，拿第一个可写角色兼任规划。
	// 它仍然留在 executors 里：兼任的人也要能领活，否则两人小群会
	// 只剩一个执行者，规划者自己反而不能动手。
	if lead == nil && len(executors) > 0 {
		lead = executors[0]
	}
	return lead, reviewers, executors
}

// ValidateTeamAgents 检查这个群能不能跑团队任务。
//
// 规划/验收角色可以是只读或审查权限；真正执行的角色必须是
// approval 或 full —— ask 会在第一次写文件时停下来等审批，
// 而团队流水线没有人在中间点「允许」。
func ValidateTeamAgents(agents []TeamAgent) error {
	lead, _, executors := splitTeamAgents(agents)
	if lead == nil {
		return errors.New("群里没有规划角色：需要一个只读的架构/负责人，或可写的执行角色兼任")
	}
	if len(executors) == 0 {
		return errors.New("群里没有可执行的角色：至少一个角色需要「自动接受编辑」或「完全访问」权限")
	}
	var blocked []string
	for _, a := range executors {
		if a.Permission == PermissionAsk {
			blocked = append(blocked, a.Name)
		}
	}
	if len(blocked) > 0 {
		return fmt.Errorf("团队执行角色不能是「逐项批准」：%s。改成「自动接受编辑」或「完全访问」后再试", strings.Join(blocked, "、"))
	}
	return nil
}

// IsWritablePermission 该权限能真正写文件（readonly/audit 不行）。
func IsWritablePermission(p Permission) bool {
	switch p {
	case PermissionAsk, PermissionApproval, PermissionFull:
		return true
	}
	return false
}

func findTeamAgent(agents []TeamAgent, key string) *TeamAgent {
	for i := range agents {
		if agents[i].Key == key || agents[i].Name == key {
			return &agents[i]
		}
	}
	return nil
}

func agentKeys(agents []TeamAgent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.Key)
	}
	return out
}

func agentKeysOf(as []domain.TeamAssignment) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.AgentKey)
	}
	return out
}

// normalizePlan 把计划收敛到群里的真实角色：不存在的角色剔除，
// 没给 scope 的用角色自带 scope 补齐，验收人必须在群里。
func normalizePlan(plan domain.TeamPlan, executors, reviewers []*TeamAgent) domain.TeamPlan {
	allowed := make(map[string]*TeamAgent)
	for _, a := range executors {
		allowed[a.Key] = a
	}
	out := plan.Assignments[:0]
	for _, a := range plan.Assignments {
		ta, ok := allowed[a.AgentKey]
		if !ok {
			continue
		}
		a.AgentName = ta.Name
		// 角色自带的 scope 是硬边界：计划可以把任务写得更具体，
		// 但不能把角色权限扩到它原本不该碰的目录。
		// 自定义角色没有 scope 时才采用计划里的范围。
		if len(ta.Scope) > 0 {
			a.Scope = append([]string{}, ta.Scope...)
		} else if len(a.Scope) == 0 {
			a.Scope = ta.Scope
		}
		if strings.TrimSpace(a.Task) == "" {
			continue
		}
		// 同一角色被拆成多条时合并，避免同一个 agent 并行跑两个 Run
		// 改同一片目录（文件级互斥会让它们互相阻塞）。
		merged := false
		for i := range out {
			if out[i].AgentKey != a.AgentKey {
				continue
			}
			out[i].Task = out[i].Task + "\n" + a.Task
			out[i].Scope = mergeScope(out[i].Scope, a.Scope)
			merged = true
			break
		}
		if merged {
			continue
		}
		out = append(out, a)
	}
	plan.Assignments = out
	if len(plan.Assignments) == 0 && len(executors) > 0 {
		// 模型给的角色全都不存在：退化为第一个执行角色做完。
		a := executors[0]
		plan.Assignments = []domain.TeamAssignment{{
			AgentKey: a.Key, AgentName: a.Name,
			Task:  "按计划完成全部实现与测试，并实际运行验证。",
			Scope: a.Scope,
		}}
	}
	if plan.ReviewerKey != "" && findTeamAgentPtr(reviewers, plan.ReviewerKey) == nil {
		plan.ReviewerKey = ""
	}
	return plan
}

// mergeScope 合并两组路径，去重保序。
func mergeScope(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func findTeamAgentPtr(list []*TeamAgent, key string) *TeamAgent {
	for _, a := range list {
		if a.Key == key || a.Name == key {
			return a
		}
	}
	return nil
}

func pickReviewer(agents []TeamAgent, key string, reviewers, executors []*TeamAgent) *TeamAgent {
	if key != "" {
		for i := range agents {
			if agents[i].Key == key || agents[i].Name == key {
				return &agents[i]
			}
		}
	}
	if len(reviewers) > 0 {
		return reviewers[0]
	}
	return nil
}

func failedCheckNames(checks []domain.TeamCheck) []string {
	out := make([]string, 0)
	for _, c := range checks {
		if c.Skipped || c.Passed {
			continue
		}
		msg := fmt.Sprintf("验证「%s」失败（退出码 %d）", c.Name, c.ExitCode)
		if c.Output != "" {
			msg += "：" + clip(strings.ReplaceAll(c.Output, "\n", " "), 300)
		}
		out = append(out, msg)
	}
	return out
}

// reviewByChecksOnly 判断是否可以只凭确定性验证收工。
// 必须有检查项、全部通过、且没有跳过（跳过 = 没验证）。
func reviewByChecksOnly(checks []domain.TeamCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		if c.Skipped || !c.Passed {
			return false
		}
	}
	return true
}

func checkNames(checks []domain.TeamCheck) []string {
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		out = append(out, c.Name)
	}
	return out
}

func noFailedChecks(checks []domain.TeamCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		// 跳过 = 没验证，不等于通过。验收人不能因为「命令没跑」就默认放行。
		if c.Skipped || !c.Passed {
			return false
		}
	}
	return true
}

// blockersOf 只返回会阻塞验收的问题。
//
// 验收里常常混着「更严格也合理」的建议；确定性验证已经全过时，
// 这些建议不该触发返工——返工既慢又可能把好代码改坏。
func blockersOf(review *domain.TeamReview) []string {
	if review == nil {
		return nil
	}
	out := make([]string, 0, len(review.Blockers))
	for _, s := range review.Blockers {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	// 兼容旧结论（只有 Issues、没有 Blockers 字段）。
	if len(out) == 0 && review.Verdict == "issues" {
		for _, s := range review.Issues {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// allChecksPlanLevel 所有失败都是计划级，且至少有一条失败。
func allChecksPlanLevel(checks []domain.TeamCheck) bool {
	failed := 0
	for _, c := range checks {
		if c.Skipped || c.Passed {
			continue
		}
		failed++
		if !planLevelCheckFailure(c) {
			return false
		}
	}
	return failed > 0
}

// planLevelCheckFailure 判断失败是不是「计划本身错了」而不是「代码错了」。
//
// 典型：go test 在错误目录跑、命令不存在、cwd 不存在。
// 这类问题返工改代码没用，应该直接标成计划问题，不烧一轮 token。
func planLevelCheckFailure(c domain.TeamCheck) bool {
	out := strings.ToLower(c.Output)
	for _, sig := range []string{
		"does not contain main module",
		"command not found",
		"executable file not found",
		"directory prefix",
		"cannot find package",
		"missing go.sum entry",
		"chdir",
	} {
		if strings.Contains(out, sig) {
			return true
		}
	}
	return false
}

func reviewIssues(summary string) []string {
	lines := strings.Split(summary, "\n")
	out := make([]string, 0, 8)
	inFence := false
	var cur string
	flush := func() {
		if strings.TrimSpace(cur) != "" {
			out = append(out, strings.TrimSpace(cur))
		}
		cur = ""
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" {
			continue
		}
		// 顶层问题标题：`## 1) xxx`、`### 2. xxx`、`1. xxx`、`- 【阻断】xxx`
		if isTopIssue(line) {
			flush()
			cur = trimIssuePrefix(line)
			continue
		}
		// 「结论：…」是总结，不是问题；遇到就结束当前问题。
		if strings.HasPrefix(line, "结论") {
			flush()
			continue
		}
		// 顶层标题下的正文/子条目，归并到当前问题（最多两行，保持可读）。
		if cur != "" && !strings.HasPrefix(line, "#") {
			cur += " " + line
			if len([]rune(cur)) > 400 {
				cur = string([]rune(cur)[:400])
				flush()
			}
		}
	}
	flush()
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// isTopIssue 判断这一行是不是一个顶层问题标题。
func isTopIssue(line string) bool {
	if strings.HasPrefix(line, "## ") {
		return true
	}
	if strings.HasPrefix(line, "### ") {
		return true
	}
	if strings.HasPrefix(line, "- 【") || strings.HasPrefix(line, "* 【") {
		return true
	}
	return false
}

func trimIssuePrefix(line string) string {
	line = strings.TrimLeft(line, "#-* ")
	for i, r := range line {
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '.' || r == '、' || r == ')' || r == '）' {
			return strings.TrimSpace(line[i+1:])
		}
		break
	}
	return strings.TrimSpace(line)
}

// routeRepair 把问题路由到真正负责的角色。
// 先用 scope 命中，其次按关键词，最后让所有执行角色各看一眼。
func routeRepair(assignments []domain.TeamAssignment, failedChecks []string, review *domain.TeamReview, executors []*TeamAgent) []domain.TeamAssignment {
	issues := append([]string{}, failedChecks...)
	issues = append(issues, blockersOf(review)...)
	if len(issues) == 0 {
		return nil
	}
	targets := make(map[string]domain.TeamAssignment)
	for _, a := range assignments {
		for _, issue := range issues {
			if scopeMentions(a.Scope, issue) || keywordMatches(a.AgentKey, issue) {
				targets[a.AgentKey] = a
				break
			}
		}
	}
	// 验证提到某个路径、但没有任何分工覆盖它：说明计划漏了归属。
	// 这种问题不能只丢给关键词匹配者；按路径选一个 scope 最接近的执行者，
	// 都没有就交给第一个执行者（由它决定是否新增该文件）。
	if len(targets) == 0 {
		for _, issue := range issues {
			for _, m := range pathHints(issue) {
				owner := ownerForPath(assignments, m)
				if owner != nil {
					targets[owner.AgentKey] = *owner
					break
				}
			}
		}
	}
	if len(targets) == 0 {
		for _, a := range assignments {
			targets[a.AgentKey] = a
		}
	}
	out := make([]domain.TeamAssignment, 0, len(targets))
	for _, a := range assignments {
		if t, ok := targets[a.AgentKey]; ok {
			out = append(out, t)
		}
	}
	return out
}

// pathHints 从一句问题里抽出可能的文件/目录路径。
func pathHints(issue string) []string {
	out := make([]string, 0, 4)
	for _, tok := range strings.FieldsFunc(issue, func(r rune) bool {
		return r == ' ' || r == '，' || r == '。' || r == '：' || r == '；' ||
			r == '「' || r == '」' || r == '（' || r == '）' || r == '`' || r == '"' || r == '\''
	}) {
		t := strings.Trim(tok, ".,;:!?()[]{}<>'\"")
		if strings.Contains(t, "/") || strings.HasSuffix(t, ".go") || strings.HasSuffix(t, ".js") ||
			strings.HasSuffix(t, ".html") || strings.HasSuffix(t, ".css") || strings.HasSuffix(t, ".md") {
			out = append(out, t)
			if len(out) >= 4 {
				break
			}
		}
	}
	return out
}

// requiredPaths 从任务描述里抽出「明确要求交付的路径」。
//
// 只认两类：
//   - 带斜杠的目录/路径（backend/、web/、lru/、src/main.go）
//   - 明确的文件名（README.md、go.mod）
//
// 不做语义猜测。任务没写路径时不强加覆盖，避免给简单任务硬拆分工。
func requiredPaths(task string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	fields := strings.FieldsFunc(task, func(r rune) bool {
		switch r {
		case ' ', '\n', '\t', '，', '。', '；', '：', '、', '「', '」', '（', '）',
			'`', '"', '\'', '(', ')', '[', ']', '<', '>', '*':
			return true
		}
		return false
	})
	for _, raw := range fields {
		// 「./...」这类是 go 命令的通配参数，不是交付路径。
		// 必须在去标点之前判断：Trim 会把 "./..." 削成 "/"。
		if strings.Contains(raw, "...") {
			continue
		}
		// 只去掉标点，保留结尾的 "/"：它是「目录」的语义，去掉就认不出来了。
		t := strings.Trim(raw, ".,;:!?")
		t = strings.TrimSuffix(t, "/*")
		if t == "" || t == "/" || t == "./" || len(t) > 120 {
			continue
		}
		isPath := strings.Contains(t, "/")
		isFile := false
		for _, ext := range []string{".go", ".js", ".ts", ".html", ".css", ".md", ".json", ".yml", ".yaml", ".mod", ".sum"} {
			if strings.HasSuffix(strings.ToLower(t), ext) {
				isFile = true
				break
			}
		}
		// 大写文档名（README、LICENSE、Makefile、Dockerfile）没有扩展名，
		// 但任务里写「根目录写 README」时它就是明确交付物。
		// 只认白名单，避免把 LRU / API 这类缩写当成文件。
		switch t {
		case "README", "LICENSE", "Makefile", "Dockerfile", "Procfile", "CHANGELOG":
			isFile = true
		}
		if !isPath && !isFile {
			continue
		}
		// 绝对路径、命令行 flag（-web ../web）都不是「交付路径」。
		if strings.HasPrefix(t, "..") || strings.HasPrefix(t, "/") || strings.HasPrefix(t, "-") {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// ensurePlanCoverage 检查任务要求的每个路径是否落在某个分工的 scope 里。
// 缺失时，找 scope 天然覆盖该路径的执行角色补一条分工；
// 找不到能覆盖的角色就返回 missing，由调用方明确失败。
func ensurePlanCoverage(plan *domain.TeamPlan, task string, executors []*TeamAgent) (added, missing []string) {
	paths := requiredPaths(task)
	if len(paths) == 0 {
		return nil, nil
	}
	covered := func(p string) bool {
		for _, a := range plan.Assignments {
			if len(a.Scope) == 0 || Scope(a.Scope).Allows(p) {
				return true
			}
		}
		return false
	}
	assigned := map[string]bool{}
	for _, a := range plan.Assignments {
		assigned[a.AgentKey] = true
	}
	for _, p := range paths {
		if covered(p) {
			continue
		}
		var owner *TeamAgent
		for _, e := range executors {
			if !IsWritablePermission(e.Permission) {
				continue
			}
			if Scope(e.Scope).Allows(p) {
				owner = e
				break
			}
		}
		// 没有角色的默认 scope 覆盖它（典型是根目录 README/LICENSE/scripts/）。
		// 团队不能因为一个文档路径就整单失败：交给第一个可写角色，
		// 任务里明确写清要补什么，并把这次的写入边界收窄到这个路径。
		if owner == nil {
			for _, e := range executors {
				if IsWritablePermission(e.Permission) {
					owner = e
					break
				}
			}
		}
		if owner == nil {
			missing = append(missing, p)
			continue
		}
		scope := []string{p}
		if !isLikelyFilePath(p) && !strings.HasSuffix(p, "/") {
			scope = []string{p + "/"}
		}
		// 同一个人已经领了活：把路径并进原分工，避免同一角色起两个并行 Run。
		merged := false
		for i := range plan.Assignments {
			if plan.Assignments[i].AgentKey != owner.Key {
				continue
			}
			plan.Assignments[i].Scope = append(plan.Assignments[i].Scope, scope...)
			plan.Assignments[i].Task += "；并补齐「" + p + "」。"
			merged = true
			break
		}
		if !merged {
			plan.Assignments = append(plan.Assignments, domain.TeamAssignment{
				AgentKey:  owner.Key,
				AgentName: owner.Name,
				Task: "补齐「" + p + "」的交付。团队目标是：" + clip(plan.Objective, 300) +
					"。完成后运行你职责范围内的验证。",
				Scope: scope,
			})
		}
		assigned[owner.Key] = true
		added = append(added, owner.Name+" → "+p)
	}
	return added, missing
}

// ownerForPath 找 scope 覆盖该路径的分工。
func ownerForPath(assignments []domain.TeamAssignment, p string) *domain.TeamAssignment {
	p = strings.TrimPrefix(p, "./")
	for i := range assignments {
		if Scope(assignments[i].Scope).Allows(p) {
			return &assignments[i]
		}
	}
	return nil
}

func scopeMentions(scope []string, issue string) bool {
	low := strings.ToLower(issue)
	for _, s := range scope {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimSuffix(s, "/")
		if s == "" {
			continue
		}
		if strings.Contains(low, s) || strings.Contains(low, strings.TrimPrefix(s, "*")) {
			return true
		}
	}
	return false
}

func keywordMatches(agentKey, issue string) bool {
	low := strings.ToLower(issue)
	switch strings.ToLower(agentKey) {
	case "frontend":
		return strings.ContainsAny(low, "前端") || strings.Contains(low, "web/") ||
			strings.Contains(low, "html") || strings.Contains(low, "css") || strings.Contains(low, "javascript")
	case "backend":
		return strings.ContainsAny(low, "后端") || strings.Contains(low, "api") ||
			strings.Contains(low, "backend/") || strings.Contains(low, "go ")
	}
	return false
}

func runError(r *Run) error {
	if r == nil {
		return errors.New("任务没有返回")
	}
	if r.Status != StatusDone {
		if r.Error != "" {
			return errors.New(r.Error)
		}
		return fmt.Errorf("状态 %s", r.Status)
	}
	return nil
}

func waitRunDone(ctx context.Context, agents *Manager, id string) *Run {
	for {
		run, ok := agents.RunView(id)
		if !ok {
			return nil
		}
		switch run.Status {
		case StatusDone, StatusFailed, StatusCancelled, StatusAborted:
			return run
		}
		select {
		case <-ctx.Done():
			return run
		case <-time.After(1200 * time.Millisecond):
		}
	}
}

func maxStepsOr(v, def int) int {
	if v <= 0 {
		return def
	}
	if v > MaxAllowedSteps {
		return MaxAllowedSteps
	}
	return v
}

func scoreTeam(v *domain.TeamRun, failedWorkers int) int {
	score := 100
	for _, c := range v.Checks {
		if !c.Skipped && !c.Passed {
			score -= 30
		}
	}
	if v.Review != nil {
		switch v.Review.Verdict {
		case "issues":
			score -= 25
		case "error":
			score -= 30
		}
	}
	if failedWorkers > 0 {
		score -= 15 * failedWorkers
	}
	score -= v.Repairs * 5
	if score < 0 {
		score = 0
	}
	return score
}

func buildPlanTask(req TeamRequest, files []string) string {
	var b strings.Builder
	b.WriteString("任务：\n")
	b.WriteString(req.Task)
	b.WriteString("\n\n可用角色（agentKey 只能用这些）：\n")
	b.WriteString(teamRoster(req.Agents))
	if len(files) > 0 {
		b.WriteString("\n仓库现有文件（最多 40 个）：\n")
		for i, f := range files {
			if i >= 40 {
				b.WriteString("…\n")
				break
			}
			b.WriteString(f + "\n")
		}
	}
	b.WriteString(`
只做分工：谁做什么、只改哪些目录、验收人是谁，并给出真实可执行的服务端验证命令。
文件覆盖和跨角色文件归属由服务端补齐，不需要在计划里穷举。`)
	return b.String()
}

func teamRoster(agents []TeamAgent) string {
	var b strings.Builder
	for _, a := range agents {
		role := a.Role
		if role == "" {
			role = "成员"
		}
		fmt.Fprintf(&b, "- %s（%s，%s）权限 %s", a.Key, a.Name, role, a.Permission)
		if len(a.Scope) > 0 {
			b.WriteString("，范围 " + strings.Join(a.Scope, " "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func buildWorkerTask(req TeamRequest, plan domain.TeamPlan, a domain.TeamAssignment, peers int, files []string) string {
	var b strings.Builder
	b.WriteString("团队目标：")
	b.WriteString(clip(plan.Objective, 800))
	b.WriteString("\n\n你这次的分工：\n")
	b.WriteString(a.Task)
	if len(a.Scope) > 0 {
		b.WriteString("\n\n你只能改：" + strings.Join(a.Scope, "、") + "。越界的写入会被执行器拒绝。")
	}
	if peers > 1 {
		b.WriteString(fmt.Sprintf("\n\n现在有 %d 个角色并行工作。不要改别人的目录，不要删别人的文件。", peers))
	}
	// 契约只给跟自己目录相关的段落：整份契约会让每个 worker 的上下文
	// 和单聊一样大，每一步都更慢。
	if c := contractsFor(a, plan.Contracts); c != "" {
		b.WriteString("\n\n相关接口契约：\n" + c)
	}
	// 仓库结构直接给到：省掉每个 worker 开头 list/glob 摸目录的 2–3 步。
	if len(files) > 0 {
		b.WriteString("\n\n仓库现有文件：\n")
		for i, f := range files {
			if i >= 60 {
				b.WriteString("…\n")
				break
			}
			b.WriteString(f + "\n")
		}
	}
	if len(plan.Acceptance) > 0 {
		b.WriteString("\n\n验收标准（服务端会统一检查）：\n")
		for _, s := range plan.Acceptance {
			b.WriteString("- " + s + "\n")
		}
	}
	b.WriteString(`
\n完成后用一两句话说明：改了哪些文件、验证结果、还剩什么。
只做构建/测试类验证（go test、go build、node --check 等）；不要启动常驻服务、不要联网做端到端冒烟——那一步由服务端统一执行。
不要在工作区留下构建产物、临时脚本或探测文件。`)
	return b.String()
}

// contractsFor 从整份契约里挑出与这个分工的 scope 相关的行。
// 契约通常按接口/目录分段；匹配不到时返回空，不硬塞整份。
func contractsFor(a domain.TeamAssignment, contracts string) string {
	contracts = strings.TrimSpace(contracts)
	if contracts == "" {
		return ""
	}
	lines := strings.Split(contracts, "\n")
	var picked []string
	for _, line := range lines {
		l := strings.ToLower(line)
		for _, s := range a.Scope {
			key := strings.ToLower(strings.Trim(s, "/*"))
			if key == "" || strings.HasSuffix(key, ".go") || strings.HasSuffix(key, ".js") || strings.HasSuffix(key, ".html") || strings.HasSuffix(key, ".css") {
				continue
			}
			if strings.Contains(l, key) {
				picked = append(picked, line)
				break
			}
		}
	}
	if len(picked) == 0 {
		return ""
	}
	return clip(strings.Join(picked, "\n"), 1200)
}

func buildRepairTask(req TeamRequest, plan domain.TeamPlan, a domain.TeamAssignment, issues []string) string {
	var b strings.Builder
	b.WriteString("团队任务：\n")
	b.WriteString(req.Task)
	b.WriteString("\n\n验收发现以下问题：\n")
	for i, s := range issues {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s)
	}
	b.WriteString("\n你负责的分工：\n")
	b.WriteString(a.Task)
	b.WriteString("\n\n只修与你职责相关的问题，不要顺手改别的地方，不要改接口路径和字段名。修完必须运行验证，并在总结里逐条说明怎么修的。")
	if len(a.Scope) > 0 {
		b.WriteString("\n你只能改：" + strings.Join(a.Scope, "、") + "。")
	}
	return b.String()
}

// deterministicPlan 在「用户已经把边界写死」时跳过规划模型调用。
//
// 触发条件很窄，全部满足才走快路径：
//   - 至少两个可写执行角色；
//   - 任务里显式写了路径，且每个路径只被一个角色的 scope 覆盖；
//   - 路径能覆盖到所有执行角色（否则说明还有角色没活，需要模型判断）。
//
// 这样用户说「后端放 backend/、前端放 web/，接口是 POST /api/x」时，
// 服务端直接按目录拆分，不必再等模型把同样的信息复述一遍。
// 任何一条不满足都退回模型规划——快路径只省延迟，不改变语义。
func deterministicPlan(req TeamRequest, executors []*TeamAgent, files []string) (*domain.TeamPlan, string) {
	if len(executors) < 2 {
		return nil, ""
	}
	paths := requiredPaths(req.Task)
	if len(paths) < 2 {
		return nil, ""
	}
	owner := map[string]*TeamAgent{}
	for _, p := range paths {
		var found *TeamAgent
		for _, e := range executors {
			if !IsWritablePermission(e.Permission) {
				continue
			}
			if Scope(e.Scope).Allows(p) {
				if found != nil && found.Key != e.Key {
					return nil, "" // 路径归属有歧义，交给模型
				}
				found = e
			}
		}
		if found == nil {
			return nil, "" // 没有角色能覆盖，交给模型/覆盖率补齐
		}
		owner[found.Key] = found
	}
	if len(owner) != len(executors) {
		return nil, ""
	}
	plan := domain.TeamPlan{
		Objective: "按用户给定的目录边界并行交付：" + strings.Join(paths, "、"),
		// 任务原文里已经写明接口与验收要求，直接作为契约传给 worker。
		Contracts: req.Task,
	}
	for _, e := range executors {
		var mine []string
		for _, p := range paths {
			if owner[e.Key] != nil && Scope(e.Scope).Allows(p) {
				mine = append(mine, p)
			}
		}
		if len(mine) == 0 {
			return nil, ""
		}
		plan.Assignments = append(plan.Assignments, domain.TeamAssignment{
			AgentKey: e.Key, AgentName: e.Name,
			Task:  "交付 " + strings.Join(mine, "、") + " 下的全部实现与测试，按任务里的接口契约实现。",
			Scope: mine,
		})
	}
	return &plan, "任务已明确目录边界与接口契约，跳过规划模型调用，直接按目录并行"
}

// collectReviewEvidence 读取关键源码片段，供验收人直接判断。
//
// 只收文本类文件，跳过构建产物、依赖锁和隐藏目录；单文件截断，
// 总量有上限，避免把验收 prompt 撑爆。
func collectReviewEvidence(w *Workspace, files []string) string {
	if w == nil || w.ws == nil {
		return ""
	}
	exts := map[string]bool{
		".go": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
		".jsx": true, ".html": true, ".css": true, ".md": true, ".json": true,
		".mod": true, ".yml": true, ".yaml": true, ".toml": true,
	}
	var b strings.Builder
	for _, p := range files {
		lower := strings.ToLower(p)
		if strings.Contains(lower, "/.") || strings.HasPrefix(lower, ".") ||
			strings.HasSuffix(lower, ".sum") || strings.HasSuffix(lower, ".lock") ||
			strings.Contains(lower, "node_modules/") {
			continue
		}
		dot := strings.LastIndex(lower, ".")
		if dot < 0 || !exts[lower[dot:]] {
			continue
		}
		chunk, err := w.ws.Read(p, 0, 3000)
		if err != nil || chunk == nil || strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		if b.Len() > 12000 {
			break
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", p, clip(chunk.Content, 2600))
	}
	return b.String()
}

func workerSystemPrompt(ta *TeamAgent, req TeamRequest) string {
	base := strings.TrimSpace(ta.Prompt)
	if base != "" {
		base += "\n\n"
	}
	base += DefaultSystemPrompt(ta.Name)
	base += `

团队协作规则：
- 你是团队里的一个执行者，只做分给你的部分；别人的目录不要碰。
- 接口契约是硬约束，字段名、路径、状态码都不能自行改。
- 完成后必须实际运行验证命令，不要只说「应该没问题」。
- 遇到无法解决的冲突（例如权限被拒、契约缺失），在总结里明确写出，不要静默绕过。`
	return base
}

// parseReviewVerdict 从验收正文里取「最后一条结论」。
//
// 验收模型常把结论写成 `**结论：通过**`、`结论: 通过`，或者在一大段
// 分析后补一行；只做精确子串匹配会把明确结论误判成「没给结论」。
// 这里取最后一次出现的「结论」，只看它后面的一小段，避免把正文里
// 「如果……则结论：有问题」之类的假设句当成最终结论。
func parseReviewVerdict(lower string) (pass, issues bool) {
	const window = 60
	if i := strings.LastIndex(lower, "verdict"); i >= 0 {
		tail := clip(lower[i:], window)
		if strings.Contains(tail, "pass") {
			return true, false
		}
		if strings.Contains(tail, "issue") {
			return false, true
		}
	}
	if i := strings.LastIndex(lower, "结论"); i >= 0 {
		tail := clip(lower[i:], window)
		hasPass := strings.Contains(tail, "通过") || strings.Contains(tail, "pass")
		hasIssues := strings.Contains(tail, "有问题") || strings.Contains(tail, "不通过") ||
			strings.Contains(tail, "问题") || strings.Contains(tail, "issue")
		// 「不通过」里也含「通过」，先判问题。
		if hasIssues {
			return false, true
		}
		if hasPass {
			return true, false
		}
	}
	return false, false
}

// isLikelyFilePath 判断规划里的交付路径是文件还是目录。
// 有扩展名，或根目录常见无扩展名文件（README/LICENSE/...）按文件处理；
// 其余按目录处理，补 scope 时加尾斜杠。
func isLikelyFilePath(p string) bool {
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	if strings.Contains(base, ".") {
		return true
	}
	switch base {
	case "README", "LICENSE", "Makefile", "Dockerfile", "Procfile", "CHANGELOG":
		return true
	}
	return false
}

// issuesOf 优先用结构化验收给出的清单；旧数据（只有正文）再解析正文。
func issuesOf(review *domain.TeamReview) []string {
	if review == nil {
		return nil
	}
	if len(review.Issues) > 0 {
		out := make([]string, 0, len(review.Issues))
		for _, s := range review.Issues {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return reviewIssues(review.Summary)
}
