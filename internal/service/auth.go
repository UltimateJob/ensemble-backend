// Package service 承载业务用例，不涉及 HTTP 细节。
package service

import (
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

	mu    sync.Mutex
	codes map[string]codeEntry
}

type codeEntry struct {
	code string
	exp  time.Time
}

func NewAuth(s store.Store, secret string) *Auth {
	return &Auth{
		store:  s,
		secret: []byte(secret),
		ttl:    7 * 24 * time.Hour,
		codes:  make(map[string]codeEntry),
	}
}

// SendCode 发送验证码。首版固定 123456 并回显，接入短信网关后替换。
func (a *Auth) SendCode(target string) (string, error) {
	code := "123456"
	a.mu.Lock()
	a.codes[target] = codeEntry{code: code, exp: time.Now().Add(5 * time.Minute)}
	a.mu.Unlock()
	return code, nil
}

// Login 手机号或邮箱登录，不存在则自动建号。
func (a *Auth) Login(kind, target, code, name string) (*domain.User, string, error) {
	a.mu.Lock()
	entry, ok := a.codes[target]
	a.mu.Unlock()

	// 演示账号允许跳过验证码
	demo := target == "13800000000" || target == "linmo@studio.com"
	if !demo {
		if !ok || entry.code != code || time.Now().After(entry.exp) {
			return nil, "", ErrInvalidCode
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

	u.LastSeen = time.Now()
	_ = a.store.UpdateUser(u)

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
