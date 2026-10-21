package store

import (
	"encoding/json"
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

	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = fs.CreateUser(newUser("u1", "13800000000"))
	_ = fs.CreateConv(&domain.Conversation{ID: "c1", UserID: "u1", Title: "标书攻坚组", Studio: "bid"})
	_ = fs.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "帮我拆一下"})

	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "users.json")); err != nil {
		t.Fatalf("用户分片不存在：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "messages", "c1.json")); err != nil {
		t.Fatalf("消息分片不存在：%v", err)
	}

	// 重新打开
	fs2, err := NewFileStore(dir)
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
	fs, err := NewFileStore(dir)
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
			_ = fs.Flush()
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

func TestFileStoreSharding(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	_ = fs.CreateUser(newUser("u1", "13800000000"))
	_ = fs.CreateConv(&domain.Conversation{ID: "c1", UserID: "u1", Title: "甲"})
	_ = fs.CreateConv(&domain.Conversation{ID: "c2", UserID: "u1", Title: "乙"})
	_ = fs.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "给 c1"})
	_ = fs.AppendMessage(&domain.Message{ConversationID: "c2", Role: domain.RoleUser, Content: "给 c2"})
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}

	// 每个会话应该有独立的消息文件
	for _, id := range []string{"c1", "c2"} {
		p := filepath.Join(dir, "messages", id+".json")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("会话 %s 的消息分片不存在：%v", id, err)
		}
		var msgs []*domain.Message
		if err := json.Unmarshal(b, &msgs); err != nil {
			t.Fatalf("会话 %s 分片不是合法 JSON：%v", id, err)
		}
		if len(msgs) != 1 || msgs[0].ConversationID != id {
			t.Fatalf("会话 %s 分片内容不对：%+v", id, msgs)
		}
	}

	// 重开后应能读回
	fs2, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()

	if len(fs2.ListConvs("")) != 2 {
		t.Fatal("重开后会话数不对")
	}
	if got := fs2.ListMessages("c1", 0); len(got) != 1 || got[0].Content != "给 c1" {
		t.Fatalf("重开后 c1 消息不对：%+v", got)
	}
	if got := fs2.ListMessages("c2", 0); len(got) != 1 || got[0].Content != "给 c2" {
		t.Fatalf("重开后 c2 消息不对：%+v", got)
	}
}

func TestFileStoreDeleteConvClearsShard(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	_ = fs.CreateConv(&domain.Conversation{ID: "c1", UserID: "u1"})
	_ = fs.AppendMessage(&domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: "x"})
	_ = fs.Flush()

	if _, err := os.Stat(filepath.Join(dir, "messages", "c1.json")); err != nil {
		t.Fatal("删除前分片应存在")
	}

	_ = fs.DeleteConv("c1")
	_ = fs.Flush()

	// 分片应被重写为空数组，而不是残留旧消息
	b, err := os.ReadFile(filepath.Join(dir, "messages", "c1.json"))
	if err != nil {
		t.Fatalf("分片文件应保留但为空：%v", err)
	}
	var msgs []*domain.Message
	_ = json.Unmarshal(b, &msgs)
	if len(msgs) != 0 {
		t.Fatalf("删会话后分片应清空，还剩 %d 条", len(msgs))
	}
}

func TestFileStoreMigratesLegacy(t *testing.T) {
	dir := t.TempDir()

	// 写一个旧版单文件
	snap := Snapshot{
		Version: 1,
		Users:   []*domain.User{{ID: "u1", Phone: "13800000000", Name: "老用户"}},
		Convs:   []*domain.Conversation{{ID: "c1", UserID: "u1", Title: "老群"}},
		Msgs:    []*domain.Message{{ConversationID: "c1", Role: domain.RoleUser, Content: "旧消息"}},
	}
	b, _ := json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(dir, "ensemble.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if u, ok := fs.GetUser("u1"); !ok || u.Name != "老用户" {
		t.Fatal("旧版用户未迁移")
	}
	if got := fs.ListMessages("c1", 0); len(got) != 1 || got[0].Content != "旧消息" {
		t.Fatal("旧版消息未迁移")
	}

	// 旧文件应被改名，避免下次重复迁移
	if _, err := os.Stat(filepath.Join(dir, "ensemble.json")); err == nil {
		t.Fatal("旧文件应已改名")
	}
	if _, err := os.Stat(filepath.Join(dir, "ensemble.json.migrated")); err != nil {
		t.Fatal("应留下 .migrated 备份")
	}
	// 新格式文件应已生成
	if _, err := os.Stat(filepath.Join(dir, "users.json")); err != nil {
		t.Fatal("迁移后应写出新格式")
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"c_abc123": "c_abc123",
		"../evil":  "___evil",
		"":         "unknown",
		"中文":       "__",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Fatalf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListMessagesPage(t *testing.T) {
	m := NewMemory()
	_ = m.CreateConv(&domain.Conversation{ID: "c1", UserID: "u1"})
	var ids []string
	for i := 0; i < 25; i++ {
		msg := &domain.Message{ConversationID: "c1", Role: domain.RoleUser, Content: string(rune('a' + i%26))}
		_ = m.AppendMessage(msg)
		ids = append(ids, msg.ID)
	}
	// 另一个会话的消息不应混进来
	_ = m.AppendMessage(&domain.Message{ConversationID: "c2", Role: domain.RoleUser, Content: "别的"})

	// 第一页：最新 10 条
	page1, more := m.ListMessagesPage("c1", "", 10)
	if len(page1) != 10 {
		t.Fatalf("第一页应有 10 条，得到 %d", len(page1))
	}
	if !more {
		t.Fatal("还有更早的消息，hasMore 应为 true")
	}
	// 正序返回：最后一条应是第 25 条
	if page1[9].ID != ids[24] {
		t.Fatal("第一页最后一条应是最新消息")
	}
	if page1[0].ID != ids[15] {
		t.Fatal("第一页第一条应是第 16 条")
	}

	// 第二页：before = 第一页第一条
	page2, more2 := m.ListMessagesPage("c1", page1[0].ID, 10)
	if len(page2) != 10 {
		t.Fatalf("第二页应有 10 条，得到 %d", len(page2))
	}
	if page2[9].ID != ids[14] {
		t.Fatal("第二页应紧接第一页")
	}
	if !more2 {
		t.Fatal("还剩 5 条，hasMore 应为 true")
	}

	// 第三页：只剩 5 条
	page3, more3 := m.ListMessagesPage("c1", page2[0].ID, 10)
	if len(page3) != 5 {
		t.Fatalf("第三页应有 5 条，得到 %d", len(page3))
	}
	if more3 {
		t.Fatal("已经到最早，hasMore 应为 false")
	}

	// 三段拼起来应恰好是 25 条，无重复无遗漏
	total := len(page3) + len(page2) + len(page1)
	if total != 25 {
		t.Fatalf("三页合计应为 25 条，得到 %d", total)
	}
	seen := map[string]bool{}
	for _, m := range append(append(page3, page2...), page1...) {
		if seen[m.ID] {
			t.Fatalf("消息重复：%s", m.ID)
		}
		seen[m.ID] = true
	}

	// 不存在的 beforeID：退化成取最新一页，不应 panic
	fallback, _ := m.ListMessagesPage("c1", "no-such-id", 10)
	if len(fallback) != 10 {
		t.Fatalf("无效游标应退化为最新一页，得到 %d 条", len(fallback))
	}

	// 空会话
	empty, moreE := m.ListMessagesPage("c_empty", "", 10)
	if len(empty) != 0 || moreE {
		t.Fatal("空会话应返回空且 hasMore=false")
	}
}

func TestFileStoreLock(t *testing.T) {
	dir := t.TempDir()

	fs1, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 第二个实例抢同一个目录应失败
	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("同一目录不应允许两个实例同时打开")
	}

	// 关掉之后可以重新打开
	if err := fs1.Close(); err != nil {
		t.Fatal(err)
	}
	fs2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("释放锁后应能重新打开：%v", err)
	}
	fs2.Close()
}
