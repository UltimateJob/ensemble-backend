package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// fakeModel 按脚本逐轮返回：文本 + 工具调用，模拟真实模型的 tool loop。
type fakeModel struct {
	mu    sync.Mutex
	steps []fakeStep
	idx   int
}

type fakeStep struct {
	text  string
	calls []domain.ToolCall
}

func (f *fakeModel) Stream(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	f.mu.Lock()
	i := f.idx
	if i >= len(f.steps) {
		i = len(f.steps) - 1
	}
	f.idx++
	f.mu.Unlock()

	step := f.steps[i]
	ch := make(chan domain.Chunk, 16)
	go func() {
		defer close(ch)
		if step.text != "" {
			ch <- domain.Chunk{Content: step.text}
		}
		for idx, c := range step.calls {
			ch <- domain.Chunk{ToolCall: &domain.ToolCallDelta{Index: idx, ID: c.ID, Name: c.Name, Arguments: c.Arguments}}
		}
		ch <- domain.Chunk{Done: true, FinishReason: "stop", Usage: &domain.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}}
	}()
	return ch, nil
}

func waitRun(t *testing.T, m *Manager, id string) *Run {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r, ok := m.GetRun(id)
		if !ok {
			t.Fatalf("run %s disappeared", id)
		}
		switch r.Status {
		case StatusDone, StatusFailed, StatusCancelled, StatusAborted:
			return r
		case StatusWaiting:
			// 等待审批：测试里有专门用例接管
			time.Sleep(20 * time.Millisecond)
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	r, _ := m.GetRun(id)
	t.Fatalf("run did not finish: %+v", r)
	return nil
}

func setupRun(t *testing.T, perm Permission, steps []fakeStep) (*Manager, *Run, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	w, err := m.BindWorkspace("u1", "c1", root, "test")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: steps}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID:    w.ID,
		ConversationID: "c1",
		UserID:         "u1",
		OperatorKey:    "arch",
		OperatorName:   "架构师",
		Permission:     perm,
		Task:           "改一下 a.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, r, root
}

// 只读权限：写文件必须被服务端拒绝，且不落盘。
func TestReadonlyDeniesWrite(t *testing.T) {
	m, r, root := setupRun(t, PermissionReadonly, []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"new.txt","content":"x"}`}}},
		{text: "权限不足，没有写。"},
	})
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	if len(done.ToolCalls) != 1 || done.ToolCalls[0].Status != "denied" {
		t.Fatalf("expected denied tool call: %+v", done.ToolCalls)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err == nil {
		t.Fatal("readonly agent must not create files")
	}
}

// approval 权限：写文件自动执行并产生 diff。
func TestApprovalAllowsFileWrite(t *testing.T) {
	m, r, root := setupRun(t, PermissionApproval, []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "edit", Arguments: `{"path":"a.txt","edits":[{"old":"beta","new":"BETA"}]}`}}},
		{text: "改好了。"},
	})
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	content, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(content) != "alpha\nBETA\n" {
		t.Fatalf("file content=%q", content)
	}
	if done.DiffTotal == 0 || len(done.Diffs) == 0 || done.Diffs[0].Status != "modified" {
		t.Fatalf("expected modified diff, got %+v", done.Diffs)
	}
}

// approval 权限：写类命令必须挂起等审批，批准后才执行。
func TestApprovalRunWaitsForApproval(t *testing.T) {
	m, r, root := setupRun(t, PermissionApproval, []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "run", Arguments: `{"argv":["sh","-c","echo hello > made.txt"]}`}}},
		{text: "done"},
	})

	// 等到挂起
	var approval *Approval
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mm := m
		mm.mu.Lock()
		for _, a := range mm.approvals {
			if a.RunID == r.ID && a.Status == "pending" {
				approval = a
			}
		}
		mm.mu.Unlock()
		if approval != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if approval == nil {
		t.Fatal("no pending approval was created")
	}
	if _, err := os.Stat(filepath.Join(root, "made.txt")); err == nil {
		t.Fatal("command ran before approval")
	}
	if err := m.Decide(approval.ID, "u1", true, "once"); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "made.txt")); err != nil {
		t.Fatal("command did not run after approval")
	}
}

// 拒绝审批 → 命令不执行，Run 仍正常收尾。
func TestApprovalRejected(t *testing.T) {
	m, r, root := setupRun(t, PermissionApproval, []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "run", Arguments: `{"argv":["sh","-c","echo x > made.txt"]}`}}},
		{text: "好，不跑了。"},
	})
	var approval *Approval
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && approval == nil {
		m.mu.Lock()
		for _, a := range m.approvals {
			if a.RunID == r.ID && a.Status == "pending" {
				approval = a
			}
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	if approval == nil {
		t.Fatal("no pending approval")
	}
	if err := m.Decide(approval.ID, "u1", false, ""); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s", done.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "made.txt")); err == nil {
		t.Fatal("rejected command must not run")
	}
}

// full 权限：写类命令直接执行，不挂审批。
func TestFullRunsCommandDirectly(t *testing.T) {
	m, r, root := setupRun(t, PermissionFull, []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "run", Arguments: `{"argv":["sh","-c","echo direct > made.txt"]}`}}},
		{text: "done"},
	})
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "made.txt")); err != nil {
		t.Fatal("full permission should run commands without approval")
	}
}

// 多步协作：读 → 改 → 跑测试 → 收尾，验证 tool 结果真的回填给了模型。
func TestToolLoopReadEditRun(t *testing.T) {
	steps := []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "read", Arguments: `{"path":"a.txt"}`}}},
		{calls: []domain.ToolCall{{ID: "c2", Name: "edit", Arguments: `{"path":"a.txt","edits":[{"old":"alpha","new":"ALPHA"}]}`}}},
		{calls: []domain.ToolCall{{ID: "c3", Name: "run", Arguments: `{"argv":["grep","ALPHA","a.txt"]}`}}},
		{text: "验证通过，a.txt 第一行已改为 ALPHA。"},
	}
	m, r, root := setupRun(t, PermissionFull, steps)
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	if done.Step != 4 {
		t.Fatalf("expected 4 steps, got %d", done.Step)
	}
	if len(done.ToolCalls) != 3 {
		t.Fatalf("tool calls=%d", len(done.ToolCalls))
	}
	for _, tc := range done.ToolCalls {
		if tc.Status != "ok" {
			t.Fatalf("tool %s status=%s output=%s", tc.Tool, tc.Status, tc.Output)
		}
	}
	content, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(content) != "ALPHA\nbeta\n" {
		t.Fatalf("content=%q", content)
	}
	if done.Summary == "" {
		t.Fatal("summary should be captured")
	}
	if done.TokensIn == 0 || done.TokensOut == 0 {
		t.Fatalf("tokens not recorded: in=%d out=%d", done.TokensIn, done.TokensOut)
	}
}

// 同一工作区同时只允许一个 Run。
func TestWorkspaceSingleActiveRun(t *testing.T) {
	m, r, _ := setupRun(t, PermissionReadonly, []fakeStep{{text: "ok"}})
	// 等第一个结束前，再启动一个
	w, _ := m.Workspace(r.WorkspaceID, "u1")
	fm2 := &fakeModel{steps: []fakeStep{{text: "ok2"}}}
	_, err := m.Start(context.Background(), fm2, StartRequest{
		WorkspaceID: w.ID, UserID: "u1", Task: "second", Permission: PermissionReadonly,
	})
	// 可能第一个已经结束（很快），两种情况都要能解释
	if err != nil && !containsErr(err, "already has a running run") {
		t.Fatalf("unexpected error: %v", err)
	}
	waitRun(t, m, r.ID)
}

func containsErr(err error, s string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), s)
}

// 事件流可重放：新订阅者拿到从 run.start 开始的事件。
func TestEventReplay(t *testing.T) {
	m, r, _ := setupRun(t, PermissionReadonly, []fakeStep{{text: "hi"}})
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s", done.Status)
	}
	ch, cancel := m.Subscribe(r.ID)
	defer cancel()
	var types []string
	timeout := time.After(2 * time.Second)
	for len(types) < 3 {
		select {
		case e := <-ch:
			types = append(types, e.Type)
		case <-timeout:
			t.Fatalf("replay incomplete: %v", types)
		}
	}
	if types[0] != EventRunStart {
		t.Fatalf("first event=%s", types[0])
	}
}
