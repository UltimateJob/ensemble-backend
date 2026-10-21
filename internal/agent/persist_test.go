package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 重启后工作区和 Run 记录都还在。
func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	state := filepath.Join(dir, "agent-state.json")

	m1 := NewManager()
	m1.SetPersistence(state)
	ws, err := m1.BindWorkspace("u1", "c1", root, "persist")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"a.go","content":"package a"}`}}},
		{text: "done"},
	}}
	r, err := m1.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval, Task: "写文件",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m1, r.ID)
	if err := m1.Save(); err != nil {
		t.Fatal(err)
	}

	// 模拟重启
	m2 := NewManager()
	if err := m2.Load(state); err != nil {
		t.Fatal(err)
	}
	got, ok := m2.Workspace(ws.ID, "u1")
	if !ok {
		t.Fatal("workspace lost after reload")
	}
	if got.Root != ws.Root {
		t.Fatalf("root=%s want %s", got.Root, ws.Root)
	}
	rr, ok := m2.GetRun(done.ID)
	if !ok {
		t.Fatal("run lost after reload")
	}
	if rr.Status != StatusDone || len(rr.ToolCalls) != 1 {
		t.Fatalf("run not restored: %+v", rr)
	}
	// 目录没了的工作区要被跳过，而不是让整个加载失败
}

// 在途的 Run 重启后标记为中断。
func TestPersistenceMarksRunningAsAborted(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "s.json")
	root := t.TempDir()

	m1 := NewManager()
	m1.SetPersistence(state)
	ws, _ := m1.BindWorkspace("u1", "c1", root, "")
	// 手工塞一个 running 状态的 Run，模拟"写到一半进程被杀"
	m1.mu.Lock()
	m1.runs["run_fake"] = &Run{
		ID: "run_fake", WorkspaceID: ws.ID, UserID: "u1",
		Status: StatusRunning, StartedAt: time.Now(),
	}
	m1.mu.Unlock()
	if err := m1.Save(); err != nil {
		t.Fatal(err)
	}

	m2 := NewManager()
	if err := m2.Load(state); err != nil {
		t.Fatal(err)
	}
	r, ok := m2.GetRun("run_fake")
	if !ok {
		t.Fatal("run lost")
	}
	if r.Status != StatusAborted {
		t.Fatalf("running run should be aborted after reload, got %s", r.Status)
	}
	if r.Error == "" {
		t.Fatal("should explain why it was aborted")
	}
}

// 目录被删掉时，只丢那个工作区，不影响其它状态。
func TestPersistenceSkipsMissingRoot(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "s.json")
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	m1 := NewManager()
	m1.SetPersistence(state)
	if _, err := m1.BindWorkspace("u1", "c1", gone, ""); err != nil {
		t.Fatal(err)
	}
	if err := m1.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager()
	if err := m2.Load(state); err != nil {
		t.Fatalf("missing root should not fail the whole load: %v", err)
	}
	if len(m2.ListWorkspaces("u1")) != 0 {
		t.Fatal("missing workspace should be skipped")
	}
}
