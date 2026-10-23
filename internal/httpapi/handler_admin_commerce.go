package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// handleAdminPlans 套餐配置：列表与新增/编辑。
func (s *Server) handleAdminPlans(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		ok(w, map[string]interface{}{
			"items":    s.admin.Plans(),
			"currency": "CNY",
		})
		return
	}
	var p domain.PlanConfig
	if err := decode(r, &p); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if err := s.admin.UpsertPlan(p); err != nil {
		badRequest(w, err.Error())
		return
	}
	saved, _ := s.admin.Plan(p.ID)
	s.admin.Audit("admin", "plan_upsert", saved.ID, saved.Name, ClientIP(r))
	ok(w, map[string]interface{}{
		"items":    s.admin.Plans(),
		"currency": "CNY",
	})
}

// handleAdminPlanDelete 删除套餐。仍被用户使用时拒绝，
// 避免用户落在一个不存在的计费维度上。
func (s *Server) handleAdminPlanDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if _, found := s.admin.Plan(id); !found {
		notFound(w, "套餐不存在")
		return
	}
	if n := s.admin.UsersOnPlan(id); n > 0 {
		fail(w, 400, "plan_in_use", "还有 "+strconv.Itoa(n)+" 个用户使用这个套餐，请先为他们换套餐")
		return
	}
	if err := s.admin.DeletePlan(id); err != nil {
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "plan_delete", id, "", ClientIP(r))
	ok(w, map[string]interface{}{
		"items":    s.admin.Plans(),
		"currency": "CNY",
	})
}

// handleAdminCommerce 成本、收入与毛利。
func (s *Server) handleAdminCommerce(w http.ResponseWriter, r *http.Request) {
	days := clampInt(queryInt(r, "days", 30), 7, 90)
	ok(w, s.admin.Commerce(days))
}
