package httpapi

import (
	"encoding/base64"
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
		// ImageBase64 图生图的原图（不含 data: 前缀）。
		// 用 base64 传而不是先上传再引用：图生图的图通常就是上一张
		// 生成结果，用户不会为「改一下这张」专门走一次上传流程。
		ImageBase64 string `json:"imageBase64"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}

	// 原图：只收常见位图格式，限 8MB。
	// 不校验类型的话，用户传一个 PDF 进来，上游会以
	// 「content policy violation」这种完全误导的文案失败。
	var srcImg []byte
	if b64 := strings.TrimSpace(req.ImageBase64); b64 != "" {
		if i := strings.Index(b64, "base64,"); i >= 0 {
			b64 = b64[i+len("base64,"):] // 容忍前端直接传 data URI
		}
		raw, derr := base64.StdEncoding.DecodeString(b64)
		if derr != nil {
			badRequest(w, "原图不是有效的 base64")
			return
		}
		if len(raw) > 8<<20 {
			badRequest(w, "原图太大了（上限 8MB）")
			return
		}
		if !isRasterImage(raw) {
			badRequest(w, "原图只支持 PNG / JPEG / WebP")
			return
		}
		srcImg = raw
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
		Prompt:      strings.TrimSpace(req.Prompt),
		Size:        req.Size,
		N:           req.N,
		Model:       req.Model,
		Provider:    strings.TrimSpace(req.Provider),
		SourceImage: srcImg,
		SourceName:  srcImageName(srcImg),
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

// isRasterImage 按文件头判断是不是位图。
// 看魔数而不是扩展名：用户从剪贴板粘贴时没有扩展名。
func isRasterImage(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	if b[0] == 0xFF && b[1] == 0xD8 { // JPEG
		return true
	}
	if b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' { // PNG
		return true
	}
	if b[0] == 'R' && b[1] == 'I' && b[2] == 'F' && b[3] == 'F' &&
		b[8] == 'W' && b[9] == 'E' && b[10] == 'B' && b[11] == 'P' { // WebP
		return true
	}
	return false
}

// srcImageName 按魔数给个文件名。部分网关靠 MIME 推断格式，
// 名字和内容对不上会解不出来。
func srcImageName(b []byte) string {
	switch {
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8:
		return "source.jpg"
	case len(b) > 3 && b[0] == 0x89 && b[1] == 'P':
		return "source.png"
	case len(b) > 11 && b[8] == 'W':
		return "source.webp"
	}
	return "source.png"
}
