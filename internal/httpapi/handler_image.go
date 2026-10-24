package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// 生图接口。异步：提交返回 job id，前端轮询。
//
// 为什么不做成同步：一张图 10-60 秒，DALL·E 3 更慢。同步接口会把
// HTTP 连接挂在那里，网关、反代、浏览器任一层超时就前功尽弃，
// 而上游可能已经出了图、账单已经产生。

// handleImageSubmit 提交一个生图任务。
func (s *Server) handleImageSubmit(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.imageJobs == nil || !s.imageJobs.Enabled() {
		fail(w, 503, "image_disabled", "还没有配置生图渠道。到运维台的「上游渠道」里新增一个图片生成类型的渠道。")
		return
	}
	if !s.allow("img:" + u.ID) {
		tooManyRequests(w)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
		N      int    `json:"n"`
		Model  string `json:"model"`
		// Provider 固定风格时点名渠道；空表示随机（后端轮询）。
		Provider string `json:"provider"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		badRequest(w, "请填写画面描述")
		return
	}
	if len([]rune(req.Prompt)) > 4000 {
		badRequest(w, "描述太长了（上限 4000 字）")
		return
	}

	job, err := s.imageJobs.Submit(u.ID, gateway.ImageRequest{
		Prompt:   strings.TrimSpace(req.Prompt),
		Size:     req.Size,
		N:        req.N,
		Model:    req.Model,
		Provider: strings.TrimSpace(req.Provider),
	})
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	created(w, job)
}

// handleImageJob 查一个任务的状态。
func (s *Server) handleImageJob(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	id := r.PathValue("id")
	job, err := s.imageJobs.Get(u.ID, id)
	if err != nil {
		notFound(w, "任务不存在")
		return
	}
	ok(w, s.withImageURLs(u.ID, job))
}

// withImageURLs 给任务的每张图补一个可直接塞进 <Image> 的地址。
//
// 地址里带短时效签名：<Image> 带不了 Authorization 头，
// 而把接口改成公开又等于谁都能读别人的图。
func (s *Server) withImageURLs(userID string, job *service.ImageJob) map[string]any {
	files := make([]map[string]any, 0, len(job.Files))
	for i, f := range job.Files {
		u := ""
		if s.auth != nil {
			u = fmt.Sprintf("/v1/images/%s/file?i=%d&s=%s", job.ID, i,
				s.auth.SignImage(userID, job.ID, 30*time.Minute))
		}
		files = append(files, map[string]any{"path": f.Path, "size": f.Size, "url": u})
	}
	return map[string]any{
		"id":            job.ID,
		"status":        job.Status,
		"prompt":        job.Prompt,
		"error":         job.Error,
		"files":         files,
		"revisedPrompt": job.RevisedPrompt,
		"provider":      job.Provider,
		"createdAt":     job.CreatedAt,
		"endedAt":       job.EndedAt,
	}
}

// handleImageJobs 列出该用户的最近任务。
func (s *Server) handleImageJobs(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.imageJobs == nil {
		ok(w, map[string]any{"enabled": false, "items": []any{}, "providers": []string{}})
		return
	}
	type provView struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	var provs []provView
	for _, name := range s.imageJobs.Providers() {
		v := provView{Name: name}
		if g := s.imageJobs.Gen(name); g != nil {
			v.Model = g.Model()
		}
		provs = append(provs, v)
	}
	ok(w, map[string]any{
		"enabled":   s.imageJobs.Enabled(),
		"items":     s.imageJobs.List(u.ID, 20),
		"providers": provs,
	})
}

// handleImageFile 返回生成好的图片文件。
//
// 按 job id + 序号取，不接受任意路径——否则等于开了个能读服务器
// 任意文件的接口。鉴权和其它接口一致。
func (s *Server) handleImageFile(w http.ResponseWriter, r *http.Request) {
	// 两条路进来：
	//   1. 常规请求，Authorization 头里带令牌
	//   2. <Image> 标签，带 ?s=<签名>（标签带不了 header）
	var userID string
	if u, found := UserFrom(r.Context()); found {
		userID = u.ID
	} else if sig := r.URL.Query().Get("s"); sig != "" && s.auth != nil {
		uid, ok := s.auth.VerifyImageSig(r.PathValue("id"), sig)
		if !ok {
			unauthorized(w, "链接已过期")
			return
		}
		userID = uid
	} else {
		unauthorized(w, "未登录")
		return
	}
	if s.imageJobs == nil {
		notFound(w, "未启用生图")
		return
	}
	idx := 0
	if v := r.URL.Query().Get("i"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			idx = n
		}
	}
	path, err := s.imageJobs.File(userID, r.PathValue("id"), idx)
	if err != nil {
		notFound(w, "图片不存在")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

// handleImageCancel 中止一个还在跑的任务。
func (s *Server) handleImageCancel(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if err := s.imageJobs.Cancel(u.ID, r.PathValue("id")); err != nil {
		notFound(w, "任务不存在")
		return
	}
	ok(w, map[string]string{"status": "cancelled"})
}

var _ = service.ErrImageDisabled

// handleAvatarSubmit 按角色名字 + 简介生成一张头像。
//
// 和普通生图分开，因为提示词由平台固定：用户只提供名字和一句简介，
// 风格、构图、尺寸、负面约束都由模板决定。这样：
//   - 同一批头像看起来是一套的（都走同一个黏土肖像风格）
//   - 用户不能借这个接口写任意提示词（它不是一个通用生图入口）
func (s *Server) handleAvatarSubmit(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.imageJobs == nil || !s.imageJobs.Enabled() {
		fail(w, 503, "image_disabled", "还没有配置生图渠道，暂时不能生成头像。")
		return
	}
	if !s.allow("img:" + u.ID) {
		tooManyRequests(w)
		return
	}

	var req struct {
		Name string `json:"name"`
		Bio  string `json:"bio"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		badRequest(w, "先给角色起个名字")
		return
	}
	// 名字和简介会原样进提示词。不截断的话，一段几万字的输入
	// 会直接变成上游的 token 账单。
	nameRunes := []rune(req.Name)
	if len(nameRunes) > 30 {
		nameRunes = nameRunes[:30]
	}
	bioRunes := []rune(strings.TrimSpace(req.Bio))
	if len(bioRunes) > 200 {
		bioRunes = bioRunes[:200]
	}

	prompt := avatarPrompt(string(nameRunes), string(bioRunes))
	job, err := s.imageJobs.Submit(u.ID, gateway.ImageRequest{
		Prompt: prompt,
		Size:   "1024x1024",
		N:      1,
	})
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	created(w, job)
}

// avatarPrompt 平台固定的头像提示词。
//
// 三条约束来自实际效果：
//   - 方形头像构图 + 居中半身，裁成圆形后不会切掉脸
//   - 柔和单色背景，避免头像在深浅两种主题下都不协调
//   - 明确负面词，减少文字、水印、多手多眼这类常见崩坏
func avatarPrompt(name, bio string) string {
	var sb strings.Builder
	sb.WriteString("A stylized clay-sculpture portrait avatar of a character named \"")
	sb.WriteString(name)
	sb.WriteString("\". ")
	if bio != "" {
		sb.WriteString("Personality and role: ")
		sb.WriteString(bio)
		sb.WriteString(". ")
	}
	sb.WriteString("Head and shoulders centered, facing slightly off-camera, calm confident expression. ")
	sb.WriteString("Soft matte clay texture with gentle studio lighting, muted warm earth tones, ")
	sb.WriteString("plain soft beige background, subtle ambient shadow. ")
	sb.WriteString("Square compose, generous margin around the head so it reads well when cropped to a circle. ")
	sb.WriteString("Clean, premium, editorial character design. ")
	sb.WriteString("No text, no watermark, no logo, no frame, no additional people, no extra limbs.")
	return sb.String()
}
