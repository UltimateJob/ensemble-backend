package httpapi

import (
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// handleListAgents 我的角色库。
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	ok(w, s.store.ListAgents(u.ID))
}

// handleCreateAgent 把角色存进库。
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	var req domain.SavedAgent
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		badRequest(w, "角色名字必填")
		return
	}
	if len([]rune(req.Prompt)) < 10 {
		badRequest(w, "人设太短，至少写一句能指导对话的话")
		return
	}
	if n := len(s.store.ListAgents(u.ID)); n >= 50 {
		fail(w, 400, "too_many", "角色库已满（50 个）")
		return
	}

	req.UserID = u.ID
	req.Emoji = firstRune(req.Emoji, req.Name)
	if req.Source == "" {
		req.Source = "manual"
	}
	if err := s.store.CreateAgent(&req); err != nil {
		serverErr(w, err.Error())
		return
	}
	created(w, req)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	cur, ok2 := s.store.GetAgent(r.PathValue("id"))
	if !ok2 || cur.UserID != u.ID {
		notFound(w, "角色不存在")
		return
	}
	var req struct {
		Name   string `json:"name"`
		Title  string `json:"title"`
		Prompt string `json:"prompt"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if req.Name != "" {
		cur.Name = req.Name
	}
	if req.Title != "" {
		cur.Title = req.Title
	}
	if req.Prompt != "" {
		cur.Prompt = req.Prompt
	}
	if err := s.store.UpdateAgent(cur); err != nil {
		serverErr(w, err.Error())
		return
	}
	ok(w, cur)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	if err := s.store.DeleteAgent(u.ID, r.PathValue("id")); err != nil {
		notFound(w, "角色不存在")
		return
	}
	ok(w, map[string]string{"deleted": "true"})
}

// handleAgentToConversation 把库里的角色拉进某个群（复制成快照）。
func (s *Server) handleAgentToConversation(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	agent, ok2 := s.store.GetAgent(r.PathValue("id"))
	if !ok2 || agent.UserID != u.ID {
		notFound(w, "角色不存在")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("convId"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if len(conv.Agents) >= 12 {
		fail(w, 400, "too_many", "这个群的自定义角色已满（12 个）")
		return
	}
	for _, a := range conv.Agents {
		if a.Name == agent.Name {
			fail(w, 409, "duplicate", "群里已经有同名角色了")
			return
		}
	}
	conv.Agents = append(conv.Agents, domain.ConvAgent{
		Key: "lib_" + agent.ID, Name: agent.Name, Title: agent.Title,
		Emoji: agent.Emoji, Color: agent.Color, Prompt: agent.Prompt,
	})
	if err := s.chat.Update(conv); err != nil {
		serverErr(w, err.Error())
		return
	}
	ok(w, conv)
}

func firstRune(preferred, fallback string) string {
	if r := []rune(preferred); len(r) > 0 {
		return string(r[0])
	}
	if r := []rune(fallback); len(r) > 0 {
		return string(r[0])
	}
	return "?"
}
