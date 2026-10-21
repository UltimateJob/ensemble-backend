package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// Event 编排过程中推给前端的一件事。
type Event struct {
	Type    string `json:"type"`              // start | delta | end | artifact | error | done
	Agent   string `json:"agent,omitempty"`   // agent key
	Name    string `json:"name,omitempty"`    // 显示名
	Color   string `json:"color,omitempty"`   // 角色色
	Content string `json:"content,omitempty"` // 增量文本
	Title   string `json:"title,omitempty"`   // artifact 标题
	Detail  string `json:"detail,omitempty"`  // artifact 说明
	Tokens  int    `json:"tokens,omitempty"`
	Latency int    `json:"latency,omitempty"`
}

// Orchestrator 让群里的多个 Agent 依次发言，后一个能看到前面的内容。
type Orchestrator struct {
	store store.Store
	gw    *gateway.Gateway
}

func NewOrchestrator(s store.Store, gw *gateway.Gateway) *Orchestrator {
	return &Orchestrator{store: s, gw: gw}
}

// Run 执行一轮群聊。emit 在调用方 goroutine 里同步执行。
func (o *Orchestrator) Run(
	ctx context.Context,
	conv *domain.Conversation,
	user *domain.User,
	userText string,
	emit func(Event),
) {
	sc, ok := FindScenario(conv.Studio)
	if !ok {
		sc, _ = FindScenario("bid")
	}

	// 用户消息入库
	_ = o.store.AppendMessage(&domain.Message{
		ConversationID: conv.ID,
		Role:           domain.RoleUser,
		Content:        userText,
	})

	history := o.store.ListMessages(conv.ID, 12)
	round := sc.Round
	if round <= 0 || round > len(sc.Agents) {
		round = len(sc.Agents)
	}

	// 本轮已发生的发言，供后续 Agent 参考
	var transcript strings.Builder
	transcript.WriteString("【本轮群聊记录】\n用户：" + userText + "\n")

	for i := 0; i < round; i++ {
		agent := sc.Agents[i]
		select {
		case <-ctx.Done():
			emit(Event{Type: "done"})
			return
		default:
		}

		emit(Event{Type: "start", Agent: agent.Key, Name: agent.Name, Color: agent.Color})

		msgs := o.buildMessages(sc, agent, history, transcript.String())
		req := domain.ChatRequest{
			Model:    conv.Model,
			Messages: msgs,
			Stream:   true,
		}

		start := time.Now()
		stream, err := o.gw.Stream(ctx, req)
		if err != nil {
			emit(Event{Type: "error", Agent: agent.Key, Name: agent.Name, Content: "上游不可用：" + err.Error()})
			break
		}

		var buf strings.Builder
		tokens := 0
		for chunk := range stream {
			if chunk.Err != nil {
				emit(Event{Type: "error", Agent: agent.Key, Name: agent.Name, Content: chunk.Err.Error()})
				break
			}
			if chunk.Content != "" {
				buf.WriteString(chunk.Content)
				emit(Event{Type: "delta", Agent: agent.Key, Name: agent.Name, Content: chunk.Content})
			}
			if chunk.Usage != nil {
				tokens = chunk.Usage.CompletionTokens
			}
			if chunk.Done {
				break
			}
		}

		text := strings.TrimSpace(buf.String())
		latency := int(time.Since(start).Milliseconds())

		if text != "" {
			_ = o.store.AppendMessage(&domain.Message{
				ConversationID: conv.ID,
				Role:           domain.RoleAssistant,
				Agent:          agent.Name,
				Content:        text,
				Model:          conv.Model,
				TokensOut:      tokens,
				LatencyMS:      latency,
			})
			transcript.WriteString(agent.Name + "：" + text + "\n")
		}

		emit(Event{
			Type: "end", Agent: agent.Key, Name: agent.Name,
			Color: agent.Color, Tokens: tokens, Latency: latency,
		})

		// 有产物的角色，结束后推一张文件卡
		if agent.Artifact != "" && text != "" {
			emit(Event{
				Type: "artifact", Agent: agent.Key, Name: agent.Name,
				Title: agent.Artifact, Detail: summarize(text),
			})
		}

		// 用户额度
		user.Used++
		_ = o.store.UpdateUser(user)
	}

	_ = o.store.UpdateConv(conv)
	emit(Event{Type: "done"})
}

// buildMessages 组装单个 Agent 的上下文。
func (o *Orchestrator) buildMessages(sc Scenario, agent Agent, history []*domain.Message, transcript string) []domain.ChatMessage {
	sys := fmt.Sprintf(`%s

你是群聊「%s」里的一员，你的名字是「%s」，职责是「%s」。
群里还有：%s。
规则：
- 只说你自己会说的话，不要替别人说话，不要写旁白。
- 不要复述别人刚说过的内容。
- 简短，像真人在群里发消息。通常 1-3 句，最多 120 字。
- 中文，不要用 markdown 标题，不要用「首先/其次/总之」这类套话。`,
		agent.Prompt, sc.Name, agent.Name, agent.Title, otherAgents(sc, agent.Key))

	out := []domain.ChatMessage{{Role: "system", Content: sys}}

	// 历史上下文（跳过本轮刚入库的用户消息，稍后单独加）
	for i, m := range history {
		if i == len(history)-1 && m.Role == domain.RoleUser {
			continue
		}
		content := m.Content
		if m.Role == domain.RoleAssistant && m.Agent != "" {
			content = m.Agent + "：" + content
		}
		out = append(out, domain.ChatMessage{Role: string(m.Role), Content: content})
	}

	if transcript != "" {
		out = append(out, domain.ChatMessage{Role: "user", Content: transcript})
		out = append(out, domain.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("现在轮到你（%s）说话。只回你要说的内容。", agent.Name),
		})
	}
	return out
}

func otherAgents(sc Scenario, self string) string {
	var names []string
	for _, a := range sc.Agents {
		if a.Key != self {
			names = append(names, a.Name+"（"+a.Title+"）")
		}
	}
	return strings.Join(names, "、")
}

func summarize(text string) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) > 28 {
		return string(r[:28]) + "…"
	}
	return string(r)
}
