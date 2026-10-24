package httpapi

import (
	"net/http"
	"strconv"
	"strings"

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
	ok(w, job)
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
	u, found := UserFrom(r.Context())
	if !found {
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
	path, err := s.imageJobs.File(u.ID, r.PathValue("id"), idx)
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
