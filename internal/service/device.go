package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

var (
	ErrBadPairCode  = errors.New("配对码不正确或已过期")
	ErrDeviceGone   = errors.New("这台设备已被移除，请重新配对")
	ErrTooManyCodes = errors.New("待配对的码太多，等一会儿再试")
)

const (
	// pairCodeTTL 配对码有效期。手机就在手边，5 分钟足够；
	// 再长就只是给暴力枚举留窗口。
	pairCodeTTL = 5 * time.Minute
	// pairCodeMaxTries 单个码最多试几次。6 位数字空间一百万，
	// 配合「按 IP 限流」后，3 次足够挡住手滑，也让枚举不划算。
	pairCodeMaxTries = 5
	// pairCodePerUser 同一用户同时最多几个待配对码。
	// 防止「生成一堆码留着自己慢慢试」。
	pairCodePerUser = 3
)

// Devices 设备配对与远程访问。
//
// 设计要点：
//   - 配对码在内存里（一次性、短命），设备记录落盘；
//   - 设备令牌与账号令牌分开签发，吊销互不影响；
//   - 库里只存令牌的 SHA-256，泄露数据文件拿不到可用凭证。
type Devices struct {
	store  store.Store
	secret []byte

	mu    sync.Mutex
	codes map[string]*domain.PairCode // code → 记录
	now   func() time.Time
}

func NewDevices(s store.Store, secret string) *Devices {
	return &Devices{
		store:  s,
		secret: []byte(secret),
		codes:  make(map[string]*domain.PairCode),
		now:    time.Now,
	}
}

// IssueCode 为某个用户生成一次性配对码（电脑端调用，仅 loopback）。
func (d *Devices) IssueCode(userID string) (code string, expires time.Time, err error) {
	now := d.now()

	d.mu.Lock()
	defer d.mu.Unlock()

	// 顺手清理过期码，并统计该用户还有几个未用
	pending := 0
	for k, v := range d.codes {
		if now.After(v.Exp) {
			delete(d.codes, k)
			continue
		}
		if v.UserID == userID {
			pending++
		}
	}
	if pending >= pairCodePerUser {
		return "", time.Time{}, ErrTooManyCodes
	}

	code, err = randomDigits(6)
	if err != nil {
		return "", time.Time{}, err
	}
	// 极小概率撞上别人未过期的码：重抽一次即可，不必循环
	if _, exists := d.codes[code]; exists {
		code, err = randomDigits(6)
		if err != nil {
			return "", time.Time{}, err
		}
	}

	exp := now.Add(pairCodeTTL)
	d.codes[code] = &domain.PairCode{Code: code, UserID: userID, Exp: exp}
	return code, exp, nil
}

// Pair 用配对码换设备令牌。
//
// 失败原因对外只说「码不正确或已过期」——区分「没这个码」和
// 「码过期了」等于告诉枚举者哪一位猜对了。
func (d *Devices) Pair(code, name, platform string) (*domain.Device, string, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "我的设备"
	}
	if platform != "ios" && platform != "android" && platform != "web" && platform != "desktop" {
		platform = "ios"
	}

	now := d.now()
	d.mu.Lock()
	pc, ok := d.codes[code]
	if !ok || now.After(pc.Exp) {
		delete(d.codes, code)
		d.mu.Unlock()
		return nil, "", ErrBadPairCode
	}
	pc.Tries++
	if pc.Tries > pairCodeMaxTries {
		delete(d.codes, code) // 试满了就作废，逼对方重新生成
		d.mu.Unlock()
		return nil, "", ErrBadPairCode
	}
	// 配对成功即消费，一次性
	delete(d.codes, code)
	userID := pc.UserID
	d.mu.Unlock()

	raw, hash, err := d.newToken()
	if err != nil {
		return nil, "", err
	}
	dev := &domain.Device{
		UserID:          userID,
		Name:            trimRunes(name, 40),
		Platform:        platform,
		TokenHash:       hash,
		CreatedAt:       now,
		LastSeen:        now,
		RequireApproval: true, // 默认远程操作先问一句，用户可在手机上关
	}
	if err := d.store.CreateDevice(dev); err != nil {
		return nil, "", err
	}
	return dev, raw, nil
}

// VerifyDevice 校验设备令牌，返回设备与其用户。
func (d *Devices) VerifyDevice(token string) (*domain.Device, *domain.User, error) {
	hash := hashToken(token)
	dev, ok := d.store.GetDeviceByTokenHash(hash)
	if !ok {
		return nil, nil, ErrDeviceGone
	}
	if !dev.RevokedAt.IsZero() {
		return nil, nil, ErrDeviceGone
	}
	u, ok := d.store.GetUser(dev.UserID)
	if !ok || u.Status == "disabled" {
		return nil, nil, ErrDisabled
	}
	return dev, u, nil
}

// Touch 记录设备活跃时间。10 分钟粒度足够——
// 每个请求都写盘的话，一部手机刷列表就能把磁盘写满。
func (d *Devices) Touch(dev *domain.Device, ip string) {
	now := d.now()
	if now.Sub(dev.LastSeen) < 10*time.Minute {
		return
	}
	dev.LastSeen = now
	dev.LastSeenIP = ip
	_ = d.store.UpdateDevice(dev)
}

// List 某用户的全部设备（含已吊销，便于展示历史）。
func (d *Devices) List(userID string) []*domain.Device {
	return d.store.ListDevices(userID)
}

// Revoke 吊销一台设备。只能吊销自己的。
func (d *Devices) Revoke(userID, deviceID string) error {
	dev, ok := d.store.GetDevice(deviceID)
	if !ok || dev.UserID != userID {
		return ErrDeviceGone
	}
	if !dev.RevokedAt.IsZero() {
		return nil // 幂等
	}
	dev.RevokedAt = d.now()
	return d.store.UpdateDevice(dev)
}

// SetRequireApproval 开关这台设备是否需要审批。
//
// 这是用户自己的决定：远程操作默认要审批，但他可以关掉。
// 关掉时由 httpapi 层记一条审计——不是阻拦，是让「谁在什么时候
// 放宽了哪台设备」有据可查。
func (d *Devices) SetRequireApproval(userID, deviceID string, require bool) (*domain.Device, error) {
	dev, ok := d.store.GetDevice(deviceID)
	if !ok || dev.UserID != userID {
		return nil, ErrDeviceGone
	}
	dev.RequireApproval = require
	if err := d.store.UpdateDevice(dev); err != nil {
		return nil, err
	}
	return dev, nil
}

func (d *Devices) newToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = "dev_" + hex.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// randomDigits 生成 n 位十进制数字串（含前导零）。
func randomDigits(n int) (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*s", n, v.String()), nil
}
