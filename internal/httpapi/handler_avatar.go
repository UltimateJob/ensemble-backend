package httpapi

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// 角色头像生成。
//
// 用户在「新建/编辑角色」里点「生成头像」，平台用固定风格提示词
// （角色名 + 简介做主体线索）出一张，存到 data/avatars/ 下并写回角色。
//
// 为什么同步返回而不是走异步任务：头像是一次性的、用户站在表单前等
// 结果的操作，十几秒可以接受；走任务队列反而要多一套轮询和状态。

// handleGenAgentAvatar 为一个已保存的角色生成头像。
func (s *Server) handleGenAgentAvatar(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.imageJobs == nil || !s.imageJobs.Enabled() {
		fail(w, 503, "image_disabled", "还没有配置生图渠道，暂时无法生成头像")
		return
	}
	if !s.allow("avatar:" + u.ID) {
		tooManyRequests(w)
		return
	}

	var req struct {
		// 三个都可以空：先在表单里生成头像、还没保存角色时也要能用。
		AgentID string `json:"agentId"`
		Name    string `json:"name"`
		Title   string `json:"title"`
		Prompt  string `json:"prompt"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}

	// 指定了 agentId 就以库里的为准，表单里传的只作兜底——
	// 避免前端拿着旧数据覆盖用户刚在库里改过的人设。
	if req.AgentID != "" {
		cur, okc := s.store.GetAgent(req.AgentID)
		if !okc || cur.UserID != u.ID {
			notFound(w, "角色不存在")
			return
		}
		req.Name, req.Title, req.Prompt = cur.Name, cur.Title, cur.Prompt
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		badRequest(w, "先给角色起个名字")
		return
	}

	res, promptUsed, err := s.imageJobs.GenerateAvatar(r.Context(), u.ID,
		req.Name, req.Title, req.Prompt, s.avatarDir)
	if err != nil {
		fail(w, 502, "image_failed", err.Error())
		return
	}
	if len(res.Files) == 0 {
		fail(w, 502, "image_failed", "上游没有返回图片")
		return
	}

	// 存成固定文件名（每个角色一张），重新生成时覆盖旧的。
	// 用哈希而不是角色 id：还没保存的角色也能先预览。
	name := avatarFileName(req.AgentID, req.Name)
	dst := filepath.Join(s.avatarDir, name)
	if err := copyFile(res.Files[0].Path, dst); err != nil {
		serverErr(w, err.Error())
		return
	}
	url := "/v1/agents/avatar/" + name

	// 角色已存在时顺手写回；不存在就只返回 URL 让前端拿去建号。
	if req.AgentID != "" {
		if cur, okc := s.store.GetAgent(req.AgentID); okc {
			cur.Avatar = url
			_ = s.store.UpdateAgent(cur)
		}
	}
	ok(w, map[string]any{
		"avatar": url,
		"prompt": promptUsed,
	})
}

// handleAgentAvatar 提供头像图片。公开可读——头像是展示用的，
// 带鉴权会让 <img> 加载不了（标签发不出 Authorization）。
func (s *Server) handleAgentAvatar(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name == "" || strings.Contains(name, "..") {
		notFound(w, "头像不存在")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.avatarDir, name))
}

func avatarFileName(agentID, name string) string {
	// 文件名里带一点名字的痕迹，便于人工排查；id 保证唯一。
	base := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>| `, r) {
			return -1
		}
		return r
	}, name)
	if len([]rune(base)) > 12 {
		base = string([]rune(base)[:12])
	}
	if base == "" {
		base = "agent"
	}
	if agentID == "" {
		return base + ".png"
	}
	return agentID + "-" + base + ".png"
}

// copyFile 把生成结果拷到头像目录。
//
// 不直接 rename：生成结果在 data/images 下（任务的输出目录），
// 跨目录 + 任务记录还要指向原文件，拷一份最省事。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

var _ = service.AvatarPrompt
