package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func newUser(id, phone string) *domain.User {
	return &domain.User{ID: id, Phone: phone, Name: "测试", Plan: "free", Quota: 100, Status: "active"}
}

func TestMemoryUserCRUD(t *testing.T) {
	m := NewMemory()
	u := newUser("u1", "13800000000")
	if err := m.CreateUser(u); err != nil {
		t.Fatal(err)
	}

	// 按 ID 取
	got, ok := m.GetUser("u1")
	if !ok || got.Phone != "13800000000" {
		t.Fatal("按 ID 查不到刚建的用户")
	}

	// 按手机号取
	got, ok = m.GetUserByPhone("13800000000")
	if !ok || got.ID != "u1" {
		t.Fatal("按手机号查不到")
	}

	// 空手机号不应误匹配
	if _, ok := m.GetUserByPhone(""); ok {
		t.Fatal("空手机号不应匹配到任何用户")
	}

	// 更新
	got.Used = 42
	if err := m.UpdateUser(got); err != nil {
		t.Fatal(err)
	}
	again, _ := m.GetUser("u1")
	if again.Used != 42 {
		t.Fatal("更新未生效")
	}

	// 副本隔离：改返回值不应影响存储
	again.Used = 999
	fresh, _ := m.GetUser("u1")
	if fresh.Used == 999 {
		t.Fatal("GetUser 应返回副本，外部修改不应污染存储")
	}
}

func TestMemoryDeleteConvCascades(t *testing.T) {
	m := NewMemory()
	conv := &domain.Conversation{ID: "c1", UserID: "u1", Title: "测试群"}
	_ = m.CreateConv(conv)
	_ = m.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "你好"})
	_ = m.AppendMessage(&domain.Message{ConversationID: "c2", Role: domain.RoleUser, Content: "别的群"})

	_ = m.DeleteConv("c1")

	if _, ok := m.GetConv("c1"); ok {
		t.Fatal("会话应已删除")
	}
	if msgs := m.ListMessages("c1", 0); len(msgs) != 0 {
		t.Fatalf("删会话应连带删消息，还剩 %d 条", len(msgs))
	}
	if msgs := m.ListMessages("c2", 0); len(msgs) != 1 {
		t.Fatal("不应误删其他会话的消息")
	}
}

func TestListMessagesLimit(t *testing.T) {
	m := NewMemory()
	_ = m.CreateConv(&domain.Conversation{ID: "c1", UserID: "u1"})
	for i := 0; i < 10; i++ {
		_ = m.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: string(rune('a' + i))})
	}
	if got := m.ListMessages("c1", 3); len(got) != 3 {
		t.Fatalf("limit=3 应返回 3 条，得到 %d", len(got))
	}
	// 取的是最近几条
	if got := m.ListMessages("c1", 3); got[2].Content != "j" {
		t.Fatalf("应返回最近的 3 条，最后一条是 %q", got[2].Content)
	}
}

func TestFileStorePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	fs, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = fs.CreateUser(newUser("u1", "13800000000"))
	_ = fs.CreateConv(&domain.Conversation{ID: "c1", UserID: "u1", Title: "标书攻坚组", Studio: "bid"})
	_ = fs.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "帮我拆一下"})

	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("落盘文件不存在：%v", err)
	}

	// 重新打开
	fs2, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()

	if u, ok := fs2.GetUser("u1"); !ok || u.Phone != "13800000000" {
		t.Fatal("重启后用户丢失")
	}
	if c, ok := fs2.GetConv("c1"); !ok || c.Title != "标书攻坚组" {
		t.Fatal("重启后会话丢失")
	}
	if msgs := fs2.ListMessages("c1", 0); len(msgs) != 1 {
		t.Fatalf("重启后消息丢失，剩 %d 条", len(msgs))
	}
}

func TestFileStoreConcurrent(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFile(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = fs.CreateUser(newUser("u"+string(rune('a'+n)), ""))
			_ = fs.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "并发"})
			_, _ = fs.GetUser("u1")
			fs.Dump()
		}(i)
	}
	wg.Wait()

	// 能正常落盘且不 panic 即可（竞态由 -race 检测）
	if err := fs.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestListAuditOrder(t *testing.T) {
	m := NewMemory()
	for i := 0; i < 3; i++ {
		_ = m.AppendAudit(&domain.AuditLog{Actor: "admin", Action: "op", Target: string(rune('a' + i))})
		time.Sleep(2 * time.Millisecond)
	}
	got := m.ListAudit(10)
	if len(got) != 3 {
		t.Fatalf("应有 3 条审计，得到 %d", len(got))
	}
	if got[0].Target != "c" {
		t.Fatalf("审计应按时间倒序，首条应是 c，得到 %q", got[0].Target)
	}
}
