package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func TestScopesOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"web/", "*.go", false},
		{"web/", "web/app.css", true},
		{"web/", "web/", true},
		{"web/", "web/sub/", true},
		{"*.go", "*.go", true},
		{"*.go", "*.ts", true}, // 两边都是 glob，保守视为重叠
		{"", "web/", true},     // 空 = 不限
	}
	for _, c := range cases {
		if got := scopesOverlap([]string{c.a}, []string{c.b}); got != c.want {
			t.Errorf("overlap(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

// 不相交的 scope 可以并行；重叠的必须排队。
func TestParallelRuns(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", root, "p")
	if err != nil {
		t.Fatal(err)
	}
	fe := &fakeModel{steps: []fakeStep{{text: "ok"}}}
	be := &fakeModel{steps: []fakeStep{{text: "ok"}}}

	r1, err := m.Start(context.Background(), fe, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "a", Permission: PermissionApproval, Scope: []string{"web/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := m.Start(context.Background(), be, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "b", Permission: PermissionApproval, Scope: []string{"*.go"},
	})
	if err != nil {
		t.Fatalf("disjoint scopes must run in parallel: %v", err)
	}
	// 重叠 scope 现在允许（文件级互斥兜底），但同时最多 3 个
	r3, err := m.Start(context.Background(), &fakeModel{steps: []fakeStep{{text: "x"}}}, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "c", Permission: PermissionApproval, Scope: []string{"web/sub/"},
	})
	if err != nil {
		t.Fatalf("third run should be allowed: %v", err)
	}
	if _, err := m.Start(context.Background(), &fakeModel{steps: []fakeStep{{text: "y"}}}, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "d", Permission: PermissionApproval,
	}); err == nil {
		t.Fatal("fourth concurrent run must be rejected")
	}
	waitRun(t, m, r1.ID)
	waitRun(t, m, r2.ID)
	waitRun(t, m, r3.ID)

	// 并行结束后，可以启动不限 scope 的 Run
	r4, err := m.Start(context.Background(), &fakeModel{steps: []fakeStep{{text: "y"}}}, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "e", Permission: PermissionApproval,
	})
	if err != nil {
		t.Fatalf("workspace should be free after runs finish: %v", err)
	}
	waitRun(t, m, r4.ID)
}

// 并行时 diff 只报自己 scope 内的文件。
func TestParallelDiffFilteredByScope(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", root, "p")
	if err != nil {
		t.Fatal(err)
	}
	slow := make(chan struct{})
	_ = slow
	fe := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"web/a.html","content":"x"}`}}},
		{text: "front done"},
	}}
	be := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"main.go","content":"package main"}`}}},
		{text: "back done"},
	}}
	r1, err := m.Start(context.Background(), fe, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "a", Permission: PermissionApproval, Scope: []string{"web/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := m.Start(context.Background(), be, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "b", Permission: PermissionApproval, Scope: []string{"*.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d1 := waitRun(t, m, r1.ID)
	d2 := waitRun(t, m, r2.ID)
	for _, d := range d1.Diffs {
		if d.Path != "web/a.html" {
			t.Fatalf("fe diff leaked out of scope: %+v", d.Path)
		}
	}
	for _, d := range d2.Diffs {
		if d.Path != "main.go" {
			t.Fatalf("be diff leaked out of scope: %+v", d.Path)
		}
	}
	if len(d1.Diffs) == 0 || len(d2.Diffs) == 0 {
		t.Fatalf("each run should report its own change: fe=%v be=%v", d1.Diffs, d2.Diffs)
	}
}

var _ = time.Second

// 文件级互斥：同一个文件同一时间只允许一个 Run 写。
func TestFileClaim(t *testing.T) {
	m := NewManager()
	if owner := m.claimFile("a.go", "run1"); owner != "" {
		t.Fatalf("first claim should succeed, got owner=%q", owner)
	}
	if owner := m.claimFile("a.go", "run2"); owner != "run1" {
		t.Fatalf("second claim should report run1, got %q", owner)
	}
	if owner := m.claimFile("a.go", "run1"); owner != "" {
		t.Fatal("same run should re-claim freely")
	}
	if owner := m.claimFile("b.go", "run2"); owner != "" {
		t.Fatal("different file should be independent")
	}
	m.releaseFiles("run1")
	if owner := m.claimFile("a.go", "run2"); owner != "" {
		t.Fatalf("after release, run2 should claim, got %q", owner)
	}
}
