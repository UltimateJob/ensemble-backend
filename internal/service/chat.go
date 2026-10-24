package service

import (
	"context"
	"errors"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

var (
	ErrTooManyCollaborators = errors.New("协作者数量已达上限（20 人）")
	ErrQuotaExceeded        = errors.New("本月额度已用完")
	ErrConvNotFound         = errors.New("会话不存在")
	ErrForbidden            = errors.New("无权访问该会话")
)

// Chat 会话与对话用例。
type Chat struct {
	store store.Store
	gw    *gateway.Gateway
}

func NewChat(s store.Store, gw *gateway.Gateway) *Chat {
	return &Chat{store: s, gw: gw}
}

// CreateOptions 新建会话时的扩展字段。
type CreateOptions struct {
	Workspace         *domain.ConvWorkspace
	DefaultPermission string
	AgentPermissions  map[string]string
	// Tagline 一句话简介，显示在房间标题下方。
	Tagline string
}

// Create 新建会话。
func (c *Chat) Create(userID, title, studio, model string, members []string, agents []domain.ConvAgent) (*domain.Conversation, error) {
	return c.CreateWithOptions(userID, title, studio, model, members, agents, CreateOptions{})
}

// CreateWithOptions 新建会话，可绑定工作区与权限。
func (c *Chat) CreateWithOptions(userID, title, studio, model string, members []string, agents []domain.ConvAgent, opts CreateOptions) (*domain.Conversation, error) {
	if title == "" {
		title = "新群聊"
	}
	defaultPerm := strings.TrimSpace(opts.DefaultPermission)
	if defaultPerm == "" && opts.Workspace != nil {
		// 绑定本地目录的代码群默认最安全的「逐项批准」。
		// 没绑工作区时不写死默认值：内置代码角色各自的权限（架构只读、
		// 前后端自动接受编辑）才能生效，团队任务开箱可用。
		defaultPerm = "ask"
	}
	conv := &domain.Conversation{
		UserID:            userID,
		Title:             title,
		Studio:            studio,
		Model:             model,
		Members:           members,
		Agents:            agents,
		Tagline:           strings.TrimSpace(opts.Tagline),
		Workspace:         opts.Workspace,
		DefaultPermission: defaultPerm,
		AgentPermissions:  opts.AgentPermissions,
	}
	if err := c.store.CreateConv(conv); err != nil {
		return nil, err
	}
	// 场景有开场白时，由第一个角色先开口，新群不会一片空白。
	// 单聊不要这条：成员只有一个，开场白却来自另一个角色，会显得莫名其妙。
	if sc, ok := FindScenario(studio); ok && sc.Opening != "" && len(sc.Agents) > 0 &&
		len(agents) == 0 && len(members) != 1 {
		first := sc.Agents[0]
		_ = c.store.AppendMessage(&domain.Message{
			ConversationID: conv.ID,
			Role:           domain.RoleAssistant,
			Agent:          first.Name,
			Content:        sc.Opening,
		})
	}
	return conv, nil
}

// AppendUserMessage 落一条用户消息（执行入口用，保证历史里有这句话）。
func (c *Chat) AppendUserMessage(convID, content string, mentions []string) *domain.Message {
	msg := &domain.Message{
		ConversationID: convID,
		Role:           domain.RoleUser,
		Content:        content,
		Mentions:       mentions,
	}
	_ = c.store.AppendMessage(msg)
	return msg
}

// AppendSystemNote 落一条系统提示（绑定工作区、权限变更等）。
func (c *Chat) AppendSystemNote(convID, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	_ = c.store.AppendMessage(&domain.Message{
		ConversationID: convID,
		Role:           domain.RoleSystem,
		Content:        content,
		Kind:           "system",
	})
}

func (c *Chat) List(userID string) []*domain.Conversation {
	return c.store.ListConvs(userID)
}

func (c *Chat) Get(userID, convID string) (*domain.Conversation, error) {
	conv, ok := c.store.GetConv(convID)
	if !ok {
		return nil, ErrConvNotFound
	}
	if !conv.CanAccess(userID) {
		return nil, ErrForbidden
	}
	return conv, nil
}

// AddCollaborator 邀请一个用户加入会话。只有创建者能邀请。
func (c *Chat) AddCollaborator(ownerID, convID string, user *domain.User) (*domain.Conversation, error) {
	conv, ok := c.store.GetConv(convID)
	if !ok {
		return nil, ErrConvNotFound
	}
	if !conv.IsOwner(ownerID) {
		return nil, ErrForbidden
	}
	if user.ID == conv.UserID {
		return conv, nil // 创建者本来就有权限
	}
	for _, id := range conv.Collaborators {
		if id == user.ID {
			return conv, nil
		}
	}
	if len(conv.Collaborators) >= 20 {
		return nil, ErrTooManyCollaborators
	}
	conv.Collaborators = append(conv.Collaborators, user.ID)
	if err := c.store.UpdateConv(conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// RemoveCollaborator 移出协作者。创建者不能移除自己。
func (c *Chat) RemoveCollaborator(ownerID, convID, userID string) (*domain.Conversation, error) {
	conv, ok := c.store.GetConv(convID)
	if !ok {
		return nil, ErrConvNotFound
	}
	if !conv.IsOwner(ownerID) {
		return nil, ErrForbidden
	}
	if userID == conv.UserID {
		return nil, errors.New("不能移除创建者")
	}
	next := conv.Collaborators[:0]
	for _, id := range conv.Collaborators {
		if id != userID {
			next = append(next, id)
		}
	}
	conv.Collaborators = next
	if err := c.store.UpdateConv(conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// Leave 主动退出（协作者用）。
func (c *Chat) Leave(userID, convID string) error {
	conv, ok := c.store.GetConv(convID)
	if !ok {
		return ErrConvNotFound
	}
	if conv.IsOwner(userID) {
		return errors.New("创建者不能退出，只能删除群聊")
	}
	next := conv.Collaborators[:0]
	for _, id := range conv.Collaborators {
		if id != userID {
			next = append(next, id)
		}
	}
	conv.Collaborators = next
	return c.store.UpdateConv(conv)
}

// Update 保存会话改动（改名、换模型）。
func (c *Chat) Update(conv *domain.Conversation) error {
	return c.store.UpdateConv(conv)
}

func (c *Chat) Messages(userID, convID string, limit int) ([]*domain.Message, error) {
	if _, err := c.Get(userID, convID); err != nil {
		return nil, err
	}
	return c.store.ListMessages(convID, limit), nil
}

// MessagesPage 游标分页。
func (c *Chat) MessagesPage(userID, convID, beforeID string, limit int) ([]*domain.Message, bool, error) {
	if _, err := c.Get(userID, convID); err != nil {
		return nil, false, err
	}
	msgs, hasMore := c.store.ListMessagesPage(convID, beforeID, limit)
	return msgs, hasMore, nil
}

// Send 发一条消息并返回模型流。
// 额度不在这里扣——之前在发起上游调用前预扣，上游挂了也照扣；
// 现在改到 SaveAssistant 拿到完整回复并落库后才扣。
func (c *Chat) Send(ctx context.Context, user *domain.User, convID, content string) (<-chan domain.Chunk, *domain.Conversation, error) {
	conv, err := c.Get(user.ID, convID)
	if err != nil {
		return nil, nil, err
	}
	if user.Remaining() <= 0 {
		return nil, nil, ErrQuotaExceeded
	}

	// 之前是 ListMessages(convID, 20)：固定 20 条，多一条都不给。
	// 长对话里前 20 条早就是几个小时前的内容，最近的反而被挤掉。
	// 改成按字符预算取，加上早期摘要——和群聊编排走同一套规则。
	all := c.store.ListMessages(convID, 0)
	_, history := buildHistoryWith(ctx, c.store, c.gw, conv, all)
	req := domain.ChatRequest{
		Model:  conv.Model,
		Stream: true,
	}
	sys := systemPrompt(conv)
	if block := summaryBlock(conv.Summary); block != "" {
		sys += block
	}
	req.Messages = append(req.Messages, domain.ChatMessage{
		Role:    "system",
		Content: sys,
	})
	for _, m := range history {
		req.Messages = append(req.Messages, domain.ChatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	req.Messages = append(req.Messages, domain.ChatMessage{Role: "user", Content: content})

	_ = c.store.AppendMessage(&domain.Message{
		ConversationID: convID,
		Role:           domain.RoleUser,
		Content:        content,
	})

	stream, err := c.gw.Stream(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return stream, conv, nil
}

// SaveAssistant 把模型回包落库，成功后才扣额度。
// 这个方法由 HTTP 层在流结束后调用——是 service 里唯一能确认
// 「拿到了完整回复」的时机。拿不到 userID（历史签名），按会话创建者扣。
func (c *Chat) SaveAssistant(convID, model, content string, tokens int, latency int) {
	if strings.TrimSpace(content) == "" {
		// 上游什么都没吐出来：不落空消息，也不扣费
		return
	}
	_ = c.store.AppendMessage(&domain.Message{
		ConversationID: convID,
		Role:           domain.RoleAssistant,
		Agent:          "工头",
		Content:        content,
		Model:          model,
		TokensOut:      tokens,
		LatencyMS:      latency,
	})
	if conv, ok := c.store.GetConv(convID); ok {
		_ = c.store.IncrementUsed(conv.UserID, 1)
	}
}

func systemPrompt(conv *domain.Conversation) string {
	base := "你是纸团工作台里的 Agent 团队。任务要拆成可交付的步骤，每一步都落成文件。回答简洁，不寒暄。"
	switch conv.Studio {
	case "bid":
		return base + " 当前是投标应答场景：先出合规点对表，再写应答，最后核对废标风险。"
	case "code":
		return base + " 当前是研发场景：先给架构拆解，再分模块实现。"
	case "research":
		return base + " 当前是研报场景：每个数字都要能溯源。"
	}
	return base
}
