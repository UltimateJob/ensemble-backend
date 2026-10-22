package agent

import (
	"context"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func TestScopeAllows(t *testing.T) {
	cases := []struct {
		scope  Scope
		path   string
		allows bool
	}{
		{nil, "anything/x.go", true},
		{Scope{"web/"}, "web/index.html", true},
		{Scope{"web/"}, "web/sub/app.js", true},
		{Scope{"web/"}, "main.go", false},
		{Scope{"*.go"}, "main.go", true},
		{Scope{"*.go"}, "internal/agent/agent.go", true},
		{Scope{"*.go"}, "web/index.html", false},
		{Scope{"web/**/*.html"}, "web/index.html", true},
		{Scope{"web/**/*.html"}, "web/a/b/c.html", true},
		{Scope{"web/**/*.html"}, "web/app.css", false},
		{Scope{"web/", "*.go"}, "calc.go", true},
		{Scope{"web/", "*.go"}, "docs/readme.md", false},
		{Scope{"./"}, "x/y.txt", false},
	}
	for _, c := range cases {
		if got := c.scope.Allows(c.path); got != c.allows {
			t.Errorf("scope %v path %q => %v, want %v", c.scope, c.path, got, c.allows)
		}
	}
}

// 职责边界必须在执行器里生效：前端不能改后端文件。
func TestScopeBlocksOutOfBoundsWrite(t *testing.T) {
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"main.go","content":"package main"}`}}},
		{calls: []domain.ToolCall{{ID: "c2", Name: "write", Arguments: `{"path":"web/index.html","content":"<h1>ok</h1>"}`}}},
		{text: "只改了前端。"},
	}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", OperatorKey: "fe",
		Permission: PermissionApproval, Scope: []string{"web/"}, Task: "改页面",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	if len(done.ToolCalls) != 2 {
		t.Fatalf("tool calls=%d", len(done.ToolCalls))
	}
	if done.ToolCalls[0].Status != "denied" {
		t.Fatalf("out-of-scope write must be denied: %+v", done.ToolCalls[0])
	}
	if done.ToolCalls[1].Status != "ok" {
		t.Fatalf("in-scope write must pass: %+v", done.ToolCalls[1])
	}
}
