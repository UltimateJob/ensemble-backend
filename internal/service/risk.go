package service

import (
	"fmt"
	"sort"
	"time"
)

// RiskReport 风控页的数据。这里只做「看见」，不改变请求行为；
// 拦截类的开关在运营设置里（维护模式、余额不足拦截）。
type RiskReport struct {
	GeneratedAt time.Time   `json:"generatedAt"`
	Settings    Settings    `json:"settings"`
	Summary     RiskSummary `json:"summary"`
	Alerts      []RiskAlert `json:"alerts"`
	Users       []RiskUser  `json:"users"`
}

// RiskSummary 顶部 KPI。
type RiskSummary struct {
	Critical           int     `json:"critical"`
	Warning            int     `json:"warning"`
	Info               int     `json:"info"`
	MonthCostCNY       float64 `json:"monthCostCny"`
	MRRCNY             float64 `json:"mrrCny"`
	MarginPct          float64 `json:"marginPct"`
	TodayCostCNY       float64 `json:"todayCostCny"`
	UsersOverThreshold int     `json:"usersOverThreshold"`
	NegativeBalances   int     `json:"negativeBalances"`
	DebtCNY            float64 `json:"debtCny"`
}

// RiskAlert 一条风险提示。
type RiskAlert struct {
	ID        string    `json:"id"`
	Level     string    `json:"level"` // critical | warning | info
	Kind      string    `json:"kind"`  // margin | user_cost | share | balance | daily_spend | error_rate | provider | models
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	UserID    string    `json:"userId,omitempty"`
	UserName  string    `json:"userName,omitempty"`
	Value     float64   `json:"value,omitempty"`
	Page      string    `json:"page,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// RiskUser 单个用户的风险画像。
type RiskUser struct {
	UserID       string  `json:"userId"`
	Name         string  `json:"name"`
	Plan         string  `json:"plan"`
	PlanName     string  `json:"planName,omitempty"`
	Status       string  `json:"status"`
	PlanPrice    float64 `json:"planPrice"`
	CostMonth    float64 `json:"costMonth"`
	CostToday    float64 `json:"costToday"`
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	BalanceCNY   float64 `json:"balanceCny"`
	SharePct     float64 `json:"sharePct"`
	Level        string  `json:"level"` // critical | warning | normal
	Remaining    int     `json:"remaining"`
	MonthlyQuota int     `json:"monthlyQuota"`
}

// Risk 汇总本月成本、收入、用户集中度与异常。
func (a *Admin) Risk(days int) RiskReport {
	if days <= 0 {
		days = 30
	}
	st := a.Settings()
	rep := RiskReport{
		GeneratedAt: time.Now(), Settings: st,
		Alerts: make([]RiskAlert, 0, 8),
		Users:  make([]RiskUser, 0, 16),
	}
	b := a.billing
	if b == nil {
		b, _ = NewBilling("")
	}
	stats := b.Stats(days)
	users := a.store.ListUsers()
	byID := make(map[string]*RiskUser, len(users))

	// 月成本包含 system（后台任务）；用户集中度只按用户可归因成本算。
	monthCost := stats.Month.CostCNY
	var userCost float64
	for _, u := range users {
		uu := b.User(u.ID)
		userCost += uu.Month.CostCNY
	}

	plans := a.plans.List()
	planByID := make(map[string]domainPlan, len(plans))
	for _, p := range plans {
		planByID[p.ID] = domainPlan{Name: p.Name, Price: p.PriceMonthly, Quota: p.MonthlyQuota}
	}
	var mrr float64
	for _, u := range users {
		if u.Status != "active" {
			continue
		}
		if p, ok := planByID[u.Plan]; ok {
			mrr += p.Price
		}
	}

	wallet := a.Wallet()
	for _, u := range users {
		uu := b.User(u.ID)
		ru := &RiskUser{
			UserID: u.ID, Name: u.Name, Plan: u.Plan, Status: u.Status,
			CostMonth: uu.Month.CostCNY, CostToday: uu.Today.CostCNY,
			Requests: uu.Month.Requests, Tokens: uu.Month.TotalTokens(),
			BalanceCNY: wallet.Balance(u.ID), Remaining: u.Remaining(),
			Level: "normal",
		}
		if p, ok := planByID[u.Plan]; ok {
			ru.PlanName, ru.PlanPrice, ru.MonthlyQuota = p.Name, p.Price, p.Quota
		}
		if userCost > 0 {
			ru.SharePct = ru.CostMonth / userCost * 100
		}
		if st.RiskUserCostCNY > 0 && ru.CostMonth >= st.RiskUserCostCNY*3 {
			ru.Level = "critical"
		} else if (st.RiskUserCostCNY > 0 && ru.CostMonth >= st.RiskUserCostCNY) || ru.BalanceCNY < 0 {
			ru.Level = "warning"
		}
		byID[u.ID] = ru
	}

	rep.Summary = RiskSummary{
		MonthCostCNY: monthCost, MRRCNY: mrr, TodayCostCNY: stats.Today.CostCNY,
	}
	if mrr > 0 {
		rep.Summary.MarginPct = (mrr - monthCost) / mrr * 100
	}
	if st.RiskUserCostCNY > 0 {
		for _, ru := range byID {
			if ru.CostMonth >= st.RiskUserCostCNY {
				rep.Summary.UsersOverThreshold++
			}
		}
	}

	now := time.Now()
	add := func(al RiskAlert) {
		al.CreatedAt = now
		switch al.Level {
		case "critical":
			rep.Summary.Critical++
		case "warning":
			rep.Summary.Warning++
		default:
			rep.Summary.Info++
		}
		rep.Alerts = append(rep.Alerts, al)
	}

	// 1) 整体毛利
	switch {
	case mrr <= 0 && monthCost > 0:
		add(RiskAlert{
			Level: "critical", Kind: "margin", Title: "有成本、无订阅收入",
			Detail: fmt.Sprintf("本月上游成本 %s，MRR 为 0。免费用户或未设置套餐的用户正在消耗上游额度。", moneyText(monthCost)),
			Value:  monthCost, Page: "billing",
		})
	case mrr > 0 && rep.Summary.MarginPct < st.RiskMarginPct-20:
		add(RiskAlert{
			Level: "critical", Kind: "margin", Title: "毛利率严重低于阈值",
			Detail: fmt.Sprintf("本月毛利率 %.1f%%，低于阈值 %.1f%%（MRR %s / 成本 %s）。", rep.Summary.MarginPct, st.RiskMarginPct, moneyText(mrr), moneyText(monthCost)),
			Value:  rep.Summary.MarginPct, Page: "billing",
		})
	case mrr > 0 && rep.Summary.MarginPct < st.RiskMarginPct:
		add(RiskAlert{
			Level: "warning", Kind: "margin", Title: "毛利率低于阈值",
			Detail: fmt.Sprintf("本月毛利率 %.1f%%，低于阈值 %.1f%%（MRR %s / 成本 %s）。", rep.Summary.MarginPct, st.RiskMarginPct, moneyText(mrr), moneyText(monthCost)),
			Value:  rep.Summary.MarginPct, Page: "billing",
		})
	}

	// 2) 单日支出
	if st.RiskDailySpendCNY > 0 && stats.Today.CostCNY >= st.RiskDailySpendCNY {
		add(RiskAlert{
			Level: "warning", Kind: "daily_spend", Title: "今日上游支出超过阈值",
			Detail: fmt.Sprintf("今日成本 %s，阈值 %s。若是异常调用，先看成本页的最近用量和模型构成。", moneyText(stats.Today.CostCNY), moneyText(st.RiskDailySpendCNY)),
			Value:  stats.Today.CostCNY, Page: "billing",
		})
	}

	// 3) 高成本用户与集中度
	if st.RiskUserCostCNY > 0 {
		high := make([]*RiskUser, 0)
		for _, ru := range byID {
			if ru.CostMonth >= st.RiskUserCostCNY {
				high = append(high, ru)
			}
		}
		sort.Slice(high, func(i, j int) bool { return high[i].CostMonth > high[j].CostMonth })
		for i, ru := range high {
			if i >= 10 {
				break
			}
			lvl := "warning"
			if ru.CostMonth >= st.RiskUserCostCNY*3 {
				lvl = "critical"
			}
			add(RiskAlert{
				Level: lvl, Kind: "user_cost", Title: "高成本用户：" + displayName(ru),
				Detail: fmt.Sprintf("本月成本 %s（阈值 %s），套餐 %s，当前余额 %s。", moneyText(ru.CostMonth), moneyText(st.RiskUserCostCNY), planLabel(ru), moneyText(ru.BalanceCNY)),
				UserID: ru.UserID, UserName: ru.Name, Value: ru.CostMonth, Page: "users",
			})
		}
		if userCost > 0 {
			for _, ru := range byID {
				if ru.CostMonth > 0 && ru.SharePct >= 50 {
					add(RiskAlert{
						Level: "warning", Kind: "share", Title: "成本集中在单个用户：" + displayName(ru),
						Detail: fmt.Sprintf("该用户占本月用户成本的 %.1f%%（%s）。单点波动会直接影响整体毛利。", ru.SharePct, moneyText(ru.CostMonth)),
						UserID: ru.UserID, UserName: ru.Name, Value: ru.SharePct, Page: "users",
					})
				}
			}
		}
	}

	// 4) 负余额（欠费）
	var debt float64
	for _, ru := range byID {
		if ru.BalanceCNY < 0 {
			rep.Summary.NegativeBalances++
			debt += -ru.BalanceCNY
		}
	}
	rep.Summary.DebtCNY = debt
	if rep.Summary.NegativeBalances > 0 {
		add(RiskAlert{
			Level: "warning", Kind: "balance", Title: "有用户余额为负",
			Detail: fmt.Sprintf("%d 个用户合计欠费 %s。余额不足拦截目前%s。", rep.Summary.NegativeBalances, moneyText(debt), map[bool]string{true: "已开启", false: "未开启"}[st.EnforceBalance]),
			Value:  debt, Page: "wallet",
		})
	}

	// 5) 错误率
	if st.RiskErrorRatePct > 0 {
		m := a.gw.Metrics()
		if m.Requests > 0 {
			ratePct := float64(m.Errors) / float64(m.Requests) * 100
			if ratePct >= st.RiskErrorRatePct {
				add(RiskAlert{
					Level: "warning", Kind: "error_rate", Title: "上游错误率偏高",
					Detail: fmt.Sprintf("累计错误率 %.1f%%（%d / %d），阈值 %.1f%%。", ratePct, m.Errors, m.Requests, st.RiskErrorRatePct),
					Value:  ratePct, Page: "performance",
				})
			}
		}
	}

	// 6) 上游熔断与模型可用性
	m := a.gw.Metrics()
	open := 0
	for _, p := range m.Providers {
		if p.State == "open" {
			open++
		}
	}
	if open > 0 {
		add(RiskAlert{
			Level: "critical", Kind: "provider", Title: "上游熔断",
			Detail: fmt.Sprintf("%d 个上游处于熔断状态，请求会自动避开它们；若全部熔断，对话将失败。", open),
			Value:  float64(open), Page: "upstreams",
		})
	}
	enabled := 0
	for _, mod := range a.gw.Models().List() {
		if mod.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		add(RiskAlert{
			Level: "critical", Kind: "models", Title: "没有启用的模型",
			Detail: "所有对话与任务请求都会失败（no_provider）。",
			Page:   "models",
		})
	}

	sort.SliceStable(rep.Alerts, func(i, j int) bool {
		rank := func(l string) int {
			switch l {
			case "critical":
				return 0
			case "warning":
				return 1
			}
			return 2
		}
		if rank(rep.Alerts[i].Level) != rank(rep.Alerts[j].Level) {
			return rank(rep.Alerts[i].Level) < rank(rep.Alerts[j].Level)
		}
		return rep.Alerts[i].Kind < rep.Alerts[j].Kind
	})

	all := make([]RiskUser, 0, len(byID))
	for _, ru := range byID {
		all = append(all, *ru)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CostMonth != all[j].CostMonth {
			return all[i].CostMonth > all[j].CostMonth
		}
		return all[i].BalanceCNY < all[j].BalanceCNY
	})
	if len(all) > 50 {
		all = all[:50]
	}
	rep.Users = all
	return rep
}

// domainPlan 只取风控需要的套餐字段，避免和 Plans() 返回值耦合。
type domainPlan struct {
	Name  string
	Price float64
	Quota int
}

// RiskForUser 单个用户的成本、余额与额度，用户详情页用。
func (a *Admin) RiskForUser(id string) (RiskUser, bool) {
	u, ok := a.store.GetUser(id)
	if !ok {
		return RiskUser{}, false
	}
	uu := a.UserBilling(id)
	ru := RiskUser{
		UserID: u.ID, Name: u.Name, Plan: u.Plan, Status: u.Status,
		CostMonth: uu.Month.CostCNY, CostToday: uu.Today.CostCNY,
		Requests: uu.Month.Requests, Tokens: uu.Month.TotalTokens(),
		BalanceCNY: a.Wallet().Balance(id), Remaining: u.Remaining(),
		Level: "normal",
	}
	if p, ok := a.plans.Get(u.Plan); ok {
		ru.PlanName, ru.PlanPrice, ru.MonthlyQuota = p.Name, p.PriceMonthly, p.MonthlyQuota
	}
	return ru, true
}

func displayName(ru *RiskUser) string {
	if ru.Name != "" {
		return ru.Name
	}
	return ru.UserID
}

func planLabel(ru *RiskUser) string {
	if ru.PlanName != "" {
		return ru.PlanName
	}
	return ru.Plan
}

func moneyText(v float64) string {
	return fmt.Sprintf("¥%.2f", v)
}
