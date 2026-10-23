package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleAdminWallet 钱包总览：余额、充值/消费、兑换码与最近订单。
func (s *Server) handleAdminWallet(w http.ResponseWriter, r *http.Request) {
	st := s.admin.Wallet().Stats()
	// 订单/余额榜只存 userID，这里补一份名字，前端不必再拉全量用户。
	names := make(map[string]string)
	for _, a := range st.TopAccounts {
		if u, ok := s.store.GetUser(a.UserID); ok {
			names[a.UserID] = u.Name
		}
	}
	for _, o := range st.RecentOrders {
		if _, ok := names[o.UserID]; ok {
			continue
		}
		if u, ok := s.store.GetUser(o.UserID); ok {
			names[o.UserID] = u.Name
		}
	}
	ok(w, map[string]interface{}{
		"stats":    st,
		"names":    names,
		"settings": s.admin.Settings(),
		"currency": "CNY",
	})
}

// handleAdminWalletUser 单个用户的账户、流水与订单。
func (s *Server) handleAdminWalletUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, found := s.store.GetUser(id); !found {
		notFound(w, "用户不存在")
		return
	}
	wlt := s.admin.Wallet()
	ok(w, map[string]interface{}{
		"account":  wlt.Account(id),
		"txns":     wlt.Txns(id, 200),
		"orders":   wlt.Orders(id, 100),
		"risk":     firstRisk(s, id),
		"currency": "CNY",
	})
}

func firstRisk(s *Server, id string) interface{} {
	if ru, ok := s.admin.RiskForUser(id); ok {
		return ru
	}
	return nil
}

// handleAdminWalletTopUp 人工充值（订单渠道 manual）。
func (s *Server) handleAdminWalletTopUp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, found := s.store.GetUser(id); !found {
		notFound(w, "用户不存在")
		return
	}
	var req struct {
		Amount float64 `json:"amount"`
		Note   string  `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	txn, err := s.admin.Wallet().TopUp(id, req.Amount, req.Note, "admin")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "wallet_topup", id,
		"+"+strconv.FormatFloat(txn.AmountCNY, 'f', 2, 64)+" "+txn.Note, ClientIP(r))
	ok(w, map[string]interface{}{
		"account": s.admin.Wallet().Account(id),
		"txn":     txn,
	})
}

// handleAdminWalletAdjust 人工调整余额，可正可负。
func (s *Server) handleAdminWalletAdjust(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, found := s.store.GetUser(id); !found {
		notFound(w, "用户不存在")
		return
	}
	var req struct {
		Amount float64 `json:"amount"`
		Note   string  `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	txn, err := s.admin.Wallet().Adjust(id, req.Amount, req.Note, "admin")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "wallet_adjust", id,
		strconv.FormatFloat(txn.AmountCNY, 'f', 2, 64)+" "+txn.Note, ClientIP(r))
	ok(w, map[string]interface{}{
		"account": s.admin.Wallet().Account(id),
		"txn":     txn,
	})
}

// handleAdminRedeemCodes 列出兑换码。
func (s *Server) handleAdminRedeemCodes(w http.ResponseWriter, r *http.Request) {
	items := s.admin.Wallet().ListCodes()
	now := time.Now()
	type codeRow struct {
		service.RedeemCode
		Status string `json:"status"`
	}
	rows := make([]codeRow, 0, len(items))
	for _, c := range items {
		rows = append(rows, codeRow{RedeemCode: c, Status: c.Status(now)})
	}
	ok(w, map[string]interface{}{"items": rows})
}

// handleAdminRedeemCodeCreate 新建兑换码。不填 code 时自动生成。
func (s *Server) handleAdminRedeemCodeCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code      string  `json:"code"`
		AmountCNY float64 `json:"amountCny"`
		Plan      string  `json:"plan"`
		MaxUses   int     `json:"maxUses"`
		ExpiresAt string  `json:"expiresAt"`
		Note      string  `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Plan = strings.TrimSpace(req.Plan)
	if req.Plan != "" {
		if _, ok := s.admin.Plan(req.Plan); !ok {
			badRequest(w, "套餐不存在："+req.Plan)
			return
		}
	}
	var expires time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			// 管理台用 datetime-local，格式是 2006-01-02T15:04
			t, err = time.ParseInLocation("2006-01-02T15:04", req.ExpiresAt, time.Local)
			if err != nil {
				badRequest(w, "过期时间格式不正确")
				return
			}
		}
		expires = t
	}
	code, err := s.admin.Wallet().CreateCode(service.RedeemCode{
		Code: req.Code, AmountCNY: req.AmountCNY, Plan: req.Plan,
		MaxUses: req.MaxUses, ExpiresAt: expires, Note: req.Note,
	})
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "redeem_create", code.Code,
		strconv.FormatFloat(code.AmountCNY, 'f', 2, 64)+" 次 "+strconv.Itoa(code.MaxUses), ClientIP(r))
	ok(w, code)
}

// handleAdminRedeemCodeDisable 停用/启用兑换码。
func (s *Server) handleAdminRedeemCodeDisable(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var req struct {
		Disabled *bool `json:"disabled"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	disabled := true
	if req.Disabled != nil {
		disabled = *req.Disabled
	}
	out, err := s.admin.Wallet().SetCodeDisabled(code, disabled)
	if err != nil {
		if err == service.ErrCodeNotFound {
			notFound(w, err.Error())
			return
		}
		badRequest(w, err.Error())
		return
	}
	action := "redeem_disable"
	if !disabled {
		action = "redeem_enable"
	}
	s.admin.Audit("admin", action, out.Code, "", ClientIP(r))
	ok(w, out)
}

// handleAdminRedeemCodeDelete 删除未使用过的兑换码。
func (s *Server) handleAdminRedeemCodeDelete(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if err := s.admin.Wallet().DeleteCode(code); err != nil {
		if err == service.ErrCodeNotFound {
			notFound(w, err.Error())
			return
		}
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "redeem_delete", code, "", ClientIP(r))
	ok(w, map[string]string{"deleted": code})
}

// handleAdminOrders 最近的充值/兑换订单。
func (s *Server) handleAdminOrders(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(queryInt(r, "limit", 200), 1, 500)
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	ok(w, map[string]interface{}{
		"items":    s.admin.Wallet().Orders(user, limit),
		"currency": "CNY",
	})
}

// handleAdminOps 运营设置。
func (s *Server) handleAdminOps(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]interface{}{
		"settings": s.admin.Settings(),
		"plans":    s.admin.Plans(),
		"currency": "CNY",
	})
}

// handleAdminOpsSave 保存运营设置。默认套餐必须是已存在的套餐。
func (s *Server) handleAdminOpsSave(w http.ResponseWriter, r *http.Request) {
	var in service.Settings
	if err := decode(r, &in); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	saved, err := s.admin.UpdateSettings(in)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	s.admin.Audit("admin", "ops_settings", "settings",
		"注册="+boolText(saved.RegistrationOpen)+" 维护="+boolText(saved.MaintenanceMode)+
			" 自动扣费="+boolText(saved.AutoDeduct)+" 余额拦截="+boolText(saved.EnforceBalance),
		ClientIP(r))
	ok(w, map[string]interface{}{
		"settings": saved,
		"plans":    s.admin.Plans(),
		"currency": "CNY",
	})
}

// handleAdminRisk 风控报告。
func (s *Server) handleAdminRisk(w http.ResponseWriter, r *http.Request) {
	days := clampInt(queryInt(r, "days", 30), 7, 90)
	ok(w, s.admin.Risk(days))
}

func boolText(v bool) string {
	if v {
		return "开"
	}
	return "关"
}
