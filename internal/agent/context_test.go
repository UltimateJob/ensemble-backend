package agent

import (
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 超出预算时，旧的工具输出要被裁掉——执行器会跑十几步，
// 每步的工具输出都在消息里累积，不裁就会超上下文。
func TestTrimExecContextDropsOldToolOutputs(t *testing.T) {
	var msgs []domain.ChatMessage
	msgs = append(msgs, domain.ChatMessage{Role: "system", Content: "你是执行者"})
	msgs = append(msgs, domain.ChatMessage{Role: "user", Content: "改一下 calc.go"})

	big := strings.Repeat("x", 8000)
	// 造 12 轮工具调用，总量远超预算
	for i := 0; i < 12; i++ {
		msgs = append(msgs, domain.ChatMessage{Role: "assistant", Content: "读文件"})
		msgs = append(msgs, domain.ChatMessage{Role: "tool", Content: big})
	}

	got := trimExecContext(msgs)

	// 收集所有工具输出的位置，按出现顺序
	var idx []int
	for i := range got {
		if got[i].Role == "tool" {
			idx = append(idx, i)
		}
	}
	if len(idx) != 12 {
		t.Fatalf("工具消息数应保持 12，实际 %d", len(idx))
	}
	// 最后 keepRecentToolOutputs 个保留完整，之前的被裁
	cut := len(idx) - keepRecentToolOutputs
	for n, i := range idx {
		if n < cut {
			if got[i].Content != trimmedToolNote {
				t.Fatalf("第 %d 个（较早的）应被裁剪，实际保留了 %d 字", n+1, len(got[i].Content))
			}
		} else {
			if got[i].Content != big {
				t.Fatalf("第 %d 个（最近的）不该被裁", n+1)
			}
		}
	}
}

// assistant 的正文全部保留：那是模型的思路，裁了就接不上。
func TestTrimExecContextKeepsAssistantText(t *testing.T) {
	var msgs []domain.ChatMessage
	big := strings.Repeat("y", 8000)
	for i := 0; i < 12; i++ {
		msgs = append(msgs, domain.ChatMessage{Role: "assistant", Content: "步骤说明" + big})
		msgs = append(msgs, domain.ChatMessage{Role: "tool", Content: big})
	}
	got := trimExecContext(msgs)
	for i := range got {
		if got[i].Role == "assistant" && !strings.HasPrefix(got[i].Content, "步骤说明") {
			t.Fatalf("assistant 正文被改动了：%s", got[i].Content[:20])
		}
	}
}

// 没超预算时原样返回，避免无谓的拷贝影响性能。
func TestTrimExecContextPassthroughWhenSmall(t *testing.T) {
	msgs := []domain.ChatMessage{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"},
		{Role: "tool", Content: "小输出"},
	}
	got := trimExecContext(msgs)
	if len(got) != len(msgs) {
		t.Fatalf("长度变了：%d", len(got))
	}
	if got[2].Content != "小输出" {
		t.Fatal("小上下文不该被裁剪")
	}
}
