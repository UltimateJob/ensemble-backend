package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
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

// 删除会话后，消息文件应该被删掉，不是留一个空数组。
// 之前每删一个群就在 messages/ 里留一个 2 字节的 []，
// 跑几十个群之后里面全是孤儿文件。
func TestDeleteConvRemovesMessageFile(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	conv := &domain.Conversation{ID: "c_del", UserID: "u1", Title: "要删的群"}
	if err := fs.CreateConv(conv); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_ = fs.AppendMessage(&domain.Message{
			ConversationID: conv.ID, Role: domain.RoleUser, Content: "消息",
		})
	}
	if err := fs.Flush(); err != nil {
		t.Fatal(err)
	}

	msgFile := filepath.Join(dir, "messages", "c_del.json")
	if _, err := os.Stat(msgFile); err != nil {
		t.Fatal("消息文件应该存在")
	}

	if err := fs.DeleteConv(conv.ID); err != nil {
		t.Fatal(err)
	}
	if err := fs.Flush(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(msgFile); !os.IsNotExist(err) {
		t.Fatalf("删除会话后消息文件应该被删掉，实际还在")
	}
}

// 写盘失败时要能被观察到。
// 写不进磁盘的时候接口照样返回 200，数据只在内存里，重启就没了——
// 这种状态必须让运维/监控看到，不能只写一条日志。
func TestPersistHealthReflectsFlushFailure(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(dir, 0o755)
		fs.Close()
	}()

	if h := fs.PersistHealth(); !h.OK {
		t.Fatalf("初始状态应该是健康的，得到 %+v", h)
	}

	// 让目录不可写，再触发一次落盘
	_ = fs.CreateConv(&domain.Conversation{ID: "c_h", UserID: "u1", Title: "x"})
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("改不了权限")
	}
	_ = fs.Flush() // 这次应该失败

	h := fs.PersistHealth()
	if h.OK {
		t.Fatal("落盘失败后 PersistHealth 应该报不健康")
	}
	if h.Failures == 0 {
		t.Fatal("应该记下失败次数")
	}
	if h.LastError == "" {
		t.Fatal("应该带上最后一次错误信息")
	}

	// 恢复权限后，下一次成功的 Flush 应该清零
	_ = os.Chmod(dir, 0o755)
	if err := fs.Flush(); err == nil {
		if h2 := fs.PersistHealth(); !h2.OK || h2.Failures != 0 {
			t.Fatalf("恢复后应该健康，得到 %+v", h2)
		}
	}
}

// 单个会话消息文件损坏：改名隔离后继续启动，原内容保留在隔离文件里。
// 旧实现只是 WARN 跳过——内存里没有这个会话的消息，下一次 Flush
// 就会用空数据覆盖原文件，历史无声消失。
func TestCorruptMessageFileQuarantined(t *testing.T) {
	dir := t.TempDir()

	// 先正常建库：一个会话 + 一条消息
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = fs.CreateConv(&domain.Conversation{ID: "c1", UserID: "u1", Title: "文件会坏的群"})
	_ = fs.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "历史"})
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}

	// 把消息文件写坏（截断的 JSON）
	msgFile := filepath.Join(dir, "messages", "c1.json")
	bad := []byte(`[{"id":"m1","conversationId":"c1"`)
	if err := os.WriteFile(msgFile, bad, 0o600); err != nil {
		t.Fatal(err)
	}

	// 主文件没坏，应能启动；坏文件被改名隔离而不是被跳过
	fs2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("消息文件损坏不应拒启动：%v", err)
	}
	if _, err := os.Stat(msgFile); !os.IsNotExist(err) {
		t.Fatal("启动后原 c1.json 应已被改名隔离")
	}

	// 原文件改名保留，内容原封不动
	entries, err := os.ReadDir(filepath.Join(dir, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	quarantined := ""
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "c1.json.corrupt-") {
			quarantined = e.Name()
		}
	}
	if quarantined == "" {
		t.Fatalf("应存在 c1.json.corrupt-* 隔离文件，实际目录：%v", entries)
	}
	kept, err := os.ReadFile(filepath.Join(dir, "messages", quarantined))
	if err != nil || !bytes.Equal(kept, bad) {
		t.Fatalf("隔离文件应保留原始损坏内容，err=%v", err)
	}

	// 再写一条新消息并落盘：c1.json 重新生成、只含新消息，
	// 历史仍在隔离文件里，人工可救，而不是被空数据覆盖掉。
	_ = fs2.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "新消息"})
	if err := fs2.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := fs2.Close(); err != nil {
		t.Fatal(err)
	}

	// 再重启一次：隔离文件（扩展名不是 .json）不会被再次加载或再次隔离
	fs3, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs3.Close()

	entries2, err := os.ReadDir(filepath.Join(dir, "messages"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != 2 {
		t.Fatalf("messages/ 应只有 c1.json 和一个隔离文件，实际 %d 个", len(entries2))
	}
	if got := fs3.ListMessages("c1", 0); len(got) != 1 || got[0].Content != "新消息" {
		t.Fatalf("重启后应只读到新消息，得到 %+v", got)
	}
}
