package httpapi

import (
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/config"
	"strconv"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	ok(w, map[string]interface{}{
		"user":      u,
		"remaining": u.Remaining(),
		// 让前端知道现在是真模型还是演示台词
		"demo":     !s.cfg.UpstreamConfigured(),
		"upstream": upstreamName(s.cfg),
	})
}

// upstreamName 给前端看的当前上游名。
func upstreamName(cfg config.Config) string {
	if cfg.UpstreamConfigured() {
		return "DeepSeek"
	}
	return "演示台词"
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	var req struct {
		Name   string `json:"name"`
		Avatar string `json:"avatar"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	// 重新读一次最新状态，避免用中间件里的旧副本整体覆盖
	cur, ok2 := s.store.GetUser(u.ID)
	if !ok2 {
		unauthorized(w, "账号不存在")
		return
	}
	if req.Name != "" {
		cur.Name = req.Name
	}
	if req.Avatar != "" {
		cur.Avatar = req.Avatar
	}
	_ = s.store.UpdateUser(cur)
	ok(w, cur)
}

// 互动/行为记录：登录、审批、分享、错误上报。
func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	var req struct {
		Kind   string            `json:"kind"`
		Target string            `json:"target"`
		Meta   map[string]string `json:"meta"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if req.Kind == "" {
		badRequest(w, "缺少事件类型")
		return
	}
	_ = s.store.AppendEvent(&domain.Event{
		UserID: u.ID, Kind: req.Kind, Target: req.Target, Meta: req.Meta,
		IP: ClientIP(r), UA: r.UserAgent(),
	})
	ok(w, map[string]string{"recorded": "true"})
}

func (s *Server) handleMyEvents(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	limit := queryInt(r, "limit", 100)
	all := s.store.ListEvents("", 500)
	out := make([]*domain.Event, 0, len(all))
	for _, e := range all {
		if e.UserID == u.ID {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	ok(w, out)
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
