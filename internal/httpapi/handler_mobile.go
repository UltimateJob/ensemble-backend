package httpapi

import (
	"net/http"
	"strconv"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

/* ---------------- 目标看板 ---------------- */

func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.goals == nil {
		notFound(w, "未启用目标看板")
		return
	}
	ok(w, s.goals.List(u.ID))
}

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.goals == nil {
		notFound(w, "未启用目标看板")
		return
	}
	var req struct {
		Title          string   `json:"title"`
		Description    string   `json:"description"`
		Status         string   `json:"status"`
		Assignees      []string `json:"assignees"`
		RunIDs         []string `json:"runIds"`
		ConversationID string   `json:"conversationId"`
		ManualProgress *int     `json:"manualProgress"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	g := domain.Goal{
		Title: req.Title, Description: req.Description, Status: req.Status,
		Assignees: req.Assignees, RunIDs: req.RunIDs,
		ConversationID: req.ConversationID, ManualProgress: -1,
	}
	if req.ManualProgress != nil {
		g.ManualProgress = *req.ManualProgress
	}
	// 关联会话必须是自己能访问的
	if g.ConversationID != "" {
		if _, err := s.chat.Get(u.ID, g.ConversationID); err != nil {
			notFound(w, "会话不存在")
			return
		}
	}
	v, err := s.goals.Create(u.ID, g)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	created(w, v)
}

func (s *Server) handleUpdateGoal(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.goals == nil {
		notFound(w, "未启用目标看板")
		return
	}
	var req struct {
		Title          *string   `json:"title"`
		Description    *string   `json:"description"`
		Status         *string   `json:"status"`
		Assignees      *[]string `json:"assignees"`
		RunIDs         *[]string `json:"runIds"`
		ManualProgress *int      `json:"manualProgress"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	v, err := s.goals.Update(u.ID, r.PathValue("id"), service.GoalPatch{
		Title: req.Title, Description: req.Description, Status: req.Status,
		Assignees: req.Assignees, RunIDs: req.RunIDs, ManualProgress: req.ManualProgress,
	})
	if err != nil {
		if err == service.ErrGoalNotFound {
			notFound(w, err.Error())
			return
		}
		badRequest(w, err.Error())
		return
	}
	ok(w, v)
}

func (s *Server) handleDeleteGoal(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.goals == nil {
		notFound(w, "未启用目标看板")
		return
	}
	if err := s.goals.Delete(u.ID, r.PathValue("id")); err != nil {
		notFound(w, err.Error())
		return
	}
	ok(w, map[string]bool{"deleted": true})
}

/* ---------------- 连接器 ---------------- */

func (s *Server) handleListConnectors(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.conns == nil {
		notFound(w, "未启用连接器")
		return
	}
	ok(w, map[string]any{
		"items": s.conns.List(u.ID),
		"kinds": s.conns.Kinds(),
	})
}

func (s *Server) handleCreateConnector(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.conns == nil {
		notFound(w, "未启用连接器")
		return
	}
	var req struct {
		Kind    string            `json:"kind"`
		Name    string            `json:"name"`
		Scope   string            `json:"scope"`
		Config  map[string]string `json:"config"`
		Secrets map[string]string `json:"secrets"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	c, err := s.conns.Create(u.ID, domain.Connector{
		Kind: req.Kind, Name: req.Name, Scope: req.Scope,
		Config: req.Config, Secrets: req.Secrets,
	})
	if err != nil {
		if err == service.ErrConnKind {
			badRequest(w, "不支持的服务类型")
			return
		}
		badRequest(w, err.Error())
		return
	}
	created(w, c)
}

func (s *Server) handleUpdateConnector(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.conns == nil {
		notFound(w, "未启用连接器")
		return
	}
	var req struct {
		Name    *string            `json:"name"`
		Enabled *bool              `json:"enabled"`
		Scope   *string            `json:"scope"`
		Config  *map[string]string `json:"config"`
		Secrets *map[string]string `json:"secrets"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	c, err := s.conns.Update(u.ID, r.PathValue("id"), service.ConnPatch{
		Name: req.Name, Enabled: req.Enabled, Scope: req.Scope,
		Config: req.Config, Secrets: req.Secrets,
	})
	if err != nil {
		if err == service.ErrConnNotFound {
			notFound(w, err.Error())
			return
		}
		badRequest(w, err.Error())
		return
	}
	ok(w, c)
}

func (s *Server) handleDeleteConnector(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.conns == nil {
		notFound(w, "未启用连接器")
		return
	}
	if err := s.conns.Delete(u.ID, r.PathValue("id")); err != nil {
		notFound(w, err.Error())
		return
	}
	ok(w, map[string]bool{"deleted": true})
}

// handleProbeConnector 真发一次请求验证凭据。
func (s *Server) handleProbeConnector(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.conns == nil {
		notFound(w, "未启用连接器")
		return
	}
	c, err := s.conns.Probe(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		if err == service.ErrConnNotFound {
			notFound(w, err.Error())
			return
		}
		serverErr(w, err.Error())
		return
	}
	ok(w, c)
}

/* ---------------- 资料库 ---------------- */

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.library == nil {
		notFound(w, "未启用资料库")
		return
	}
	convID := r.URL.Query().Get("conversationId")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ok(w, s.library.List(u.ID, convID, limit))
}
func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.library == nil {
		notFound(w, "未启用资料库")
		return
	}
	a, err := s.library.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, err.Error())
		return
	}
	ok(w, a)
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.library == nil {
		notFound(w, "未启用资料库")
		return
	}
	if err := s.library.Delete(u.ID, r.PathValue("id")); err != nil {
		notFound(w, err.Error())
		return
	}
	ok(w, map[string]bool{"deleted": true})
}

/* ---------------- Agent 状态环 ---------------- */

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.activity == nil {
		notFound(w, "未启用状态环")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	ok(w, s.activity.ForConversation(conv.ID, service.ConversationAgents(conv)))
}
