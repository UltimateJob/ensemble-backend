package httpapi

import (
	"net/http"
	"strings"
)

// handleSearch 跨会话搜索消息。
// 只返回当前用户能访问的会话，避免串数据。
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		ok(w, map[string]interface{}{"query": "", "hits": []string{}, "total": 0})
		return
	}
	hits := s.chat.Search(u.ID, q, queryInt(r, "limit", 60))
	ok(w, map[string]interface{}{
		"query": q,
		"hits":  hits,
		"total": len(hits),
	})
}
