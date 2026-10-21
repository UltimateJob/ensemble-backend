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
	// 重叠 scope 必须被拒
	if _, err := m.Start(context.Background(), &fakeModel{steps: []fakeStep{{text: "x"}}}, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "c", Permission: PermissionApproval, Scope: []string{"web/sub/"},
	}); err == nil {
		t.Fatal("overlapping scope must be rejected")
	}
	waitRun(t, m, r1.ID)
	waitRun(t, m, r2.ID)

	// 并行结束后，可以启动不限 scope 的 Run
	r3, err := m.Start(context.Background(), &fakeModel{steps: []fakeStep{{text: "y"}}}, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Task: "d", Permission: PermissionApproval,
	})
	if err != nil {
		t.Fatalf("workspace should be free after runs finish: %v", err)
	}
	waitRun(t, m, r3.ID)
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
