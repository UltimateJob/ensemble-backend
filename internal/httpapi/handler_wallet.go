package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleMyWallet 当前用户的钱包：余额、最近流水与订单。
func (s *Server) handleMyWallet(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	ok(w, s.userWallet(u))
}

// handleRedeemCode 用户兑换充值码。码里带套餐时顺带升级套餐与额度。
func (s *Server) handleRedeemCode(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		badRequest(w, "请填写兑换码")
		return
	}
	res, err := s.admin.Wallet().Redeem(u.ID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCodeNotFound),
			errors.Is(err, service.ErrCodeDisabled),
			errors.Is(err, service.ErrCodeExpired),
			errors.Is(err, service.ErrCodeUsedUp),
			errors.Is(err, service.ErrCodeUsedByUser):
			fail(w, 400, "redeem_failed", err.Error())
		default:
			badRequest(w, err.Error())
		}
		return
	}
	planApplied := false
	if res.Plan != "" {
		if updated, ok := s.admin.SetUserPlanWithQuota(u.ID, res.Plan, true); ok {
			planApplied = true
			u.Plan = updated.Plan
			u.Quota = updated.Quota
		}
	}
	_ = s.store.AppendEvent(&domain.Event{UserID: u.ID, Kind: "redeem", Target: res.Code, IP: ClientIP(r)})
	s.admin.Audit("user:"+u.ID, "redeem_use", res.Code,
		"+"+strconv.FormatFloat(res.AmountCNY, 'f', 2, 64), ClientIP(r))
	ok(w, map[string]interface{}{
		"redeemed":    res,
		"planApplied": planApplied,
		"wallet":      s.userWallet(u),
	})
}
