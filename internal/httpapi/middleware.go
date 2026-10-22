package httpapi

import (
	"context"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

type ctxKey int

const (
	userKey ctxKey = iota + 1
)

// Middleware 类型别名，便于链式组合。
type Middleware func(http.Handler) http.Handler

// Chain 从后往前包裹。
func Chain(h http.Handler, ms ...Middleware) http.Handler {
	for i := len(ms) - 1; i >= 0; i-- {
		h = ms[i](h)
	}
	return h
}

// Recover 捕获 panic，保证进程不挂。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logx.Req(r.Context()).Error("panic recovered",
					"panic", rec, "stack", string(debug.Stack()))
				serverErr(w, "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Trace 注入 trace id，回写响应头。
func Trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Trace-Id")
		if id == "" {
			id = logx.NewTraceID()
		}
		w.Header().Set("X-Trace-Id", id)
		next.ServeHTTP(w, r.WithContext(logx.WithTrace(r.Context(), id)))
	})
}

// CORS 允许前端跨域调用。
// LimitBody 限制请求体大小。
//
// 之前没有限制：一个 100MB 的请求会被整个读进内存，
// 并发几个就能把进程撑爆（实测单个请求让内存从 80MB 涨到 331MB）。
//
// 2MB 足够宽裕——消息上限 4000 字约 12KB，附件功能将来另说。
func LimitBody(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Trace-Id")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter 记录状态码。
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Access 访问日志。
func Access(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/health") {
			return
		}
		logx.Req(r.Context()).Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"ms", time.Since(start).Milliseconds(),
			"ip", ClientIP(r),
		)
	})
}

// Auth 解析 Bearer token，写入 context。
type AuthResolver func(token string) (*domain.User, error)

func Auth(resolve AuthResolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				unauthorized(w, "缺少登录凭证")
				return
			}
			u, err := resolve(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				unauthorized(w, err.Error())
				return
			}
			ctx := context.WithValue(r.Context(), userKey, u)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFrom 取当前登录用户。
func UserFrom(ctx context.Context) (*domain.User, bool) {
	u, ok := ctx.Value(userKey).(*domain.User)
	return u, ok
}

// ClientIP 取真实 IP，兼容反向代理。
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
