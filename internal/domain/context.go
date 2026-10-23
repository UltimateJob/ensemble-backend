package domain

import "context"

// userIDKey 是请求上下文里的用户标识。放在 domain 包，
// 让 gateway / service 都能读取，而不必依赖 HTTP 层。
type userIDKey struct{}

// WithUserID 把当前登录用户写进上下文。
func WithUserID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, userIDKey{}, id)
}

// UserIDFromContext 取上下文里的用户标识，没有则返回空串。
func UserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(userIDKey{}).(string)
	return id
}

// allowedModelsKey 当前套餐允许的模型列表。为空表示不限制。
// 放在上下文里而不是 ChatRequest 字段上，是为了让编排、任务执行这些
// 「间接发起上游调用」的路径也自动带上同一份权限，客户端也无法伪造。
type allowedModelsKey struct{}

// WithAllowedModels 把套餐允许的模型写进上下文。
func WithAllowedModels(ctx context.Context, models []string) context.Context {
	if len(models) == 0 {
		return ctx
	}
	cp := make([]string, len(models))
	copy(cp, models)
	return context.WithValue(ctx, allowedModelsKey{}, cp)
}

// AllowedModelsFromContext 取套餐允许的模型；空切片表示不限。
func AllowedModelsFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	out, _ := ctx.Value(allowedModelsKey{}).([]string)
	return out
}
