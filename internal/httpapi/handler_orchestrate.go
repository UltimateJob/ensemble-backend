package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleOrchestrate 多 Agent 群聊：SSE 推送每个角色的发言。
func (s *Server) handleOrchestrate(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
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

	// 只发空格或换行等于没说话。
	// 之前只判了 == ""，一串空格能进去，编排器还会认真回一句。
	content := strings.TrimSpace(req.Content)
	if content == "" {
		badRequest(w, "消息不能为空")
		return
	}

	// 长度上限。群聊里没人粘贴十万字——真粘贴了，
	// 一条消息就会吃掉几万 token，而且模型也接不住。
	// 4000 字大约是一篇长文，够用了。
	const maxRunes = 4000
	if n := len([]rune(content)); n > maxRunes {
		fail(w, 400, "too_long",
			fmt.Sprintf("消息太长了（%d 字），最多 %d 字。拆成几条发更好", n, maxRunes))
		return
	}
	req.Content = content

	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}

	// 心跳：等第一个角色开口期间连接可能长时间无字节，
	// 有空闲超时的代理会把连接掐掉，见 sseWriter 的说明。
	sw := startSSE(w)
	defer sw.stop()

	s.orchestrator.Run(r.Context(), conv, u, req.Content, func(ev service.Event) {
		sw.writeData(ev)
	})

	_ = s.store.AppendEvent(&domain.Event{
		UserID: u.ID, Kind: "chat", Target: conv.ID,
		Meta: map[string]string{"studio": conv.Studio},
	})
}

// handleKickoff 让群里的角色自己聊起来（没有用户消息也能触发）。
func (s *Server) handleKickoff(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	var req struct {
		Topic string `json:"topic"`
	}
	_ = decode(r, &req)

	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	// 和 orchestrate 一样在写 SSE 头之前挡住没额度的人，
	// 否则头已经写出去了，只能再往流里塞一个 error 事件。
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}

	sw := startSSE(w)
	defer sw.stop()

	s.orchestrator.Kickoff(r.Context(), conv, u, req.Topic, func(ev service.Event) {
		sw.writeData(ev)
	})
}

// handleDraftAgent 用模型生成一个角色设定。
func (s *Server) handleDraftAgent(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	// AI 生成走的是真上游，也是成本。没额度的人和发消息入口一样挡在门外。
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}
	var req struct {
		Name     string `json:"name"`
		Keywords string `json:"keywords"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	// 入参直接拼进提示词，不设上限的话一段几万字的输入
	// 会原样吃掉等量 token。
	if n := len([]rune(req.Name)); n > 50 {
		fail(w, 400, "too_long", fmt.Sprintf("名字太长（%d 字），最多 50 字", n))
		return
	}
	if n := len([]rune(req.Keywords)); n > 200 {
		fail(w, 400, "too_long", fmt.Sprintf("关键词太长（%d 字），最多 200 字", n))
		return
	}
	agent, err := s.drafter.Draft(r.Context(), req.Name, req.Keywords)
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	// Agent.Prompt 在场景列表里不对外，这里单独返回
	ok(w, map[string]string{
		"key":    agent.Key,
		"name":   agent.Name,
		"title":  agent.Title,
		"emoji":  agent.Emoji,
		"color":  agent.Color,
		"prompt": agent.Prompt,
	})
}

// handlePolishAgent 检索并润色已有角色/公众人物。
func (s *Server) handlePolishAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Kind   string `json:"kind"`
		Note   string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		badRequest(w, "请先填写角色名")
		return
	}
	res, err := s.drafter.Polish(r.Context(), req.Name, req.Source, req.Kind, req.Note)
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	ok(w, res)
}

// handleScenarios 列出所有内置场景。
func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	ok(w, service.Scenarios)
}
