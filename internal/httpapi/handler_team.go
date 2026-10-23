package httpapi

import (
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleStartTeam 启动一次团队任务。
//
// 与 /execute 的区别：execute 是「@ 谁，谁干一件具体的事」；
// team 是「把这个目标交给团队」，由后端规划分工、并行执行、验证、返工。
func (s *Server) handleStartTeam(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.teams == nil || s.agents == nil {
		serverErr(w, "团队任务未启用")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if !conv.IsOwner(u.ID) {
		fail(w, 403, "owner_only", "只有群主可以启动团队任务")
		return
	}
	if !service.WorkspaceBound(conv) {
		fail(w, 409, "workspace_required", "先为这个群选择本地工作区")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}
	var req struct {
		Task     string `json:"task"`
		MaxSteps int    `json:"maxSteps"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Task = strings.TrimSpace(req.Task)
	if req.Task == "" {
		badRequest(w, "任务不能为空")
		return
	}
	if n := len([]rune(req.Task)); n > 6000 {
		fail(w, 400, "too_long", "任务描述太长了，最多 6000 字")
		return
	}

	agents := service.ConversationAgents(conv)
	teamAgents := make([]agent.TeamAgent, 0, len(agents))
	for i := range agents {
		a := agents[i]
		perm := service.ResolveAgentPermission(conv, a)
		p := agent.Permission(strings.TrimSpace(perm))
		if p == "" || !agent.ValidPermission(p) {
			continue
		}
		teamAgents = append(teamAgents, agent.TeamAgent{
			Key:        a.Key,
			Name:       a.Name,
			Prompt:     a.Prompt,
			Permission: p,
			Scope:      a.Scope,
		})
	}
	if len(teamAgents) < 2 {
		fail(w, 400, "team_required", "团队任务至少需要两个角色；单聊请直接发消息")
		return
	}
	// 团队流水线会在无人值守的情况下连续写文件、跑验证。
	// 「逐项批准」会让第一个写操作就停住等审批，整条流水线失去意义。
	// 只读/审查角色不受影响——它们不承担执行。
	if err := agent.ValidateTeamAgents(teamAgents); err != nil {
		fail(w, 409, "team_permission_required", err.Error())
		return
	}

	// 用户原话落一条群消息；团队内部各阶段的提示词不再写入聊天。
	msg := s.chat.AppendUserMessage(conv.ID, req.Task, nil)
	team, err := s.teams.StartTeam(r.Context(), s.agents, s.gw, agent.TeamRequest{
		ConversationID:    conv.ID,
		UserID:            u.ID,
		Title:             conv.Title,
		Task:              req.Task,
		MessageID:         msg.ID,
		Model:             conv.Model,
		MaxSteps:          req.MaxSteps,
		Agents:            teamAgents,
		DefaultPermission: conv.DefaultPermission,
	})
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	created(w, map[string]any{"team": team, "message": msg})
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	t, ok2 := s.teams.Get(r.PathValue("id"), u.ID)
	if !ok2 {
		notFound(w, "团队任务不存在")
		return
	}
	ok(w, t)
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if _, err := s.chat.Get(u.ID, r.PathValue("id")); err != nil {
		notFound(w, "会话不存在")
		return
	}
	ok(w, s.teams.List(r.PathValue("id"), u.ID, queryInt(r, "limit", 20)))
}

func (s *Server) handleCancelTeam(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if err := s.teams.Cancel(r.PathValue("id"), u.ID, s.agents); err != nil {
		notFound(w, err.Error())
		return
	}
	ok(w, map[string]bool{"cancelled": true})
}

// handleTeamEvents 推送团队事件（历史重放 + 实时），团队结束后关闭。
func (s *Server) handleTeamEvents(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	after := atoiDefault(r.URL.Query().Get("after"), 0)
	history, live, cancel, ok2 := s.teams.Attach(r.PathValue("id"), u.ID, after)
	if !ok2 {
		notFound(w, "团队任务不存在")
		return
	}
	defer cancel()

	sw := startSSE(w)
	defer sw.stop()

	for _, ev := range history {
		if !sw.writeData(ev) {
			return
		}
		if ev.Type == agent.TeamEventEnd {
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, more := <-live:
			if !more {
				return
			}
			if !sw.writeData(ev) {
				return
			}
			if ev.Type == agent.TeamEventEnd {
				return
			}
		}
	}
}
