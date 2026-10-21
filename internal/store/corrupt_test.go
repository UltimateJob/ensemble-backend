package store

import (
	"os"
	"path/filepath"
	"testing"
)

// 数据文件损坏时不能「从空库继续」——内存是空的，
// 下一次 Flush 会用空数组覆盖原文件，用户数据就真没了。
func TestCorruptFileRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	// 写一个截断的 conversations.json
	if err := os.WriteFile(filepath.Join(dir, "conversations.json"),
		[]byte(`[{"id":"c1","userId":"u_`), 0o600); err != nil {
		t.Fatal(err)
	}

	fs, err := NewFileStore(dir)
	if err == nil {
		fs.Close()
		t.Fatal("损坏的数据文件应该让 NewFileStore 返回错误")
	}
	// 错误信息要能指导处理，不能只说「出错了」
	msg := err.Error()
	for _, want := range []string{"拒绝启动", "备份", "ALLOW_CORRUPT_START"} {
		if !containsStr(msg, want) {
			t.Fatalf("错误信息缺少「%s」，实际：%s", want, msg)
		}
	}
}

func TestCorruptAllowedWithFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "conversations.json"),
		[]byte(`not json at all`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ALLOW_CORRUPT_START", "true")
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("ALLOW_CORRUPT_START=true 时应该能启动，得到 %v", err)
	}
	defer fs.Close()

	// 启动后不该立刻把原文件覆盖成空——先确认它还在
	raw, _ := os.ReadFile(filepath.Join(dir, "conversations.json"))
	if len(raw) == 0 {
		t.Fatal("原文件不该被清空")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
