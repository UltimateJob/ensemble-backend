package service

import (
	"context"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// 群聊上下文压缩。
//
// 为什么需要：trimHistory 是硬截断——按字符预算保留最近的消息，更早的
// 直接丢。群聊一长，角色就忘了之前定过什么方案、谁负责哪块，开始重复
// 讨论已经决定过的事。
//
// 做法：把「即将被挤出去」的那部分压成几句话，附在上下文最前面。
// 不是丢弃，也不是全文保留——是在有限的预算里留下事实。
//
// 三个约束：
//   - 只在真的超出预算时才压缩，不要每轮都跑（省钱、省延迟）
//   - 用最便宜的模型做压缩：为了省 token 而多花 token 是本末倒置
//   - 压缩失败就退回硬截断，不能因为摘要挂了让聊天也挂掉

const (
	// historyBudget 送进模型的历史字符预算。
	// 6000 字符约 3000-4000 token，对 deepseek 的 64k 上下文来说很保守——
	// 但要给系统提示、记忆卡、本轮输出留出空间。
	historyBudget = 6000
	// summaryTrigger 超过这个长度才触发压缩。
	// 等于预算的 1.5 倍：留出余量，避免每轮都压一次。
	summaryTrigger = historyBudget * 3 / 2
	// summaryMaxLen 摘要本身的长度上限（字符）。
	summaryMaxLen = 800
)

// buildHistory 组装送进模型的历史：摘要 + 最近原文。
//
// 返回值里 summary 可能为空（还没压缩过，或压缩失败）。
func (o *Orchestrator) buildHistory(ctx context.Context, conv *domain.Conversation, all []*domain.Message) (string, []*domain.Message) {
	return buildHistoryWith(ctx, o.store, o.gw, conv, all)
}

// buildHistoryWith 是 buildHistory 的无接收者版本，供 Chat 复用。
//
// 两条聊天路径（群聊编排、单聊直连）都要同一套压缩规则：
// 分开实现迟早会走样，而用户感知不到自己在哪条路径上。
func buildHistoryWith(ctx context.Context, st store.Store, gw *gateway.Gateway, conv *domain.Conversation, all []*domain.Message) (string, []*domain.Message) {
	trimmed := trimHistory(all, historyBudget)
	summary := conv.Summary

	// 没超出预算：不压缩。这是最常见的情况，也是最该省的那条路径。
	if len(trimmed) >= len(all) {
		return summary, trimmed
	}

	// 超出预算：找出被挤掉的部分。
	cut := len(all) - len(trimmed)
	dropped := all[:cut]
	if len(dropped) == 0 {
		return summary, trimmed
	}

	// 已经被摘要覆盖过的部分不再重复压缩。
	fresh := unsummarized(conv, dropped)
	if len(fresh) == 0 {
		return summary, trimmed
	}

	// 只有积攒到一定量才值得花一次模型调用。
	if runeLenOf(fresh) < summaryTrigger-historyBudget {
		return summary, trimmed
	}

	compressed, lastID2, cerr := compressHistory(ctx, gw, summary, fresh)
	if newSummary, lastID, err := compressed, lastID2, cerr; err == nil && newSummary != "" {
		// 压缩成功才记一笔：失败退回硬截断，用户无感，但排查时需要线索。
		logx.L().Info("已压缩早期对话", "conv", conv.ID,
			"dropped", len(fresh), "summaryLen", len(newSummary))
		conv.Summary = newSummary
		conv.SummaryUpTo = lastID
		_ = st.UpdateConv(conv)
		return newSummary, trimmed
	}
	// 压缩失败：退回硬截断。聊天不能因为摘要挂了而中断。
	return summary, trimmed
}

// unsummarized 返回 dropped 里还没被摘要覆盖的消息。
func unsummarized(conv *domain.Conversation, dropped []*domain.Message) []*domain.Message {
	if conv.SummaryUpTo == "" {
		return dropped
	}
	for i, m := range dropped {
		if m.ID == conv.SummaryUpTo {
			return dropped[i+1:]
		}
	}
	// 游标找不到（消息被删了之类）：保守起见重新压全部
	return dropped
}

// compress 调一次模型，把 old + fresh 压成新摘要。
//
// 用 conv.Model（会话选的模型）而不是硬编码：多一次调用已经是额外成本，
// 再指定一个用户没选的模型会让账单更难解释。
func compressHistory(ctx context.Context, gw *gateway.Gateway, old string, fresh []*domain.Message) (string, string, error) {
	var lines []string
	for _, m := range fresh {
		who := "用户"
		if m.Role == domain.RoleAssistant {
			who = m.Agent
			if who == "" {
				who = "角色"
			}
		}
		// 每条截断到 300 字：压缩阶段不需要全文，只要事实
		lines = append(lines, who+"："+clipRunes(m.Content, 300))
	}

	prompt := `你在压缩一段群聊记录，目的是让后续发言的角色不丢失关键信息。

已有的摘要：
` + orNone(old) + `

需要并入的新对话：
` + strings.Join(lines, "\n") + `

要求：
1. 输出合并后的新摘要，不超过 400 字
2. 只保留「事实」：已确定的目标、方案、分工、约束、结论、待办
3. 丢掉寒暄、重复讨论、被否决的方案
4. 用陈述句，不要「用户说」「角色提到」这类转述结构
5. 如果新旧摘要冲突，以新的为准
6. 直接输出摘要正文，不要任何前缀`

	ch, err := gw.Stream(ctx, domain.ChatRequest{
		Model:       "",
		Messages:    []domain.ChatMessage{{Role: string(domain.RoleUser), Content: prompt}},
		Stream:      true,
		MaxTokens:   600,
		Temperature: 0.2,
	})
	if err != nil {
		return "", "", err
	}

	var sb strings.Builder
	for c := range ch {
		if c.Err != nil {
			return "", "", c.Err
		}
		sb.WriteString(c.Content)
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", "", nil
	}
	out = clipRunes(out, summaryMaxLen)

	lastID := ""
	if len(fresh) > 0 {
		lastID = fresh[len(fresh)-1].ID
	}
	return out, lastID, nil
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（无）"
	}
	return s
}

func runeLenOf(msgs []*domain.Message) int {
	n := 0
	for _, m := range msgs {
		n += len([]rune(m.Content))
	}
	return n
}

func clipRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// summaryBlock 把摘要包装成送进系统提示的一段。
func summaryBlock(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	return "\n\n## 早前对话摘要\n以下是更早的对话压缩成的要点，作为背景。不要重复讨论已经定下的事：\n" + summary
}
