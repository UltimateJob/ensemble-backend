package service

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
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
	mem   *MemoryService
}

// NewOrchestrator 创建编排器。mem 可选，不传时行为与之前一致（无长期记忆）。
func NewOrchestrator(s store.Store, gw *gateway.Gateway, slow *SlowLog, mem ...*MemoryService) *Orchestrator {
	o := &Orchestrator{store: s, gw: gw, locks: newConvLocks(), slow: slow}
	if len(mem) > 0 {
		o.mem = mem[0]
	}
	return o
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
	o.loop(ctx, conv, user, emit, fmt.Sprintf("用户刚说：%s", userText), false, wantsEveryone(userText))
}

// Kickoff 没有用户消息时，让角色之间自己聊起来。
func (o *Orchestrator) Kickoff(ctx context.Context, conv *domain.Conversation, user *domain.User, topic string, emit func(Event)) {
	prompt := "群里现在没人说话，安静了一会儿。"
	if topic != "" {
		prompt = "用户刚才提到：" + topic + "，但还没人回应。"
	}
	// 没有人先开口时，第一个人必须说话，否则整轮都是沉默
	o.loop(ctx, conv, user, emit, prompt, true, false)
}

// agentsFromConversation 把会话里的自定义角色转成编排器使用的角色。
//
// 角色库回来的角色可能没有 key（早期客户端只传了名字和设定），
// 而 key 是选人时去重的依据：全是空字符串会让所有人被当成同一个人，
// 一轮只能开口一次，甚至完全不说话。这里给缺失/重复的 key 补一个兜底。
func agentsFromConversation(conv *domain.Conversation) []Agent {
	if len(conv.Agents) == 0 {
		return nil
	}
	out := make([]Agent, 0, len(conv.Agents))
	used := make(map[string]bool, len(conv.Agents))
	for i, a := range conv.Agents {
		key := strings.TrimSpace(a.Key)
		if key == "" || used[key] {
			key = fmt.Sprintf("agent_%d", i+1)
		}
		for used[key] {
			key += "_"
		}
		used[key] = true
		cid := strings.TrimSpace(a.CharacterID)
		if cid == "" {
			switch {
			case strings.HasPrefix(key, "lib_"):
				cid = "lib:" + strings.TrimPrefix(key, "lib_")
			case strings.HasPrefix(key, "char_"):
				cid = key
			default:
				cid = CharacterKey(a.Name)
			}
		}
		out = append(out, Agent{
			Key: key, CharacterID: cid, Name: a.Name, Title: a.Title,
			Emoji: a.Emoji, Color: a.Color, Prompt: a.Prompt,
		})
	}
	return out
}

// ResolveAgents 返回会话最终使用的场景与角色（含稳定的 CharacterID）。
// 内置场景用「场景:角色」作为身份；自定义角色用库 ID / 名字 hash。
func ResolveAgents(conv *domain.Conversation) (Scenario, []Agent) {
	sc, ok := FindScenario(conv.Studio)
	if !ok {
		sc = Scenario{Key: "custom", Name: conv.Title, Opening: "", Round: 3}
	}
	agents := sc.Agents
	if custom := agentsFromConversation(conv); len(custom) > 0 {
		agents = custom
	}
	out := make([]Agent, len(agents))
	copy(out, agents)
	for i := range out {
		if out[i].CharacterID != "" {
			continue
		}
		if sc.Key != "custom" && out[i].Key != "" {
			out[i].CharacterID = sc.Key + ":" + out[i].Key
		} else {
			out[i].CharacterID = CharacterKey(out[i].Name)
		}
	}
	return sc, out
}

// ConversationAgents 给 HTTP 层用：拿到当前会话的角色与跨群身份。
func ConversationAgents(conv *domain.Conversation) []Agent {
	_, agents := ResolveAgents(conv)
	return agents
}

// trimHistory 按字符预算保留最近的消息。固定 16 条会丢掉太多上下文；
// 但也不能无限塞，按预算裁剪更稳。
func trimHistory(msgs []*domain.Message, maxRunes int) []*domain.Message {
	if len(msgs) == 0 {
		return nil
	}
	total, start := 0, len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		n := len([]rune(msgs[i].Content)) + 24
		if start < len(msgs) && total+n > maxRunes {
			break
		}
		total += n
		start = i
	}
	if len(msgs)-start > 200 {
		start = len(msgs) - 200
	}
	return msgs[start:]
}

func humanGap(d time.Duration) string {
	if d < 2*time.Minute {
		return ""
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d 小时", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 天"
	}
	return fmt.Sprintf("%d 天", days)
}

func worldLine(world string) string {
	if strings.TrimSpace(world) == "" {
		return ""
	}
	return "[世界设定]\n" + strings.TrimSpace(world)
}

func memoryCardLine(card, timeLine string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(card) != "" {
		parts = append(parts, "[记得的事]\n"+card)
	} else {
		parts = append(parts, "[记得的事]\n暂无长期记忆。不要假装记得用户以前说过的事；需要就请对方提醒。")
	}
	if strings.TrimSpace(timeLine) != "" {
		parts = append(parts, timeLine)
	}
	return strings.Join(parts, "\n")
}

// loop 核心循环：最多让 N 个人开口，其余可以沉默。
// 整轮持有会话锁，避免两轮编排交错。
func (o *Orchestrator) loop(ctx context.Context, conv *domain.Conversation, user *domain.User, emit func(Event), anchor string, forceFirst, addressAll bool) {
	if !o.locks.TryLock(conv.ID) {
		// 有人在说：这一轮直接跳过，不产生交错的发言
		emit(Event{Type: "busy"})
		emit(Event{Type: "done"})
		return
	}
	defer o.locks.Unlock(conv.ID)

	sc, agents := ResolveAgents(conv)
	if sc.Round <= 0 || len(agents) < sc.Round {
		sc.Round = len(agents)
	}
	if len(agents) == 0 {
		emit(Event{Type: "done"})
		return
	}

	// 闲聊场景（二次元 / 日常陪聊 / 自定义群）允许寒暄和关心；
	// 工作场景保持先定目标、再给结论。
	casual := sc.Category == "fun" || sc.Key == "custom"
	// ask 需要知道完整成员列表，否则自定义群会把「群里还有」拼成空字符串，
	// 角色不知道自己在跟谁聊。
	promptSc := sc
	promptSc.Agents = agents

	allMessages := o.store.ListMessages(conv.ID, 0)
	history := trimHistory(allMessages, 6000)
	now := time.Now()
	gap := ""
	if len(history) >= 2 {
		gap = humanGap(now.Sub(history[len(history)-2].CreatedAt))
	}
	timeLine := "现在是 " + now.Format("2006-01-02 15:04") + "。"
	if gap != "" {
		timeLine += "距离上一条消息 " + gap + "。"
	}

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
	// 用户明确说「每个人/所有人」时，别只让五个人代表全船。
	if wantsAll(anchor) {
		maxSpeakers = len(agents)
	}

	spoken := map[string]bool{}
	speakers := 0
	lastKey := ""
	// 用户点名谁，谁先答。一句话里出现两个名字时，按出现顺序排，
	// 比如「布鲁克，你怎么看罗宾？」应该布鲁克先答，而不是罗宾先答。
	addressedOrder := addressedAgentsOrdered(agents, anchor)
	// 「你们觉得路飞怎么样」是问大家，不是点名路飞；群问不抢答。
	if addressAll {
		addressedOrder = nil
	}
	addressed := make(map[string]bool, len(addressedOrder))
	for _, a := range addressedOrder {
		addressed[a.Key] = true
	}

	// 最多尝试 2 倍于发言人数的次数，允许一部分人沉默
	for attempt := 0; attempt < len(agents)*2 && speakers < maxSpeakers; attempt++ {
		select {
		case <-ctx.Done():
			emit(Event{Type: "done"})
			return
		default:
		}

		agent := pickSpeaker(agents, spoken, lastKey, speakers == 0 && !casual)
		if len(addressedOrder) > 0 {
			for _, a := range addressedOrder {
				if !spoken[a.Key] {
					agent = a
					break
				}
			}
		}
		if agent == nil || agent.Key == "" {
			break
		}

		force := (forceFirst && speakers == 0) || (len(addressed) > 0 && addressed[agent.Key] && !spoken[agent.Key])
		memoryCard := ""
		if o.mem != nil {
			memoryCard = o.mem.Context(conv.ID, agent.CharacterID, user.ID, anchor)
		}
		res, err := o.ask(ctx, conv, promptSc, *agent, history, anchor, emit, force, addressAll, casual, memoryCard, timeLine)
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

		_ = o.store.IncrementUsed(user.ID, 1)

		if res.asksUser {
			break
		}
	}

	_ = o.store.UpdateConv(conv)
	emit(Event{Type: "done"})

	// 异步整理长期记忆；不阻塞本轮，也不消耗用户额度。
	if o.mem != nil {
		o.mem.MaybeExtract(conv, user, agents, o.store.ListMessages(conv.ID, 0))
	}
}

// addressedAgentsOrdered 找出用户在句子里直接点名的角色，按出现位置排序。
// 只用于决定谁先答、谁必须答，不改变角色自己的人设。
func addressedAgentsOrdered(agents []Agent, text string) []*Agent {
	type ref struct {
		agent *Agent
		pos   int
	}
	refs := make([]ref, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		names := append([]string{a.Name}, a.Aliases...)
		for _, part := range strings.Split(a.Name, "·") {
			if len([]rune(part)) >= 2 {
				names = append(names, part)
			}
		}
		pos := -1
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if idx := strings.Index(text, n); idx >= 0 && (pos < 0 || idx < pos) {
				pos = idx
			}
		}
		if pos >= 0 {
			refs = append(refs, ref{agent: a, pos: pos})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].pos < refs[j].pos })
	out := make([]*Agent, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.agent)
	}
	return out
}

func addressedAgents(agents []Agent, text string) map[string]bool {
	out := map[string]bool{}
	for _, a := range addressedAgentsOrdered(agents, text) {
		out[a.Key] = true
	}
	return out
}

// pickSpeaker 选择下一个发言者。
//
// 工作场景第一轮固定给负责人（agents[0]）：先由拆解角色接住、定个调子。
// 闲聊/自定义群不这样——否则第一个人永远定调，整场像她的个人发布会。
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
func (o *Orchestrator) ask(ctx context.Context, conv *domain.Conversation, sc Scenario, agent Agent, history []*domain.Message, anchor string, emit func(Event), force, addressAll, casual bool, memoryCard, timeLine string) (speakResult, error) {
	emit(Event{Type: "start", Agent: agent.Key, Name: agent.Name, Color: agent.Color})

	mode := "这是工作群：先确认目标，再给结论；必要时问一句。"
	if casual {
		mode = "这是朋友之间的闲聊，不是评审会：可以寒暄、开玩笑、关心对方；先接住对方的话和情绪，再表达自己的看法。"
	}

	sys := fmt.Sprintf(`%s

你正在一个群聊里，群名「%s」，你的名字是「%s」，身份是「%s」。
群里还有：%s。

%s
%s
%s
这是真实群聊，不是客服，也不是评审会。规则：
1. 先接住对方：如果对方在讲自己的处境或情绪，先用一句回应（理解、关心或追问），再表达看法。不要只评判对错。
2. 结合前面的聊天和群成员，不要每次从零开始；相关的事可以带一句。
3. 如果话题跟你无关、或者别人已经说清楚了，可以只回复 [PASS]。但如果用户在直接问你们（「你们」「分别」「每个人」），或者轮到你时还没人接住用户，不要 PASS，给出你自己的看法。
4. 想说话就直接说，不要写自己的名字，也不要写旁白。
5. 如果缺关键信息、必须问用户才能继续，就直接问，用「@用户」开头。
6. 一句话到三句话，像真人在群里打字。不要用 markdown、不要列点、不要客服腔。
7. 不要复述别人刚说过的话，也不要只点评上一位；接着话题往下说，或者直接回应用户。
8. 如果上面有「记得的事」，自然接一句，不要逐条背；群里有其他人时，不要主动说出用户的私事，用户自己提起时可以接。
9. 记忆卡里没有的事就是不知道。不要假装记得，也不要编造；需要就请用户提醒。
10. 历史行首的「[时间] 角色名：」只是记录格式，不是你要模仿的内容；回复时不要带时间和自己的名字。
11. 引用记忆时用记忆卡上的日期；不确定就说「之前」，不要自己编「昨天/上周」。
12. 如果「距离上一条消息」超过一天，先自然承认时间过去了（比如「好久没聊了」），不要假装昨天刚聊过；但不要夸张。
13. 只用自己的口癖、句长和称呼；不要模仿其他角色的口头禅。不确定的人名、地名、事件不要提，更不要编；听来的事不要说得像亲眼见过。
%s`,
		agent.Prompt, sc.Name, agent.Name, agent.Title, otherAgents(sc, agent.Key),
		worldLine(sc.World), mode, memoryCardLine(memoryCard, timeLine), forceLine(force))

	msgs := []domain.ChatMessage{{Role: "system", Content: sys}}
	for _, m := range history {
		who := "用户"
		if m.Role == domain.RoleAssistant && m.Agent != "" {
			who = m.Agent
		}
		stamp := ""
		if !m.CreatedAt.IsZero() {
			stamp = "[" + m.CreatedAt.Format("01-02 15:04") + "] "
		}
		msgs = append(msgs, domain.ChatMessage{
			Role:    string(m.Role),
			Content: fmt.Sprintf("%s%s：%s", stamp, who, m.Content),
		})
	}
	instruction := "现在轮到你判断：要不要说话？说就直说，不说就回 [PASS]。"
	if force {
		instruction = "群里冷场了，必须由你起个头。直接说一句话把话头挑起来，不要说 [PASS]。"
	} else if addressAll {
		instruction = "用户在直接问你们每一个人。请给出你自己的看法，可以很短，但不要回 [PASS]。"
	}
	msgs = append(msgs, domain.ChatMessage{Role: "user", Content: anchor + "\n\n" + instruction})

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
				// 先缓冲开头：既要确认不是 [PASS]，也要等够长度把
				// 「[10-21 21:58] 角色名：」这类历史格式前缀剥掉。
				// 只等 2 个字符就推流的话，用户会先看到时间和名字。
				pending.WriteString(chunk.Content)
				if pending.Len() >= 96 {
					if !looksLikePass(pending.String()) {
						flushed = true
						out := stripSpeakerPrefix(strings.TrimSpace(pending.String()), sc, agent)
						emit(Event{Type: "delta", Agent: agent.Key, Name: agent.Name, Color: agent.Color, Content: out})
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

	// 太短的回复可能还没触发首块推流（不足 24 字节），收尾时补一次。
	if !flushed && text != "" {
		emit(Event{Type: "delta", Agent: agent.Key, Name: agent.Name, Color: agent.Color, Content: text})
	}

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

// wantsEveryone 判断用户是不是在问「你们每一个人」。
// 只做很轻的启发式：明确要意见，或者同时点名群体 + 提问。
func wantsAll(text string) bool {
	for _, w := range []string{"每个人", "所有人", "全员", "一个一个", "各自", "每位"} {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func wantsEveryone(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	group := strings.Contains(t, "你们") || strings.Contains(t, "大家") || strings.Contains(t, "各位") ||
		strings.Contains(t, "每个人") || strings.Contains(t, "三个") || strings.Contains(t, "几个")
	ask := strings.ContainsAny(t, "？?") || strings.Contains(t, "吗") || strings.Contains(t, "呢")
	opinion := strings.Contains(t, "分别") || strings.Contains(t, "各自") || strings.Contains(t, "怎么看") ||
		strings.Contains(t, "怎么想") || strings.Contains(t, "意见") || strings.Contains(t, "说说") ||
		strings.Contains(t, "聊聊")
	return opinion || (group && ask)
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
		return "\n\n现在群里冷场了，你必须开口起个头——问一句、抛个话题、或者对刚才的事表态。"
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
	for i := 0; i < 4; i++ {
		trimmed := false
		// 历史格式：[10-21 21:58] 角色名：正文
		if strings.HasPrefix(text, "[") {
			if idx := strings.Index(text, "]"); idx > 0 && idx < 24 {
				inner := text[1:idx]
				if strings.ContainsAny(inner, "0123456789") && (strings.Contains(inner, ":") || strings.Contains(inner, "-")) {
					text = strings.TrimSpace(text[idx+1:])
					trimmed = true
				}
			}
		}
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
