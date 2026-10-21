package service

import (
	"context"
	"errors"

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

// Create 新建会话。
func (c *Chat) Create(userID, title, studio, model string, members []string, agents []domain.ConvAgent) (*domain.Conversation, error) {
	if title == "" {
		title = "新群聊"
	}
	conv := &domain.Conversation{
		UserID:  userID,
		Title:   title,
		Studio:  studio,
		Model:   model,
		Members: members,
		Agents:  agents,
	}
	if err := c.store.CreateConv(conv); err != nil {
		return nil, err
	}
	// 场景有开场白时，由第一个角色先开口，新群不会一片空白
	if sc, ok := FindScenario(studio); ok && sc.Opening != "" && len(sc.Agents) > 0 && len(agents) == 0 {
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

// Send 发一条消息并返回模型流。同时预扣额度。
func (c *Chat) Send(ctx context.Context, user *domain.User, convID, content string) (<-chan domain.Chunk, *domain.Conversation, error) {
	conv, err := c.Get(user.ID, convID)
	if err != nil {
		return nil, nil, err
	}
	if user.Remaining() <= 0 {
		return nil, nil, ErrQuotaExceeded
	}

	history := c.store.ListMessages(convID, 20)
	req := domain.ChatRequest{
		Model:  conv.Model,
		Stream: true,
	}
	req.Messages = append(req.Messages, domain.ChatMessage{
		Role:    "system",
		Content: systemPrompt(conv),
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

	user.Used++
	_ = c.store.UpdateUser(user)

	stream, err := c.gw.Stream(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return stream, conv, nil
}

// SaveAssistant 把模型回包落库。
func (c *Chat) SaveAssistant(convID, model, content string, tokens int, latency int) {
	_ = c.store.AppendMessage(&domain.Message{
		ConversationID: convID,
		Role:           domain.RoleAssistant,
		Agent:          "工头",
		Content:        content,
		Model:          model,
		TokensOut:      tokens,
		LatencyMS:      latency,
	})
}

func systemPrompt(conv *domain.Conversation) string {
	base := "你是合奏工作台里的 Agent 团队。任务要拆成可交付的步骤，每一步都落成文件。回答简洁，不寒暄。"
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
