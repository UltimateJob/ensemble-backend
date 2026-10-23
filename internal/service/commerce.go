package service

import (
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// CommerceReport 成本与利润页的数据。成本来自上游账单估算，
// 收入来自用户当前套餐的月价汇总，两者都按人民币计。
type CommerceReport struct {
	Since    time.Time     `json:"since"`
	Currency string        `json:"currency"`
	Totals   UsageAgg      `json:"totals"`
	Today    UsageAgg      `json:"today"`
	Month    UsageAgg      `json:"month"`
	Days     []DayUsage    `json:"days"`
	Models   []ModelUsage  `json:"models"`
	Revenue  Revenue       `json:"revenue"`
	Profit   Profit        `json:"profit"`
	Plans    []PlanRevenue `json:"plans"`
	TopUsers []TopUser     `json:"topUsers"`
	Recent   []UsageRecord `json:"recent"`
}

// Revenue 当前套餐结构下的月度经常性收入估算。
type Revenue struct {
	MRR         float64 `json:"mrr"`
	PaidUsers   int     `json:"paidUsers"`
	ActiveUsers int     `json:"activeUsers"`
	ARPU        float64 `json:"arpu"`
}

// Profit 本月成本与毛利估算。
type Profit struct {
	Cost      float64 `json:"cost"`
	Gross     float64 `json:"gross"`
	MarginPct float64 `json:"marginPct"`
}

// PlanRevenue 单个套餐的用户数、收入与本月成本。
type PlanRevenue struct {
	PlanID       string  `json:"planId"`
	Name         string  `json:"name"`
	PriceMonthly float64 `json:"priceMonthly"`
	Users        int     `json:"users"`
	Revenue      float64 `json:"revenue"`
	Cost         float64 `json:"cost"`
	Gross        float64 `json:"gross"`
}

// TopUser 成本最高的用户。
type TopUser struct {
	UserID    string  `json:"userId"`
	Name      string  `json:"name"`
	Plan      string  `json:"plan"`
	PlanPrice float64 `json:"planPrice"`
	UsageAgg
}

// Commerce 汇总成本、收入和毛利。
func (a *Admin) Commerce(days int) CommerceReport {
	b := a.billing
	if b == nil {
		b, _ = NewBilling("")
	}
	st := b.Stats(days)
	rep := CommerceReport{
		Since: st.Since, Currency: "CNY",
		Totals: st.Totals, Today: st.Today, Month: st.Month,
		Days: st.Days, Models: st.Models, Recent: st.Recent,
	}

	users := a.store.ListUsers()
	byID := make(map[string]*domain.User, len(users))
	planAgg := make(map[string]*PlanRevenue)
	plans := a.plans.List()
	for _, p := range plans {
		planAgg[p.ID] = &PlanRevenue{
			PlanID: p.ID, Name: p.Name, PriceMonthly: p.PriceMonthly,
		}
	}
	for _, u := range users {
		byID[u.ID] = u
		if u.Status != "active" {
			continue
		}
		rep.Revenue.ActiveUsers++
		p, ok := a.plans.Get(u.Plan)
		if !ok {
			continue
		}
		rep.Revenue.MRR += p.PriceMonthly
		if p.PriceMonthly > 0 {
			rep.Revenue.PaidUsers++
		}
		pr := planAgg[p.ID]
		if pr == nil {
			pr = &PlanRevenue{PlanID: p.ID, Name: p.Name, PriceMonthly: p.PriceMonthly}
			planAgg[p.ID] = pr
		}
		pr.Users++
		pr.Revenue += p.PriceMonthly
	}

	// 成本按用户当前套餐归因；月中切套餐的用户会全部落在当前套餐。
	for _, uu := range st.Users {
		if uu.UserID == "system" {
			continue
		}
		u := byID[uu.UserID]
		if u == nil {
			continue
		}
		if pr := planAgg[u.Plan]; pr != nil {
			pr.Cost += uu.Month.CostCNY
		}
	}

	rep.Plans = make([]PlanRevenue, 0, len(plans))
	for _, p := range plans {
		pr := planAgg[p.ID]
		if pr == nil {
			pr = &PlanRevenue{PlanID: p.ID, Name: p.Name, PriceMonthly: p.PriceMonthly}
		}
		pr.Gross = pr.Revenue - pr.Cost
		rep.Plans = append(rep.Plans, *pr)
	}
	if rep.Revenue.PaidUsers > 0 {
		rep.Revenue.ARPU = rep.Revenue.MRR / float64(rep.Revenue.PaidUsers)
	}
	rep.Profit.Cost = st.Month.CostCNY
	rep.Profit.Gross = rep.Revenue.MRR - rep.Profit.Cost
	if rep.Revenue.MRR > 0 {
		rep.Profit.MarginPct = rep.Profit.Gross / rep.Revenue.MRR * 100
	}

	rep.TopUsers = make([]TopUser, 0, 50)
	for _, uu := range st.Users {
		if len(rep.TopUsers) >= 50 || uu.UserID == "system" {
			break
		}
		u := byID[uu.UserID]
		if u == nil {
			continue
		}
		price := 0.0
		if p, ok := a.plans.Get(u.Plan); ok {
			price = p.PriceMonthly
		}
		rep.TopUsers = append(rep.TopUsers, TopUser{
			UserID: u.ID, Name: u.Name, Plan: u.Plan, PlanPrice: price,
			UsageAgg: uu.Month,
		})
	}
	return rep
}

// BillingStatus 系统页使用的成本账本状态。
func (a *Admin) BillingStatus() BillingStatus {
	if a.billing == nil {
		return BillingStatus{Currency: "CNY"}
	}
	return a.billing.Status()
}

// UserBilling 用户详情里的成本摘要。
func (a *Admin) UserBilling(id string) UserUsage {
	if a.billing == nil {
		return UserUsage{UserID: id}
	}
	return a.billing.User(id)
}
