package memory

import (
	"path/filepath"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func TestConversationAndAgentScopesAreIsolated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memories.json")
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := st.AddConversation("c1", []*domain.Memory{
		{Kind: domain.KindFact, Text: "这个群的投标截止是下周三"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAgent("char1", "雪之下雪乃", "u1", []*domain.Memory{
		{Kind: domain.KindFact, Text: "用户养了一只叫豆豆的猫"},
	}); err != nil {
		t.Fatal(err)
	}

	if got := len(st.ListConversation("c1")); got != 1 {
		t.Fatalf("c1 应有 1 条群记忆，得到 %d", got)
	}
	if got := len(st.ListConversation("c2")); got != 0 {
		t.Fatalf("c2 不应读到 c1 的记忆，得到 %d", got)
	}
	if got := len(st.ListAgent("char1", "u1")); got != 1 {
		t.Fatalf("char1/u1 应有 1 条角色记忆，得到 %d", got)
	}
	if got := len(st.ListAgent("char1", "u2")); got != 0 {
		t.Fatalf("不同用户之间不能共享角色记忆，得到 %d", got)
	}
	if got := len(st.ListAgent("char2", "u1")); got != 0 {
		t.Fatalf("不同角色之间不能共享记忆，得到 %d", got)
	}
}

func TestPersistenceAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memories.json")
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.AddConversation("c1", []*domain.Memory{{Kind: domain.KindFact, Text: "群记忆"}})
	_ = st.AddAgent("char1", "雪乃", "u1", []*domain.Memory{
		{Kind: domain.KindFact, Text: "角色记忆"},
		{Kind: domain.KindRelation, Text: "你们最近有点疏远"},
	})
	_ = st.SetCovered("c1", "m_last")

	st2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(st2.ListConversation("c1")); got != 1 {
		t.Fatalf("重开后群记忆应还在，得到 %d", got)
	}
	if got := len(st2.ListAgent("char1", "u1")); got != 2 {
		t.Fatalf("重开后角色记忆应还在，得到 %d", got)
	}
	if got := st2.CoveredUpTo("c1"); got != "m_last" {
		t.Fatalf("处理游标应保留，得到 %q", got)
	}

	if n := st2.ClearAgent("char1", "u1"); n != 2 {
		t.Fatalf("应清掉 2 条角色记忆，得到 %d", n)
	}
	if got := len(st2.ListAgent("char1", "u1")); got != 0 {
		t.Fatalf("清空后角色记忆应为 0，得到 %d", got)
	}
	if n := st2.ClearConversation("c1"); n != 1 {
		t.Fatalf("应清掉 1 条群记忆，得到 %d", n)
	}
	if got := st2.CoveredUpTo("c1"); got != "m_last" {
		t.Fatal("清空群记忆不应重置游标，否则会把旧消息重新学一遍")
	}
}

func TestDedupe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memories.json")
	st, _ := NewFileStore(path)
	_ = st.AddConversation("c1", []*domain.Memory{{Kind: domain.KindFact, Text: "下周三交标书"}})
	_ = st.AddConversation("c1", []*domain.Memory{{Kind: domain.KindFact, Text: " 下周三交标书 "}})
	if got := len(st.ListConversation("c1")); got != 1 {
		t.Fatalf("同一件事不应重复存，得到 %d", got)
	}
}
