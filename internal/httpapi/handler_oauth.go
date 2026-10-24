package httpapi

import (
	"net/http"
)

// 连接器的 OAuth 授权接口。
//
//   GET /v1/connectors/oauth/{kind}/start    → {url}  前端跳过去
//   GET /v1/connectors/oauth/{kind}/callback → 302    平台授权后回跳
//
// 回调是浏览器发起的，没有 Authorization 头，身份靠 state 认。
// 所以 callback 必须先于鉴权中间件注册，也不能要求登录态。

// handleOAuthStart 生成授权 URL。
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	u, found := UserFrom(r.Context())
	if !found {
		unauthorized(w, "未登录")
		return
	}
	if s.conns == nil {
		notFound(w, "未启用连接器")
		return
	}
	kind := r.PathValue("kind")
	if !s.conns.OAuthAvailable(kind) {
		fail(w, 400, "oauth_unavailable",
			"这个平台还没有配置 OAuth 应用凭据，请用手填 Token 的方式绑定")
		return
	}
	url, err := s.conns.StartOAuth(u.ID, kind)
	if err != nil {
		serverErr(w, err.Error())
		return
	}
	ok(w, map[string]string{"url": url})
}

// handleOAuthCallback 接收平台的回跳，换取令牌并落到连接器。
//
// 成功后 302 回前端页面；失败也 302，但带错误码——
// 用户是在浏览器里被跳过来的，返回 JSON 会让他看到一屏裸数据。
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.conns == nil {
		http.Redirect(w, r, "/app/#/connectors?oauth=error&reason=disabled", http.StatusFound)
		return
	}
	kind := r.PathValue("kind")
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	// 平台可能在回调里直接带错误（用户点了"拒绝"）
	if e := r.URL.Query().Get("error"); e != "" {
		http.Redirect(w, r, "/app/#/connectors?oauth=denied&platform="+kind, http.StatusFound)
		return
	}

	conn, back, err := s.conns.Exchange(r.Context(), state, code)
	if err != nil {
		http.Redirect(w, r, "/app/#/connectors?oauth=error&msg="+urlEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, back+"?oauth=ok&platform="+kind+"&id="+conn.ID, http.StatusFound)
}

// handleOAuthProviders 告诉前端哪些平台支持一键授权。
func (s *Server) handleOAuthProviders(w http.ResponseWriter, r *http.Request) {
	if s.conns == nil {
		ok(w, map[string]any{"providers": []string{}})
		return
	}
	var kinds []string
	for _, p := range s.conns.OAuthProviders() {
		kinds = append(kinds, p.Kind)
	}
	ok(w, map[string]any{"providers": kinds})
}

func urlEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b = append(b, c)
		} else {
			b = append(b, '%', hex[c>>4], hex[c&15])
		}
	}
	return string(b)
}
