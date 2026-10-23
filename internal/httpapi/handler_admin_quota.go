package httpapi

import (
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/gateway"
)

// handleAdminQuotas 返回所有支持额度展示的渠道。
//
// 结果由服务端缓存 45 秒；管理台点「刷新」时带 refresh=1 跳过缓存。
func (s *Server) handleAdminQuotas(w http.ResponseWriter, r *http.Request) {
	if s.quotas == nil {
		fail(w, 503, "unavailable", "渠道额度服务未初始化")
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	ok(w, map[string]interface{}{
		"items":   s.quotas.Snapshot(r.Context(), refresh),
		"configs": s.quotas.Configs(),
	})
}

// handleAdminQuotaConfig 保存渠道额度查询配置。
// OpenDesign 需要 Control Key 和工作区 ID；其它渠道通常留空自动识别。
func (s *Server) handleAdminQuotaConfig(w http.ResponseWriter, r *http.Request) {
	if s.quotas == nil {
		fail(w, 503, "unavailable", "渠道额度服务未初始化")
		return
	}
	var in gateway.QuotaConfig
	if err := decode(r, &in); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if err := s.quotas.SaveConfig(in); err != nil {
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "quota_config", in.Provider, in.Kind, ClientIP(r))
	ok(w, map[string]interface{}{"configs": s.quotas.Configs()})
}

// handleAdminQuotaConfigDelete 删除配置，回到自动识别。
func (s *Server) handleAdminQuotaConfigDelete(w http.ResponseWriter, r *http.Request) {
	if s.quotas == nil {
		fail(w, 503, "unavailable", "渠道额度服务未初始化")
		return
	}
	name := r.PathValue("name")
	if err := s.quotas.DeleteConfig(name); err != nil {
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "quota_config_delete", name, "", ClientIP(r))
	ok(w, map[string]interface{}{"configs": s.quotas.Configs()})
}
