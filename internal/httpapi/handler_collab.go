package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleAddCollaborator 通过手机号或邮箱邀请一个人进群。
// 只有创建者能邀请。
func (s *Server) handleAddCollaborator(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	var req struct {
		Target string `json:"target"` // 手机号或邮箱
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	req.Target = strings.TrimSpace(req.Target)
	if req.Target == "" {
		badRequest(w, "请填写对方的手机号或邮箱")
		return
	}

	// 只允许邀请已经注册过的人，避免往库里塞不存在的账号
	var invitee *domain.User
	if strings.Contains(req.Target, "@") {
		if v, ok := s.store.GetUserByEmail(req.Target); ok {
			invitee = v
		}
	} else if v, ok := s.store.GetUserByPhone(req.Target); ok {
		invitee = v
	}
	if invitee == nil {
		notFound(w, "这个人还没注册过，先让对方登录一次")
		return
	}

	conv, err := s.chat.AddCollaborator(u.ID, r.PathValue("id"), invitee)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w)
			return
		}
		fail(w, 400, "collab_failed", err.Error())
		return
	}

	s.admin.Audit(u.ID, "collab_add", conv.ID, invitee.ID, ClientIP(r))
	ok(w, map[string]interface{}{
		"conversation": conv,
		"invitee": map[string]string{
			"id": invitee.ID, "name": invitee.Name,
		},
	})
}

func (s *Server) handleRemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	conv, err := s.chat.RemoveCollaborator(u.ID, r.PathValue("id"), r.PathValue("uid"))
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w)
			return
		}
		fail(w, 400, "collab_failed", err.Error())
		return
	}
	s.admin.Audit(u.ID, "collab_remove", conv.ID, r.PathValue("uid"), ClientIP(r))
	ok(w, conv)
}

// handleLeave 协作者主动退出群聊。
func (s *Server) handleLeave(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	if err := s.chat.Leave(u.ID, r.PathValue("id")); err != nil {
		fail(w, 400, "leave_failed", err.Error())
		return
	}
	s.admin.Audit(u.ID, "conv_leave", r.PathValue("id"), "", ClientIP(r))
	ok(w, map[string]string{"left": "true"})
}

// handleListCollaborators 列出协作者（需要能看到这个群）。
func (s *Server) handleListCollaborators(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	conv, err := s.chat.Get(u.ID, r.PathValue("id"))
	if err != nil {
		notFound(w, "会话不存在")
		return
	}
	// online 是真实判断：5 分钟内有请求才算在线。
	// 之前界面上的绿点是写死的，等于骗人。
	const onlineWindow = 5 * time.Minute

	people := make([]map[string]interface{}, 0, len(conv.Collaborators)+1)
	add := func(usr *domain.User, role string) {
		people = append(people, map[string]interface{}{
			"id":     usr.ID,
			"name":   usr.Name,
			"role":   role,
			"online": time.Since(usr.LastSeen) < onlineWindow,
		})
	}
	if owner, ok := s.store.GetUser(conv.UserID); ok {
		add(owner, "owner")
	}
	for _, id := range conv.Collaborators {
		if usr, ok := s.store.GetUser(id); ok {
			add(usr, "member")
		}
	}
	ok(w, people)
}
