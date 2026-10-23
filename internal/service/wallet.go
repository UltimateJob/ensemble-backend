package service

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// 钱包账本。这里记录的是「用户的钱」：
//   - 余额（人民币）
//   - 充值 / 调整 / 兑换 / 按量消费流水
//   - 兑换码
//   - 订单（目前只有人工充值与兑换码入账，不接支付通道）
//
// 上游成本在 service.Billing；钱包是面向用户的负债，两者不要混在一起。

var (
	ErrWalletAmount   = errors.New("金额不正确")
	ErrCodeNotFound   = errors.New("兑换码不存在")
	ErrCodeDisabled   = errors.New("兑换码已停用")
	ErrCodeExpired    = errors.New("兑换码已过期")
	ErrCodeUsedUp     = errors.New("兑换码已用完")
	ErrCodeUsedByUser = errors.New("你已经使用过这个兑换码")
)

var codePattern = regexp.MustCompile(`^[A-Z0-9-]{4,32}$`)

// WalletAccount 单个用户的钱包。
type WalletAccount struct {
	UserID     string    `json:"userId"`
	BalanceCNY float64   `json:"balanceCny"`
	TotalTopup float64   `json:"totalTopup"`
	TotalSpent float64   `json:"totalSpent"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// WalletTxn 一条资金流水。AmountCNY 有符号：充值为正、消费为负。
type WalletTxn struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	Kind         string    `json:"kind"` // topup | adjust | redeem | usage
	AmountCNY    float64   `json:"amountCny"`
	BalanceAfter float64   `json:"balanceAfter"`
	Note         string    `json:"note,omitempty"`
	Actor        string    `json:"actor,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// RedeemCode 兑换码。可充值，也可以附带一个套餐。
type RedeemCode struct {
	Code      string    `json:"code"`
	AmountCNY float64   `json:"amountCny"`
	Plan      string    `json:"plan,omitempty"`
	MaxUses   int       `json:"maxUses"`
	Uses      int       `json:"uses"`
	Users     []string  `json:"users,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	Disabled  bool      `json:"disabled"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Status 给管理台显示的状态。
func (c RedeemCode) Status(now time.Time) string {
	switch {
	case c.Disabled:
		return "disabled"
	case !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt):
		return "expired"
	case c.MaxUses > 0 && c.Uses >= c.MaxUses:
		return "used"
	default:
		return "active"
	}
}

// WalletOrder 一笔入账订单。没有接支付通道时由管理员补录。
type WalletOrder struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Kind      string    `json:"kind"` // topup | adjust | redeem
	AmountCNY float64   `json:"amountCny"`
	Plan      string    `json:"plan,omitempty"`
	Channel   string    `json:"channel"` // manual | admin | redeem
	Note      string    `json:"note,omitempty"`
	Actor     string    `json:"actor,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// WalletStats 管理台首页的汇总。
type WalletStats struct {
	Accounts         int             `json:"accounts"`
	BalanceCNY       float64         `json:"balanceCny"`
	TotalTopupCNY    float64         `json:"totalTopupCny"`
	TotalSpentCNY    float64         `json:"totalSpentCny"`
	NegativeAccounts int             `json:"negativeAccounts"`
	Codes            int             `json:"codes"`
	ActiveCodes      int             `json:"activeCodes"`
	TotalUses        int             `json:"totalUses"`
	Orders           int             `json:"orders"`
	TopAccounts      []WalletAccount `json:"topAccounts"`
	RecentOrders     []WalletOrder   `json:"recentOrders"`
}

// RedeemResult 兑换结果。
type RedeemResult struct {
	Code       string  `json:"code"`
	AmountCNY  float64 `json:"amountCny"`
	Plan       string  `json:"plan,omitempty"`
	BalanceCNY float64 `json:"balanceCny"`
}

type walletSnapshot struct {
	Version  int                       `json:"version"`
	Accounts map[string]*WalletAccount `json:"accounts,omitempty"`
	Txns     []WalletTxn               `json:"txns,omitempty"`
	Codes    map[string]*RedeemCode    `json:"codes,omitempty"`
	Orders   []WalletOrder             `json:"orders,omitempty"`
}

const (
	walletTxnLimit   = 2000
	walletOrderLimit = 2000
)

// Wallet 钱包账本。每次变更同步原子落盘：资金数据不能靠后台定时器，
// 进程被杀时已经发生的充值必须已经在磁盘上。
type Wallet struct {
	mu     sync.RWMutex
	path   string
	acct   map[string]*WalletAccount
	txns   []WalletTxn
	codes  map[string]*RedeemCode
	orders []WalletOrder
}

// OpenWallet 打开钱包账本。坏文件隔离后按空账本启动——钱包数据是运营账，
// 不该像用户数据一样阻断服务；但会在日志里留下显眼的记录。
func OpenWallet(path string) (*Wallet, error) {
	w := &Wallet{
		path:  path,
		acct:  make(map[string]*WalletAccount),
		codes: make(map[string]*RedeemCode),
	}
	if path == "" {
		return w, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return w, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return w, nil
	}
	var snap walletSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		quarantine := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		_ = os.Rename(path, quarantine)
		logx.L().Warn("钱包账本损坏，已隔离并按空账本启动",
			"file", path, "隔离为", quarantine, "err", err.Error())
		return w, nil
	}
	if snap.Accounts != nil {
		w.acct = snap.Accounts
	}
	if snap.Codes != nil {
		w.codes = snap.Codes
	}
	w.txns = snap.Txns
	w.orders = snap.Orders
	return w, nil
}

// Balance 当前余额。
func (w *Wallet) Balance(userID string) float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if a := w.acct[userID]; a != nil {
		return a.BalanceCNY
	}
	return 0
}

// Account 单个账户快照。
func (w *Wallet) Account(userID string) WalletAccount {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if a := w.acct[userID]; a != nil {
		return *a
	}
	return WalletAccount{UserID: userID}
}

// TopUp 人工充值。同时生成一笔订单。
func (w *Wallet) TopUp(userID string, amount float64, note, actor string) (WalletTxn, error) {
	if err := validAmount(amount, true); err != nil {
		return WalletTxn{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	a := w.accountLocked(userID, now)
	a.BalanceCNY = roundMoney(a.BalanceCNY + amount)
	a.TotalTopup = roundMoney(a.TotalTopup + amount)
	a.UpdatedAt = now
	txn := WalletTxn{
		ID: store.NewID("wt"), UserID: userID, Kind: "topup",
		AmountCNY: roundMoney(amount), BalanceAfter: a.BalanceCNY,
		Note: clip(note, 200), Actor: actor, CreatedAt: now,
	}
	w.appendTxnLocked(txn)
	w.orders = append(w.orders, WalletOrder{
		ID: store.NewID("wo"), UserID: userID, Kind: "topup",
		AmountCNY: txn.AmountCNY, Channel: "manual",
		Note: txn.Note, Actor: actor, CreatedAt: now,
	})
	w.trimOrdersLocked()
	return txn, w.saveLocked()
}

// Adjust 人工调整（可正可负），不生成订单。
func (w *Wallet) Adjust(userID string, amount float64, note, actor string) (WalletTxn, error) {
	if err := validAmount(amount, false); err != nil {
		return WalletTxn{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	a := w.accountLocked(userID, now)
	a.BalanceCNY = roundMoney(a.BalanceCNY + amount)
	if amount > 0 {
		a.TotalTopup = roundMoney(a.TotalTopup + amount)
	}
	a.UpdatedAt = now
	txn := WalletTxn{
		ID: store.NewID("wt"), UserID: userID, Kind: "adjust",
		AmountCNY: roundMoney(amount), BalanceAfter: a.BalanceCNY,
		Note: clip(note, 200), Actor: actor, CreatedAt: now,
	}
	w.appendTxnLocked(txn)
	return txn, w.saveLocked()
}

// Spend 按量消费。金额写负数流水；允许余额变成负数，
// 是「欠费」而不是拒绝记账——是否拦截由 EnforceBalance 决定。
func (w *Wallet) Spend(userID string, amount float64, note string) {
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	a := w.accountLocked(userID, now)
	a.BalanceCNY = roundMoney(a.BalanceCNY - amount)
	a.TotalSpent = roundMoney(a.TotalSpent + amount)
	a.UpdatedAt = now
	w.appendTxnLocked(WalletTxn{
		ID: store.NewID("wt"), UserID: userID, Kind: "usage",
		AmountCNY: -roundMoney(amount), BalanceAfter: a.BalanceCNY,
		Note: clip(note, 200), Actor: "system", CreatedAt: now,
	})
	if err := w.saveLocked(); err != nil {
		logx.L().Warn("钱包流水落盘失败", "err", err.Error())
	}
}

// CreateCode 新建兑换码。Code 为空时自动生成。
func (w *Wallet) CreateCode(in RedeemCode) (RedeemCode, error) {
	in.Code = normalizeCode(in.Code)
	if in.Code == "" {
		in.Code = newRedeemCode()
	}
	if !codePattern.MatchString(in.Code) {
		return RedeemCode{}, errors.New("兑换码只能包含大写字母、数字和短横线，长度 4–32")
	}
	if math.IsNaN(in.AmountCNY) || math.IsInf(in.AmountCNY, 0) || in.AmountCNY < 0 || in.AmountCNY > 10000000 {
		return RedeemCode{}, errors.New("兑换码金额需在 0–1000 万之间（0 表示只送套餐）")
	}
	in.Plan = strings.TrimSpace(in.Plan)
	in.Note = clip(in.Note, 200)
	if in.MaxUses <= 0 {
		in.MaxUses = 1
	}
	if in.MaxUses > 100000 {
		return RedeemCode{}, errors.New("兑换码使用次数不能超过 100000")
	}
	if in.AmountCNY == 0 && in.Plan == "" {
		return RedeemCode{}, errors.New("兑换码至少要有金额或套餐")
	}
	now := time.Now()
	if !in.ExpiresAt.IsZero() && in.ExpiresAt.Before(now) {
		return RedeemCode{}, errors.New("过期时间必须晚于现在")
	}
	in.Uses = 0
	in.Users = nil
	in.Disabled = false
	in.CreatedAt = now

	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.codes[in.Code]; exists {
		return RedeemCode{}, errors.New("兑换码已存在")
	}
	cp := in
	w.codes[in.Code] = &cp
	return cp, w.saveLocked()
}

// ListCodes 全部兑换码，按创建时间倒序。
func (w *Wallet) ListCodes() []RedeemCode {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]RedeemCode, 0, len(w.codes))
	for _, c := range w.codes {
		cp := *c
		cp.Users = append([]string(nil), c.Users...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// DeleteCode 删除兑换码。已经兑换过的不允许删，保留账目可追溯。
func (w *Wallet) DeleteCode(code string) error {
	code = normalizeCode(code)
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.codes[code]
	if !ok {
		return ErrCodeNotFound
	}
	if c.Uses > 0 {
		return errors.New("该兑换码已被使用，不能删除；可以停用")
	}
	delete(w.codes, code)
	return w.saveLocked()
}

// SetCodeDisabled 停用/启用兑换码。
func (w *Wallet) SetCodeDisabled(code string, disabled bool) (RedeemCode, error) {
	code = normalizeCode(code)
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.codes[code]
	if !ok {
		return RedeemCode{}, ErrCodeNotFound
	}
	c.Disabled = disabled
	return *c, w.saveLocked()
}

// Redeem 用户兑换。每个用户对同一个码只能兑换一次。
func (w *Wallet) Redeem(userID, code string) (RedeemResult, error) {
	code = normalizeCode(code)
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.codes[code]
	if !ok {
		return RedeemResult{}, ErrCodeNotFound
	}
	if c.Disabled {
		return RedeemResult{}, ErrCodeDisabled
	}
	if !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt) {
		return RedeemResult{}, ErrCodeExpired
	}
	// 先判「本人是否用过」，再判总量：本人重复兑换时给出更准确的原因。
	for _, id := range c.Users {
		if id == userID {
			return RedeemResult{}, ErrCodeUsedByUser
		}
	}
	if c.MaxUses > 0 && c.Uses >= c.MaxUses {
		return RedeemResult{}, ErrCodeUsedUp
	}

	a := w.accountLocked(userID, now)
	if c.AmountCNY > 0 {
		a.BalanceCNY = roundMoney(a.BalanceCNY + c.AmountCNY)
		a.TotalTopup = roundMoney(a.TotalTopup + c.AmountCNY)
	}
	a.UpdatedAt = now
	c.Uses++
	c.Users = append(c.Users, userID)
	if len(c.Users) > 10000 {
		c.Users = c.Users[len(c.Users)-10000:]
	}
	if c.AmountCNY > 0 {
		w.appendTxnLocked(WalletTxn{
			ID: store.NewID("wt"), UserID: userID, Kind: "redeem",
			AmountCNY: roundMoney(c.AmountCNY), BalanceAfter: a.BalanceCNY,
			Note: "兑换码 " + code, Actor: userID, CreatedAt: now,
		})
	}
	w.orders = append(w.orders, WalletOrder{
		ID: store.NewID("wo"), UserID: userID, Kind: "redeem",
		AmountCNY: roundMoney(c.AmountCNY), Plan: c.Plan, Channel: "redeem",
		Note: "兑换码 " + code, Actor: userID, CreatedAt: now,
	})
	w.trimOrdersLocked()
	res := RedeemResult{
		Code: code, AmountCNY: roundMoney(c.AmountCNY), Plan: c.Plan,
		BalanceCNY: a.BalanceCNY,
	}
	return res, w.saveLocked()
}

// Txns 用户的资金流水，最新在前。
func (w *Wallet) Txns(userID string, limit int) []WalletTxn {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := make([]WalletTxn, 0, limit)
	for i := len(w.txns) - 1; i >= 0 && len(out) < limit; i-- {
		if t := w.txns[i]; t.UserID == userID {
			out = append(out, t)
		}
	}
	return out
}

// Orders 最近的订单，最新在前；userID 为空表示全部。
func (w *Wallet) Orders(userID string, limit int) []WalletOrder {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := make([]WalletOrder, 0, limit)
	for i := len(w.orders) - 1; i >= 0 && len(out) < limit; i-- {
		if o := w.orders[i]; userID == "" || o.UserID == userID {
			out = append(out, o)
		}
	}
	return out
}

// Stats 管理台汇总。
func (w *Wallet) Stats() WalletStats {
	now := time.Now()
	w.mu.RLock()
	defer w.mu.RUnlock()
	st := WalletStats{Accounts: len(w.acct)}
	accts := make([]WalletAccount, 0, len(w.acct))
	for _, a := range w.acct {
		st.BalanceCNY += a.BalanceCNY
		st.TotalTopupCNY += a.TotalTopup
		st.TotalSpentCNY += a.TotalSpent
		if a.BalanceCNY < 0 {
			st.NegativeAccounts++
		}
		accts = append(accts, *a)
	}
	st.BalanceCNY = roundMoney(st.BalanceCNY)
	st.TotalTopupCNY = roundMoney(st.TotalTopupCNY)
	st.TotalSpentCNY = roundMoney(st.TotalSpentCNY)
	sort.Slice(accts, func(i, j int) bool { return accts[i].BalanceCNY > accts[j].BalanceCNY })
	if len(accts) > 20 {
		accts = accts[:20]
	}
	st.TopAccounts = accts
	st.Codes = len(w.codes)
	for _, c := range w.codes {
		if c.Status(now) == "active" {
			st.ActiveCodes++
		}
		st.TotalUses += c.Uses
	}
	st.Orders = len(w.orders)
	start := len(w.orders) - 100
	if start < 0 {
		start = 0
	}
	for i := len(w.orders) - 1; i >= start; i-- {
		st.RecentOrders = append(st.RecentOrders, w.orders[i])
	}
	return st
}

func (w *Wallet) accountLocked(userID string, now time.Time) *WalletAccount {
	a := w.acct[userID]
	if a == nil {
		a = &WalletAccount{UserID: userID}
		w.acct[userID] = a
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}
	return a
}

func (w *Wallet) appendTxnLocked(t WalletTxn) {
	w.txns = append(w.txns, t)
	if len(w.txns) > walletTxnLimit {
		w.txns = w.txns[len(w.txns)-walletTxnLimit:]
	}
}

func (w *Wallet) trimOrdersLocked() {
	if len(w.orders) > walletOrderLimit {
		w.orders = w.orders[len(w.orders)-walletOrderLimit:]
	}
}

func (w *Wallet) saveLocked() error {
	if w.path == "" {
		return nil
	}
	snap := walletSnapshot{
		Version:  1,
		Accounts: make(map[string]*WalletAccount, len(w.acct)),
		Txns:     append([]WalletTxn(nil), w.txns...),
		Codes:    make(map[string]*RedeemCode, len(w.codes)),
		Orders:   append([]WalletOrder(nil), w.orders...),
	}
	for k, v := range w.acct {
		cp := *v
		snap.Accounts[k] = &cp
	}
	for k, v := range w.codes {
		cp := *v
		cp.Users = append([]string(nil), v.Users...)
		snap.Codes[k] = &cp
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, w.path)
}

func validAmount(v float64, positive bool) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ErrWalletAmount
	}
	if positive && v <= 0 {
		return errors.New("金额必须大于 0")
	}
	if !positive && v == 0 {
		return errors.New("调整金额不能为 0")
	}
	if math.Abs(v) > 10000000 {
		return errors.New("单笔金额不能超过 1000 万")
	}
	return nil
}

func roundMoney(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func normalizeCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func newRedeemCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// fallback：不可能发生，但别让接口崩
		return fmt.Sprintf("TW%d", time.Now().UnixNano()%1000000000000)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "TW" + string(b)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > n {
		return string([]rune(s)[:n])
	}
	return s
}
