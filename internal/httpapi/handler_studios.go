package httpapi

import (
	"net/http"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleListStudios 我的工作室模板。
func (s *Server) handleListStudios(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	ok(w, s.store.ListStudios(u.ID))
}

// handleCreateStudio 保存一个模板。
// 两种来源：直接传 agents，或者传 fromConvId 从现有群快照。
func (s *Server) handleCreateStudio(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	var req struct {
		Name       string             `json:"name"`
		Tagline    string             `json:"tagline"`
		Accent     string             `json:"accent"`
		Icon       string             `json:"icon"`
		Opening    string             `json:"opening"`
		Agents     []domain.ConvAgent `json:"agents"`
		FromConvID string             `json:"fromConvId"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	// 从现有群复制
	if req.FromConvID != "" {
		conv, err := s.chat.Get(u.ID, req.FromConvID)
		if err != nil {
			notFound(w, "会话不存在")
			return
		}
		if len(conv.Agents) == 0 {
			badRequest(w, "这个群还没有自定义角色，没什么可存的")
			return
		}
		req.Agents = conv.Agents
		if req.Name == "" {
			req.Name = conv.Title
		}
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		badRequest(w, "给工作室起个名字")
		return
	}
	if len(req.Agents) == 0 {
		badRequest(w, "至少要有 1 个角色")
		return
	}
	if n := len(s.store.ListStudios(u.ID)); n >= 20 {
		fail(w, 400, "too_many", "工作室模板已满（20 个）")
		return
	}
	if req.Accent == "" {
		req.Accent = "#4F46E5"
	}
	if req.Icon == "" {
		req.Icon = "sparkles"
	}

	st := &domain.SavedStudio{
		UserID: u.ID, Name: req.Name, Tagline: req.Tagline,
		Accent: req.Accent, Icon: req.Icon, Opening: req.Opening,
		Agents: req.Agents,
	}
	if err := s.store.CreateStudio(st); err != nil {
		serverErr(w, err.Error())
		return
	}
	created(w, st)
}

func (s *Server) handleDeleteStudio(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	if err := s.store.DeleteStudio(u.ID, r.PathValue("id")); err != nil {
		notFound(w, "工作室不存在")
		return
	}
	ok(w, map[string]string{"deleted": "true"})
}

// handleScenariosAll 内置场景 + 我的工作室，前端一次拿全。
func (s *Server) handleScenariosAll(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	list := make([]map[string]interface{}, 0, 8)

	for _, sc := range service.AllScenarios() {
		list = append(list, map[string]interface{}{
			"key": sc.Key, "name": sc.Name, "tagline": sc.Tagline,
			"category": sc.Category, "icon": sc.Icon, "accent": sc.Accent,
			"agents": sc.Agents, "custom": false,
			// 随机场景的角色是开群时生成的，前端要靠这个字段
			// 解释「为什么这里是空的」
			"random": sc.Random,
		})
	}
	// 用户自己的模板排在后面
	if u != nil {
		for _, st := range s.store.ListStudios(u.ID) {
			list = append(list, map[string]interface{}{
				"key": "ws_" + st.ID, "name": st.Name, "tagline": st.Tagline,
				"category": "mine", "icon": st.Icon, "accent": st.Accent,
				"agents": st.Agents, "custom": true, "studioId": st.ID,
			})
		}
	}
	ok(w, list)
}
