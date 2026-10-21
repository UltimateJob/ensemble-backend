package service

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// Event 编排过程中推给前端的一件事。
type Event struct {
	Type    string `json:"type"` // start | delta | end | artifact | pass | ask | error | done
	Agent   string `json:"agent,omitempty"`
	Name    string `json:"name,omitempty"`
	Color   string `json:"color,omitempty"`
	Content string `json:"content,omitempty"`
	Title   string `json:"title,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Tokens  int    `json:"tokens,omitempty"`
	Latency int    `json:"latency,omitempty"`
}

// Orchestrator 模拟真实群聊：谁想说谁说，可以不说，也可以直接问人。
//
// 与"轮流发言"的区别：
//   - 每个角色独立判断这个话题跟自己有没有关系，无关就 [PASS]
//   - 说话顺序不固定，由"上一位是谁 + 是否说过"决定，带一点随机
//   - 角色可以直接向用户提问，问到人就停下来等回复
type Orchestrator struct {
	store store.Store
	gw    *gateway.Gateway
	locks *convLocks
	slow  *SlowLog
}

func NewOrchestrator(s store.Store, gw *gateway.Gateway, slow *SlowLog) *Orchestrator {
	return &Orchestrator{store: s, gw: gw, locks: newConvLocks(), slow: slow}
}

// Slow 暴露给运维台。
func (o *Orchestrator) Slow() *SlowLog { return o.slow }

// ErrConversationBusy 同一会话上一轮还没跑完。
var ErrConversationBusy = fmt.Errorf("这个群正在说话，稍等一下")

// Acquire 尝试占用会话。返回 false 表示有人在说。
func (o *Orchestrator) Acquire(convID string) bool { return o.locks.TryLock(convID) }
func (o *Orchestrator) Release(convID string)      { o.locks.Unlock(convID) }

// Busy 给运维台看这个群是不是卡在编排里。
func (o *Orchestrator) Busy(convID string) bool { return o.locks.Busy(convID) }

// Run 用户发言后，让群里的人自己决定接不接话。
func (o *Orchestrator) Run(ctx context.Context, conv *domain.Conversation, user *domain.User, userText string, emit func(Event)) {
	// 先说后调度：消息无论如何都落库，用户说的话不能因为并发被吞掉
	_ = o.store.AppendMessage(&domain.Message{
		ConversationID: conv.ID,
		Role:           domain.RoleUser,
		Content:        userText,
	})
	o.loop(ctx, conv, user, emit, fmt.Sprintf("用户刚说：%s", userText), false)
}

// Kickoff 没有用户消息时，让角色之间自己聊起来。
func (o *Orchestrator) Kickoff(ctx context.Context, conv *domain.Conversation, user *domain.User, topic string, emit func(Event)) {
	prompt := "群里现在没人说话，安静了一会儿。"
	if topic != "" {
		prompt = "用户刚才提到：" + topic + "，但还没人回应。"
	}
	// 没有人先开口时，第一个人必须说话，否则整轮都是沉默
	o.loop(ctx, conv, user, emit, prompt, true)
}

// loop 核心循环：最多让 N 个人开口，其余可以沉默。
// 整轮持有会话锁，避免两轮编排交错。
func (o *Orchestrator) loop(ctx context.Context, conv *domain.Conversation, user *domain.User, emit func(Event), trigger string, forceFirst bool) {
	if !o.locks.TryLock(conv.ID) {
		// 有人在说：这一轮直接跳过，不产生交错的发言
		emit(Event{Type: "busy"})
		emit(Event{Type: "done"})
		return
	}
	defer o.locks.Unlock(conv.ID)

	sc, ok := FindScenario(conv.Studio)
	if !ok {
		// 自定义群没有预设场景，用中性上下文，避免被别的场景带偏
		sc = Scenario{Key: "custom", Name: conv.Title, Opening: "", Round: 3}
	}

	// 会话里有自定义角色时，以它们为准
	agents := sc.Agents
	if len(conv.Agents) > 0 {
		agents = make([]Agent, 0, len(conv.Agents))
		for _, a := range conv.Agents {
			agents = append(agents, Agent{
				Key: a.Key, Name: a.Name, Title: a.Title,
				Emoji: a.Emoji, Color: a.Color, Prompt: a.Prompt,
			})
		}
	}
	if sc.Round <= 0 || len(agents) < sc.Round {
		sc.Round = len(agents)
	}
	if len(agents) == 0 {
		emit(Event{Type: "done"})
		return
	}

	history := o.store.ListMessages(conv.ID, 16)
	maxSpeakers := sc.Round
	if maxSpeakers <= 0 {
		maxSpeakers = 3
	}

	// 按剩余额度裁剪本轮发言人数。
	// 不裁剪的话：只剩 1 次额度也会跑满 3 个角色，used 冲成负数。
	if left := user.Remaining(); left < maxSpeakers {
		if left <= 0 {
			emit(Event{Type: "quota"})
			emit(Event{Type: "done"})
			_ = o.store.UpdateConv(conv)
			return
		}
		maxSpeakers = left
	}

	if maxSpeakers > len(agents) {
		maxSpeakers = len(agents)
	}

	spoken := map[string]bool{}
	speakers := 0
	lastKey := ""

	// 最多尝试 2 倍于发言人数的次数，允许一部分人沉默
	for attempt := 0; attempt < len(agents)*2 && speakers < maxSpeakers; attempt++ {
		select {
		case <-ctx.Done():
			emit(Event{Type: "done"})
			return
		default:
		}

		agent := pickSpeaker(agents, spoken, lastKey, speakers == 0)
		if agent.Key == "" {
			break
		}

		force := forceFirst && speakers == 0
		res, err := o.ask(ctx, conv, sc, *agent, history, trigger, emit, force)
		if err != nil {
			logx.Req(ctx).Warn("agent speak failed", "agent", agent.Name, "err", err.Error())
			spoken[agent.Key] = true
			continue
		}

		if res.pass {
			// 沉默：告诉前端这条支线走过了，不产生气泡
			emit(Event{Type: "pass", Agent: agent.Key, Name: agent.Name, Color: agent.Color})
			spoken[agent.Key] = true
			continue
		}

		// 入库
		_ = o.store.AppendMessage(&domain.Message{
			ConversationID: conv.ID,
			Role:           domain.RoleAssistant,
			Agent:          agent.Name,
			Content:        res.text,
			Model:          conv.Model,
			TokensOut:      res.tokens,
			LatencyMS:      res.latency,
		})
		history = append(history, &domain.Message{
			ConversationID: conv.ID,
			Role:           domain.RoleAssistant,
			Agent:          agent.Name,
			Content:        res.text,
		})

		spoken[agent.Key] = true
		lastKey = agent.Key
		speakers++

		// 被点名问到，就停下等用户
		trigger = fmt.Sprintf("刚才 %s 说：%s", agent.Name, res.text)

		_ = o.store.IncrementUsed(user.ID, 1)

		if res.asksUser {
			break
		}
	}

	_ = o.store.UpdateConv(conv)
	emit(Event{Type: "done"})
}

// pickSpeaker 选择下一个发言者。
//
// 第一轮固定给负责人（agents[0]）：用户说了一句话，应该先由拆解角色接住、
// 定个调子，而不是随机抽到某个专业角色直接反问用户——那样整轮就只有一个
// 问句，群里显得没人。
//
// 之后按「没说过话 + 不是上一个」随机，保持自主。
func pickSpeaker(agents []Agent, spoken map[string]bool, lastKey string, first bool) *Agent {
	if first && len(agents) > 0 && !spoken[agents[0].Key] {
		return &agents[0]
	}

	var pool []Agent
	for _, a := range agents {
		if spoken[a.Key] || a.Key == lastKey {
			continue
		}
		pool = append(pool, a)
	}
	if len(pool) == 0 {
		return nil
	}
	// 带一点随机，避免每次都是同一个顺序
	i := rand.Intn(len(pool))
	return &pool[i]
}

type speakResult struct {
	text     string
	pass     bool
	asksUser bool
	tokens   int
	latency  int
}

// ask 让一个角色自己决定要不要说话、说什么。
func (o *Orchestrator) ask(ctx context.Context, conv *domain.Conversation, sc Scenario, agent Agent, history []*domain.Message, trigger string, emit func(Event), force bool) (speakResult, error) {
	emit(Event{Type: "start", Agent: agent.Key, Name: agent.Name, Color: agent.Color})

	sys := fmt.Sprintf(`%s

你正在一个群聊里，群名「%s」，你的名字是「%s」，身份是「%s」。
群里还有：%s。

这是真实群聊，不是客服。规则：
1. 你有权保持沉默。如果这个话题跟你无关、或者别人已经说清楚了，只回复 [PASS]，不要硬接。
2. 想说话就直接说你要说的话，不要加「%s：」这样的前缀，不要写旁白。
3. 如果缺关键信息、必须问用户才能继续，就直接问，用「@用户」开头。
4. 一句话到三句话，像真人在群里打字。不要用 markdown、不要列点、不要客套。
5. 不要复述别人刚说过的话。%s`,
		agent.Prompt, sc.Name, agent.Name, agent.Title, otherAgents(sc, agent.Key), agent.Name,
		forceLine(force))

	msgs := []domain.ChatMessage{{Role: "system", Content: sys}}
	for _, m := range history {
		content := m.Content
		if m.Role == domain.RoleAssistant && m.Agent != "" {
			content = m.Agent + "：" + content
		}
		msgs = append(msgs, domain.ChatMessage{Role: string(m.Role), Content: content})
	}
	instruction := "现在轮到你判断：要不要说话？说就直说，不说就回 [PASS]。"
	if force {
		instruction = "群里冷场了，必须由你起个头。直接说一句话把话头挑起来，不要说 [PASS]。"
	}
	msgs = append(msgs, domain.ChatMessage{Role: "user", Content: trigger + "\n\n" + instruction})

	start := time.Now()
	stream, err := o.gw.Stream(ctx, domain.ChatRequest{Model: "", Messages: msgs, Stream: true})
	if err != nil {
		if o.slow != nil {
			o.slow.MaybeRecord(SlowEntry{
				ConvID: conv.ID, ConvTitle: conv.Title, Agent: agent.Name,
				Err: err.Error(),
			})
		}
		return speakResult{}, err
	}

	var buf strings.Builder
	var pending strings.Builder
	flushed := false
	tokens := 0

	for chunk := range stream {
		if chunk.Err != nil {
			return speakResult{}, chunk.Err
		}
		if chunk.Content != "" {
			buf.WriteString(chunk.Content)
			if flushed {
				emit(Event{Type: "delta", Agent: agent.Key, Name: agent.Name, Color: agent.Color, Content: chunk.Content})
			} else {
				// 先缓冲，确认不是 [PASS] 再开始推流
				pending.WriteString(chunk.Content)
				if pending.Len() >= 6 {
					if !looksLikePass(pending.String()) {
						flushed = true
						emit(Event{Type: "delta", Agent: agent.Key, Name: agent.Name, Color: agent.Color, Content: pending.String()})
						pending.Reset()
					}
				}
			}
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

	// 判定沉默
	if isPass(text) {
		emit(Event{Type: "end", Agent: agent.Key, Name: agent.Name, Color: agent.Color, Latency: latency})
		return speakResult{pass: true, latency: latency}, nil
	}

	// 清理模型带出的说话人前缀。
	// 它有时会替别人说话（"桃桃：凛酱又…"），或者在开头补自己的名字。
	text = stripSpeakerPrefix(text, sc, agent)

	asksUser := strings.Contains(text, "@用户") || strings.Contains(text, "@你")
	if asksUser {
		emit(Event{Type: "ask", Agent: agent.Key, Name: agent.Name, Color: agent.Color, Content: text})
	}

	emit(Event{
		Type: "end", Agent: agent.Key, Name: agent.Name, Color: agent.Color,
		Tokens: tokens, Latency: latency,
	})

	// 慢的话记一笔：平均和 P95 只说明整体，定位问题需要知道是谁慢
	if o.slow != nil {
		o.slow.MaybeRecord(SlowEntry{
			ConvID: conv.ID, ConvTitle: conv.Title,
			Agent: agent.Name, LatencyMS: int64(latency),
			Tokens: tokens, Model: conv.Model,
		})
	}

	if agent.Artifact != "" {
		emit(Event{
			Type: "artifact", Agent: agent.Key, Name: agent.Name,
			Title: agent.Artifact, Detail: summarize(text),
		})
	}

	return speakResult{text: text, asksUser: asksUser, tokens: tokens, latency: latency}, nil
}

// looksLikePass 前缀判定，用于流式缓冲阶段。
// 传入的是还没读完的片段，所以要判断「它可能是 [PASS] 的开头」。
func looksLikePass(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "" {
		return true
	}
	// 片段本身还是标记的开头（如 "[PA"、"PAS"）
	if strings.HasPrefix("[PASS]", u) || strings.HasPrefix("PASS", u) {
		return true
	}
	// 片段已经以标记开头（如 "[PASS] 我不懂"、"PASS 跳过"）
	return strings.HasPrefix(u, "[PASS") || strings.HasPrefix(u, "PASS")
}

// isPass 完整判定。
// 只要以 [PASS] 或 PASS 开头就算沉默——模型有时会补一句解释，
// 那也是在表达「我不参与」，不该当成发言推给用户。
func isPass(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	u := strings.ToUpper(t)
	if u == "（无）" || u == "无" {
		return true
	}
	if strings.HasPrefix(u, "[PASS]") || strings.HasPrefix(u, "PASS") {
		return true
	}
	return false
}

func forceLine(force bool) string {
	if force {
		return "\n6. 现在群里冷场了，你必须开口起个头——问一句、抛个话题、或者对刚才的事表态。"
	}
	return ""
}

// stripSpeakerPrefix 去掉行首的「某某：」。
// 只认群里真实存在的角色名，避免误伤正文里的冒号。
func stripSpeakerPrefix(text string, sc Scenario, self Agent) string {
	names := []string{self.Name}
	for _, a := range sc.Agents {
		names = append(names, a.Name)
	}
	// 循环清理：模型偶尔会连写两层（"小苏：小苏：…"）
	for i := 0; i < 3; i++ {
		trimmed := false
		for _, n := range names {
			for _, sep := range []string{"：", ":"} {
				prefix := n + sep
				if strings.HasPrefix(text, prefix) {
					text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
					trimmed = true
				}
			}
		}
		if !trimmed {
			break
		}
	}
	return text
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
