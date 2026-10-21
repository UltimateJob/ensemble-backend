package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestWS(t *testing.T) *Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestOpenRejectsMissingAndFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing dir should fail")
	}
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(f); err == nil {
		t.Fatal("file should fail")
	}
	if _, err := Open(""); err == nil {
		t.Fatal("empty should fail")
	}
}

func TestPathGuard(t *testing.T) {
	ws := newTestWS(t)
	cases := []string{"../escape.txt", "../../etc/passwd", "/etc/passwd", "src/../../x"}
	for _, c := range cases {
		if _, err := ws.Read(c, 0, 0); err == nil {
			t.Fatalf("path %q should be rejected", c)
		}
	}
	if _, err := ws.Read("a.txt", 0, 0); err != nil {
		t.Fatalf("normal path should work: %v", err)
	}
}

func TestSymlinkEscape(t *testing.T) {
	ws := newTestWS(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws.Root, "link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("symlink unsupported")
	}
	if _, err := ws.Read("link.txt", 0, 0); err == nil {
		t.Fatal("symlink pointing outside root must be rejected")
	}
	// 目录 symlink 逃逸同样要拦住
	dirLink := filepath.Join(ws.Root, "dirlink")
	if err := os.Symlink(outside, dirLink); err == nil {
		if _, err := ws.Read("dirlink/secret.txt", 0, 0); err == nil {
			t.Fatal("dir symlink escape must be rejected")
		}
	}
}

func TestReadPaging(t *testing.T) {
	ws := newTestWS(t)
	c, err := ws.Read("a.txt", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if c.Content != "alpha\nbeta" || c.TotalLines != 3 || !c.Truncated || c.StartLine != 1 {
		t.Fatalf("unexpected chunk: %+v", c)
	}
	c, err = ws.Read("a.txt", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if c.Content != "gamma" || c.Truncated || c.StartLine != 3 {
		t.Fatalf("unexpected tail chunk: %+v", c)
	}
}

func TestWriteAndEdit(t *testing.T) {
	ws := newTestWS(t)
	n, created, err := ws.Write("src/new.txt", []byte("hello\nworld\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 12 || !created {
		t.Fatalf("write: n=%d created=%v", n, created)
	}
	res, err := ws.Edit("src/new.txt", []Edit{{Old: "world", New: "there"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Replaced != 1 {
		t.Fatalf("replaced=%d", res.Replaced)
	}
	got, _ := os.ReadFile(filepath.Join(ws.Root, "src", "new.txt"))
	if string(got) != "hello\nthere\n" {
		t.Fatalf("content=%q", got)
	}
	// 锚点不存在
	if _, err := ws.Edit("src/new.txt", []Edit{{Old: "zzz", New: "y"}}); err == nil {
		t.Fatal("missing anchor should fail")
	}
	// 锚点不唯一
	if err := os.WriteFile(filepath.Join(ws.Root, "dup.txt"), []byte("x\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Edit("dup.txt", []Edit{{Old: "x", New: "y"}}); err == nil {
		t.Fatal("ambiguous anchor should fail")
	}
	if _, err := ws.Edit("dup.txt", []Edit{{Old: "x", New: "y", All: true}}); err != nil {
		t.Fatalf("replace-all should work: %v", err)
	}
	// 超限
	if _, _, err := ws.Write("big.bin", make([]byte, MaxWriteBytes+1)); err == nil {
		t.Fatal("oversize write should fail")
	}
}

func TestGlobAndGrep(t *testing.T) {
	ws := newTestWS(t)
	got, err := ws.Glob("**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "src/main.go" {
		t.Fatalf("glob=%v", got)
	}
	if _, err := ws.Glob("../*.go"); err == nil {
		t.Fatal("escaping glob should fail")
	}
	matches, err := ws.Grep("beta", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Path != "a.txt" || matches[0].Line != 2 {
		t.Fatalf("grep=%+v", matches)
	}
}

func TestRunCommand(t *testing.T) {
	ws := newTestWS(t)
	res, err := ws.Run(context.Background(), Command{Argv: []string{"sh", "-c", "echo hi; exit 3"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout) != "hi" || res.ExitCode != 3 {
		t.Fatalf("result=%+v", res)
	}
	if res.Risk != RiskWrite {
		t.Fatalf("risk=%s", res.Risk)
	}
	// 只读命令的分类
	if RiskOf([]string{"ls", "-la"}, false) != RiskRead {
		t.Fatal("ls should be read")
	}
	if RiskOf([]string{"git", "push"}, false) != RiskGitPush {
		t.Fatal("git push should be git-push")
	}
	if !IsCritical([]string{"rm -rf /"}, true) {
		t.Fatal("rm -rf / must be critical")
	}
}

func TestRunTimeoutKillsProcess(t *testing.T) {
	ws := newTestWS(t)
	start := time.Now()
	res, err := ws.Run(context.Background(), Command{
		Argv:    []string{"sh", "-c", "sleep 30"},
		Timeout: 300 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.ExitCode != 124 {
		t.Fatalf("expected timeout: %+v", res)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout did not kill the process group")
	}
}

func TestRunEnvFiltersSecrets(t *testing.T) {
	ws := newTestWS(t)
	res, err := ws.Run(context.Background(), Command{
		Argv: []string{"sh", "-c", "echo key=[$MY_API_KEY] home=[$HOME]"},
		Env:  map[string]string{"MY_API_KEY": "sk-secret", "FOO": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Stdout, "sk-secret") {
		t.Fatalf("secret leaked to child: %s", res.Stdout)
	}
	// HOME 必须指向工作区外的临时目录，不能是真实用户 HOME
	if strings.Contains(res.Stdout, "/Users/") {
		t.Fatalf("real HOME leaked: %s", res.Stdout)
	}
}

func TestRunCwdGuard(t *testing.T) {
	ws := newTestWS(t)
	if _, err := ws.Run(context.Background(), Command{Argv: []string{"pwd"}, Cwd: "../"}); err == nil {
		t.Fatal("escaping cwd should fail")
	}
	res, err := ws.Run(context.Background(), Command{Argv: []string{"pwd"}, Cwd: "src"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(res.Stdout), "/src") {
		t.Fatalf("cwd=%q", res.Stdout)
	}
}

func TestSnapshotDiffRevert(t *testing.T) {
	ws := newTestWS(t)
	snap, err := ws.Snapshot("t1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snap.Cleanup(); err != nil {
			t.Fatal(err)
		}
	}()

	// 改一个、删一个、加一个
	if _, _, err := ws.Write("a.txt", []byte("alpha\nBETA\ngamma\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(ws.Root, "src", "main.go")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.Write("added.txt", []byte("new file\n")); err != nil {
		t.Fatal(err)
	}

	diffs, total, err := ws.Diff(snap, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("expected 3 changes, got %d (%+v)", total, diffs)
	}
	status := map[string]string{}
	for _, d := range diffs {
		status[d.Path] = d.Status
	}
	if status["a.txt"] != "modified" || status["src/main.go"] != "deleted" || status["added.txt"] != "added" {
		t.Fatalf("status=%v", status)
	}
	// patch 必须体现实质内容
	for _, d := range diffs {
		if d.Path == "a.txt" && !strings.Contains(d.Patch, "+BETA") {
			t.Fatalf("patch missing change: %s", d.Patch)
		}
	}

	changed, err := ws.Revert(snap)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 3 {
		t.Fatalf("revert changed=%d", changed)
	}
	got, _ := os.ReadFile(filepath.Join(ws.Root, "a.txt"))
	if string(got) != "alpha\nbeta\ngamma\n" {
		t.Fatalf("a.txt not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "src", "main.go")); err != nil {
		t.Fatal("deleted file not restored")
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "added.txt")); err == nil {
		t.Fatal("added file not removed")
	}
}

func TestSnapshotSkipsVendorDirs(t *testing.T) {
	ws := newTestWS(t)
	if err := os.MkdirAll(filepath.Join(ws.Root, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Root, "node_modules", "x", "f.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := ws.Snapshot("t2")
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Cleanup()
	if snap.Files != 2 {
		t.Fatalf("node_modules should be skipped, files=%d", snap.Files)
	}
}
