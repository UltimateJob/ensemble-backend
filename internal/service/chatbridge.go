package service

import (
	"fmt"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// AppendRunMessage 把一次主 Run 的结果沉淀成群聊消息。
//
// 实时气泡负责「过程」，这条消息负责「刷新后还在」。
// 返回消息 id，由 run.end 事件带给前端，避免同一次执行出现两条消息。
func (c *Chat) AppendRunMessage(r *agent.Run) string {
	if r == nil || strings.TrimSpace(r.ConversationID) == "" {
		return ""
	}
	kind := "run"
	content := strings.TrimSpace(r.Summary)
	switch r.Status {
	case agent.StatusFailed, agent.StatusAborted:
		kind = "run.failed"
		if content == "" {
			content = "任务没有完成"
			if r.Error != "" {
				content += "：" + r.Error
			}
		}
	case agent.StatusDone:
		if content == "" {
			content = fmt.Sprintf("完成 %d 步，改动 %d 个文件", r.Step, len(r.Diffs))
		}
	default:
		// 运行中不应该走到这里；留个可读兜底。
		if content == "" {
			content = "任务已结束"
		}
	}
	msg := &domain.Message{
		ConversationID: r.ConversationID,
		Role:           domain.RoleAssistant,
		Agent:          r.OperatorName,
		Content:        content,
		Attachments:    append([]domain.ArtifactRef(nil), r.Artifacts...),
		Kind:           kind,
		Ref:            r.ID,
		TokensIn:       r.TokensIn,
		TokensOut:      r.TokensOut,
		Model:          r.Model,
	}
	_ = c.store.AppendMessage(msg)
	return msg.ID
}

// AppendTeamMessage 团队任务结束时沉淀成一条群消息。
// 只在真正结束（done/partial/failed）时调用；中途状态由团队事件流推送，
// 不往聊天里刷阶段消息。
func (c *Chat) AppendTeamMessage(t *domain.TeamRun) string {
	if t == nil || strings.TrimSpace(t.ConversationID) == "" {
		return ""
	}
	kind := "team"
	title := "团队任务完成"
	switch t.Status {
	case "partial":
		kind = "team.partial"
		title = "团队任务部分完成"
	case "failed":
		kind = "team.failed"
		title = "团队任务失败"
	case "cancelled":
		kind = "team.failed"
		title = "团队任务已取消"
	}

	var b strings.Builder
	b.WriteString(title)
	if t.Score > 0 {
		fmt.Fprintf(&b, " · 评分 %d", t.Score)
	}
	b.WriteString("\n")
	if len(t.Assignments) > 0 {
		names := make([]string, 0, len(t.Assignments))
		for _, a := range t.Assignments {
			n := a.AgentName
			if n == "" {
				n = a.AgentKey
			}
			names = append(names, n)
		}
		b.WriteString("参与：" + strings.Join(names, "、") + "\n")
	}
	if len(t.Checks) > 0 {
		passed := 0
		for _, c := range t.Checks {
			if c.Passed && !c.Skipped {
				passed++
			}
		}
		fmt.Fprintf(&b, "验证：%d/%d 通过\n", passed, len(t.Checks))
	}
	if t.Review != nil && t.Review.Verdict != "" {
		verdict := "未通过"
		if t.Review.Verdict == "pass" {
			verdict = "通过"
		}
		b.WriteString("验收：" + verdict + "\n")
	}
	if t.Repairs > 0 {
		fmt.Fprintf(&b, "返工：%d 轮\n", t.Repairs)
	}
	if t.Error != "" {
		b.WriteString("原因：" + t.Error)
	}
	content := strings.TrimSpace(b.String())

	msg := &domain.Message{
		ConversationID: t.ConversationID,
		Role:           domain.RoleAssistant,
		Agent:          "团队",
		Content:        content,
		Kind:           kind,
		Ref:            t.ID,
		TokensIn:       t.TokensIn,
		TokensOut:      t.TokensOut,
	}
	_ = c.store.AppendMessage(msg)
	return msg.ID
}
