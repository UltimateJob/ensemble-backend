package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// Remote 把「智团自己的线上后端」当成一个模型上游。
//
// 为什么需要它：桌面端是自带后端的 .app，本地负责执行（读写工作区、
// 跑命令），但模型调用应该走线上——Key 在服务器上，不该塞进客户端。
//
// 协议上它就是一个 OpenAI 兼容上游：线上暴露 /v1/chat/completions。
// 所以内部复用 DeepSeek 的客户端，只换 base 与令牌。
type Remote struct {
	mu     sync.RWMutex
	base   string
	token  string
	model  string
	inner  *DeepSeek
	client *http.Client
}

// NewRemote 创建一个指向线上后端的 provider。
func NewRemote(base, token, model string, timeout time.Duration) *Remote {
	// 线上暴露的是 /v1/chat/completions，而底层客户端拼的是
	// base + "/chat/completions"，所以这里补上 /v1。
	// 用户配 REMOTE_BASE 时既可以写 https://teamwise.cn，
	// 也可以写带 /v1 的完整地址。
	b := strings.TrimSuffix(strings.TrimSpace(base), "/")
	if !strings.HasSuffix(b, "/v1") {
		b += "/v1"
	}
	r := &Remote{
		base:   b,
		token:  token,
		model:  model,
		client: &http.Client{Timeout: timeout},
	}
	r.rebuild()
	return r
}

// rebuild 按当前令牌重建内部客户端。调用方需持有锁或确保无并发。
func (r *Remote) rebuild() {
	r.inner = NewDeepSeek(r.base, r.token, r.model, r.client.Timeout)
}

// Name 返回 provider 名。管理台与熔断状态都按这个名字区分。
func (r *Remote) Name() string { return "teamwise" }

// Chat 发起一次对话。令牌为空时直接报错，不静默降级——
// 客户端不该在用户不知情的情况下变成 mock。
func (r *Remote) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	r.mu.RLock()
	inner := r.inner
	token := r.token
	r.mu.RUnlock()
	if token == "" {
		return nil, fmt.Errorf("remote: 未登录，无法调用线上模型")
	}
	return inner.Chat(ctx, req)
}

// SetToken 更新登录令牌（用户重新登录后调用）。
func (r *Remote) SetToken(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.token = token
	r.rebuild()
}

// Probe 探一次线上是否可用。
//
// 只打 /healthz：不需要鉴权，能区分「服务器没起来」和「令牌过期」。
func (r *Remote) Probe(ctx context.Context) error {
	r.mu.RLock()
	base := r.base
	r.mu.RUnlock()
	// healthz 在根路径上，不在 /v1 下面
	root := strings.TrimSuffix(base, "/v1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote: healthz 返回 %d", resp.StatusCode)
	}
	return nil
}
