package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// deviceKey 设备信息的 context key。
type deviceKeyT struct{}

var deviceKey = deviceKeyT{}

// DeviceFrom 取当前请求来自哪台设备。非设备请求（浏览器/桌面端
// 用账号令牌）返回 nil。
func DeviceFrom(ctx context.Context) *domain.Device {
	d, _ := ctx.Value(deviceKey).(*domain.Device)
	return d
}

// handleIssuePairCode 生成配对码（电脑端，仅 loopback）。
//
// 为什么必须限制 loopback：拿到配对码 = 拿到一台能操作本机代码的
// 设备。这个接口如果公网可调，等于把钥匙挂在门上。手机端配对是
// 「在电脑上生成码 → 手机输入」，所以生成这一步天然发生在本地。
func (s *Server) handleIssuePairCode(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if !isLoopback(r.RemoteAddr) {
		forbidden(w)
		return
	}
	if s.devices == nil {
		notFound(w, "未启用设备配对")
		return
	}
	code, exp, err := s.devices.IssueCode(u.ID)
	if err != nil {
		if err == service.ErrTooManyCodes {
			fail(w, 429, "too_many_codes", err.Error())
			return
		}
		serverErr(w, err.Error())
		return
	}
	// host 给手机端拼二维码用：取本机局域网地址，比 127.0.0.1 有用。
	ok(w, map[string]any{
		"code":      code,
		"expiresAt": exp,
		"host":      lanAddr(r.Host),
		"ttl":       int(time.Until(exp).Seconds()),
	})
}

// handlePair 手机端用码换设备令牌（免登录）。
//
// 这个接口不能要求登录——手机首次配对时还没有任何凭证。
// 安全性由「5 分钟 + 单次使用 + 最多 5 次尝试 + 按 IP 限流」保证。
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if s.devices == nil {
		notFound(w, "未启用设备配对")
		return
	}
	// 按 IP 限流，避免拿这个接口穷举 6 位码
	if !s.allow("pair:" + ClientIP(r)) {
		tooManyRequests(w)
		return
	}
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		badRequest(w, "请填配对码")
		return
	}
	dev, token, err := s.devices.Pair(req.Code, req.Name, req.Platform)
	if err != nil {
		if err == service.ErrBadPairCode {
			fail(w, 401, "bad_pair_code", "配对码不正确或已过期")
			return
		}
		serverErr(w, err.Error())
		return
	}
	_ = s.store.AppendAudit(&domain.AuditLog{
		Actor: "device", Action: "device.pair", Target: dev.ID,
		Detail: dev.Name + " (" + dev.Platform + ")",
	})
	ok(w, map[string]any{
		"device": map[string]any{
			"id": dev.ID, "name": dev.Name, "platform": dev.Platform,
			"requireApproval": dev.RequireApproval,
		},
		"token": token,
	})
}

// handleListDevices 我的设备列表。
func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.devices == nil {
		notFound(w, "未启用设备配对")
		return
	}
	list := s.devices.List(u.ID)
	// 不把 TokenHash 发出去
	for _, d := range list {
		d.TokenHash = ""
	}
	ok(w, list)
}

// handleRevokeDevice 吊销设备。
func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.devices == nil {
		notFound(w, "未启用设备配对")
		return
	}
	if err := s.devices.Revoke(u.ID, r.PathValue("id")); err != nil {
		notFound(w, err.Error())
		return
	}
	_ = s.store.AppendAudit(&domain.AuditLog{
		Actor: u.ID, Action: "device.revoke", Target: r.PathValue("id"),
	})
	ok(w, map[string]bool{"revoked": true})
}

// handleDeviceApproval 开关「这台设备是否需要审批」。
//
// 用户的决定，不是服务端强制。关闭会记一条审计——
// 不是为了拦，是为了事后能查「谁在什么时候放宽了哪台设备」。
func (s *Server) handleDeviceApproval(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.devices == nil {
		notFound(w, "未启用设备配对")
		return
	}
	var req struct {
		RequireApproval *bool `json:"requireApproval"`
	}
	if err := decode(r, &req); err != nil || req.RequireApproval == nil {
		badRequest(w, "需要 requireApproval 字段")
		return
	}
	dev, err := s.devices.SetRequireApproval(u.ID, r.PathValue("id"), *req.RequireApproval)
	if err != nil {
		notFound(w, err.Error())
		return
	}
	action := "device.approval.on"
	if !*req.RequireApproval {
		action = "device.approval.off"
	}
	_ = s.store.AppendAudit(&domain.AuditLog{
		Actor: u.ID, Action: action, Target: dev.ID, Detail: dev.Name,
	})
	dev.TokenHash = ""
	ok(w, dev)
}

// lanAddr 从 Host 头推出局域网可访问的地址。
//
// 手机连的是电脑的局域网 IP，而 Host 可能是 localhost:8080——
// 那种情况直接用不了，退回本机第一个非回环 IPv4。
func lanAddr(host string) string {
	h, port, err := net.SplitHostPort(host)
	if err == nil && h != "" && h != "localhost" && h != "127.0.0.1" && h != "::1" {
		return host
	}
	ip := firstLANIP()
	if ip == "" {
		return host
	}
	if err != nil || port == "" {
		port = "8080"
	}
	return net.JoinHostPort(ip, port)
}

func firstLANIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			if v4 := ipn.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}
