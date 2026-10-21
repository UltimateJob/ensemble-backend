package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// audit 权限：能读、能跑命令，但不能写文件。
func TestAuditPermission(t *testing.T) {
	cases := []struct {
		tool    string
		risk    string
		allowed bool
	}{
		{"read", "read", true},
		{"grep", "read", true},
		{"run", "read", true},
		{"run", "write", true},
		{"write", "write", false},
		{"edit", "write", false},
		{"docx_create", "write", false},
		{"run", "critical", false},
	}
	for _, c := range cases {
		got, need, _ := permissionFor(PermissionAudit, c.tool, c.risk)
		if got != c.allowed {
			t.Errorf("audit %s(%s) allowed=%v want %v", c.tool, c.risk, got, c.allowed)
		}
		if need {
			t.Errorf("audit %s should never require approval", c.tool)
		}
	}
}

// AutoReview：主 Run 成功后自动起一轮 new-context 复审。
func TestAutoReviewSpawnsAuditRun(t *testing.T) {
	root := t.TempDir()
	m := NewManager()
	m.SetModel(&fakeModel{steps: []fakeStep{{text: "复审：没发现问题。"}}})
	ws, err := m.BindWorkspace("u1", "c1", root, "rev")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"a.go","content":"package a"}`}}},
		{text: "写好了。"},
	}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval,
		Task: "写个文件", AutoReview: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s", done.Status)
	}
	var rev *Run
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && rev == nil {
		m.mu.Lock()
		if done.ReviewRunID != "" {
			rev = m.runs[done.ReviewRunID]
		}
		m.mu.Unlock()
		if rev == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if rev == nil {
		t.Fatal("auto review run was not spawned")
	}
	if rev.Permission != PermissionAudit {
		t.Fatalf("review permission=%s", rev.Permission)
	}
	if !strings.Contains(rev.Task, "只读复审") {
		t.Fatalf("review task=%s", rev.Task)
	}
	waitRun(t, m, rev.ID)
}

// 复审轮尝试改文件必须被拒。
func TestReviewCannotWrite(t *testing.T) {
	root := t.TempDir()
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", root, "rev2")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"hack.go","content":"x"}`}}},
		{text: "改不了。"},
	}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionAudit, Task: "复审",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if len(done.ToolCalls) == 0 || done.ToolCalls[0].Status != "denied" {
		t.Fatalf("audit write must be denied: %+v", done.ToolCalls)
	}
	if _, err := os.Stat(filepath.Join(root, "hack.go")); err == nil {
		t.Fatal("audit run must not create files")
	}
}

// 链汇总：主 Run 的 Chain 要包含复审轮的 token / 步数 / 耗时。
func TestChainRollupIncludesReview(t *testing.T) {
	root := t.TempDir()
	m := NewManager()
	m.SetModel(&fakeModel{steps: []fakeStep{{text: "复审：没问题。"}}})
	ws, err := m.BindWorkspace("u1", "c1", root, "chain")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: "write", Arguments: `{"path":"a.go","content":"package a"}`}}},
		{text: "写好了。"},
	}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval,
		Task: "写文件", AutoReview: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, m, r.ID)

	// 等复审跑完并回填
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		revID := r.ReviewRunID
		var rev *Run
		if revID != "" {
			rev = m.runs[revID]
		}
		m.mu.Unlock()
		if rev != nil && rev.Status == StatusDone {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Chain == nil {
		t.Fatal("chain not rolled up")
	}
	if r.Chain.Runs < 2 {
		t.Fatalf("chain should include review run, got %d runs", r.Chain.Runs)
	}
	if r.Chain.TokensIn == 0 || r.Chain.TokensOut == 0 {
		t.Fatalf("chain tokens empty: %+v", r.Chain)
	}
	if r.Chain.Steps < 2 {
		t.Fatalf("chain steps=%d", r.Chain.Steps)
	}
}

// 复审报 issues → 自动起修复 → 修复后再复审，最多 2 轮。
func TestAutoFixChain(t *testing.T) {
	root := t.TempDir()
	m := NewManager()
	// 复审/修复共用这个假模型：每次都说有事要改
	m.SetModel(&fakeModel{steps: []fakeStep{{text: "发现两个问题：\n1. 测试覆盖薄\n2. 溢出未处理\nVERDICT: issues"}}})
	ws, err := m.BindWorkspace("u1", "c1", root, "fixchain")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{{text: "写好了。"}}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval,
		Task: "写点东西", AutoReview: true, AutoFix: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, m, r.ID)

	// 等整条链跑完（最多 2 轮复审 + 1 次修复）
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		roles := map[string]int{}
		active := 0
		for _, x := range m.runs {
			if x.ParentRunID == r.ID {
				roles[x.Role]++
				if x.Status == StatusRunning || x.Status == StatusWaiting {
					active++
				}
			}
		}
		m.mu.Unlock()
		if active == 0 && roles["review"] >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var reviews, fixes int
	for _, x := range m.runs {
		if x.ParentRunID != r.ID {
			continue
		}
		switch x.Role {
		case "review":
			reviews++
		case "fix":
			fixes++
		}
	}
	if reviews != maxReviewRounds {
		t.Fatalf("expected %d review rounds, got %d", maxReviewRounds, reviews)
	}
	if fixes != 1 {
		t.Fatalf("expected 1 fix run, got %d", fixes)
	}
	if r.Chain == nil || r.Chain.Runs < 4 {
		t.Fatalf("chain should count every run: %+v", r.Chain)
	}
}

// 复审说 clean 时不该起修复轮。
func TestAutoFixSkippedWhenClean(t *testing.T) {
	root := t.TempDir()
	m := NewManager()
	m.SetModel(&fakeModel{steps: []fakeStep{{text: "看起来没问题。\nVERDICT: clean"}}})
	ws, err := m.BindWorkspace("u1", "c1", root, "clean")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Start(context.Background(), &fakeModel{steps: []fakeStep{{text: "done"}}}, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval,
		Task: "x", AutoReview: true, AutoFix: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, m, r.ID)
	time.Sleep(800 * time.Millisecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.runs {
		if x.ParentRunID == r.ID && x.Role == "fix" {
			t.Fatal("clean verdict must not trigger a fix run")
		}
	}
}
