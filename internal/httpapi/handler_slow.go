package httpapi

import "net/http"

// handleAdminSlow 最近的慢记录。
func (s *Server) handleAdminSlow(w http.ResponseWriter, r *http.Request) {
	if s.orchestrator == nil || s.orchestrator.Slow() == nil {
		ok(w, map[string]interface{}{"items": []string{}, "total": 0, "thresholdMs": 0})
		return
	}
	log := s.orchestrator.Slow()
	total, threshold := log.Stats()
	ok(w, map[string]interface{}{
		"items":       log.List(queryInt(r, "limit", 50)),
		"total":       total,
		"thresholdMs": threshold,
	})
}
