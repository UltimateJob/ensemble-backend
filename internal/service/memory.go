package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/memory"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

const (
	minPendingMessages = 8  // 少于这个数先不调用整理模型
	maxBatchMessages   = 40 // 一次整理最多看多少条，避免 prompt 失控
)

// MemoryService 负责两件事：
//  1. 异步把原始消息整理成长期记忆；
//  2. 在角色开口前，按当前话题取回相关记忆，拼成 memory card。
//
// 它不负责生成回复，也不占用户额度。
type MemoryService struct {
	store memory.Store
	gw    *gateway.Gateway

	mu         sync.Mutex
	extracting map[string]bool
}

func NewMemoryService(st memory.Store, gw *gateway.Gateway) *MemoryService {
	return &MemoryService{store: st, gw: gw, extracting: map[string]bool{}}
}

// MaybeExtract 在后台整理尚未处理的群聊消息。
// 不阻塞回复；失败就保留游标，下一轮再试。
func (m *MemoryService) MaybeExtract(conv *domain.Conversation, user *domain.User, agents []Agent, messages []*domain.Message) {
	if m == nil || m.store == nil || m.gw == nil || conv == nil || user == nil || len(messages) == 0 {
		return
	}

	start := 0
	if covered := m.store.CoveredUpTo(conv.ID); covered != "" {
		for i, msg := range messages {
			if msg.ID == covered {
				start = i + 1
				break
			}
		}
	}
	if start >= len(messages) || len(messages)-start < minPendingMessages {
		return
	}

	batch := messages[start:]
	if len(batch) > maxBatchMessages {
		batch = batch[:maxBatchMessages]
	}
	lastID := batch[len(batch)-1].ID

	m.mu.Lock()
	if m.extracting[conv.ID] {
		m.mu.Unlock()
		return
	}
	m.extracting[conv.ID] = true
	m.mu.Unlock()

	snapshot := append([]*domain.Message(nil), batch...)
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.extracting, conv.ID)
			m.mu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if err := m.extract(ctx, conv, user, agents, snapshot); err != nil {
			logx.L().Warn("记忆整理失败，保留游标下次重试", "conv", conv.ID, "err", err.Error())
			return
		}
		if err := m.store.SetCovered(conv.ID, lastID); err != nil {
			logx.L().Warn("记忆游标写入失败", "conv", conv.ID, "err", err.Error())
		}
	}()
}

type extractResult struct {
	Conversation struct {
		Episodes []struct {
			Title       string   `json:"title"`
			Summary     string   `json:"summary"`
			Quotes      []string `json:"quotes"`
			Emotion     int      `json:"emotion"`
			OpenThreads []string `json:"openThreads"`
		} `json:"episodes"`
		Facts []string `json:"facts"`
	} `json:"conversation"`
	Agents map[string]struct {
		Relation    string   `json:"relation"`
		Memories    []string `json:"memories"`
		OpenThreads []string `json:"openThreads"`
	} `json:"agents"`
}

func (m *MemoryService) extract(ctx context.Context, conv *domain.Conversation, user *domain.User, agents []Agent, batch []*domain.Message) error {
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, a.Name)
	}

	var transcript strings.Builder
	for _, msg := range batch {
		who := "用户"
		if msg.Role == domain.RoleAssistant && msg.Agent != "" {
			who = msg.Agent
		}
		fmt.Fprintf(&transcript, "[%s] %s：%s\n", msg.CreatedAt.Format("01-02 15:04"), who, msg.Content)
	}

	sys := `你是智团的长期记忆整理器。读取一段群聊，提取对以后聊天有用的记忆。

规则：
1. 只记录用户本人，以及每个角色自己说过/做过的事。不要把其他角色的私事写进某个角色的记忆。
2. 区分：事件（episode）、用户事实/偏好（fact）、承诺/待办（promise）、没聊完的话题（open_thread）、关系变化（relation）。
3. 事件要能回答「后来怎么样了」；保留一两句关键原话，不要全部改写成书面摘要。
4. 用户明确表达情绪、处境、重要日期、决定时，优先记录。
5. 每个角色都必须写 relation：用一句话描述 TA 现在和用户的关系/态度，不要留空。
6. 不记录密码、证件号、金融账号；遇到只写「用户提到过敏感信息」，不写内容。
7. 没有值得记的就返回空数组，不要凑数；不要编造。
8. 只输出严格 JSON，不要 markdown，不要解释。`

	schema := `{
  "conversation": {
    "episodes": [
      {"title": "短标题", "summary": "这件事发生了什么、结果如何", "quotes": ["关键原话"], "emotion": -5..5, "openThreads": ["还没完成的事"]}
    ],
    "facts": ["群内稳定事实或决定，一句话"]
  },
  "agents": {
    "角色名": {
      "relation": "必填：这个角色现在和用户的关系/态度，一句话（即使没变化也写当前状态）",
      "memories": ["这个角色应该记住的、关于用户或你们之间的事"],
      "openThreads": ["这个角色答应过/还没完成的事"]
    }
  }
}`

	prompt := fmt.Sprintf("群名：%s\n角色：%s\n用户：%s\n\n聊天片段：\n%s\n\n输出 JSON（agents 的 key 必须是上面的角色名）：\n%s",
		conv.Title, strings.Join(names, "、"), user.Name, transcript.String(), schema)

	start := time.Now()
	stream, err := m.gw.Stream(ctx, domain.ChatRequest{
		Messages: []domain.ChatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: prompt},
		},
		Stream:    true,
		MaxTokens: 1400,
	})
	if err != nil {
		return err
	}
	var buf strings.Builder
	for chunk := range stream {
		if chunk.Err != nil {
			return chunk.Err
		}
		buf.WriteString(chunk.Content)
		if chunk.Done {
			break
		}
	}

	raw := strings.TrimSpace(buf.String())
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	first, last := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if first < 0 || last <= first {
		return fmt.Errorf("整理结果不是 JSON")
	}
	var res extractResult
	if err := json.Unmarshal([]byte(raw[first:last+1]), &res); err != nil {
		return fmt.Errorf("解析记忆 JSON 失败: %w", err)
	}

	sourceIDs := make([]string, 0, len(batch))
	for _, msg := range batch {
		sourceIDs = append(sourceIDs, msg.ID)
	}
	now := time.Now()
	var convMems []*domain.Memory
	for _, ep := range res.Conversation.Episodes {
		text := strings.TrimSpace(ep.Title + "：" + ep.Summary)
		if text == "" {
			continue
		}
		convMems = append(convMems, &domain.Memory{
			Kind: domain.KindEpisode, Text: text, Quotes: ep.Quotes,
			OccurredAt: lastMessageTime(batch), CreatedAt: now,
			Salience: 65 + minInt(absInt(ep.Emotion)*3, 25), Emotion: ep.Emotion,
			SourceMessageIDs: sourceIDs,
		})
		for _, thread := range ep.OpenThreads {
			if strings.TrimSpace(thread) != "" {
				convMems = append(convMems, &domain.Memory{
					Kind: domain.KindOpenThread, Text: thread,
					OccurredAt: lastMessageTime(batch), CreatedAt: now,
					Salience: 62, Emotion: ep.Emotion, SourceMessageIDs: sourceIDs,
				})
			}
		}
	}
	for _, fact := range res.Conversation.Facts {
		if strings.TrimSpace(fact) != "" {
			convMems = append(convMems, &domain.Memory{
				Kind: domain.KindFact, Text: fact,
				OccurredAt: lastMessageTime(batch), CreatedAt: now,
				Salience: 55, SourceMessageIDs: sourceIDs,
			})
		}
	}
	if len(convMems) > 0 {
		if err := m.store.AddConversation(conv.ID, convMems); err != nil {
			return err
		}
	}

	agentByName := map[string]Agent{}
	for _, a := range agents {
		agentByName[a.Name] = a
	}
	for name, info := range res.Agents {
		a, ok := agentByName[name]
		if !ok {
			continue
		}
		var mems []*domain.Memory
		if strings.TrimSpace(info.Relation) != "" {
			mems = append(mems, &domain.Memory{
				Kind: domain.KindRelation, Text: strings.TrimSpace(info.Relation),
				OccurredAt: lastMessageTime(batch), CreatedAt: now,
				Salience: 85, SourceMessageIDs: sourceIDs,
				SourceConversationID: conv.ID,
			})
		}
		for _, text := range info.Memories {
			if strings.TrimSpace(text) == "" {
				continue
			}
			mems = append(mems, &domain.Memory{
				Kind: domain.KindFact, Text: strings.TrimSpace(text),
				OccurredAt: lastMessageTime(batch), CreatedAt: now,
				Salience: 60, SourceMessageIDs: sourceIDs,
				SourceConversationID: conv.ID,
			})
		}
		for _, thread := range info.OpenThreads {
			if strings.TrimSpace(thread) == "" {
				continue
			}
			mems = append(mems, &domain.Memory{
				Kind: domain.KindOpenThread, Text: strings.TrimSpace(thread),
				OccurredAt: lastMessageTime(batch), CreatedAt: now,
				Salience: 62, SourceMessageIDs: sourceIDs,
				SourceConversationID: conv.ID,
			})
		}
		if len(mems) > 0 {
			if err := m.store.AddAgent(a.CharacterID, a.Name, user.ID, mems); err != nil {
				return err
			}
		}
	}

	logx.L().Info("记忆已整理",
		"conv", conv.ID,
		"messages", len(batch),
		"conversationMemories", len(convMems),
		"agents", len(res.Agents),
		"ms", time.Since(start).Milliseconds())
	return nil
}

// Context 生成注入给某个角色的 memory card。
// 群记忆只来自当前 conversation；角色记忆来自 (character_id, user_id)，可以跨群。
func (m *MemoryService) Context(convID, characterID, userID, query string) string {
	if m == nil || m.store == nil {
		return ""
	}
	convMems := m.store.ListConversation(convID)
	agentMems := m.store.ListAgent(characterID, userID)

	convTop := rankMemories(convMems, query, domain.ScopeConversation, 5)
	agentTop := rankMemories(agentMems, query, domain.ScopeAgentUser, 6)

	var relation *domain.Memory
	for _, mem := range agentMems {
		if mem.Kind == domain.KindRelation && (relation == nil || mem.CreatedAt.After(relation.CreatedAt)) {
			relation = mem
		}
	}

	var b strings.Builder
	if len(convTop) > 0 {
		b.WriteString("[群记忆]\n")
		for _, mem := range convTop {
			fmt.Fprintf(&b, "- %s %s：%s\n", memoryDate(mem.OccurredAt, time.Now()), kindLabel(mem.Kind), mem.Text)
		}
	}
	if len(agentTop) > 0 || relation != nil {
		b.WriteString("[你和这位用户]\n")
		for _, mem := range agentTop {
			if mem.Kind == domain.KindRelation {
				continue
			}
			fmt.Fprintf(&b, "- %s %s：%s\n", memoryDate(mem.OccurredAt, time.Now()), kindLabel(mem.Kind), mem.Text)
		}
		if relation != nil {
			fmt.Fprintf(&b, "- 你对 TA 的感觉：%s\n", relation.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	r := []rune(out)
	if len(r) > 900 {
		out = string(r[:900]) + "…"
	}
	return out
}

func memoryDate(t, now time.Time) string {
	if t.IsZero() {
		return "之前"
	}
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return "今天"
	}
	yesterday := now.AddDate(0, 0, -1)
	y3, m3, d3 := yesterday.Date()
	if y1 == y3 && m1 == m3 && d1 == d3 {
		return "昨天"
	}
	return t.Format("01-02")
}

func kindLabel(k domain.MemoryKind) string {
	switch k {
	case domain.KindEpisode:
		return "事件"
	case domain.KindFact:
		return "记得"
	case domain.KindPromise:
		return "答应过"
	case domain.KindOpenThread:
		return "没聊完"
	case domain.KindRelation:
		return "关系"
	default:
		return "记忆"
	}
}

func rankMemories(list []*domain.Memory, query string, scope domain.MemoryScope, limit int) []*domain.Memory {
	if len(list) == 0 {
		return nil
	}
	qk := keywords(query)
	now := time.Now()
	type scored struct {
		m *domain.Memory
		s float64
	}
	var items []scored
	for _, m := range list {
		if scope == domain.ScopeAgentUser && m.Kind == domain.KindRelation {
			continue
		}
		tk := keywords(m.Text + " " + strings.Join(m.Quotes, " "))
		overlap := 0
		for k := range qk {
			if tk[k] {
				overlap++
			}
		}
		kw := 0.0
		if len(qk) > 0 {
			kw = float64(overlap) / float64(len(qk)) * 3
		}
		hours := now.Sub(m.CreatedAt).Hours()
		if hours < 0 {
			hours = 0
		}
		recency := math.Exp(-hours/720) * 0.8
		emotion := math.Abs(float64(m.Emotion)) / 5 * 0.3
		s := kw + float64(m.Salience)/100*1.2 + recency + emotion
		items = append(items, scored{m: m, s: s})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].s == items[j].s {
			return items[i].m.CreatedAt.After(items[j].m.CreatedAt)
		}
		return items[i].s > items[j].s
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]*domain.Memory, 0, len(items))
	for _, it := range items {
		out = append(out, it.m)
	}
	return out
}

func keywords(s string) map[string]bool {
	out := map[string]bool{}
	r := []rune(strings.ToLower(s))
	var word strings.Builder
	flush := func() {
		if word.Len() > 1 {
			out[word.String()] = true
		}
		word.Reset()
	}
	for i, ch := range r {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			if ch < 128 {
				word.WriteRune(ch)
			} else {
				flush()
				if i+1 < len(r) && (unicode.IsLetter(r[i+1]) || unicode.IsDigit(r[i+1])) {
					out[string([]rune{ch, r[i+1]})] = true
				}
			}
		} else {
			flush()
		}
	}
	flush()
	return out
}

func lastMessageTime(msgs []*domain.Message) time.Time {
	if len(msgs) == 0 {
		return time.Now()
	}
	t := msgs[len(msgs)-1].CreatedAt
	if t.IsZero() {
		return time.Now()
	}
	return t
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Snapshot 给「他们记得」面板。
func (m *MemoryService) Snapshot(convID, userID string, agents []Agent) domain.MemorySnapshot {
	out := domain.MemorySnapshot{Conversation: []*domain.Memory{}, Agents: []domain.AgentMemories{}}
	if m == nil || m.store == nil {
		return out
	}
	if ms := m.store.ListConversation(convID); ms != nil {
		out.Conversation = ms
	}
	for _, a := range agents {
		ms := m.store.ListAgent(a.CharacterID, userID)
		if len(ms) == 0 {
			continue
		}
		item := domain.AgentMemories{CharacterID: a.CharacterID, CharacterName: a.Name, Memories: []*domain.Memory{}}
		for _, mem := range ms {
			if mem.Kind == domain.KindRelation {
				if item.Relation == nil || mem.CreatedAt.After(item.Relation.CreatedAt) {
					item.Relation = mem
				}
				continue
			}
			item.Memories = append(item.Memories, mem)
		}
		out.Agents = append(out.Agents, item)
	}
	return out
}

func (m *MemoryService) ClearConversation(convID string) int {
	if m == nil || m.store == nil {
		return 0
	}
	return m.store.ClearConversation(convID)
}

func (m *MemoryService) ClearAgent(characterID, userID string) int {
	if m == nil || m.store == nil {
		return 0
	}
	return m.store.ClearAgent(characterID, userID)
}
