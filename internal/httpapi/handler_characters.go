package httpapi

import (
	"net/http"

	"github.com/UltimateJob/ensemble-backend/internal/service"
)

// handleSearchCharacters 搜索内置角色库。
func (s *Server) handleSearchCharacters(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	list := service.SearchCharacters(q)
	ok(w, list)
}

// handleCharacterToAgent 把角色转成一个可以直接拉进群的自定义角色。
func (s *Server) handleCharacterToAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		badRequest(w, "请求格式不正确")
		return
	}
	// 从库里找回这个角色
	matches := service.SearchCharacters(req.Name)
	var found *service.Character
	for i := range matches {
		if matches[i].Name == req.Name {
			found = &matches[i]
			break
		}
	}
	if found == nil {
		notFound(w, "角色库里没有这个人")
		return
	}
	title, prompt := service.CharacterPrompt(*found)
	ok(w, map[string]string{
		"key":    service.CharacterKey(found.Name),
		"name":   found.Name,
		"title":  title,
		"prompt": prompt,
		"color":  "#7C3AED",
		"work":   found.Work,
		"source": "library",
	})
}
