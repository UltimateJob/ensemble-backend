package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
		Task         string   `json:"task"`
		OperatorKey  string   `json:"operatorKey"`
		Permission   string   `json:"permission"`
		Scope        []string `json:"scope"`
		Model        string   `json:"model"`
		MaxSteps     int      `json:"maxSteps"`
		AutoReview   bool     `json:"autoReview"`
		AutoFix      bool     `json:"autoFix"`
		SystemPrompt string   `json:"systemPrompt"`
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
	operatorScope := req.Scope
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
					operatorScope = a.Scope
					break
				}
			}
		}
	}
	if !agent.ValidPermission(perm) {
		badRequest(w, "permission 只能是 readonly / ask / approval / full")
		return
	}

	// 远程设备策略：设备开着「需要审批」时，即使是 full 权限，
	// 写文件与跑命令也先问一句。关掉是用户自己的选择，不阻拦。
	forceApproval, origin := false, ""
	if dev := DeviceFrom(r.Context()); dev != nil {
		forceApproval = dev.RequireApproval
		origin = "device:" + dev.ID
	}

	run, err := s.agents.Start(r.Context(), s.gw, agent.StartRequest{
		WorkspaceID:    bw.ID,
		ConversationID: bw.ConversationID,
		UserID:         u.ID,
		OperatorKey:    req.OperatorKey,
		OperatorName:   operatorName,
		Permission:     perm,
		Scope:          operatorScope,
		Task:           req.Task,
		Model:          req.Model,
		MaxSteps:       req.MaxSteps,
		AutoReview:     req.AutoReview,
		AutoFix:        req.AutoFix,
		SystemPrompt:   req.SystemPrompt,
		ForceApproval:  forceApproval,
		Origin:         origin,
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

// handleCollab 群聊讨论 + 分头执行。
//
// 先让群里的角色讨论一轮（同步，通常 3-6 秒），再把任务派给有写权限的角色。
// 这是「架构师接需求 → 群里对齐 → 前后端各自开干」的完整链路，
// 之前只有脚本能跑，界面上没有入口。
func (s *Server) handleCollab(w http.ResponseWriter, r *http.Request) {
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
	if bw.ConversationID == "" {
		badRequest(w, "这个工作区没有绑定群聊")
		return
	}
	var req struct {
		Task           string `json:"task"`
		MaxStepsPerRun int    `json:"maxStepsPerRun"`
		DiscussOnly    bool   `json:"discussOnly"`
	}
	if err := decode(r, &req); err != nil || strings.TrimSpace(req.Task) == "" {
		badRequest(w, "task 不能为空")
		return
	}
	conv, err := s.chat.Get(u.ID, bw.ConversationID)
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}

	// 1) 讨论：同步跑一轮，收集每个角色的发言
	type line struct {
		Agent string `json:"agent"`
		Text  string `json:"text"`
	}
	var lines []line
	s.orchestrator.Run(r.Context(), conv, u, req.Task, func(ev service.Event) {
		if ev.Type == "end" && strings.TrimSpace(ev.Content) != "" {
			lines = append(lines, line{Agent: ev.Name, Text: strings.TrimSpace(ev.Content)})
		}
	})

	if req.DiscussOnly {
		ok(w, map[string]any{"discussion": lines, "runs": []any{}})
		return
	}

	// 2) 分头干：只派给「能写」的角色，readonly 留下看
	agents := service.ConversationAgents(conv)
	plan := ""
	for _, l := range lines {
		for _, a := range agents {
			if a.Name == l.Agent {
				plan = l.Text
			}
		}
	}
	if plan == "" && len(lines) > 0 {
		plan = lines[0].Text
	}

	type started struct {
		Agent string `json:"agent"`
		RunID string `json:"runId"`
		Error string `json:"error,omitempty"`
	}
	var startedRuns []started
	maxSteps := req.MaxStepsPerRun
	if maxSteps <= 0 {
		maxSteps = 10
	}
	for _, a := range agents {
		perm := agent.Permission(a.Permission)
		if perm != agent.PermissionApproval && perm != agent.PermissionFull {
			continue
		}
		task := strings.TrimSpace(req.Task)
		if plan != "" {
			task += "\n\n架构师的方案：\n" + plan
		}
		run, err := s.agents.Start(r.Context(), s.gw, agent.StartRequest{
			WorkspaceID:    bw.ID,
			ConversationID: bw.ConversationID,
			UserID:         u.ID,
			OperatorKey:    a.Key,
			OperatorName:   a.Name,
			Permission:     perm,
			Scope:          a.Scope,
			Task:           task,
			MaxSteps:       maxSteps,
		})
		if err != nil {
			startedRuns = append(startedRuns, started{Agent: a.Name, Error: err.Error()})
			continue
		}
		startedRuns = append(startedRuns, started{Agent: a.Name, RunID: run.ID})
	}
	if len(startedRuns) == 0 {
		ok(w, map[string]any{"discussion": lines, "runs": []any{}, "note": "群里没有可执行的角色（权限都是只读）"})
		return
	}
	ok(w, map[string]any{"discussion": lines, "runs": startedRuns})
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
	// 带上审批记录：手机端不在电脑前，看不到终端的审批提示，
	// 必须能主动查「有没有卡在等我」。
	ok(w, struct {
		*agent.Run
		Approvals []*agent.Approval `json:"approvals,omitempty"`
	}{run, s.agents.ApprovalsOf(run.ID)})
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

func (s *Server) handleBindConvWorkspace(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if !isLoopback(r.RemoteAddr) {
		fail(w, 403, "forbidden", "只能在本机绑定本地工作区")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if !conv.IsOwner(u.ID) {
		fail(w, 403, "forbidden", "只有群主能绑定工作区")
		return
	}
	if s.agents == nil {
		serverErr(w, "工作区服务未启用")
		return
	}
	var req struct {
		Kind  string `json:"kind"`
		Root  string `json:"root"`
		Label string `json:"label"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式错误")
		return
	}
	switch strings.TrimSpace(req.Kind) {
	case "cloud":
		fail(w, 400, "cloud_not_available", "云端工作区即将支持")
		return
	case "", "local":
	default:
		badRequest(w, "只支持本地工作区")
		return
	}
	root := strings.TrimSpace(req.Root)
	if root == "" {
		badRequest(w, "root 不能为空")
		return
	}
	label := strings.TrimSpace(req.Label)
	bw, err := s.agents.BindWorkspaceFor(u.ID, conv.ID, root, label)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	// 重复绑同一个目录不该再写一条系统提示。
	//
	// 前端在打开选择面板、保存设置、切回房间等好几个地方都会调这个接口，
	// 每次落一条「已绑定工作区」，聊天气泡里就堆了三条一模一样的。
	alreadyBound := conv.Workspace != nil && conv.Workspace.Root == bw.Root
	if label == "" {
		label = filepath.Base(bw.Root)
	}
	conv.Workspace = &domain.ConvWorkspace{
		ID:      bw.ID,
		Kind:    "local",
		Root:    bw.Root,
		Label:   label,
		BoundAt: time.Now(),
	}
	// 刚绑上目录时，谁都没选过档位——落到最安全的「逐项批准」。
	// 已经有明确选择（包括显式空字符串以外的值）就不覆盖。
	if strings.TrimSpace(conv.DefaultPermission) == "" {
		conv.DefaultPermission = "ask"
	}
	if err := s.chat.Update(conv); err != nil {
		serverErr(w, err.Error())
		return
	}
	if !alreadyBound {
		s.chat.AppendSystemNote(conv.ID, "已绑定本地工作区："+bw.Root+"\n目录只是就位，还没有人派活。要说清楚要做什么。")
	}
	ok(w, conv)
}

// handleUnbindConvWorkspace 解绑会话工作区。有任务在跑时返回 409。

func (s *Server) handleUnbindConvWorkspace(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if !conv.IsOwner(u.ID) {
		fail(w, 403, "forbidden", "只有群主能解绑工作区")
		return
	}
	if s.agents == nil || !s.agents.UnbindByConversation(conv.ID, u.ID) {
		fail(w, 409, "workspace_busy", "工作区还有任务在运行，等结束再解绑")
		return
	}
	conv.Workspace = nil
	if err := s.chat.Update(conv); err != nil {
		serverErr(w, err.Error())
		return
	}
	s.chat.AppendSystemNote(conv.ID, "已解绑本地工作区")
	ok(w, map[string]bool{"removed": true})
}

// handleWorkspaceRaw 下载工作区内的原始文件（docx/xlsx 等二进制）。
// 与 /file 的分页文本读取不同，这里是整文件字节流，交给 ServeContent 处理 Range。

func (s *Server) handleWorkspaceRaw(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if !isLoopback(r.RemoteAddr) {
		fail(w, 403, "forbidden", "只能在本机下载工作区文件")
		return
	}
	bw, okw := s.agents.Workspace(r.PathValue("id"), u.ID)
	if !okw {
		notFound(w, "工作区不存在")
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		badRequest(w, "path 不能为空")
		return
	}
	f, info, err := bw.FS().OpenFile(p)
	if err != nil {
		switch {
		case errors.Is(err, workspace.ErrNotFound):
			notFound(w, "文件不存在")
		default:
			badRequest(w, err.Error())
		}
		return
	}
	defer f.Close()
	name := info.Name()
	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if cd := mime.FormatMediaType("attachment", map[string]string{"filename": name}); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// handleWorkspaceTree 列出目录（一层）。

func (s *Server) handleExecute(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if !conv.IsOwner(u.ID) {
		fail(w, 403, "owner_only", "只有群主可以派活，协作者暂时只能围观")
		return
	}
	if s.agents == nil || !service.WorkspaceBound(conv) {
		fail(w, 409, "workspace_required", "先为这个群选择本地工作区")
		return
	}
	bw, okw := s.agents.WorkspaceByConversation(conv.ID)
	if !okw {
		fail(w, 409, "workspace_required", "先为这个群选择本地工作区")
		return
	}
	if u.Remaining() <= 0 {
		fail(w, 402, "quota_exceeded", "本月额度已用完")
		return
	}

	var req struct {
		Content   string   `json:"content"`
		Task      string   `json:"task"`
		AgentKeys []string `json:"agentKeys"`
		MaxSteps  int      `json:"maxSteps"`
		// Silent 只给团队流水线用：后续阶段的派活不重复落一条用户消息，
		// 否则一次团队任务会在聊天里留下三四条内部提示词。
		Silent bool `json:"silent"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
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

	agents := service.ConversationAgents(conv)
	targets := service.AgentsByKeys(agents, req.AgentKeys)
	if len(targets) == 0 {
		targets = service.MentionedAgents(agents, content)
	}
	if len(targets) == 0 {
		fail(w, 400, "mention_required", "请 @ 一个角色来领这个任务")
		return
	}

	mentionKeys := make([]string, 0, len(targets))
	for _, a := range targets {
		mentionKeys = append(mentionKeys, a.Key)
	}
	// 只落一条用户消息：这是「用户说了什么」的唯一真相。
	// silent 阶段（架构->前端/后端->测试）只跑任务，不再写聊天记录。
	var msg *domain.Message
	if !req.Silent {
		msg = s.chat.AppendUserMessage(conv.ID, content, mentionKeys)
	}

	// 把最近几条群聊内容附在任务后面，让领活的角色知道讨论到了哪一步。
	task := strings.TrimSpace(req.Task)
	if task == "" {
		task = content
	}
	skipID := ""
	if msg != nil {
		skipID = msg.ID
	}
	// 上下文单独传，不并进 task：task 会显示在执行记录和时间线上，
	// 塞进背景后用户看到的是自己没说过的一长串话。
	digest := s.contextDigest(conv.ID, skipID, 6)

	maxSteps := req.MaxSteps
	if maxSteps <= 0 {
		maxSteps = agent.DefaultMaxSteps
	}
	if maxSteps > agent.MaxAllowedSteps {
		maxSteps = agent.MaxAllowedSteps
	}

	type runResult struct {
		AgentKey   string `json:"agentKey"`
		AgentName  string `json:"agentName"`
		RunID      string `json:"runId,omitempty"`
		Permission string `json:"permission,omitempty"`
		Error      string `json:"error,omitempty"`
	}
	results := make([]runResult, 0, len(targets))
	for _, a := range targets {
		perm := service.ResolveAgentPermission(conv, *a)
		p := agent.Permission(strings.TrimSpace(perm))
		if p == "" || !agent.ValidPermission(p) {
			results = append(results, runResult{
				AgentKey: a.Key, AgentName: a.Name, Permission: perm,
				Error: "这个角色没有可执行权限",
			})
			continue
		}
		systemPrompt := strings.TrimSpace(a.Prompt)
		if systemPrompt != "" {
			systemPrompt += "\n\n"
		}
		systemPrompt += agent.DefaultSystemPrompt(a.Name)
		systemPrompt += "\n\n工作区规则：所有路径必须是工作区相对路径；先读再改；改完必须跑测试。"

		run, err := s.agents.Start(r.Context(), s.gw, agent.StartRequest{
			WorkspaceID:    bw.ID,
			ConversationID: conv.ID,
			UserID:         u.ID,
			OperatorKey:    a.Key,
			OperatorName:   a.Name,
			Permission:     p,
			// 单聊里用户就是要这个人把整件事做完：内置角色的 scope
			// 本来是给团队并行分工用的，单聊继续限制会让它连前端文件都改不了。
			Scope:        singleAgentScope(a.Scope, len(targets)),
			Task:         task,
			Context:      digest,
			Model:        conv.Model,
			MaxSteps:     maxSteps,
			SystemPrompt: systemPrompt,
		})
		if err != nil {
			results = append(results, runResult{
				AgentKey: a.Key, AgentName: a.Name, Permission: perm, Error: err.Error(),
			})
			continue
		}
		// Run 的生命周期由 manager 用独立 context 管理，HTTP 返回不会取消它。
		results = append(results, runResult{
			AgentKey: a.Key, AgentName: a.Name, RunID: run.ID, Permission: perm,
		})
	}
	created(w, map[string]any{"message": msg, "runs": results})
}

// contextDigest 取最近 limit 条消息做成执行任务的上下文摘要。
// 每条裁剪到 ~600 字，避免把整段聊天原样塞进 prompt。

func (s *Server) contextDigest(convID, skipMsgID string, limit int) string {
	msgs := s.store.ListMessages(convID, limit)
	if len(msgs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m == nil || m.ID == skipMsgID {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		speaker := "用户"
		switch m.Role {
		case domain.RoleAssistant:
			speaker = m.Agent
			if speaker == "" {
				speaker = "角色"
			}
		case domain.RoleSystem:
			speaker = "系统"
		}
		text = strings.Join(strings.Fields(text), " ")
		if runes := []rune(text); len(runes) > 600 {
			text = string(runes[:600]) + "…"
		}
		lines = append(lines, speaker+"："+text)
	}
	return strings.Join(lines, "\n")
}

// handleConvActivity 返回群成员状态与最近任务，供前端轮询。

func (s *Server) handleConvActivity(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}

	// workspace：以会话里的持久化副本为准，缺失时回退到管理器。
	wsOut := map[string]any{"id": "", "root": "", "label": "", "bound": false}
	if conv.Workspace != nil && strings.TrimSpace(conv.Workspace.Root) != "" {
		wsOut = map[string]any{
			"id": conv.Workspace.ID, "root": conv.Workspace.Root,
			"label": conv.Workspace.Label, "bound": true,
		}
	} else if s.agents != nil {
		if bw, okw := s.agents.WorkspaceByConversation(conv.ID); okw {
			wsOut = map[string]any{
				"id": bw.ID, "root": bw.Root, "label": bw.Label, "bound": true,
			}
		}
	}

	var runs []*agent.Run
	if s.agents != nil {
		runs = s.agents.RunsForConversation(conv.ID, false, 50)
	}

	agents := service.ConversationAgents(conv)
	agentsOut := make([]map[string]any, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		perm := service.ResolveAgentPermission(conv, *a)
		item := map[string]any{
			"key": a.Key, "name": a.Name, "title": a.Title, "color": a.Color,
			"permission": perm,
			"state":      "idle",
			"runId":      "",
			"step":       0,
			"maxSteps":   agent.DefaultMaxSteps,
			"elapsedMs":  int64(0),
			"tokens":     map[string]int{"in": 0, "out": 0},
		}

		var active, lastDone *agent.Run
		for _, run := range runs {
			if run.OperatorKey != a.Key && (a.Name == "" || run.OperatorName != a.Name) {
				continue
			}
			if (run.Status == agent.StatusRunning || run.Status == agent.StatusWaiting) && active == nil {
				active = run
			}
			if run.Status != agent.StatusRunning && run.Status != agent.StatusWaiting && lastDone == nil {
				lastDone = run
			}
		}
		if active != nil {
			item["state"] = string(active.Status)
			item["runId"] = active.ID
			item["step"] = active.Step
			item["maxSteps"] = active.MaxSteps
			item["elapsedMs"] = runElapsedMS(active)
			item["tokens"] = map[string]int{"in": active.TokensIn, "out": active.TokensOut}
			if tc := lastOpenToolCall(active); tc != nil {
				item["currentTool"] = map[string]any{
					"tool":      tc.Tool,
					"summary":   toolActivitySummary(tc.Tool, tc.Args),
					"startedAt": tc.StartedAt,
				}
			}
		} else if lastDone != nil {
			item["elapsedMs"] = runElapsedMS(lastDone)
			item["tokens"] = map[string]int{"in": lastDone.TokensIn, "out": lastDone.TokensOut}
		}
		if lastDone != nil {
			item["lastRun"] = map[string]any{
				"status":  string(lastDone.Status),
				"endedAt": lastDone.EndedAt,
				"summary": lastDone.Summary,
			}
		}
		agentsOut = append(agentsOut, item)
	}

	runsOut := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		item := map[string]any{
			"id":        run.ID,
			"agentKey":  run.OperatorKey,
			"agentName": run.OperatorName,
			"status":    run.Status,
			"task":      run.Task,
			"step":      run.Step,
			"maxSteps":  run.MaxSteps,
			"artifacts": run.Artifacts,
			"startedAt": run.StartedAt,
			"endedAt":   run.EndedAt,
		}
		if s.agents != nil {
			if pending := s.agents.PendingApprovals(run.ID); len(pending) > 0 {
				item["pendingApproval"] = pending[0]
			}
		}
		runsOut = append(runsOut, item)
	}

	ok(w, map[string]any{
		"workspace":         wsOut,
		"defaultPermission": conv.DefaultPermission,
		"agents":            agentsOut,
		"runs":              runsOut,
	})
}

// handleConvRuns 列出会话里的主任务（协作者也可查看）。

func (s *Server) handleConvRuns(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	limit := clampLimit(queryInt(r, "limit", 20))
	if s.agents == nil {
		ok(w, []*agent.Run{})
		return
	}
	ok(w, s.agents.RunsForConversation(conv.ID, false, limit))
}

// handleWorkspaceRuns 列出某个工作区的任务（群主）。

func (s *Server) handleWorkspaceRuns(w http.ResponseWriter, r *http.Request) {
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
	limit := clampLimit(queryInt(r, "limit", 20))
	ok(w, s.agents.ListRuns(bw.ID, limit))
}

func clampLimit(n int) int {
	if n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

func runElapsedMS(run *agent.Run) int64 {
	if run == nil {
		return 0
	}
	end := run.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	if end.Before(run.StartedAt) {
		return 0
	}
	return end.Sub(run.StartedAt).Milliseconds()
}

// lastOpenToolCall 返回最近一次仍在执行中的工具调用（EndedAt 为零）。
func lastOpenToolCall(run *agent.Run) *agent.ToolCall {
	if run == nil {
		return nil
	}
	for i := len(run.ToolCalls) - 1; i >= 0; i-- {
		tc := run.ToolCalls[i]
		if tc != nil && tc.EndedAt.IsZero() {
			return tc
		}
	}
	return nil
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if n <= 0 || len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// toolActivitySummary 把工具参数翻成成员栏/抽屉里能读的一句话。
// 直接展示 JSON 的话，用户看到的是 {"path":"src/main.go"}。
func toolActivitySummary(tool, raw string) string {
	var a struct {
		Path    string   `json:"path"`
		Pattern string   `json:"pattern"`
		Argv    []string `json:"argv"`
	}
	_ = json.Unmarshal([]byte(raw), &a)
	switch tool {
	case "read":
		if a.Path != "" {
			return "读取文件：" + a.Path
		}
	case "write":
		if a.Path != "" {
			return "写入文件：" + a.Path
		}
	case "edit":
		if a.Path != "" {
			return "修改文件：" + a.Path
		}
	case "list":
		if a.Path != "" {
			return "列目录：" + a.Path
		}
		return "列工作区根目录"
	case "glob":
		if a.Pattern != "" {
			return "查找文件：" + a.Pattern
		}
	case "grep":
		if a.Pattern != "" {
			return "搜索：" + a.Pattern
		}
	case "run":
		if len(a.Argv) > 0 {
			return "执行：" + strings.Join(a.Argv, " ")
		}
	case "docx_create":
		if a.Path != "" {
			return "生成 Word：" + a.Path
		}
	case "xlsx_create":
		if a.Path != "" {
			return "生成 Excel：" + a.Path
		}
	}
	return clipRunes(raw, 120)
}

// 让编译期确认 domain.ConvAgent 的权限字段被引用，避免误删。
var _ = domain.ConvAgent{}
var _ = workspace.FileInfo{}

// singleAgentScope 单聊时去掉内置角色的团队分工边界。
// 多人并行才需要「后端只改 backend/」这类约束；一个人干活时，
// 用户的心智是「把这件事做完」，不该被角色模板的默认 scope 挡住。
func singleAgentScope(scope []string, targets int) []string {
	if targets <= 1 {
		return nil
	}
	return scope
}
