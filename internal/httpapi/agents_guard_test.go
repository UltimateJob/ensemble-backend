package httpapi

import (
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// PATCH /v1/conversations/{id} 的 agents 是整组替换。
// 只想改一个人却只提交这一个人，会把其余角色静默删掉——
// 模板群的 conv.Agents 是空的（角色由场景在运行时解析），
// 一次「改设定」就能把全队清空，下一轮编排器只点得到一个人。
//
// 这个测试锁住「部分提交要能识别出缺了谁」这段判断逻辑。
func TestAgentsTruncationDetected(t *testing.T) {
	// 模板群：conv.Agents 为空，角色由 code 场景解析出 4 个
	conv := &domain.Conversation{Studio: "code", Title: "代码工作室"}
	_, current := service.ResolveAgents(conv)
	if len(current) < 2 {
		t.Fatalf("场景应解析出多个角色，实际 %d", len(current))
	}

	// 只提交第一个角色
	got := missingAgents(current, []domain.ConvAgent{
		{Key: current[0].Key, Name: current[0].Name},
	})
	if len(got) != len(current)-1 {
		t.Fatalf("应识别出 %d 个被删角色，实际 %d：%v", len(current)-1, len(got), got)
	}
	if got[0] == current[0].Name {
		t.Fatal("不该把自己算成被删的")
	}
}

// 提交完整列表时不应报缺。
func TestAgentsCompleteNoMissing(t *testing.T) {
	conv := &domain.Conversation{Studio: "code", Title: "代码工作室"}
	_, current := service.ResolveAgents(conv)

	full := make([]domain.ConvAgent, 0, len(current))
	for _, a := range current {
		full = append(full, domain.ConvAgent{Key: a.Key, Name: a.Name})
	}
	if got := missingAgents(current, full); len(got) != 0 {
		t.Fatalf("完整提交不该报缺，实际：%v", got)
	}
}

// 按 name 匹配也要认——前端有时只带 name 不带 key。
func TestAgentsMatchByName(t *testing.T) {
	current := []service.Agent{{Key: "arch", Name: "架构"}, {Key: "be", Name: "后端"}}
	got := missingAgents(current, []domain.ConvAgent{{Name: "架构"}})
	if len(got) != 1 || got[0] != "后端" {
		t.Fatalf("按名字匹配失败：%v", got)
	}
}
