package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

var (
	ErrConnNotFound = errors.New("连接器不存在")
	ErrConnTooMany  = errors.New("连接器太多了（上限 30 个）")
	ErrConnKind     = errors.New("不支持的服务类型")
)

const maxConnectors = 30

// 支持的服务类型。加一种只需在 probe 里加一个分支。
var connectorKinds = map[string]string{
	"feishu":   "飞书",
	"notion":   "Notion",
	"github":   "GitHub",
	"calendar": "企业日历",
	"webhook":  "Webhook",
}

// HTTPProbe 连接器对外的请求客户端。测试里替换。
type HTTPProbe struct {
	Client *http.Client
}

func defaultProbe() HTTPProbe {
	return HTTPProbe{Client: &http.Client{
		Timeout: 10 * time.Second,
		// 不用 http.DefaultTransport：它会读 HTTP(S)_PROXY 环境变量。
		// 开发机上配了代理、服务端却不在代理后面时，探测结果会被
		// 本机网络环境影响——用户在手机上点「测试连接」，得到的
		// 错误应该来自上游，而不是这台电脑的代理设置。
		Transport: &http.Transport{
			MaxIdleConns:        20,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     60 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}}
}

// Connectors 外部服务连接器。
//
// 第一版的能力边界（写清楚，免得被当成 OAuth）：
//   - 凭据真存（加密落盘），真发请求做连通性测试；
//   - **不做**各家的一键授权页。GitHub 这类用 Personal Access Token，
//     飞书用自建应用的 tenant_access_token，Notion 用 integration token。
//
// 这样做的好处：每个连接器都是「填进去就能用」，而且测试是真的。
// 一个只会亮绿灯的假开关比没有更糟。
type Connectors struct {
	store store.Store
	probe HTTPProbe
}

func NewConnectors(s store.Store) *Connectors {
	return &Connectors{store: s, probe: defaultProbe()}
}

// SetProbe 注入可替换的 HTTP 客户端（测试用）。
func (c *Connectors) SetProbe(p HTTPProbe) { c.probe = p }

// Kinds 返回支持的服务类型，给前端渲染下拉框。
func (c *Connectors) Kinds() map[string]string {
	out := make(map[string]string, len(connectorKinds))
	for k, v := range connectorKinds {
		out[k] = v
	}
	return out
}

func (c *Connectors) List(userID string) []*domain.Connector {
	list := c.store.ListConnectors(userID)
	// 对外永远不带 Secrets 明文；SecretKeys 用来显示「已填写」。
	for _, it := range list {
		it.Secrets = nil
		if len(it.SecretKeys) == 0 && it.Config != nil {
			it.SecretKeys = []string{}
		}
	}
	return list
}

func (c *Connectors) Create(userID string, in domain.Connector) (*domain.Connector, error) {
	if _, okk := connectorKinds[in.Kind]; !okk {
		return nil, ErrConnKind
	}
	in.UserID = userID
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = connectorKinds[in.Kind]
	}
	if in.Scope != domain.ConnScopeTeam && in.Scope != domain.ConnScopePrivate {
		in.Scope = domain.ConnScopePrivate
	}
	if n := len(c.store.ListConnectors(userID)); n >= maxConnectors {
		return nil, ErrConnTooMany
	}
	in.Enabled = true
	// 一律是「未测试」。之前填了凭据就标成 ok，界面会显示「已连通」——
	// 但那时一次请求都还没发过，是假的绿灯。只有 Probe 成功才配叫 ok。
	in.Status = domain.ConnStatusUnset
	in.SecretKeys = secretKeysOf(in.Secrets)
	if err := c.store.CreateConnector(&in); err != nil {
		return nil, err
	}
	out := in
	out.Secrets = nil
	return &out, nil
}

func (c *Connectors) Update(userID, id string, patch ConnPatch) (*domain.Connector, error) {
	cur, ok := c.store.GetConnector(id)
	if !ok || cur.UserID != userID {
		return nil, ErrConnNotFound
	}
	if patch.Name != nil {
		cur.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Enabled != nil {
		cur.Enabled = *patch.Enabled
	}
	if patch.Scope != nil {
		if *patch.Scope == domain.ConnScopeTeam || *patch.Scope == domain.ConnScopePrivate {
			cur.Scope = *patch.Scope
		}
	}
	if patch.Config != nil {
		cur.Config = *patch.Config
	}
	// 凭据按字段合并：只改一个字段不需要重填其他。
	// 空字符串表示「清除这个字段」。
	if patch.Secrets != nil {
		if cur.Secrets == nil {
			cur.Secrets = map[string]string{}
		}
		for k, v := range *patch.Secrets {
			if v == "" {
				delete(cur.Secrets, k)
			} else {
				cur.Secrets[k] = v
			}
		}
		cur.SecretKeys = secretKeysOf(cur.Secrets)
	}
	if err := c.store.UpdateConnector(cur); err != nil {
		return nil, err
	}
	out := *cur
	out.Secrets = nil
	return &out, nil
}

func (c *Connectors) Delete(userID, id string) error {
	if err := c.store.DeleteConnector(userID, id); err != nil {
		return ErrConnNotFound
	}
	return nil
}

// Probe 真的发一次请求，验证凭据可用。
//
// 「测试连接」按钮如果只检查字段非空，那就是骗人。这里对每家
// 发一个最小的鉴权请求，把真实结果（含上游错误信息）写回状态。
func (c *Connectors) Probe(ctx context.Context, userID, id string) (*domain.Connector, error) {
	cur, ok := c.store.GetConnector(id)
	if !ok || cur.UserID != userID {
		return nil, ErrConnNotFound
	}
	err := c.probeOne(ctx, cur)
	cur.CheckedAt = time.Now()
	if err != nil {
		cur.Status = domain.ConnStatusError
		cur.LastError = err.Error()
	} else {
		cur.Status = domain.ConnStatusOK
		cur.LastError = ""
	}
	if uerr := c.store.UpdateConnector(cur); uerr != nil {
		return nil, uerr
	}
	out := *cur
	out.Secrets = nil
	return &out, nil
}

func (c *Connectors) probeOne(ctx context.Context, it *domain.Connector) error {
	sec := it.Secrets
	cfg := it.Config
	base := func(def string) string {
		if v := strings.TrimSpace(cfg["baseUrl"]); v != "" {
			return strings.TrimSuffix(v, "/")
		}
		return def
	}

	switch it.Kind {
	case "github":
		token := strings.TrimSpace(sec["token"])
		if token == "" {
			return errors.New("还没填 GitHub Token")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			base("https://api.github.com")+"/user", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		return c.doJSON(req, "GitHub")

	case "notion":
		token := strings.TrimSpace(sec["token"])
		if token == "" {
			return errors.New("还没填 Notion Integration Token")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			base("https://api.notion.com")+"/v1/users/me", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Notion-Version", "2022-06-28")
		return c.doJSON(req, "Notion")

	case "feishu":
		// 先用 appId/appSecret 换 tenant_access_token，这是飞书自建应用的标准入口。
		appID := strings.TrimSpace(sec["appId"])
		appSecret := strings.TrimSpace(sec["appSecret"])
		if appID == "" || appSecret == "" {
			return errors.New("还没填飞书 App ID / App Secret")
		}
		body := fmt.Sprintf(`{"app_id":%q,"app_secret":%q}`, appID, appSecret)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			base("https://open.feishu.cn")+"/open-apis/auth/v3/tenant_access_token/internal",
			strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		return c.doJSON(req, "飞书")

	case "calendar":
		// 通用 CalDAV / ICS 订阅：GET 一次，能拿到 2xx 就算通。
		url := strings.TrimSpace(cfg["url"])
		if url == "" {
			return errors.New("还没填日历地址")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if u := strings.TrimSpace(sec["username"]); u != "" {
			req.SetBasicAuth(u, sec["password"])
		}
		_, err = c.do(req, "日历")
		return err

	case "webhook":
		url := strings.TrimSpace(cfg["url"])
		if url == "" {
			return errors.New("还没填 Webhook 地址")
		}
		// 只做到「能解析 URL」这一步，不真的往用户群里发一条消息——
		// 测试按钮不该产生外部副作用。
		if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
			return errors.New("Webhook 地址要以 https:// 开头")
		}
		return nil

	default:
		return ErrConnKind
	}
}

func (c *Connectors) doJSON(req *http.Request, label string) error {
	_, err := c.do(req, label)
	return err
}

// do 执行请求并把错误翻译成人话。上游的 4xx 正文通常很具体
// （scope 不够、token 过期），值得透传一部分。
func (c *Connectors) do(req *http.Request, label string) ([]byte, error) {
	resp, err := c.probe.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 %s：%v", label, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, nil
	}
	snippet := strings.TrimSpace(string(body))
	if len([]rune(snippet)) > 180 {
		snippet = string([]rune(snippet)[:180]) + "…"
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%s 拒绝了这组凭据（%d）：%s", label, resp.StatusCode, snippet)
	case http.StatusNotFound:
		return nil, fmt.Errorf("%s 接口地址不对（404）：%s", label, snippet)
	default:
		return nil, fmt.Errorf("%s 返回 %d：%s", label, resp.StatusCode, snippet)
	}
}

func secretKeysOf(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ConnPatch 连接器部分更新。
type ConnPatch struct {
	Name    *string
	Enabled *bool
	Scope   *string
	Config  *map[string]string
	Secrets *map[string]string
}
