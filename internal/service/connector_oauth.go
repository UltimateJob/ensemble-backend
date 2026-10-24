package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 连接器的 OAuth 授权流程。
//
// 和「手填 Token」的区别：用户不用去平台后台复制密钥，点一下「授权」
// 跳过去登录、同意，回来就绑好了。代价是需要应用凭据 + 回调地址。
//
// 流程：
//   1. 前端 GET /v1/connectors/oauth/{kind}/start
//      → 生成 state（绑到当前用户），返回平台的授权 URL
//   2. 用户在浏览器里授权，平台跳回 /v1/connectors/oauth/{kind}/callback?code=&state=
//   3. 后端用 code 换 access_token，落成一个 Connector，跳回前端页面
//
// state 必须绑定用户：回调是浏览器发起的，没有 Authorization 头，
// 只能靠 state 认出「这次授权是谁开始的」。state 一次性、限时 10 分钟。

// OAuthProvider 一个平台的 OAuth 端点与参数。
type OAuthProvider struct {
	Kind        string
	AuthURL     string
	TokenURL    string
	Scope       string
	ClientID    string
	ClientSecret string
	RedirectURI string
	// ExtraAuth 附加到授权 URL 的查询参数
	ExtraAuth url.Values
	// BasicAuth 为真时用 HTTP Basic 传 client_id/secret（Notion、GitHub 的标准）
	BasicAuth bool
	// AcceptJSON 要求返回 JSON（GitHub 默认就是 JSON，Notion 也是）
	AcceptJSON bool
}

// oauthStates 待完成的授权。key 是 state，value 是发起人。
type oauthStates struct {
	mu   sync.Mutex
	rows map[string]oauthState
}

type oauthState struct {
	UserID  string
	Kind    string
	Scope   string
	Created time.Time
}

func newOAuthStates() *oauthStates {
	return &oauthStates{rows: map[string]oauthState{}}
}

const oauthStateTTL = 10 * time.Minute

// issue 生成一个 state 并记录归属。
func (s *oauthStates) issue(userID, kind, scope string) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	st := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	s.gcLocked()
	s.rows[st] = oauthState{UserID: userID, Kind: kind, Scope: scope, Created: time.Now()}
	s.mu.Unlock()
	return st, nil
}

// consume 取出并作废一个 state。一次性使用，防止重放。
func (s *oauthStates) consume(st string) (oauthState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.rows[st]
	if ok {
		delete(s.rows, st)
	}
	if !ok || time.Since(v.Created) > oauthStateTTL {
		return oauthState{}, false
	}
	return v, true
}

func (s *oauthStates) gcLocked() {
	now := time.Now()
	for k, v := range s.rows {
		if now.Sub(v.Created) > oauthStateTTL {
			delete(s.rows, k)
		}
	}
}

// Providers 返回当前配置了凭据的 OAuth 平台。
func (c *Connectors) OAuthProviders() []OAuthProvider {
	var out []OAuthProvider
	add := func(p OAuthProvider) {
		if p.ClientID == "" || p.ClientSecret == "" || p.RedirectURI == "" {
			return
		}
		out = append(out, p)
	}

	add(OAuthProvider{
		Kind: "github", AuthURL: "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
		Scope:    "repo read:user",
		ClientID: c.oauthGitHubID, ClientSecret: c.oauthGitHubSecret,
		RedirectURI: c.oauthRedirectBase + "/v1/connectors/oauth/github/callback",
		AcceptJSON:  true,
	})
	add(OAuthProvider{
		Kind: "notion", AuthURL: "https://api.notion.com/v1/oauth/authorize",
		TokenURL: "https://api.notion.com/v1/oauth/token",
		Scope:    "",
		ClientID: c.oauthNotionID, ClientSecret: c.oauthNotionSecret,
		RedirectURI: c.oauthRedirectBase + "/v1/connectors/oauth/notion/callback",
		BasicAuth:   true, AcceptJSON: true,
		ExtraAuth: url.Values{"owner": {"user"}},
	})
	add(OAuthProvider{
		Kind: "feishu", AuthURL: "https://open.feishu.cn/open-apis/authen/v1/authorize",
		TokenURL: "https://open.feishu.cn/open-apis/authen/v2/oauth/token",
		Scope:    "docx:document:readonly wiki:wiki:readonly drive:drive:readonly offline_access",
		ClientID: c.oauthFeishuID, ClientSecret: c.oauthFeishuSecret,
		RedirectURI: c.oauthRedirectBase + "/v1/connectors/oauth/feishu/callback",
		AcceptJSON:  true,
	})
	return out
}

// OAuthAvailable 该平台能否走授权流程。
func (c *Connectors) OAuthAvailable(kind string) bool {
	for _, p := range c.OAuthProviders() {
		if p.Kind == kind {
			return true
		}
	}
	return false
}

// StartOAuth 生成授权 URL。
func (c *Connectors) StartOAuth(userID, kind string) (string, error) {
	var prov *OAuthProvider
	for _, p := range c.OAuthProviders() {
		if p.Kind == kind {
			pp := p
			prov = &pp
			break
		}
	}
	if prov == nil {
		return "", fmt.Errorf("%s 没有配置 OAuth 应用凭据，请先手填 Token", kind)
	}

	state, err := c.states.issue(userID, kind, prov.Scope)
	if err != nil {
		return "", err
	}

	u, err := url.Parse(prov.AuthURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", prov.ClientID)
	q.Set("redirect_uri", prov.RedirectURI)
	q.Set("state", state)
	q.Set("response_type", "code")
	if prov.Scope != "" {
		q.Set("scope", prov.Scope)
	}
	for k, vs := range prov.ExtraAuth {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Exchange 用回调里的 code 换令牌，并落成一个 Connector。
//
// 返回连接器与前端应跳回的路径。
func (c *Connectors) Exchange(ctx context.Context, state, code string) (*domain.Connector, string, error) {
	st, ok := c.states.consume(state)
	if !ok {
		return nil, "", errors.New("授权已过期或链接无效，请重新发起")
	}
	if strings.TrimSpace(code) == "" {
		return nil, "", errors.New("平台没有返回授权码")
	}

	var prov *OAuthProvider
	for _, p := range c.OAuthProviders() {
		if p.Kind == st.Kind {
			pp := p
			prov = &pp
			break
		}
	}
	if prov == nil {
		return nil, "", errors.New("这个平台不再支持 OAuth")
	}

	tok, err := c.tokenRequest(ctx, *prov, code)
	if err != nil {
		return nil, "", err
	}

	// 不同平台的令牌字段名不同，统一成连接器的 Secrets 形态
	secrets, config := c.secretsFromToken(*prov, tok)

	conn, err := c.Create(st.UserID, domain.Connector{
		Kind: st.Kind, Name: providerDisplayName(st.Kind), Scope: "private",
		Config: config, Secrets: secrets,
	})
	if err != nil {
		return nil, "", err
	}
	// 授权即视为已验证
	if err := c.probeAndMark(ctx, st.UserID, conn.ID); err != nil {
		// 探活失败不影响绑定：Token 可能存在权限不足，但用户可以自己判断
		_ = err
	}
	return conn, "/app/#/connectors", nil
}

func (c *Connectors) tokenRequest(ctx context.Context, p OAuthProvider, code string) (map[string]any, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("redirect_uri", p.RedirectURI)
	form.Set("grant_type", "authorization_code")

	// 先把 form 补全再一次性建请求。
	//
	// 之前是先建请求、再改 form、再替换 req.Body——ContentLength 还是
	// 旧值，GitHub 收到的是被截断的请求体，换令牌静默失败。
	if !p.BasicAuth {
		form.Set("client_id", p.ClientID)
		form.Set("client_secret", p.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.AcceptJSON {
		req.Header.Set("Accept", "application/json")
	}
	if p.BasicAuth {
		req.SetBasicAuth(p.ClientID, p.ClientSecret)
	}

	resp, err := c.probe.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("换令牌失败：%v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("平台拒绝了授权（%d）：%s", resp.StatusCode, clipText(string(body), 200))
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		// GitHub 不带 Accept:application/json 时会返回 form 编码
		return nil, fmt.Errorf("无法解析平台返回：%s", clipText(string(body), 200))
	}
	if e, ok := out["error"].(string); ok && e != "" {
		desc, _ := out["error_description"].(string)
		return nil, fmt.Errorf("平台拒绝：%s %s", e, desc)
	}
	return out, nil
}

// secretsFromToken 把各平台的令牌响应归一成连接器凭据。
func (c *Connectors) secretsFromToken(p OAuthProvider, tok map[string]any) (map[string]string, map[string]string) {
	str := func(k string) string {
		if v, ok := tok[k].(string); ok {
			return v
		}
		return ""
	}
	secrets := map[string]string{}
	config := map[string]string{}

	switch p.Kind {
	case "github":
		secrets["token"] = str("access_token")
		config["authMode"] = "oauth"
	case "notion":
		secrets["token"] = str("access_token")
		if ws, ok := tok["workspace_name"].(string); ok && ws != "" {
			config["workspace"] = ws
		}
		config["authMode"] = "oauth"
	case "feishu":
		// 飞书 user_access_token 用于读用户能看的文档。
		// refresh_token 存下来，过期后可以续，不必让用户重新授权。
		secrets["token"] = str("access_token")
		if rt := str("refresh_token"); rt != "" {
			secrets["refreshToken"] = rt
		}
		// 应用身份仍在用 appId/appSecret，OAuth 只补用户身份
		secrets["appId"] = c.oauthFeishuID
		secrets["appSecret"] = c.oauthFeishuSecret
		config["authMode"] = "oauth"
	}
	return secrets, config
}

// probeAndMark 绑完后跑一次连通性检查，把状态落到记录上。
func (c *Connectors) probeAndMark(ctx context.Context, userID, id string) error {
	_, err := c.Probe(ctx, userID, id)
	return err
}

func providerDisplayName(kind string) string {
	switch kind {
	case "github":
		return "GitHub"
	case "notion":
		return "Notion"
	case "feishu":
		return "飞书"
	}
	return kind
}
