package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/memory"
)

func TestMemoryContextIsScoped(t *testing.T) {
	st, err := memory.NewFileStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.AddConversation("c1", []*domain.Memory{
		{Kind: domain.KindFact, Text: "这个群的截止日期是下周三"},
	})
	_ = st.AddAgent("char1", "雪乃", "u1", []*domain.Memory{
		{Kind: domain.KindFact, Text: "用户养了一只叫豆豆的猫"},
		{Kind: domain.KindRelation, Text: "你们最近聊得比较多，她开始会主动问你近况"},
	})
	svc := NewMemoryService(st, nil)

	got := svc.Context("c1", "char1", "u1", "豆豆最近怎么样")
	for _, want := range []string{"[群记忆]", "下周三", "[你和这位用户]", "豆豆", "主动问你近况"} {
		if !strings.Contains(got, want) {
			t.Fatalf("记忆卡缺少 %q：\n%s", want, got)
		}
	}

	if other := svc.Context("c2", "char1", "u1", "豆豆"); strings.Contains(other, "截止日期") {
		t.Fatalf("群记忆不应跨群出现：\n%s", other)
	}
	if other := svc.Context("c1", "char2", "u1", "豆豆"); strings.Contains(other, "豆豆") {
		t.Fatalf("角色记忆不应被别的角色读到：\n%s", other)
	}
	if other := svc.Context("c1", "char1", "u2", "豆豆"); strings.Contains(other, "豆豆") {
		t.Fatalf("角色记忆不应被别的用户读到：\n%s", other)
	}
}

func TestMemoryDate(t *testing.T) {
	now := time.Now()
	if got := memoryDate(now, now); got != "今天" {
		t.Fatalf("今天：%q", got)
	}
	if got := memoryDate(now.AddDate(0, 0, -1), now); got != "昨天" {
		t.Fatalf("昨天：%q", got)
	}
}
