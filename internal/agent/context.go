package agent

import "github.com/UltimateJob/ensemble-backend/internal/domain"

// 执行器的上下文管理。
//
// 和群聊不同，执行器的消息里夹着工具输出：一次 read 可能返回几千行，
// 一次 run 最多 256KB。20 步下来很容易超出模型上下文，表现为
// 「跑到一半突然报 context length exceeded」。
//
// 策略不是压缩而是裁剪：
//   - 工具输出只在当步有用（下一步模型已经据此改了文件），旧输出可以砍
//   - 最近几步保留完整，更早的只留一行摘要
//   - assistant 的正文（说明、决策）全部保留——那才是线性的思路
//
// 为什么不摘要：执行器在写代码，摘要会丢掉具体的文件路径和错误信息，
// 而这些正是后续步骤要用的。砍工具输出更安全。

const (
	// execContextBudget 送进模型的字符预算（不含系统提示）。
	execContextBudget = 40000
	// keepRecentToolOutputs 最近几个工具输出保留完整内容。
	keepRecentToolOutputs = 6
	// trimmedToolNote 被裁掉的工具输出替换成这句话。
	trimmedToolNote = "（工具输出已省略以节省上下文；如需重新查看请再调用一次）"
)

// trimExecContext 在超出预算时裁剪历史工具输出。
//
// 返回新切片，不修改入参——调用方还持有 msgs 用于后续 append。
func trimExecContext(msgs []domain.ChatMessage) []domain.ChatMessage {
	if len(msgs) == 0 {
		return msgs
	}
	if totalRunes(msgs) <= execContextBudget {
		return msgs
	}

	// 从后往前数，标记最近 N 个工具输出为「保留」
	keep := make(map[int]bool, keepRecentToolOutputs)
	seen := 0
	for i := len(msgs) - 1; i >= 0 && seen < keepRecentToolOutputs; i-- {
		if msgs[i].Role == "tool" {
			keep[i] = true
			seen++
		}
	}

	out := make([]domain.ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if out[i].Role != "tool" || keep[i] {
			continue
		}
		// 已经裁过的不重复裁
		if out[i].Content == trimmedToolNote {
			continue
		}
		out[i].Content = trimmedToolNote
	}
	return out
}

func totalRunes(msgs []domain.ChatMessage) int {
	n := 0
	for _, m := range msgs {
		n += len([]rune(m.Content))
	}
	return n
}
