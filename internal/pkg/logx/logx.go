// Package logx 结构化日志，基于标准库 slog，带请求追踪 ID。
package logx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"time"
)

type ctxKey int

const traceKey ctxKey = 1

var base *slog.Logger

func init() {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	base = slog.New(h).With("service", "ensemble")
}

// L 返回全局 logger。
func L() *slog.Logger { return base }

// WithTrace 注入 trace id。
func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey, id)
}

// Trace 取 trace id。
func Trace(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey).(string); ok {
		return v
	}
	return ""
}

// NewTraceID 生成 16 位十六进制 trace。
func NewTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Req 返回带 trace 的 logger。
func Req(ctx context.Context) *slog.Logger {
	if id := Trace(ctx); id != "" {
		return base.With("trace", id)
	}
	return base
}

// Since 毫秒耗时。
func Since(t time.Time) int64 { return time.Since(t).Milliseconds() }
