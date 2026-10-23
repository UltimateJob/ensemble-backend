package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// planModel 模拟「先调用 submit_plan，再干活」的模型。
// 第一个 Stream 返回计划工具调用，之后按脚本执行。
type planModel struct {
	plan  string
	steps []fakeStep
	idx   int
	mu    chan struct{}
}

func newPlanModel(plan string, steps ...fakeStep) *planModel {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return &planModel{plan: plan, steps: steps, mu: ch}
}

func (p *planModel) Stream(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	ch := make(chan domain.Chunk, 32)
	go func() {
		defer close(ch)
		// 契约阶段并发执行，不能让 fake 模型消费 worker 步骤。
		if len(req.Tools) == 1 && req.Tools[0].Name == ToolSubmitPlanDetails {
			ch <- domain.Chunk{ToolCall: &domain.ToolCallDelta{
				Index: 0, ID: "details", Name: ToolSubmitPlanDetails, Arguments: `{"verification":[]}`,
			}}
			ch <- domain.Chunk{Done: true, FinishReason: "tool_calls", Usage: &domain.Usage{PromptTokens: 20, CompletionTokens: 5}}
			return
		}
		// 规划请求只带 submit_plan
		if len(req.Tools) == 1 && req.Tools[0].Name == ToolSubmitPlan {
			ch <- domain.Chunk{ToolCall: &domain.ToolCallDelta{
				Index: 0, ID: "plan1", Name: ToolSubmitPlan, Arguments: p.plan,
			}}
			ch <- domain.Chunk{Done: true, FinishReason: "tool_calls", Usage: &domain.Usage{PromptTokens: 50, CompletionTokens: 20}}
			return
		}
		<-p.mu
		i := p.idx
		if i >= len(p.steps) {
			i = len(p.steps) - 1
		}
		p.idx++
		p.mu <- struct{}{}
		step := p.steps[i]
		if step.text != "" {
			ch <- domain.Chunk{Content: step.text}
		}
		for idx, c := range step.calls {
			ch <- domain.Chunk{ToolCall: &domain.ToolCallDelta{Index: idx, ID: c.ID, Name: c.Name, Arguments: c.Arguments}}
		}
		ch <- domain.Chunk{Done: true, FinishReason: "stop", Usage: &domain.Usage{PromptTokens: 10, CompletionTokens: 5}}
	}()
	return ch, nil
}

func teamTestWorkspace(t *testing.T) (*Manager, *Workspace, string) {
	t.Helper()
	root := t.TempDir()
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", root, "test")
	if err != nil {
		t.Fatal(err)
	}
	return m, ws, root
}

func waitTeam(t *testing.T, tm *TeamManager, id string) *domain.TeamRun {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		v, ok := tm.Get(id, "u1")
		if !ok {
			t.Fatal("team 不存在")
		}
		switch v.Status {
		case "done", "partial", "failed", "cancelled":
			return v
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("team 超时")
	return nil
}

func planJSON(assignments string, verification string, reviewer string) string {
	return `{"objective":"实现一个小功能","acceptance":["测试通过"],"contracts":"无外部接口","assignments":[` +
		assignments + `],"verification":[` + verification + `],"reviewerKey":"` + reviewer + `"}`
}

// 简单任务：规划只分给一个人，验证命令通过，团队正常收工。
func TestTeamSimpleTask(t *testing.T) {
	m, ws, root := teamTestWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := NewTeamManager()
	model := newPlanModel(planJSON(
		`{"agentKey":"backend","task":"写一个文件","scope":["lru/"]}`,
		`{"name":"文件存在","argv":["test","-f","lru/x.go"]}`,
		"qa",
	),
		fakeStep{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"lru/x.go","content":"package lru"}`}}},
		fakeStep{text: "写好了"},
		fakeStep{text: "检查完毕。\n结论：通过"},
	)
	view, err := tm.StartTeam(context.Background(), m, model, TeamRequest{
		ConversationID: "c1", UserID: "u1", Task: "写一个文件",
		Agents: []TeamAgent{
			{Key: "arch", Name: "架构", Permission: PermissionReadonly},
			{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"lru/"}},
			{Key: "qa", Name: "测试", Permission: PermissionAudit},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTeam(t, tm, view.ID)
	if done.Status != "done" {
		t.Fatalf("status=%s err=%s checks=%+v", done.Status, done.Error, done.Checks)
	}
	if done.Score < 90 {
		t.Fatalf("score=%d", done.Score)
	}
	if len(done.Assignments) != 1 || done.Assignments[0].AgentKey != "backend" {
		t.Fatalf("assignments=%+v", done.Assignments)
	}
	if len(done.Checks) != 1 || !done.Checks[0].Passed {
		t.Fatalf("checks=%+v", done.Checks)
	}
	if _, err := os.Stat(filepath.Join(root, "lru/x.go")); err != nil {
		t.Fatalf("worker 没写文件：%v", err)
	}
	_ = ws
}

// 验证失败 → 返工 → 复验通过。返工只派给负责该 scope 的角色。
func TestTeamRepairsFailedCheck(t *testing.T) {
	m, _, root := teamTestWorkspace(t)
	if err := os.MkdirAll(filepath.Join(root, "lru"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 第一版写入错误内容，返工写入正确内容。
	model := newPlanModel(planJSON(
		`{"agentKey":"backend","task":"写文件","scope":["lru/"]}`,
		`{"name":"内容正确","argv":["test","-f","lru/ok.txt"]}`,
		"",
	),
		fakeStep{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"lru/bad.txt","content":"x"}`}}},
		fakeStep{text: "第一版完成"},
		fakeStep{calls: []domain.ToolCall{{ID: "c2", Name: "write", Arguments: `{"path":"lru/ok.txt","content":"ok"}`}}},
		fakeStep{text: "已按问题修复"},
	)
	tm := NewTeamManager()
	view, err := tm.StartTeam(context.Background(), m, model, TeamRequest{
		ConversationID: "c1", UserID: "u1", Task: "写对文件",
		Agents: []TeamAgent{
			{Key: "arch", Name: "架构", Permission: PermissionReadonly},
			{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"lru/"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTeam(t, tm, view.ID)
	if done.Repairs != 1 {
		t.Fatalf("应返工 1 轮，得到 %d（checks=%+v）", done.Repairs, done.Checks)
	}
	if done.Status != "done" {
		t.Fatalf("返工后应通过，status=%s checks=%+v", done.Status, done.Checks)
	}
	if len(done.Checks) == 0 || !done.Checks[0].Passed {
		t.Fatalf("复验应通过：%+v", done.Checks)
	}
}

// 规划必须调用 submit_plan；正文里写计划不算。
func TestPlanRequiresToolCall(t *testing.T) {
	m, _, _ := teamTestWorkspace(t)
	model := &fakeModel{steps: []fakeStep{{text: "我的计划是：先做后端。"}}}
	_, err := m.Plan(context.Background(), model, PlanRequest{
		Task: "做点事", LeadName: "架构", AgentKeys: []string{"arch", "backend"},
	})
	if err == nil || !strings.Contains(err.Error(), "submit_plan") {
		t.Fatalf("没有工具调用应失败，得到 %v", err)
	}
}

// 计划里的角色不存在时，收敛到真实角色而不是报错。
func TestNormalizePlanDropsUnknownAgents(t *testing.T) {
	execs := []*TeamAgent{{Key: "backend", Name: "后端"}}
	plan := domain.TeamPlan{
		Objective: "x",
		Assignments: []domain.TeamAssignment{
			{AgentKey: "ghost", Task: "不存在的角色"},
			{AgentKey: "backend", Task: "真实任务"},
		},
		ReviewerKey: "ghost",
	}
	got := normalizePlan(plan, execs, nil)
	if len(got.Assignments) != 1 || got.Assignments[0].AgentKey != "backend" {
		t.Fatalf("assignments=%+v", got.Assignments)
	}
	if got.ReviewerKey != "" {
		t.Fatalf("不存在的验收人应清空，得到 %q", got.ReviewerKey)
	}
}

// 团队事件可重放：after 游标只拿新事件。
func TestTeamEventReplay(t *testing.T) {
	m, _, _ := teamTestWorkspace(t)
	tm := NewTeamManager()
	model := newPlanModel(planJSON(
		`{"agentKey":"backend","task":"写文件","scope":["a.txt"]}`,
		`{"name":"ok","argv":["test","-f","a.txt"]}`,
		"",
	), fakeStep{text: "done"})
	view, err := tm.StartTeam(context.Background(), m, model, TeamRequest{
		ConversationID: "c1", UserID: "u1", Task: "t",
		Agents: []TeamAgent{
			{Key: "arch", Name: "架构", Permission: PermissionReadonly},
			{Key: "backend", Name: "后端", Permission: PermissionFull},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitTeam(t, tm, view.ID)
	history, _, cancel, ok := tm.Attach(view.ID, "u1", 0)
	if !ok || len(history) == 0 {
		t.Fatal("应能重放团队事件")
	}
	cancel()
	last := history[len(history)-1].Seq
	tail, _, cancel2, _ := tm.Attach(view.ID, "u1", last)
	if len(tail) != 0 {
		t.Fatalf("游标之后不应再有事件，得到 %d", len(tail))
	}
	cancel2()
}

// 团队请求的 JSON 形状（给前端和基准测试当契约）。
func TestTeamRunJSON(t *testing.T) {
	v := domain.TeamRun{
		ID: "team_1", Status: "done", Score: 95,
		Assignments: []domain.TeamAssignment{{AgentKey: "backend", Task: "x"}},
		Checks:      []domain.TeamCheck{{Name: "go test", Passed: true}},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"id"`, `"status"`, `"score"`, `"assignments"`, `"checks"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("JSON 缺 %s：%s", key, b)
		}
	}
}

// 任务要求 web/，但计划只分了 backend/：服务端必须补出前端分工，
// 否则返工时没人有权限写前端。
func TestEnsurePlanCoverageAddsMissingRole(t *testing.T) {
	execs := []*TeamAgent{
		{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"backend/"}},
		{Key: "frontend", Name: "前端", Permission: PermissionFull, Scope: []string{"web/"}},
	}
	plan := domain.TeamPlan{
		Objective: "做一个待办 Web 系统",
		Assignments: []domain.TeamAssignment{
			{AgentKey: "backend", Task: "实现 API", Scope: []string{"backend/"}},
		},
	}
	task := "后端放 backend/，前端页面放 web/"
	added, missing := ensurePlanCoverage(&plan, task, execs)
	if len(missing) > 0 {
		t.Fatalf("不应有无法覆盖的路径：%v", missing)
	}
	if len(added) == 0 {
		t.Fatal("应补齐 web/ 的分工")
	}
	var hasFE bool
	for _, a := range plan.Assignments {
		if a.AgentKey == "frontend" {
			hasFE = true
			if !Scope(a.Scope).Allows("web/index.html") {
				t.Fatalf("前端 scope 应覆盖 web/：%v", a.Scope)
			}
		}
	}
	if !hasFE {
		t.Fatalf("缺少前端分工：%+v", plan.Assignments)
	}
}

// 没有任何角色的默认 scope 覆盖要求路径时，不能整单失败：
// 交给第一个可写角色，并把这次写入边界收窄到该路径。
func TestEnsurePlanCoverageFallsBackToWritableAgent(t *testing.T) {
	execs := []*TeamAgent{
		{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"backend/"}},
	}
	plan := domain.TeamPlan{Assignments: []domain.TeamAssignment{
		{AgentKey: "backend", Task: "API", Scope: []string{"backend/"}},
	}}
	added, missing := ensurePlanCoverage(&plan, "后端 backend/，前端 web/", execs)
	if len(missing) > 0 {
		t.Fatalf("应回退给可写角色，不应报 missing：%v", missing)
	}
	if len(added) == 0 {
		t.Fatal("应记录补齐的路径")
	}
	if !Scope(plan.Assignments[0].Scope).Allows("web/index.html") {
		t.Fatalf("应把 web/ 并进已有分工：%v", plan.Assignments[0].Scope)
	}
	if len(plan.Assignments) != 1 {
		t.Fatalf("同一角色应合并分工，避免并行起两个 Run：%+v", plan.Assignments)
	}
}

// 任务没写路径时不做覆盖校验，避免给简单任务硬拆。
func TestRequiredPathsIgnoresProse(t *testing.T) {
	got := requiredPaths("写一个线程安全的 LRU 缓存，要有测试，性能要好")
	if len(got) != 0 {
		t.Fatalf("没有显式路径时不应抽出 %v", got)
	}
	got = requiredPaths("放到 lru/ 目录，根目录加 README.md")
	if len(got) != 2 {
		t.Fatalf("应抽出 lru/ 和 README.md，得到 %v", got)
	}
	got = requiredPaths("接口说明见 backend/README.md，测试跑 go test ./...")
	if len(got) != 1 || got[0] != "backend/README.md" {
		t.Fatalf("应只抽出 backend/README.md，得到 %v", got)
	}
}

// 验收达到步数上限、没有任何可执行问题：不能假装「返工成功」。
func TestTeamReviewExhaustedIsNotSuccess(t *testing.T) {
	m, _, _ := teamTestWorkspace(t)
	tm := NewTeamManager()
	// 验收每步都返回一个工具调用，永远到不了结论。
	model := newPlanModel(planJSON(
		`{"agentKey":"backend","task":"写文件","scope":["a.txt"]}`,
		`{"name":"ok","argv":["test","-f","a.txt"]}`,
		"qa",
	),
		fakeStep{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"a.txt","content":"x"}`}}},
		fakeStep{text: "写好了"},
	)
	for i := 0; i < 25; i++ {
		model.steps = append(model.steps, fakeStep{calls: []domain.ToolCall{{
			ID: "q" + string(rune('a'+i)), Name: "read", Arguments: `{"path":"a.txt"}`,
		}}})
	}
	view, err := tm.StartTeam(context.Background(), m, model, TeamRequest{
		ConversationID: "c1", UserID: "u1", Task: "t",
		Agents: []TeamAgent{
			{Key: "arch", Name: "架构", Permission: PermissionReadonly},
			{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"a.txt"}},
			{Key: "qa", Name: "测试", Permission: PermissionAudit},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTeam(t, tm, view.ID)
	if done.Status == "done" && done.Repairs > 0 && done.Review != nil && done.Review.Verdict == "error" {
		t.Fatalf("验收未完成不应记成 done+repairs：status=%s repairs=%d review=%+v", done.Status, done.Repairs, done.Review)
	}
}

func TestWritesArtifactIntoWorkspace(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"go", "build", "-o", "/dev/null", "."}, ""},
		{[]string{"go", "build", "-o", "/tmp/x", "."}, ""},
		{[]string{"go", "build", "-o", "backend", "."}, "backend"},
		{[]string{"go", "build", "--output=bin/app", "."}, "bin/app"},
		{[]string{"go", "test", "./..."}, ""},
	}
	for _, c := range cases {
		if got := writesArtifactIntoWorkspace(c.argv); got != c.want {
			t.Errorf("writesArtifact(%v)=%q want %q", c.argv, got, c.want)
		}
	}
}

// 验收问题清单：子条目要归并到父问题，不能一条拆成四条。
func TestReviewIssuesGroupsSubpoints(t *testing.T) {
	summary := `验收完成。

## 1) 【阻断】根目录缺少 README.md

复现：
` + "```" + `
ls README.md
` + "```" + `
影响：文档缺失。

## 2) 【高】构建产物被提交

- backend/todo 是二进制
- 应该加 .gitignore

## 3) 【中】错误文案误导

结论：有问题`
	issues := reviewIssues(summary)
	if len(issues) != 3 {
		t.Fatalf("应聚合成 3 条，得到 %d：%v", len(issues), issues)
	}
	if !strings.Contains(issues[0], "README") {
		t.Fatalf("第一条应是 README：%q", issues[0])
	}
	if strings.Contains(issues[0], "ls README.md") {
		t.Fatalf("代码块内容不应进入问题描述：%q", issues[0])
	}
	if !strings.Contains(issues[1], "backend/todo") {
		t.Fatalf("子条目应归并到父问题：%q", issues[1])
	}
	for _, s := range issues {
		if strings.Contains(s, "结论") {
			t.Fatalf("结论行不应算问题：%q", s)
		}
	}
}

// 根目录 README 这种大写文档名也要算交付路径。
func TestRequiredPathsIncludesReadme(t *testing.T) {
	got := requiredPaths("根目录写 README（启动、测试、接口说明），前端放 web/")
	found := false
	for _, p := range got {
		if p == "README" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应识别出 README，得到 %v", got)
	}
}

// 验证命令的 cwd 写错时，按模块文件自动定位。
func TestResolveCheckCwd(t *testing.T) {
	m, _, root := teamTestWorkspace(t)
	w, ok := m.WorkspaceByConversation("c1")
	if !ok {
		t.Fatal("workspace missing")
	}
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "go.mod"), []byte("module todo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveCheckCwd(w, []string{"go", "test", "./..."}, "")
	if got != "backend" {
		t.Fatalf("应定位到 backend，得到 %q", got)
	}
	got = resolveCheckCwd(w, []string{"go", "test", "./..."}, "backend")
	if got != "backend" {
		t.Fatalf("已正确时不应改动，得到 %q", got)
	}
	// 多个模块时保持原样，不猜
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other", "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = resolveCheckCwd(w, []string{"go", "test", "./..."}, "")
	if got != "" {
		t.Fatalf("多个模块时不应猜，得到 %q", got)
	}
}

func TestPlanLevelCheckFailure(t *testing.T) {
	yes := []string{
		"pattern ./...: directory prefix . does not contain main module",
		"sh: go: command not found",
		"chdir backend: no such file or directory",
	}
	for _, out := range yes {
		if !planLevelCheckFailure(domain.TeamCheck{Output: out}) {
			t.Errorf("应判为计划级失败：%s", out)
		}
	}
	no := []string{
		"--- FAIL: TestCreate (0.00s)\n    handler_test.go:42: status=500 want 201",
		"undefined: Todo",
	}
	for _, out := range no {
		if planLevelCheckFailure(domain.TeamCheck{Output: out}) {
			t.Errorf("不应判为计划级失败：%s", out)
		}
	}
}

// 规划被截断（半截 JSON）时应重试一次，而不是让团队任务直接失败。
type flakyPlanModel struct {
	attempt int
	plan    string
}

func (f *flakyPlanModel) Stream(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	ch := make(chan domain.Chunk, 8)
	go func() {
		defer close(ch)
		f.attempt++
		args := f.plan
		if f.attempt == 1 {
			args = `{"objective": "截断了`
		}
		ch <- domain.Chunk{ToolCall: &domain.ToolCallDelta{Index: 0, ID: "p", Name: ToolSubmitPlan, Arguments: args}}
		ch <- domain.Chunk{Done: true, FinishReason: "tool_calls"}
	}()
	return ch, nil
}

func TestPlanRetriesTruncatedJSON(t *testing.T) {
	m, _, _ := teamTestWorkspace(t)
	model := &flakyPlanModel{plan: `{"objective":"x","assignments":[{"agentKey":"backend","task":"t"}],"verification":[{"name":"ok","argv":["true"]}]}`}
	res, err := m.Plan(context.Background(), model, PlanRequest{Task: "t", LeadName: "架构", AgentKeys: []string{"backend"}})
	if err != nil {
		t.Fatalf("重试后应成功，得到 %v", err)
	}
	if res.Plan.Objective != "x" {
		t.Fatalf("plan=%+v", res.Plan)
	}
	if model.attempt != 2 {
		t.Fatalf("应重试一次，实际尝试 %d 次", model.attempt)
	}
}

// 同一角色被拆成两条分工时合并，避免并行改同一片目录。
// 两阶段规划：分工和契约分两次拿，第二次失败也不影响分工。
type twoStagePlanModel struct{ details string }

func (m twoStagePlanModel) Stream(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	ch := make(chan domain.Chunk, 8)
	go func() {
		defer close(ch)
		name := ToolSubmitPlan
		if len(req.Tools) == 1 && req.Tools[0].Name == ToolSubmitPlanDetails {
			name = ToolSubmitPlanDetails
		}
		args := `{"objective":"x","assignments":[{"agentKey":"backend","task":"a","scope":["backend/"]},{"agentKey":"frontend","task":"b","scope":["web/"]}],"reviewerKey":"qa"}`
		if name == ToolSubmitPlanDetails {
			args = m.details
		}
		ch <- domain.Chunk{ToolCall: &domain.ToolCallDelta{Index: 0, ID: "p", Name: name, Arguments: args}}
		ch <- domain.Chunk{Done: true, FinishReason: "tool_calls"}
	}()
	return ch, nil
}

func TestPlanTwoStagesMergesDetails(t *testing.T) {
	m := NewManager()
	model := twoStagePlanModel{details: `{"verification":[{"name":"ok","argv":["true"]}]}`}
	res, err := m.Plan(context.Background(), model, PlanRequest{Task: "t", LeadName: "架构", AgentKeys: []string{"backend", "frontend"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plan.Assignments) != 2 {
		t.Fatalf("plan=%+v", res.Plan)
	}
	// 验证命令由 PlanDetails 在 worker 并行期间补跑。
	cmds, _, _, err := m.PlanDetails(context.Background(), model, PlanRequest{
		Task: "t", LeadName: "架构", AgentKeys: []string{"backend", "frontend"},
	}, res.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].Name != "ok" {
		t.Fatalf("verification=%+v", cmds)
	}
}

func TestPlanDefaultsVerificationWhenDetailsFail(t *testing.T) {
	m := NewManager()
	// details 产出的 JSON 不合法，规划应继续，验证命令按 go.mod 兜底。
	model := twoStagePlanModel{details: `{"contracts":`}
	res, err := m.Plan(context.Background(), model, PlanRequest{
		Task: "t", LeadName: "架构", AgentKeys: []string{"backend", "frontend"},
		Files: []string{"backend/go.mod", "backend/main.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plan.Verification) == 0 {
		t.Fatalf("verification=%+v", res.Plan.Verification)
	}
	for _, c := range res.Plan.Verification {
		if c.Cwd != "backend" {
			t.Fatalf("verification=%+v", res.Plan.Verification)
		}
	}
}

func TestNormalizePlanMergesDuplicateAgents(t *testing.T) {
	execs := []*TeamAgent{{Key: "backend", Name: "后端", Scope: []string{"backend/"}}}
	plan := domain.TeamPlan{Assignments: []domain.TeamAssignment{
		{AgentKey: "backend", Task: "改 handler", Scope: []string{"backend/"}},
		{AgentKey: "backend", Task: "补测试", Scope: []string{"backend/"}},
	}}
	got := normalizePlan(plan, execs, nil)
	if len(got.Assignments) != 1 {
		t.Fatalf("应合并成 1 条，得到 %+v", got.Assignments)
	}
	if !strings.Contains(got.Assignments[0].Task, "改 handler") || !strings.Contains(got.Assignments[0].Task, "补测试") {
		t.Fatalf("任务应合并：%q", got.Assignments[0].Task)
	}
}

func TestNormalizePlanKeepsRoleScope(t *testing.T) {
	execs := []*TeamAgent{
		{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"backend/"}},
	}
	plan := domain.TeamPlan{Assignments: []domain.TeamAssignment{
		// 计划试图把后端扩到前端目录：必须被角色边界挡住。
		{AgentKey: "backend", Task: "API", Scope: []string{"web/", "backend/"}},
	}}
	got := normalizePlan(plan, execs, nil)
	if len(got.Assignments) != 1 {
		t.Fatalf("assignments=%+v", got.Assignments)
	}
	scope := Scope(got.Assignments[0].Scope)
	if !scope.Allows("backend/main.go") || scope.Allows("web/index.html") {
		t.Fatalf("角色 scope 应覆盖计划：%v", got.Assignments[0].Scope)
	}
}

func TestParseReviewVerdict(t *testing.T) {
	cases := []struct {
		text   string
		pass   bool
		issues bool
	}{
		{"分析完毕。\n**结论：通过**", true, false},
		{"结论: 有问题\n1. POST 返回 404", false, true},
		{"如果校验缺失，则结论：有问题；当前实现已校验。结论：通过", true, false},
		{"verdict: issues\n- missing test", false, true},
		{"看完了，但忘了写结论", false, false},
		{"结论：不通过，原因是缺少 README", false, true},
	}
	for _, c := range cases {
		pass, issues := parseReviewVerdict(strings.ToLower(c.text))
		if pass != c.pass || issues != c.issues {
			t.Errorf("%q: got pass=%v issues=%v, want %v/%v", c.text, pass, issues, c.pass, c.issues)
		}
	}
}

// 根目录文档必须按文件处理：scope 写 README/ 会让角色连 README 都写不了。
func TestEnsurePlanCoverageRootFileScope(t *testing.T) {
	execs := []*TeamAgent{
		{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"backend/"}},
	}
	plan := domain.TeamPlan{Assignments: []domain.TeamAssignment{
		{AgentKey: "backend", Task: "API", Scope: []string{"backend/"}},
	}}
	if _, missing := ensurePlanCoverage(&plan, "后端放 backend/，根目录写 README", execs); len(missing) > 0 {
		t.Fatalf("README 应能覆盖：%v", missing)
	}
	if !Scope(plan.Assignments[0].Scope).Allows("README") {
		t.Fatalf("README 应按文件加入 scope：%v", plan.Assignments[0].Scope)
	}
	if Scope(plan.Assignments[0].Scope).Allows("README/x") {
		t.Fatalf("不应把 README 当成目录：%v", plan.Assignments[0].Scope)
	}
}

func TestDeterministicPlanSplitsByDirectory(t *testing.T) {
	execs := []*TeamAgent{
		{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"backend/"}},
		{Key: "frontend", Name: "前端", Permission: PermissionFull, Scope: []string{"web/"}},
	}
	req := TeamRequest{Task: "后端放在 backend/ 目录，前端放在 web/ 目录，接口 POST /api/todos 返回 201。"}
	plan, why := deterministicPlan(req, execs, nil)
	if plan == nil {
		t.Fatalf("应命中快路径：%s", why)
	}
	if len(plan.Assignments) != 2 {
		t.Fatalf("assignments=%+v", plan.Assignments)
	}
	for _, a := range plan.Assignments {
		if len(a.Scope) != 1 || !Scope(a.Scope).Allows(a.Scope[0]+"x.go") {
			t.Fatalf("%s 的 scope 不对：%v", a.AgentKey, a.Scope)
		}
	}
}

func TestDeterministicPlanFallsBackWhenAmbiguous(t *testing.T) {
	execs := []*TeamAgent{
		{Key: "backend", Name: "后端", Permission: PermissionFull, Scope: []string{"backend/"}},
		{Key: "frontend", Name: "前端", Permission: PermissionFull, Scope: []string{"web/"}},
	}
	// 只提到一个目录：另一个角色没活，需要模型判断是否该拆分。
	if plan, _ := deterministicPlan(TeamRequest{Task: "改 backend/ 的接口"}, execs, nil); plan != nil {
		t.Fatal("单目录不应走快路径")
	}
	// 三个执行角色但只有两个目录：同样需要模型判断。
	three := append(execs, &TeamAgent{Key: "qa2", Name: "测试2", Permission: PermissionFull, Scope: []string{"tests/"}})
	if plan, _ := deterministicPlan(TeamRequest{Task: "backend/ 和 web/ 都改"}, three, nil); plan != nil {
		t.Fatal("角色数不匹配时不应走快路径")
	}
}

// 团队执行者：写过文件后，连续几步无新写入就收尾，不刷满步数。
func TestVerifyOnlyWorkerStopsAfterIdleSteps(t *testing.T) {
	m, ws, root := teamTestWorkspace(t)
	steps := []fakeStep{
		{calls: []domain.ToolCall{{ID: "w1", Name: "write", Arguments: `{"path":"a.txt","content":"x"}`}}},
		{calls: []domain.ToolCall{{ID: "r1", Name: "run", Arguments: `{"argv":["true"]}`}}},
		{calls: []domain.ToolCall{{ID: "r2", Name: "run", Arguments: `{"argv":["true"]}`}}},
		{calls: []domain.ToolCall{{ID: "r3", Name: "run", Arguments: `{"argv":["true"]}`}}},
		{calls: []domain.ToolCall{{ID: "r4", Name: "run", Arguments: `{"argv":["true"]}`}}},
		{calls: []domain.ToolCall{{ID: "r5", Name: "run", Arguments: `{"argv":["true"]}`}}},
	}
	model := newPlanModel(`{"objective":"x","assignments":[{"agentKey":"backend","task":"t"}]}`, steps...)
	run, err := m.Start(context.Background(), model, StartRequest{
		WorkspaceID: ws.ID, ConversationID: "c1", UserID: "u1",
		OperatorKey: "backend", OperatorName: "后端",
		Permission: PermissionFull, Task: "t", MaxSteps: 20, VerifyOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, ok := m.GetRun(run.ID)
		if ok && v != nil && v.Status != StatusRunning {
			if v.Step >= 20 {
				t.Fatalf("不应跑满步数：step=%d", v.Step)
			}
			if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
				t.Fatal("应已写入文件")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run 未结束")
}

func TestVerifyOnlyBlocksNetworkAndServers(t *testing.T) {
	cases := []struct {
		args string
		deny bool
	}{
		{`{"argv":["curl","-s","http://localhost:18080/api/todos"]}`, true},
		{`{"argv":["bash","-lc","cd backend && nohup go run . -addr :18080 &"]}`, true},
		{`{"argv":["sh","-c","go run . -addr :18080"]}`, true},
		{`{"argv":["go","test","./..."]}`, false},
		{`{"argv":["go","vet","./..."]}`, false},
		{`{"argv":["node","--check","web/app.js"]}`, false},
	}
	for _, c := range cases {
		got := verifyOnlyBlock(c.args) != ""
		if got != c.deny {
			t.Errorf("%s: deny=%v, want %v", c.args, got, c.deny)
		}
	}
}
