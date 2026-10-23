package domain

import "time"

// Device 已配对到某个账号的远程设备（手机/平板/另一台电脑）。
//
// 配对是「电脑端生成码 → 手机输码/扫码 → 拿到设备令牌」的流程。
// 设备令牌与账号令牌分开签发：吊销某台设备不会把账号踢下线，
// 而账号被停用时所有设备一起失效（Verify 会查用户状态）。
type Device struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	Name     string `json:"name"`     // 用户可见的名字，如「iPhone 15」
	Platform string `json:"platform"` // ios | android | web | desktop
	// TokenHash 设备令牌的 SHA-256。库里不存明文令牌——
	// 数据文件泄露时，攻击者拿不到可直接使用的凭证。
	TokenHash string `json:"tokenHash"`
	// LastSeenIP 只做展示与异常排查，不作为鉴权依据。
	LastSeenIP string    `json:"lastSeenIp,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeen   time.Time `json:"lastSeen"`
	// RevokedAt 非零表示已吊销。吊销后 Verify 直接拒绝，
	// 记录保留一段时间便于审计「谁在什么时候踢掉了哪台设备」。
	RevokedAt time.Time `json:"revokedAt"`
	// RequireApproval 手机端是否强制审批。
	// 默认 true（远程操作先问一句）；用户可在手机上关掉——
	// 这是用户的决定，服务端不替他做主，只在关掉时记录审计。
	RequireApproval bool `json:"requireApproval"`
}

// Active 设备当前是否可用。
func (d *Device) Active(now time.Time) bool {
	return d.RevokedAt.IsZero() && !now.IsZero()
}

// PairCode 一次性配对码。
//
// 存在内存里而不是落盘：5 分钟有效、单次使用，
// 重启丢失的代价只是「重新生成一个」，比把它写进数据文件小得多。
type PairCode struct {
	Code   string
	UserID string
	Exp    time.Time
	// Tries 已尝试次数。6 位码空间只有一百万，
	// 不限次就能被离线穷举；配合 IP 限流双重约束。
	Tries int
}
