package service

import (
	"sort"
	"strings"
)

// SearchHit 一条搜索结果。
type SearchHit struct {
	ConvID    string `json:"convId"`
	ConvTitle string `json:"convTitle"`
	MsgID     string `json:"msgId"`
	Agent     string `json:"agent,omitempty"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
	// Snippet 命中的那一小段，前端可以直接显示
	Snippet string `json:"snippet"`
}

// Search 在用户能访问的所有会话里搜消息。
//
// 只搜本人可见的会话，不然会串。上限 60 条——
// 搜索的目的是「找到那个群」，不是导出数据。
func (c *Chat) Search(userID, q string, limit int) []SearchHit {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 1 {
		return nil
	}
	if limit <= 0 || limit > 60 {
		limit = 60
	}
	lower := strings.ToLower(q)

	hits := make([]SearchHit, 0, limit)
	for _, conv := range c.store.ListConvs(userID) {
		if !conv.CanAccess(userID) {
			continue
		}
		// 群名命中时给一条「这个群」的结果——
		// 用户搜「竞标」多半是想找那个群，不是找某句话
		if strings.Contains(strings.ToLower(conv.Title), lower) {
			hits = append(hits, SearchHit{
				ConvID:    conv.ID,
				ConvTitle: conv.Title,
				Role:      "conv",
				Content:   conv.Title,
				CreatedAt: conv.UpdatedAt.Format("2006-01-02 15:04"),
				Snippet:   conv.Title,
			})
		}
		for _, m := range c.store.ListMessages(conv.ID, 0) {
			content := m.Content
			if content == "" {
				continue
			}
			idx := strings.Index(strings.ToLower(content), lower)
			if idx < 0 {
				continue
			}
			hits = append(hits, SearchHit{
				ConvID:    conv.ID,
				ConvTitle: conv.Title,
				MsgID:     m.ID,
				Agent:     m.Agent,
				Role:      string(m.Role),
				Content:   content,
				CreatedAt: m.CreatedAt.Format("2006-01-02 15:04"),
				Snippet:   snippet(content, idx, len([]rune(q))),
			})
		}
	}

	// 新的在前
	sort.Slice(hits, func(i, j int) bool { return hits[i].CreatedAt > hits[j].CreatedAt })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// snippet 取命中位置前后各一小段，用 … 标出省略。
func snippet(content string, byteIdx, runeLen int) string {
	r := []rune(content)
	// byteIdx 是字节位置，先转成 rune 位置
	before := []rune(content[:byteIdx])
	start := len(before) - 12
	if start < 0 {
		start = 0
	}
	end := len(before) + runeLen + 24
	if end > len(r) {
		end = len(r)
	}
	out := string(r[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(r) {
		out += "…"
	}
	return out
}
