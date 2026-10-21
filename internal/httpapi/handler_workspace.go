package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
	"github.com/UltimateJob/ensemble-backend/internal/workspace"
)

// SetAgentManager 注入工作层。未注入时工作区相关路由不注册。
func (s *Server) SetAgentManager(m *agent.Manager) { s.agents = m }

// handleListWorkspaces 列出当前用户绑定的工作区。
func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	ok(w, s.agents.ListWorkspaces(u.ID))
}

// handleBindWorkspace 绑定一个本机目录。
//
// 浏览器模式拿不到原生目录选择器，所以这里收一个绝对路径；
// 只允许本机（localhost）调用，避免把服务器路径暴露给公网用户。
func (s *Server) handleBindWorkspace(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if !isLoopback(r.RemoteAddr) {
		forbidden(w)
		return
	}
	var req struct {
		Root           string `json:"root"`
		ConversationID string `json:"conversationId"`
		Label          string `json:"label"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Root) == "" {
		badRequest(w, "root 不能为空")
		return
	}
	if req.ConversationID != "" {
		if _, err := s.chat.Get(u.ID, req.ConversationID); err != nil {
			notFound(w, "会话不存在")
			return
		}
	}
	bw, err := s.agents.BindWorkspace(u.ID, req.ConversationID, req.Root, req.Label)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	created(w, bw)
}

// handleUnbindWorkspace 解绑。
func (s *Server) handleUnbindWorkspace(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if !s.agents.UnbindWorkspace(r.PathValue("id"), u.ID) {
		notFound(w, "工作区不存在或正在运行")
		return
	}
	ok(w, map[string]bool{"removed": true})
}

// handleWorkspaceTree 列出目录（一层）。
func (s *Server) handleWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	bw, okw := s.agents.Workspace(r.PathValue("id"), u.ID)
	if !okw {
		notFound(w, "工作区不存在")
		return
	}
	items, err := bw.FS().List(r.URL.Query().Get("path"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	ok(w, map[string]any{"root": bw.Root, "path": r.URL.Query().Get("path"), "entries": items})
}

// handleWorkspaceFile 读取文件内容（按行分页）。
func (s *Server) handleWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	bw, okw := s.agents.Workspace(r.PathValue("id"), u.ID)
	if !okw {
		notFound(w, "工作区不存在")
		return
	}
	q := r.URL.Query()
	chunk, err := bw.FS().Read(q.Get("path"), atoiDefault(q.Get("offset"), 0), atoiDefault(q.Get("limit"), 0))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	ok(w, chunk)
}

// handleStartRun 启动一次任务。
func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	bw, okw := s.agents.Workspace(r.PathValue("id"), u.ID)
	if !okw {
		notFound(w, "工作区不存在")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}
	var req struct {
		Task         string `json:"task"`
		OperatorKey  string `json:"operatorKey"`
		Permission   string `json:"permission"`
		Model        string `json:"model"`
		MaxSteps     int    `json:"maxSteps"`
		SystemPrompt string `json:"systemPrompt"`
	}
	if err := decode(r, &req); err != nil || strings.TrimSpace(req.Task) == "" {
		badRequest(w, "task 不能为空")
		return
	}

	perm := agent.Permission(req.Permission)
	if perm == "" {
		perm = agent.PermissionApproval
	}
	operatorName := req.OperatorKey
	// 会话里带了角色时，从角色上读权限与名字；请求体显式给的优先。
	if bw.ConversationID != "" {
		if conv, err := s.chat.Get(u.ID, bw.ConversationID); err == nil {
			for _, a := range service.ConversationAgents(conv) {
				if req.OperatorKey == "" || a.Key == req.OperatorKey || a.Name == req.OperatorKey {
					operatorName = a.Name
					if req.OperatorKey == "" {
						req.OperatorKey = a.Key
					}
					if req.Permission == "" && a.Permission != "" {
						perm = agent.Permission(a.Permission)
					}
					break
				}
			}
		}
	}
	if !agent.ValidPermission(perm) {
		badRequest(w, "permission 只能是 readonly / approval / full")
		return
	}

	run, err := s.agents.Start(r.Context(), s.gw, agent.StartRequest{
		WorkspaceID:    bw.ID,
		ConversationID: bw.ConversationID,
		UserID:         u.ID,
		OperatorKey:    req.OperatorKey,
		OperatorName:   operatorName,
		Permission:     perm,
		Task:           req.Task,
		Model:          req.Model,
		MaxSteps:       req.MaxSteps,
		SystemPrompt:   req.SystemPrompt,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already has a running run") {
			fail(w, 409, "run_busy", "这个工作区已有正在运行的任务")
			return
		}
		badRequest(w, err.Error())
		return
	}
	created(w, run)
}

// handleGetRun 查询运行状态与工具时间线。
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	run, okr := s.agents.GetRun(r.PathValue("id"))
	if !okr || run.UserID != u.ID {
		notFound(w, "任务不存在")
		return
	}
	ok(w, run)
}

// handleRunEvents 推送运行事件（含历史重放）。
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	run, okr := s.agents.GetRun(r.PathValue("id"))
	if !okr || run.UserID != u.ID {
		notFound(w, "任务不存在")
		return
	}
	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.agents.Subscribe(run.ID)
	defer cancel()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, more := <-ch:
			if !more {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
	}
}

// handleApprove 批准或拒绝一次待审批调用。
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	var req struct {
		ApprovalID string `json:"approvalId"`
		Approve    bool   `json:"approve"`
		Scope      string `json:"scope"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	if err := s.agents.Decide(req.ApprovalID, u.ID, req.Approve, req.Scope); err != nil {
		badRequest(w, err.Error())
		return
	}
	ok(w, map[string]any{"decided": true, "approved": req.Approve})
}

// handleAbort 中止运行。
func (s *Server) handleAbort(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if err := s.agents.Abort(r.PathValue("id"), u.ID); err != nil {
		notFound(w, "任务不存在")
		return
	}
	ok(w, map[string]bool{"aborted": true})
}

// handleRunDiff 查看本次运行的改动。
func (s *Server) handleRunDiff(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	run, okr := s.agents.GetRun(r.PathValue("id"))
	if !okr || run.UserID != u.ID {
		notFound(w, "任务不存在")
		return
	}
	ok(w, map[string]any{"runId": run.ID, "files": run.Diffs, "total": run.DiffTotal})
}

// handleRevert 回滚到 Run 开始前。
func (s *Server) handleRevert(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	changed, err := s.agents.Revert(r.PathValue("id"), u.ID)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	ok(w, map[string]any{"reverted": changed})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func isLoopback(remoteAddr string) bool {
	host := remoteAddr
	if i := strings.LastIndex(remoteAddr, ":"); i > 0 {
		host = remoteAddr[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// 让编译期确认 domain.ConvAgent 的权限字段被引用，避免误删。
var _ = domain.ConvAgent{}
var _ = workspace.FileInfo{}
