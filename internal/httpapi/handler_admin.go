package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// adminTokenOK 常数时间比较管理员令牌。
// 普通 == 比较的耗时随匹配前缀长度变化，攻击者可以借此逐字节猜令牌。
func (s *Server) adminTokenOK(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AdminToken)) == 1
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if !s.adminTokenOK(req.Token) {
		_ = s.store.AppendAudit(&domain.AuditLog{
			Actor: "unknown", Action: "admin_login_failed", IP: ClientIP(r),
		})
		fail(w, 401, "unauthorized", "令牌不正确")
		return
	}
	_ = s.store.AppendAudit(&domain.AuditLog{
		Actor: "admin", Action: "admin_login", IP: ClientIP(r),
	})
	// 不回显令牌：前端自己持有输入的那份。响应里再发一遍只会
	// 多一个泄漏面（日志、代理、浏览器缓存都会存响应体）。
	ok(w, map[string]bool{"ok": true})
}

// adminAuth 管理接口鉴权，走独立 token 头。
func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if !s.adminTokenOK(token) {
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
	// provider 必须真的注册过。否则模型表指向一个不存在的上游，
	// 路由永远选不中它，或者把已有模型的 provider 改错直接把线上对话打断。
	if _, exists := s.gw.Registry().Get(m.Provider); !exists {
		fail(w, 400, "unknown_provider",
			"provider 「"+m.Provider+"」不在网关注册表里（已有："+strings.Join(s.gw.Registry().Names(), "、")+"）")
		return
	}
	s.admin.UpsertModel(m)
	s.admin.Audit("admin", "model_upsert", m.Key, m.Provider, ClientIP(r))
	ok(w, s.admin.Models())
}

func (s *Server) handleAdminModelDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	// 自砖守卫：删掉最后一个启用的模型后，所有对话请求都会以
	// no_provider 失败，等于管理员亲手把服务下线。
	enabled := 0
	var target *domain.ModelInfo
	models := s.admin.Models()
	for i := range models {
		if models[i].Enabled {
			enabled++
		}
		if models[i].Key == key {
			target = &models[i]
		}
	}
	if target == nil {
		notFound(w, "模型不存在")
		return
	}
	if target.Enabled && enabled <= 1 {
		badRequest(w, "至少保留一个启用的模型")
		return
	}
	s.admin.DeleteModel(key)
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
