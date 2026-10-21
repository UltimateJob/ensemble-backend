// Package service 承载业务用例，不涉及 HTTP 细节。
package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

var (
	ErrInvalidCode = errors.New("验证码不正确或已过期")
	ErrDisabled    = errors.New("账号已被停用")
	ErrBadToken    = errors.New("会话无效，请重新登录")
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
}

type codeEntry struct {
	code string
	exp  time.Time
}

func NewAuth(s store.Store, secret string, sender CodeSender, bypass bool) *Auth {
	if sender == nil {
		sender = LogSender{}
	}
	return &Auth{
		store:  s,
		secret: []byte(secret),
		ttl:    7 * 24 * time.Hour,
		sender: sender,
		bypass: bypass,
		codes:  make(map[string]codeEntry),
	}
}

// SendCode 生成并发送验证码。
// 返回的 devCode 只有在内置 LogSender 下非空，真短信通道返回空字符串。
func (a *Auth) SendCode(ctx context.Context, target string) (string, error) {
	code := randomCode()
	a.mu.Lock()
	a.codes[target] = codeEntry{code: code, exp: time.Now().Add(5 * time.Minute)}
	a.mu.Unlock()
	return a.sender.Send(ctx, target, code)
}

// Login 手机号或邮箱登录，不存在则自动建号。
func (a *Auth) Login(kind, target, code, name string) (*domain.User, string, error) {
	a.mu.Lock()
	entry, ok := a.codes[target]
	a.mu.Unlock()

	// 演示账号可跳过验证码（由配置控制，生产应关掉）
	demo := a.bypass && (target == "13800000000" || target == "linmo@studio.com")
	if !demo {
		if !ok || entry.code != code || time.Now().After(entry.exp) {
			return nil, "", ErrInvalidCode
		}
		// 用过的码立刻作废，避免重放
		a.mu.Lock()
		delete(a.codes, target)
		a.mu.Unlock()
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
			Quota:  800,
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

func defaultName(target string) string {
	if len(target) >= 4 && strings.Contains(target, "@") {
		return target[:strings.Index(target, "@")]
	}
	if len(target) > 4 {
		return "用户" + target[len(target)-4:]
	}
	return "用户"
}
