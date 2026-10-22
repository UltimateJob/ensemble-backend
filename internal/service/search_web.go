package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SearchClient 调用 DeepSeek 官方 Search API。
// 端点：POST https://api.deepseek.com/v1/tools/search
// 与聊天共用同一把 Bearer Key。
type SearchClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type SearchResult struct {
	RerankScore float64 `json:"rerank_score"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Snippet     string  `json:"snippet"`
	Summary     string  `json:"summary"`
	Content     string  `json:"content"`
	SiteName    string  `json:"site_name"`
	Date        string  `json:"date"`
	Authority   int     `json:"authority"`
}

type SearchSource struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"siteName,omitempty"`
	Date     string `json:"date,omitempty"`
}

func NewSearchClient(baseURL, apiKey string, timeout time.Duration) *SearchClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &SearchClient{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		http:    &http.Client{Timeout: timeout},
	}
}

// Search 返回网页结果。search 失败由调用方决定是降级还是提示用户。
func (c *SearchClient) Search(ctx context.Context, query string, count int) ([]SearchResult, error) {
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("未配置 DeepSeek Search API Key")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("搜索关键词为空")
	}
	if len([]rune(query)) > 128 {
		query = string([]rune(query)[:128])
	}
	if count <= 0 {
		count = 6
	}
	if count > 50 {
		count = 50
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"query":        query,
		"count":        count,
		"need_content": true,
	})

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/tools/search", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if attempt == 0 {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var out struct {
				Results []SearchResult `json:"results"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				return nil, fmt.Errorf("解析搜索结果失败: %w", err)
			}
			for i := range out.Results {
				out.Results[i].Summary = trimRunes(out.Results[i].Summary, 700)
				out.Results[i].Content = trimRunes(out.Results[i].Content, 1200)
				out.Results[i].Snippet = trimRunes(out.Results[i].Snippet, 240)
			}
			return out.Results, nil
		}

		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		msg := strings.TrimSpace(e.Error.Message)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		if msg == "" {
			msg = resp.Status
		}
		lastErr = fmt.Errorf("DeepSeek 搜索失败（%d）：%s", resp.StatusCode, msg)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt == 0 {
				time.Sleep(400 * time.Millisecond)
				continue
			}
		}
		return nil, lastErr
	}
	return nil, lastErr
}

func trimRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// buildSearchQuery 把名字、来源、类型、补充拼成搜索查询。
func buildSearchQuery(name, source, kind, note string) string {
	parts := []string{strings.TrimSpace(name)}
	if s := strings.TrimSpace(source); s != "" {
		parts = append(parts, s)
	}
	switch kind {
	case "tv":
		parts = append(parts, "电视剧 角色 人设 剧情 性格 关系")
	case "movie":
		parts = append(parts, "电影 角色 人设 剧情 性格 关系")
	case "celebrity":
		parts = append(parts, "公众人物 公开资料 作品 经历 公众形象")
	case "game":
		parts = append(parts, "游戏 角色 设定 性格 背景故事")
	default:
		parts = append(parts, "动漫 角色 设定 性格 能力 经历 官方资料")
	}
	if n := strings.TrimSpace(note); n != "" {
		parts = append(parts, n)
	}
	q := strings.Join(parts, " ")
	if len([]rune(q)) > 128 {
		q = string([]rune(q)[:128])
	}
	return q
}

func formatSearchBlock(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[联网检索结果]\n")
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n站点：%s\n链接：%s\n摘要：%s\n", i+1, r.Title, r.SiteName, r.URL, r.Summary)
		if r.Content != "" {
			fmt.Fprintf(&b, "内容：%s\n", r.Content)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func toSources(results []SearchResult) []SearchSource {
	out := make([]SearchSource, 0, len(results))
	seen := map[string]bool{}
	for _, r := range results {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		key := r.URL
		if u, err := url.Parse(r.URL); err == nil {
			key = u.Host + u.Path
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, SearchSource{
			Title: r.Title, URL: r.URL, SiteName: r.SiteName, Date: r.Date,
		})
	}
	return out
}
