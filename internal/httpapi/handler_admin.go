package httpapi

import (
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if req.Token != s.cfg.AdminToken {
		_ = s.store.AppendAudit(&domain.AuditLog{
			Actor: "unknown", Action: "admin_login_failed", IP: ClientIP(r),
		})
		fail(w, 401, "unauthorized", "令牌不正确")
		return
	}
	_ = s.store.AppendAudit(&domain.AuditLog{
		Actor: "admin", Action: "admin_login", IP: ClientIP(r),
	})
	ok(w, map[string]string{"adminToken": s.cfg.AdminToken})
}

// adminAuth 管理接口鉴权，走独立 token 头。
func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if token != s.cfg.AdminToken {
			unauthorized(w, "需要管理员令牌")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	ok(w, s.admin.OverviewOf())
}

func (s *Server) handleAdminModels(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		ok(w, s.admin.Models())
		return
	}
	var m domain.ModelInfo
	if err := decode(r, &m); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if m.Key == "" || m.Provider == "" {
		badRequest(w, "key 与 provider 必填")
		return
	}
	s.admin.UpsertModel(m)
	s.admin.Audit("admin", "model_upsert", m.Key, m.Provider, ClientIP(r))
	ok(w, s.admin.Models())
}

func (s *Server) handleAdminModelDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !s.admin.DeleteModel(key) {
		notFound(w, "模型不存在")
		return
	}
	s.admin.Audit("admin", "model_delete", key, "", ClientIP(r))
	ok(w, s.admin.Models())
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	ok(w, s.admin.Users())
}

func (s *Server) handleAdminUserStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	u, found := s.admin.SetUserStatus(r.PathValue("id"), req.Status)
	if !found {
		notFound(w, "用户不存在")
		return
	}
	s.admin.Audit("admin", "user_status", u.ID, req.Status, ClientIP(r))
	ok(w, u)
}

func (s *Server) handleAdminUserQuota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quota int `json:"quota"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	u, found := s.admin.SetUserQuota(r.PathValue("id"), req.Quota)
	if !found {
		notFound(w, "用户不存在")
		return
	}
	s.admin.Audit("admin", "user_quota", u.ID, "", ClientIP(r))
	ok(w, u)
}

func (s *Server) handleAdminResetProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !s.admin.ResetProvider(name) {
		notFound(w, "上游不存在")
		return
	}
	s.admin.Audit("admin", "provider_reset", name, "", ClientIP(r))
	ok(w, map[string]string{"reset": name})
}

func (s *Server) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	ok(w, s.admin.Logs(queryInt(r, "limit", 200)))
}

func (s *Server) handleAdminEvents(w http.ResponseWriter, r *http.Request) {
	ok(w, s.admin.Events(r.URL.Query().Get("kind"), queryInt(r, "limit", 200)))
}
