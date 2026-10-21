package httpapi

import (
	"net/http"
)

// handleAdminResetUsage 把某个用户的已用额度清零。
// 场景：演示前重置、误扣申诉、月度结转。
func (s *Server) handleAdminResetUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, ok2 := s.store.GetUser(id)
	if !ok2 {
		notFound(w, "用户不存在")
		return
	}
	before := u.Used
	// 走原子方法，别整体覆盖
	if err := s.store.IncrementUsed(id, -before); err != nil {
		serverErr(w, err.Error())
		return
	}
	fresh, _ := s.store.GetUser(id)
	s.admin.Audit("admin", "user_reset_usage", id, "", ClientIP(r))
	ok(w, map[string]interface{}{
		"user":  fresh,
		"reset": before,
	})
}

// handleAdminConversations 管理员视角的会话列表，带锁状态。
func (s *Server) handleAdminConversations(w http.ResponseWriter, r *http.Request) {
	convs := s.store.ListConvs("")
	type row struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		UserID   string `json:"userId"`
		Studio   string `json:"studio"`
		Busy     bool   `json:"busy"`
		Messages int    `json:"messages"`
		Updated  string `json:"updatedAt"`
	}
	out := make([]row, 0, len(convs))
	for _, c := range convs {
		busy := false
		if s.orchestrator != nil {
			busy = s.orchestrator.Busy(c.ID)
		}
		out = append(out, row{
			ID: c.ID, Title: c.Title, UserID: c.UserID, Studio: c.Studio,
			Busy: busy, Messages: len(s.store.ListMessages(c.ID, 0)),
			Updated: c.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	ok(w, out)
}

// handleAdminUnlock 强制释放某个群的会话锁。
// 场景：编排器异常退出把锁留住了，用户一直看到「群里正在说话」。
// 正常情况下 defer 会释放，这是兜底。
func (s *Server) handleAdminUnlock(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.orchestrator == nil {
		serverErr(w, "编排器不可用")
		return
	}
	was := s.orchestrator.Busy(id)
	s.orchestrator.Release(id)
	s.admin.Audit("admin", "conv_unlock", id, "was_busy="+boolStr(was), ClientIP(r))
	ok(w, map[string]interface{}{"wasBusy": was, "unlocked": true})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
