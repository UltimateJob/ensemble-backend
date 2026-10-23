// Package service 承载业务用例，不涉及 HTTP 细节。
package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

var (
	ErrInvalidCode     = errors.New("验证码不正确或已过期")
	ErrCodeTooFrequent = errors.New("验证码发得太频繁了，稍等一分钟")
	ErrDisabled        = errors.New("账号已被停用")
	ErrBadToken        = errors.New("会话无效，请重新登录")

	// 密码登录
	ErrAccountNotFound = errors.New("账号不存在")
	ErrPasswordNotSet  = errors.New("该账号尚未设置密码，请用验证码登录")
	ErrWrongPassword   = errors.New("账号或密码不正确")
	ErrOldPassword     = errors.New("原密码不正确")
	ErrWeakPassword    = errors.New("密码长度需在 8 到 72 个字符之间")
)

const (
	// codeTTL 验证码有效期。
	codeTTL = 5 * time.Minute
	// codeMaxTries 每个验证码最多尝试次数：6 位码不能让人无限猜，
	// 试满就直接作废，想登只能重新发。
	codeMaxTries = 3
	// codeSendGap 同一目标两次发码的最小间隔，防短信轰炸。
	codeSendGap = time.Minute
)

// Auth 登录与会话。
type Auth struct {
	store  store.Store
	secret []byte
	ttl    time.Duration
	sender CodeSender

	// bypass 为真时，演示账号可以跳过验证码（本地用）
	bypass bool

	mu    sync.Mutex
	codes map[string]codeEntry
	// lastSent 同一目标上次发码时间，做 60 秒节流
	lastSent map[string]time.Time
	// now 时间钩子，测试里替换成可控时钟；生产恒为 time.Now
	now func() time.Time

	// 密码凭据单独存一个文件，不进 domain.User。
	// /v1/me、后台用户列表、登录响应都会直接序列化 User，
	// 散列如果挂在 User 上必然泄漏。
	credPath string
	credMu   sync.Mutex
	creds    map[string]string // userID -> 编码后的密码散列
}

type codeEntry struct {
	code string
	exp  time.Time
	// tries 已猜错次数，到上限作废
	tries int
}

func NewAuth(s store.Store, secret string, sender CodeSender, bypass bool) *Auth {
	if sender == nil {
		sender = LogSender{}
	}
	return &Auth{
		store:    s,
		secret:   []byte(secret),
		ttl:      7 * 24 * time.Hour,
		sender:   sender,
		bypass:   bypass,
		codes:    make(map[string]codeEntry),
		lastSent: make(map[string]time.Time),
		now:      time.Now,
		creds:    make(map[string]string),
	}
}

// SendCode 生成并发送验证码。
// 返回的 devCode 只有在内置 LogSender 下非空，真短信通道返回空字符串。
func (a *Auth) SendCode(ctx context.Context, target string) (string, error) {
	now := a.now()
	a.mu.Lock()
	// 同一目标 60 秒只能发一次。演示账号在 bypass 下不受限，保证演示路径无感。
	if !a.isDemoTarget(target) {
		if t, ok := a.lastSent[target]; ok && now.Sub(t) < codeSendGap {
			a.mu.Unlock()
			return "", ErrCodeTooFrequent
		}
	}
	// 先占位再发送：不然并发两个请求都能过上面的检查，连发两条
	a.lastSent[target] = now
	// 顺带清扫过期条目，map 不能只增不减
	a.gcCodesLocked(now)
	a.mu.Unlock()

	code := randomCode()
	devCode, err := a.sender.Send(ctx, target, code)
	if err != nil {
		// 发送失败：撤掉占位，用户可以立刻重试
		a.mu.Lock()
		delete(a.lastSent, target)
		a.mu.Unlock()
		return "", err
	}

	a.mu.Lock()
	a.codes[target] = codeEntry{code: code, exp: now.Add(codeTTL)}
	a.mu.Unlock()
	return devCode, nil
}

// Login 手机号或邮箱登录，不存在则自动建号。
func (a *Auth) Login(kind, target, code, name string) (*domain.User, string, error) {
	// 演示账号可跳过验证码（由配置控制，生产应关掉）
	demo := a.isDemoTarget(target)
	if !demo {
		if err := a.checkCode(target, code); err != nil {
			return nil, "", err
		}
	}

	var (
		u   *domain.User
		ok2 bool
	)
	switch kind {
	case "phone":
		u, ok2 = a.store.GetUserByPhone(target)
	case "email":
		u, ok2 = a.store.GetUserByEmail(target)
	default:
		return nil, "", errors.New("不支持的登录方式")
	}

	if !ok2 {
		u = &domain.User{
			Phone:  ptrIf(kind == "phone", target),
			Email:  ptrIf(kind == "email", target),
			Name:   firstOr(name, defaultName(target)),
			Plan:   "free",
			Quota:  defaultQuota(),
			Role:   "user",
			Status: "active",
		}
		if err := a.store.CreateUser(u); err != nil {
			return nil, "", err
		}
	}

	if u.Status == "disabled" {
		return nil, "", ErrDisabled
	}

	_ = a.store.TouchLastSeen(u.ID)
	u.LastSeen = time.Now()

	token, err := a.sign(u)
	if err != nil {
		return nil, "", err
	}
	return u, token, nil
}

type claims struct {
	UID string `json:"uid"`
	Exp int64  `json:"exp"`
}

// SetCredentialsPath 指定密码凭据文件并加载已有内容。
// 文件不存在视为空凭据；文件损坏返回错误，但调用方可以选择继续运行
// （此时密码登录一律按「未设置密码」处理，避免把坏文件当成合法凭据）。
func (a *Auth) SetCredentialsPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("密码凭据文件路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	// 先记下路径和空凭据：即使文件损坏，后续设置密码也能覆盖修复，
	// 而密码登录在修复前一律按「未设置密码」处理。
	a.credMu.Lock()
	a.credPath = path
	a.creds = make(map[string]string)
	a.credMu.Unlock()

	creds := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		var loaded map[string]string
		if err := json.Unmarshal(data, &loaded); err != nil {
			return fmt.Errorf("解析密码凭据失败: %w", err)
		}
		for uid, hash := range loaded {
			if uid != "" && hash != "" {
				creds[uid] = hash
			}
		}
	}

	a.credMu.Lock()
	a.creds = creds
	a.credMu.Unlock()
	return nil
}

// HasPassword 该账号是否设置过密码。
func (a *Auth) HasPassword(userID string) bool {
	a.credMu.Lock()
	defer a.credMu.Unlock()
	return a.creds[userID] != ""
}

// SetPassword 设置或修改密码。
// 已设置过密码时必须提供正确的原密码；没设置过则允许直接设置
// （调用方已通过会话鉴权）。
func (a *Auth) SetPassword(userID, oldPassword, newPassword string) error {
	if userID == "" {
		return ErrAccountNotFound
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	a.credMu.Lock()
	defer a.credMu.Unlock()

	if old, exists := a.creds[userID]; exists && old != "" {
		if !verifyPassword(old, oldPassword) {
			return ErrOldPassword
		}
	}

	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	prev, had := a.creds[userID]
	a.creds[userID] = hash
	if err := a.persistCredentialsLocked(); err != nil {
		if had {
			a.creds[userID] = prev
		} else {
			delete(a.creds, userID)
		}
		return err
	}
	return nil
}

// LoginWithPassword 密码登录。账号不存在与密码错误返回同一个错误，
// 避免接口变成「手机号是否注册」的探测器。
func (a *Auth) LoginWithPassword(kind, target, password string) (*domain.User, string, error) {
	u, found := a.findByKind(kind, target)
	if !found {
		return nil, "", ErrWrongPassword
	}

	a.credMu.Lock()
	encoded := a.creds[u.ID]
	a.credMu.Unlock()
	if encoded == "" {
		return nil, "", ErrPasswordNotSet
	}
	if !verifyPassword(encoded, password) {
		return nil, "", ErrWrongPassword
	}
	if u.Status == "disabled" {
		return nil, "", ErrDisabled
	}

	_ = a.store.TouchLastSeen(u.ID)
	u.LastSeen = time.Now()

	token, err := a.sign(u)
	if err != nil {
		return nil, "", err
	}
	return u, token, nil
}

// ResetPassword 验证码找回密码：校验 OTP 后直接设置新密码。
// 演示账号在 bypass 下沿用 Login 的免码规则。
func (a *Auth) ResetPassword(kind, target, code, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if !a.isDemoTarget(target) {
		if err := a.checkCode(target, code); err != nil {
			return err
		}
	}

	u, found := a.findByKind(kind, target)
	if !found {
		return ErrAccountNotFound
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	a.credMu.Lock()
	defer a.credMu.Unlock()
	prev, had := a.creds[u.ID]
	a.creds[u.ID] = hash
	if err := a.persistCredentialsLocked(); err != nil {
		if had {
			a.creds[u.ID] = prev
		} else {
			delete(a.creds, u.ID)
		}
		return err
	}
	return nil
}

// findByKind 按手机号或邮箱取用户。
func (a *Auth) findByKind(kind, target string) (*domain.User, bool) {
	switch kind {
	case "phone":
		return a.store.GetUserByPhone(target)
	case "email":
		return a.store.GetUserByEmail(target)
	default:
		return nil, false
	}
}

func validatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < 8 || n > 72 {
		return ErrWeakPassword
	}
	return nil
}

// persistCredentialsLocked 原子写凭据文件：同目录临时文件 + 0600 + Rename。
// 调用方必须已持有 a.credMu。
func (a *Auth) persistCredentialsLocked() error {
	if a.credPath == "" {
		return errors.New("密码凭据文件尚未初始化")
	}
	data, err := json.Marshal(a.creds)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(filepath.Dir(a.credPath), ".credentials-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() {
		f.Close()
		os.Remove(tmp)
	}
	if err := f.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, a.credPath); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// checkCode 校验并消费验证码：带猜错计数与过期作废。
// 之前只有「验对才删」——6 位码可以被无限枚举，现在试满 3 次就作废。
func (a *Auth) checkCode(target, code string) error {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()

	// 验证时顺带清扫过期条目
	a.gcCodesLocked(now)
	entry, ok := a.codes[target]
	if !ok || now.After(entry.exp) {
		delete(a.codes, target)
		return ErrInvalidCode
	}
	if entry.code != code {
		entry.tries++
		if entry.tries >= codeMaxTries {
			// 试满上限：作废这个码，只能重新发
			delete(a.codes, target)
		} else {
			a.codes[target] = entry
		}
		return ErrInvalidCode
	}
	// 用过的码立刻作废，避免重放
	delete(a.codes, target)
	return nil
}

// gcCodesLocked 惰性清理：发码和验证时顺带扫掉过期条目。
// 不单开定时器，也别让 map 只增不减。调用方必须已持有 a.mu。
func (a *Auth) gcCodesLocked(now time.Time) {
	for k, e := range a.codes {
		if now.After(e.exp) {
			delete(a.codes, k)
		}
	}
	for k, t := range a.lastSent {
		// 节流窗口过了就没必要留着
		if now.Sub(t) >= codeSendGap {
			delete(a.lastSent, k)
		}
	}
}

// isDemoTarget bypass 开着时的演示账号：跳过验证码与发码节流，
// 保证 DEV_BYPASS 演示路径对这些加固无感。
func (a *Auth) isDemoTarget(target string) bool {
	return a.bypass && (target == "13800000000" || target == "linmo@studio.com")
}

// defaultQuota 新用户默认额度：默认 800，环境变量 DEFAULT_QUOTA 可调，
// 非法值（非数字、非正数）一律回退 800。config 包不归这里管，就地读。
func defaultQuota() int {
	if v := strings.TrimSpace(os.Getenv("DEFAULT_QUOTA")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 800
}

func (a *Auth) sign(u *domain.User) (string, error) {
	payload, err := json.Marshal(claims{UID: u.ID, Exp: time.Now().Add(a.ttl).Unix()})
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Verify 校验 token 并取用户。
func (a *Auth) Verify(token string) (*domain.User, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrBadToken
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return nil, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrBadToken
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, ErrBadToken
	}
	if time.Now().Unix() > c.Exp {
		return nil, ErrBadToken
	}
	u, ok := a.store.GetUser(c.UID)
	if !ok {
		return nil, ErrBadToken
	}
	if u.Status == "disabled" {
		return nil, ErrDisabled
	}
	return u, nil
}

func ptrIf(cond bool, v string) string {
	if cond {
		return v
	}
	return ""
}

func firstOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// defaultName 给新账号一个能看的名字。
//
// 之前是「用户」+ 手机号后四位，所有人看起来都像测试账号。
// 邮箱取 @ 前的部分；手机号给一个简短易记的编号（避开 0000 这类占位感）。
func defaultName(target string) string {
	if i := strings.Index(target, "@"); i > 0 {
		return target[:i]
	}
	// 手机号：用后四位生成「纸友 3847」这样的名字，
	// 既保留可识别性，又不像系统占位符。
	if len(target) >= 4 {
		tail := target[len(target)-4:]
		if tail == "0000" {
			return "纸友"
		}
		return "纸友 " + tail
	}
	return "纸友"
}
