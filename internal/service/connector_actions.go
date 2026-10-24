package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 连接器的「可执行动作」层。
//
// probe 只回答「凭据对不对」；这里回答「能拿它做什么」。
// 两者分开：用户填完凭据先看绿灯，再让 Agent 真的去读文档、提 PR。
//
// 设计约束：
//   - 只做读和有限的写，不做删除类操作
//   - 每个动作都要能说清「读/写了什么」，便于审计
//   - 不引入第三方 SDK：这些服务的 HTTP API 都很简单，多一个依赖
//     就多一份过期风险

// ConnectorAction 一次连接器调用的描述。给 Agent 看的是 Name/Description/Params。
type ConnectorAction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Params      json.RawMessage `json:"params"`
	// Risk read | write。write 走审批。
	Risk string `json:"risk"`
	// Kind 限定该动作只对哪种连接器有效；空表示通用。
	Kind string `json:"kind,omitempty"`
}

// Actions 返回某种连接器支持的动作。
func ConnectorActions(kind string) []ConnectorAction {
	read := func(name, desc, params string) ConnectorAction {
		return ConnectorAction{Name: name, Description: desc, Params: json.RawMessage(params), Risk: "read", Kind: kind}
	}
	write := func(name, desc, params string) ConnectorAction {
		return ConnectorAction{Name: name, Description: desc, Params: json.RawMessage(params), Risk: "write", Kind: kind}
	}

	switch kind {
	case "github":
		return []ConnectorAction{
			read("github_search_code", "在仓库里按关键词搜代码，返回匹配的文件与片段。",
				`{"type":"object","properties":{"q":{"type":"string","description":"搜索词"}},"required":["q"]}`),
			read("github_list_issues", "列出仓库的 issue，可按状态筛选。",
				`{"type":"object","properties":{"state":{"type":"string","enum":["open","closed","all"]},"limit":{"type":"integer"}}}`),
			read("github_get_file", "读取仓库里的一个文件。",
				`{"type":"object","properties":{"path":{"type":"string"},"ref":{"type":"string","description":"分支或 commit，默认主分支"}},"required":["path"]}`),
		}
	case "notion":
		return []ConnectorAction{
			read("notion_search", "在 Notion 工作区搜索页面，返回标题与链接。",
				`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`),
			read("notion_get_page", "读取一个页面的内容（转为纯文本）。",
				`{"type":"object","properties":{"pageId":{"type":"string"}},"required":["pageId"]}`),
			write("notion_append", "往一个页面追加一段文本。",
				`{"type":"object","properties":{"pageId":{"type":"string"},"text":{"type":"string"}},"required":["pageId","text"]}`),
		}
	case "feishu":
		return []ConnectorAction{
			read("feishu_get_doc", "读取一篇飞书文档的纯文本内容。",
				`{"type":"object","properties":{"docToken":{"type":"string","description":"文档 token，从链接里取"}},"required":["docToken"]}`),
			read("feishu_search_wiki", "在飞书知识库里搜索。",
				`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		}
	case "qqmail":
		return []ConnectorAction{
			read("mail_list_recent", "列出最近的邮件（标题、发件人、时间）。",
				`{"type":"object","properties":{"limit":{"type":"integer","description":"最多几封，默认 10"}}}`),
		}
	case "webhook":
		return []ConnectorAction{
			write("webhook_post", "向 Webhook 地址 POST 一段 JSON。",
				`{"type":"object","properties":{"payload":{"type":"object","description":"要发送的 JSON 内容"}},"required":["payload"]}`),
		}
	}
	return nil
}

// AllConnectorActions 列出某用户所有启用连接器可用的动作。
// Agent 在规划时会看到这份清单。
func (c *Connectors) AllConnectorActions(userID string) []ConnectorAction {
	var out []ConnectorAction
	for _, it := range c.List(userID) {
		if !it.Enabled || it.Status == domain.ConnStatusError {
			continue
		}
		for _, a := range ConnectorActions(it.Kind) {
			// 动作名带上连接器 id，避免同类连接器多个实例时互相覆盖
			a.Name = a.Name + "__" + it.ID
			a.Description = fmt.Sprintf("[%s] %s", it.Name, a.Description)
			out = append(out, a)
		}
	}
	return out
}

// RunAction 执行一次连接器动作。connID 为空时按动作名反查。
func (c *Connectors) RunAction(ctx context.Context, userID, connID, action string, args map[string]any) (string, error) {
	it, ok := c.store.GetConnector(connID)
	if !ok || it.UserID != userID {
		return "", ErrConnNotFound
	}
	if !it.Enabled {
		return "", errors.New("这个连接器已停用")
	}

	switch it.Kind {
	case "github":
		return c.runGitHub(ctx, it, action, args)
	case "notion":
		return c.runNotion(ctx, it, action, args)
	case "feishu":
		return c.runFeishu(ctx, it, action, args)
	case "qqmail":
		return c.runMail(ctx, it, action, args)
	case "webhook":
		return c.runWebhook(ctx, it, action, args)
	}
	return "", fmt.Errorf("不支持的操作：%s", it.Kind)
}

/* ---------------- GitHub ---------------- */

func (c *Connectors) runGitHub(ctx context.Context, it *domain.Connector, action string, args map[string]any) (string, error) {
	token := strings.TrimSpace(it.Secrets["token"])
	repo := strings.TrimSpace(it.Config["repo"])
	base := strings.TrimSuffix(strings.TrimSpace(it.Config["baseUrl"]), "/")
	if base == "" {
		base = "https://api.github.com"
	}

	do := func(path string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		return c.do(req, "GitHub")
	}

	switch action {
	case "github_search_code":
		q, _ := args["q"].(string)
		if q == "" {
			return "", errors.New("缺少搜索词")
		}
		path := "/search/code?q=" + urlQuery(q)
		if repo != "" {
			path += "&repo=" + repo
		}
		b, err := do(path)
		if err != nil {
			return "", err
		}
		return compactJSON(b, 12000), nil

	case "github_list_issues":
		if repo == "" {
			return "", errors.New("这个 GitHub 连接器没配仓库（owner/repo）")
		}
		state, _ := args["state"].(string)
		if state == "" {
			state = "open"
		}
		limit := intArg(args, "limit", 20)
		b, err := do(fmt.Sprintf("/repos/%s/issues?state=%s&per_page=%d", repo, state, limit))
		if err != nil {
			return "", err
		}
		return compactJSON(b, 12000), nil

	case "github_get_file":
		if repo == "" {
			return "", errors.New("这个 GitHub 连接器没配仓库（owner/repo）")
		}
		path, _ := args["path"].(string)
		if path == "" {
			return "", errors.New("缺少文件路径")
		}
		ref, _ := args["ref"].(string)
		u := fmt.Sprintf("/repos/%s/contents/%s", repo, strings.TrimPrefix(path, "/"))
		if ref != "" {
			u += "?ref=" + urlQuery(ref)
		}
		b, err := do(u)
		if err != nil {
			return "", err
		}
		// GitHub 返回 base64 内容，解出来直接给模型看
		var f struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		if err := json.Unmarshal(b, &f); err == nil && f.Encoding == "base64" {
			dec, derr := base64Decode(f.Content)
			if derr == nil {
				return clipText(string(dec), 20000), nil
			}
		}
		return compactJSON(b, 20000), nil
	}
	return "", fmt.Errorf("GitHub 不支持的动作：%s", action)
}

/* ---------------- Notion ---------------- */

func (c *Connectors) runNotion(ctx context.Context, it *domain.Connector, action string, args map[string]any) (string, error) {
	token := strings.TrimSpace(it.Secrets["token"])
	base := "https://api.notion.com"

	call := func(method, path string, body any) ([]byte, error) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Notion-Version", "2022-06-28")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return c.do(req, "Notion")
	}

	switch action {
	case "notion_search":
		q, _ := args["query"].(string)
		limit := intArg(args, "limit", 10)
		b, err := call(http.MethodPost, "/v1/search", map[string]any{
			"query": q, "page_size": limit,
		})
		if err != nil {
			return "", err
		}
		return compactJSON(b, 12000), nil

	case "notion_get_page":
		pageID, _ := args["pageId"].(string)
		if pageID == "" {
			return "", errors.New("缺少 pageId")
		}
		b, err := call(http.MethodGet, "/v1/blocks/"+pageID+"/children?page_size=100", nil)
		if err != nil {
			return "", err
		}
		return notionBlocksToText(b), nil

	case "notion_append":
		pageID, _ := args["pageId"].(string)
		text, _ := args["text"].(string)
		if pageID == "" || text == "" {
			return "", errors.New("缺少 pageId 或 text")
		}
		b, err := call(http.MethodPatch, "/v1/blocks/"+pageID+"/children", map[string]any{
			"children": []any{map[string]any{
				"object": "block", "type": "paragraph",
				"paragraph": map[string]any{
					"rich_text": []any{map[string]any{
						"type": "text", "text": map[string]string{"content": text},
					}},
				},
			}},
		})
		if err != nil {
			return "", err
		}
		_ = b
		return "已追加到页面 " + pageID, nil
	}
	return "", fmt.Errorf("Notion 不支持的动作：%s", action)
}

// notionBlocksToText 把 block 列表压成纯文本。模型不需要 JSON 结构。
func notionBlocksToText(raw []byte) string {
	var d struct {
		Results []struct {
			Type string                     `json:"type"`
			Body map[string]json.RawMessage `json:"-"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return compactJSON(raw, 12000)
	}
	// 简化：直接在原始 JSON 里抽 rich_text 的 plain_text
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return compactJSON(raw, 12000)
	}
	var sb strings.Builder
	walkNotionText(generic, &sb)
	s := strings.TrimSpace(sb.String())
	if s == "" {
		return compactJSON(raw, 12000)
	}
	return clipText(s, 20000)
}

func walkNotionText(v any, sb *strings.Builder) {
	switch x := v.(type) {
	case map[string]any:
		if pt, ok := x["plain_text"].(string); ok && pt != "" {
			sb.WriteString(pt)
		}
		for _, vv := range x {
			walkNotionText(vv, sb)
		}
	case []any:
		for _, vv := range x {
			walkNotionText(vv, sb)
		}
	}
}

/* ---------------- 飞书 ---------------- */

func (c *Connectors) runFeishu(ctx context.Context, it *domain.Connector, action string, args map[string]any) (string, error) {
	appID := strings.TrimSpace(it.Secrets["appId"])
	appSecret := strings.TrimSpace(it.Secrets["appSecret"])
	token, err := c.feishuToken(ctx, appID, appSecret)
	if err != nil {
		return "", err
	}
	base := "https://open.feishu.cn"

	do := func(path string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return c.do(req, "飞书")
	}

	switch action {
	case "feishu_get_doc":
		tok, _ := args["docToken"].(string)
		if tok == "" {
			return "", errors.New("缺少 docToken")
		}
		b, err := do("/open-apis/docx/v1/documents/" + tok + "/raw_content")
		if err != nil {
			return "", err
		}
		var d struct {
			Data struct {
				Content string `json:"content"`
			} `json:"data"`
		}
		if err := json.Unmarshal(b, &d); err == nil && d.Data.Content != "" {
			return clipText(d.Data.Content, 20000), nil
		}
		return compactJSON(b, 12000), nil

	case "feishu_search_wiki":
		b, err := do("/open-apis/wiki/v2/spaces?page_size=20")
		if err != nil {
			return "", err
		}
		// wiki 没有跨空间搜索接口，把空间列表给模型当线索
		return "飞书知识库暂不支持全文搜索，以下是可访问的空间：\n" + compactJSON(b, 8000), nil
	}
	return "", fmt.Errorf("飞书不支持的动作：%s", action)
}

func (c *Connectors) feishuToken(ctx context.Context, appID, appSecret string) (string, error) {
	body := fmt.Sprintf(`{"app_id":%q,"app_secret":%q}`, appID, appSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	b, err := c.do(req, "飞书")
	if err != nil {
		return "", err
	}
	var d struct {
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.Unmarshal(b, &d); err != nil || d.TenantAccessToken == "" {
		return "", errors.New("飞书没有返回访问令牌")
	}
	return d.TenantAccessToken, nil
}

/* ---------------- 邮件 ---------------- */

func (c *Connectors) runMail(ctx context.Context, it *domain.Connector, action string, args map[string]any) (string, error) {
	if action != "mail_list_recent" {
		return "", fmt.Errorf("邮箱不支持的动作：%s", action)
	}
	addr := strings.TrimSpace(it.Secrets["address"])
	code := strings.TrimSpace(it.Secrets["authCode"])
	limit := intArg(args, "limit", 10)
	msgs, err := fetchRecentMail(ctx, addr, code, limit)
	if err != nil {
		return "", err
	}
	return msgs, nil
}

/* ---------------- Webhook ---------------- */

func (c *Connectors) runWebhook(ctx context.Context, it *domain.Connector, action string, args map[string]any) (string, error) {
	if action != "webhook_post" {
		return "", fmt.Errorf("Webhook 不支持的动作：%s", action)
	}
	url := strings.TrimSpace(it.Config["url"])
	if url == "" {
		return "", errors.New("还没填 Webhook 地址")
	}
	payload, _ := json.Marshal(args["payload"])
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if sig := strings.TrimSpace(it.Secrets["secret"]); sig != "" {
		req.Header.Set("X-Signature", sig)
	}
	// 用户的 webhook 可能返回任意状态码，2xx 都算成功
	resp, err := c.probe.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("推送失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return fmt.Sprintf("已推送（HTTP %d）", resp.StatusCode), nil
	}
	return "", fmt.Errorf("对方返回 %d", resp.StatusCode)
}

/* ---------------- 小工具 ---------------- */

func intArg(m map[string]any, k string, def int) int {
	if v, ok := m[k].(float64); ok && v > 0 {
		return int(v)
	}
	return def
}

func clipText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…（已截断）"
}

// compactJSON 把上游 JSON 压成一行，过长就截断。
// 模型看的是内容不是格式，省 token。
func compactJSON(b []byte, max int) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return clipText(string(b), max)
	}
	out, _ := json.Marshal(v)
	return clipText(string(out), max)
}

func urlQuery(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "#", "%23")
}

func base64Decode(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, "\n", "")
	return base64.StdEncoding.DecodeString(s)
}
