package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/UltimateJob/ensemble-backend/internal/config"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	st := s.admin.Settings()
	ok(w, map[string]interface{}{
		"user":        u,
		"remaining":   u.Remaining(),
		"preferences": u.Preferences,
		"hasPassword": s.auth.HasPassword(u.ID),
		// 让前端知道现在是真模型还是演示台词
		"demo":     !s.cfg.UpstreamConfigured(),
		"upstream": upstreamName(s.cfg),
		"plan":     s.planSummary(u),
		"wallet":   s.admin.Wallet().Account(u.ID),
		"announcement": map[string]interface{}{
			"text":  st.Announcement,
			"level": st.AnnouncementLevel,
			"at":    st.AnnouncementAt,
		},
		"maintenance": st.MaintenanceMode,
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
		Name        *string           `json:"name"`
		Avatar      *string           `json:"avatar"`
		Preferences map[string]string `json:"preferences"`
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
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			badRequest(w, "昵称不能为空")
			return
		}
		if utf8.RuneCountInString(name) > 24 {
			badRequest(w, "昵称最多 24 个字")
			return
		}
		cur.Name = name
	}
	if req.Avatar != nil {
		avatar := strings.TrimSpace(*req.Avatar)
		if !validAvatar(avatar) {
			badRequest(w, "头像格式不正确")
			return
		}
		cur.Avatar = avatar
	}
	// 偏好按键合并：只覆盖请求里出现的键，空字符串表示删除该键。
	if len(req.Preferences) > 0 {
		if cur.Preferences == nil {
			cur.Preferences = make(map[string]string, len(req.Preferences))
		}
		for k, v := range req.Preferences {
			if v == "" {
				delete(cur.Preferences, k)
				continue
			}
			cur.Preferences[k] = v
		}
	}
	_ = s.store.UpdateUser(cur)
	ok(w, cur)
}

// validAvatar 头像只接受：空（清除）、#RRGGBB 颜色，或 emoji/短文本。
func validAvatar(avatar string) bool {
	if avatar == "" {
		return true
	}
	if utf8.RuneCountInString(avatar) > 64 {
		return false
	}
	if strings.HasPrefix(avatar, "#") {
		if len(avatar) != 7 {
			return false
		}
		for _, c := range avatar[1:] {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return true
	}
	// 非颜色值按 emoji / 短文本处理，只拒绝控制字符与空白。
	for _, r := range avatar {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// handleSetPassword 已登录用户设置或修改密码。
func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if err := s.auth.SetPassword(u.ID, req.OldPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, service.ErrWeakPassword):
			fail(w, 400, "weak_password", err.Error())
		case errors.Is(err, service.ErrOldPassword):
			fail(w, 400, "old_password_incorrect", err.Error())
		default:
			serverErr(w, err.Error())
		}
		return
	}
	ok(w, map[string]bool{"changed": true})
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
