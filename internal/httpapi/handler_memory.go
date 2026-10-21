package httpapi

import (
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleMemorySnapshot 返回当前群的群记忆 + 当前用户与群里每个角色的记忆。
// 只读；用于「他们记得」面板。
func (s *Server) handleMemorySnapshot(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	convID := r.URL.Query().Get("conversationId")
	if convID == "" {
		badRequest(w, "缺少 conversationId")
		return
	}
	conv, err := s.chat.Get(u.ID, convID)
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	if s.mem == nil {
		ok(w, domain.MemorySnapshot{
			Conversation: []*domain.Memory{},
			Agents:       []domain.AgentMemories{},
		})
		return
	}
	agents := service.ConversationAgents(conv)
	ok(w, s.mem.Snapshot(convID, u.ID, agents))
}

// handleClearMemory 手动清空记忆。
//
//	DELETE /v1/memories?conversationId=...  清空这个群的群记忆（不删角色记忆）
//	DELETE /v1/memories?characterId=...     清空这个角色对当前用户的全部记忆（跨群）
func (s *Server) handleClearMemory(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	convID := r.URL.Query().Get("conversationId")
	charID := r.URL.Query().Get("characterId")

	if convID == "" && charID == "" {
		badRequest(w, "需要 conversationId 或 characterId")
		return
	}
	if s.mem == nil {
		ok(w, map[string]int{"cleared": 0})
		return
	}

	if convID != "" {
		if _, err := s.chat.Get(u.ID, convID); err != nil {
			notFound(w, "会话不存在")
			return
		}
		n := s.mem.ClearConversation(convID)
		ok(w, map[string]int{"cleared": n})
		return
	}

	// 只清当前用户自己的角色记忆，不会影响同群其他人。
	n := s.mem.ClearAgent(charID, u.ID)
	ok(w, map[string]int{"cleared": n})
}
