package httpapi

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

func (s *Server) handleListConvs(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	ok(w, s.chat.List(u.ID))
}

func (s *Server) handleCreateConv(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	var req struct {
		Title   string             `json:"title"`
		Studio  string             `json:"studio"`
		Model   string             `json:"model"`
		Members []string           `json:"members"`
		Agents  []domain.ConvAgent `json:"agents"`
		// 创建即绑定本地工作区（代码群第一屏就要能派活）。
		Workspace         *domain.ConvWorkspace `json:"workspace"`
		DefaultPermission string                `json:"defaultPermission"`
		AgentPermissions  map[string]string     `json:"agentPermissions"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if req.Workspace != nil {
		// 本机目录只能由本机客户端选：公网请求带路径等于让我们去读服务器文件。
		if !isLoopback(r.RemoteAddr) {
			fail(w, 403, "forbidden", "只能在本机绑定本地工作区")
			return
		}
		switch strings.TrimSpace(req.Workspace.Kind) {
		case "cloud":
			fail(w, 400, "cloud_not_available", "云端工作区即将支持")
			return
		case "", "local":
		default:
			badRequest(w, "只支持本地工作区")
			return
		}
		if strings.TrimSpace(req.Workspace.Root) == "" {
			badRequest(w, "工作区路径不能为空")
			return
		}
	}
	if req.DefaultPermission != "" && !agent.ValidPermission(agent.Permission(req.DefaultPermission)) {
		badRequest(w, "默认权限不合法")
		return
	}
	for key, p := range req.AgentPermissions {
		if !agent.ValidPermission(agent.Permission(p)) {
			badRequest(w, "角色权限不合法："+key)
			return
		}
	}
	// 两个 C 端场景的角色都是开局生成的：
	//   日常陪聊 —— 随机性格的朋友（6 种性格模板）
	//   二次元茶话会 —— 从角色库随机挑（不同作品，凑一起才有话聊）
	if len(req.Agents) == 0 {
		switch req.Studio {
		case "companion":
			for _, a := range service.RandomFriends(3) {
				req.Agents = append(req.Agents, domain.ConvAgent{
					Key: a.Key, Name: a.Name, Title: a.Title,
					Emoji: a.Emoji, Color: a.Color, Prompt: a.Prompt,
				})
			}
		case "anime":
			colors := []string{"#0369A1", "#DB2777", "#7C3AED", "#047857"}
			for i, c := range service.PickRandomCharacters(3) {
				title, prompt := service.CharacterPrompt(c)
				req.Agents = append(req.Agents, domain.ConvAgent{
					Key: c.Name, CharacterID: service.CharacterKey(c.Name),
					Name: c.Name, Title: title,
					Emoji: string([]rune(c.Name)[0]), Color: colors[i%len(colors)], Prompt: prompt,
				})
			}
		}
	}

	conv, err := s.chat.CreateWithOptions(u.ID, req.Title, req.Studio, req.Model, req.Members, req.Agents, service.CreateOptions{
		Workspace:         req.Workspace,
		DefaultPermission: req.DefaultPermission,
		AgentPermissions:  req.AgentPermissions,
	})
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	if req.Workspace != nil && s.agents != nil {
		// 执行器按 convID 找工作区；只在会话里存一份路径不够。
		bw, berr := s.agents.BindWorkspaceFor(u.ID, conv.ID, req.Workspace.Root, req.Workspace.Label)
		if berr != nil {
			conv.Workspace = nil
			_ = s.chat.Update(conv)
			badRequest(w, berr.Error())
			return
		}
		label := strings.TrimSpace(req.Workspace.Label)
		if label == "" {
			label = filepath.Base(bw.Root)
		}
		conv.Workspace = &domain.ConvWorkspace{
			ID: bw.ID, Kind: "local", Root: bw.Root, Label: label, BoundAt: time.Now(),
		}
		_ = s.chat.Update(conv)
		s.chat.AppendSystemNote(conv.ID, "已绑定本地工作区："+bw.Root)
	}
	created(w, conv)
}

func (s *Server) handleGetConv(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	ok(w, conv)
}

// handleUpdateConv 改名、换默认模型，或整组替换群里的自定义角色。
func (s *Server) handleUpdateConv(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	// 改名、换模型、换角色组都是管理动作，只有创建者能改
	if !conv.IsOwner(u.ID) {
		forbidden(w)
		return
	}
	var req struct {
		Title string `json:"title"`
		Model string `json:"model"`
		// Agents 用指针区分「没带这个字段」和「带了个空数组」：
		// 没带＝不动角色；带了空数组＝把群恢复成场景预设（清空自定义角色）。
		Agents *[]domain.ConvAgent `json:"agents"`
		// 权限：带了才改。空字符串表示恢复「跟随群默认」。
		DefaultPermission *string           `json:"defaultPermission"`
		AgentPermissions  map[string]string `json:"agentPermissions"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if req.DefaultPermission != nil {
		p := strings.TrimSpace(*req.DefaultPermission)
		if p != "" && !agent.ValidPermission(agent.Permission(p)) {
			badRequest(w, "默认权限不合法")
			return
		}
		conv.DefaultPermission = p
	}
	if len(req.AgentPermissions) > 0 {
		if conv.AgentPermissions == nil {
			conv.AgentPermissions = make(map[string]string, len(req.AgentPermissions))
		}
		for key, p := range req.AgentPermissions {
			p = strings.TrimSpace(p)
			if p == "" {
				delete(conv.AgentPermissions, key)
				continue
			}
			if !agent.ValidPermission(agent.Permission(p)) {
				badRequest(w, "角色权限不合法："+key)
				return
			}
			conv.AgentPermissions[key] = p
		}
	}
	if req.Title != "" {
		if len([]rune(req.Title)) > 40 {
			badRequest(w, "群名太长（最多 40 字）")
			return
		}
		conv.Title = req.Title
	}
	if req.Model != "" {
		conv.Model = req.Model
	}
	if req.Agents != nil {
		if len(*req.Agents) > 12 {
			fail(w, 400, "too_many", "一个群最多 12 个自定义角色")
			return
		}
		conv.Agents = *req.Agents
	}
	if err := s.chat.Update(conv); err != nil {
		serverErr(w, err.Error())
		return
	}
	ok(w, conv)
}

// handleRemoveConvAgent 把一个自定义角色移出群聊（owner only）。
// 角色按 Key 匹配；连带清掉 Members 里的同名 key，
// 不然编排器还会按成员列表点到这个已经不存在的人。
func (s *Server) handleRemoveConvAgent(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if !conv.IsOwner(u.ID) {
		forbidden(w)
		return
	}
	key := r.PathValue("key")

	found := false
	agents := make([]domain.ConvAgent, 0, len(conv.Agents))
	for _, a := range conv.Agents {
		if a.Key == key {
			found = true
			continue
		}
		agents = append(agents, a)
	}
	if !found {
		notFound(w, "这个角色不在群里")
		return
	}
	conv.Agents = agents

	members := make([]string, 0, len(conv.Members))
	for _, m := range conv.Members {
		if m != key {
			members = append(members, m)
		}
	}
	conv.Members = members

	if err := s.chat.Update(conv); err != nil {
		serverErr(w, err.Error())
		return
	}
	ok(w, conv)
}

func (s *Server) handleDeleteConv(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	// 删除是全员不可逆的动作，只归创建者。协作者走 /leave。
	// 之前只要能看见这个群（含协作者）就能删，等于受邀的人可以随时解散别人的群。
	if !conv.IsOwner(u.ID) {
		fail(w, 403, "forbidden", "只有群主能删除群聊")
		return
	}
	_ = s.store.DeleteConv(conv.ID)
	if s.mem != nil {
		// 群记忆随群删除；角色记忆默认保留，由用户手动清空。
		s.mem.ClearConversation(conv.ID)
	}
	_ = s.store.AppendEvent(&domain.Event{UserID: u.ID, Kind: "conv_delete", Target: conv.ID})
	ok(w, map[string]string{"deleted": "true"})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	limit := queryInt(r, "limit", 30)
	if limit <= 0 || limit > 200 {
		limit = 30
	}

	// 带 before 参数走游标分页，否则保持旧行为（返回最近 limit 条）
	before := r.URL.Query().Get("before")
	if before != "" || r.URL.Query().Get("page") == "1" {
		msgs, hasMore, err := s.chat.MessagesPage(u.ID, r.PathValue("id"), before, limit)
		if err != nil {
			notFound(w, "会话不存在")
			return
		}
		ok(w, map[string]interface{}{
			"messages": msgs,
			"hasMore":  hasMore,
		})
		return
	}

	msgs, err := s.chat.Messages(u.ID, r.PathValue("id"), limit)
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	ok(w, msgs)
}

// handleSendMessage 建立 SSE 流，把模型输出逐段推给客户端。
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	u, okUser := UserFrom(r.Context())
	if !okUser {
		unauthorized(w, "未登录")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	// 校验与 orchestrate 对齐：只判 == "" 挡不住纯空格，
	// 不设上限的话一条超长消息会吃掉几万 token。
	content := strings.TrimSpace(req.Content)
	if content == "" {
		badRequest(w, "消息不能为空")
		return
	}
	const maxRunes = 4000
	if n := len([]rune(content)); n > maxRunes {
		fail(w, 400, "too_long",
			fmt.Sprintf("消息太长了（%d 字），最多 %d 字。拆成几条发更好", n, maxRunes))
		return
	}
	req.Content = content

	convID := r.PathValue("id")
	stream, conv, err := s.chat.Send(r.Context(), u, convID, req.Content)
	if err != nil {
		switch err {
		case service.ErrQuotaExceeded:
			fail(w, 402, "quota_exceeded", err.Error())
		case service.ErrConvNotFound:
			notFound(w, "会话不存在")
		case service.ErrForbidden:
			forbidden(w)
		default:
			serverErr(w, err.Error())
		}
		return
	}

	sw := startSSE(w)
	defer sw.stop()

	start := time.Now()
	var full string
	var tokens int
	// 客户端断开后不再发送，但流要读完、消息要照常落库：
	// 模型已经完整生成的内容不能因为对端关了页面就丢掉。
	clientGone := false

	for chunk := range stream {
		if chunk.Err != nil {
			if !clientGone {
				sw.writeData(map[string]interface{}{"error": chunk.Err.Error(), "done": true})
			}
			break
		}
		if chunk.Content != "" {
			full += chunk.Content
			if !clientGone && !sw.writeData(map[string]interface{}{"delta": chunk.Content}) {
				clientGone = true
			}
		}
		if chunk.Usage != nil {
			tokens = chunk.Usage.CompletionTokens
		}
		if chunk.Done {
			break
		}
	}

	latency := int(time.Since(start).Milliseconds())
	s.chat.SaveAssistant(convID, conv.Model, full, tokens, latency)
	_ = s.store.AppendEvent(&domain.Event{
		UserID: u.ID, Kind: "chat", Target: convID,
		Meta: map[string]string{"tokens": fmt.Sprint(tokens), "ms": fmt.Sprint(latency)},
	})

	if !clientGone {
		sw.writeData(map[string]interface{}{
			"done":    true,
			"tokens":  tokens,
			"latency": latency,
		})
	}
}
