package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

// macOS .app 包把静态资源放在 Contents/Resources/web，
// 而二进制在 Contents/MacOS——webDir 必须能找到前者。
func TestWebDirFindsBundleResources(t *testing.T) {
	root := t.TempDir()
	macOSDir := filepath.Join(root, "App.app", "Contents", "MacOS")
	appDir := filepath.Join(root, "App.app", "Contents", "Resources", "web", "app")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := exeDirFn
	exeDirFn = func() (string, error) { return macOSDir, nil }
	defer func() { exeDirFn = orig }()

	if got := string(webDir("app")); got != appDir {
		t.Fatalf("webDir(%q) = %q, want %q", "app", got, appDir)
	}
}
