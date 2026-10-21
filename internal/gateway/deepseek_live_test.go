package gateway

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 真实上游集成测试。
//
// 默认跳过（CI 不烧额度）。本地要跑：
//
//	DEEPSEEK_API_KEY=sk-... go test ./internal/gateway/ -run Live -v
//
// 存在的意义：mock 测试永远绿，但如果 DeepSeek 改了协议
// （字段名、SSE 格式、错误码），线上会直接断。这个测试是唯一能提前发现的地方。
func liveKey(t *testing.T) string {
	t.Helper()
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Skip("未设置 DEEPSEEK_API_KEY，跳过真实上游测试")
	}
	return key
}

func TestDeepSeekLiveStream(t *testing.T) {
	key := liveKey(t)
	d := NewDeepSeek("https://api.deepseek.com", key, "deepseek-chat", 60*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	stream, err := d.Chat(ctx, domain.ChatRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "只回复两个字：收到"},
		},
		MaxTokens: 20,
	})
	if err != nil {
		t.Fatalf("上游调用失败（协议可能变了）：%v", err)
	}

	var text string
	var usage *domain.Usage
	sawDone := false
	for chunk := range stream {
		if chunk.Err != nil {
			t.Fatalf("流式返回出错：%v", chunk.Err)
		}
		text += chunk.Content
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.Done {
			sawDone = true
		}
	}

	if !sawDone {
		t.Fatal("流没有正常结束（没收到 Done 块）")
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("上游返回了空内容")
	}
	t.Logf("上游返回：%q", text)

	if usage == nil {
		t.Fatal("没有收到 usage，计费统计会失效")
	}
	if usage.TotalTokens <= 0 {
		t.Fatalf("usage 不合理：%+v", usage)
	}
	t.Logf("usage: prompt=%d completion=%d total=%d",
		usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
}

func TestDeepSeekLiveBadKey(t *testing.T) {
	// 不需要真实 Key，验证错误路径
	d := NewDeepSeek("https://api.deepseek.com", "sk-invalid-key-for-test", "deepseek-chat", 30*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := d.Chat(ctx, domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("错误的 Key 应该报错，而不是静默返回空流")
	}
	t.Logf("预期内的错误：%v", err)
}

// 验证网关层在真实上游下的完整链路：选路 → 调用 → 计量。
func TestGatewayLiveWithRealUpstream(t *testing.T) {
	key := liveKey(t)

	reg := NewRegistry()
	reg.Add(NewDeepSeek("https://api.deepseek.com", key, "deepseek-chat", 60*time.Second))
	table := NewModelTable([]domain.ModelInfo{
		{Key: "deepseek-chat", Provider: "deepseek", Upstream: "deepseek-chat", Enabled: true, Weight: 1},
	})
	gw := New(reg, table, 1, 5, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := gw.Stream(ctx, domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: "用一句话说明什么是投标"}},
	})
	if err != nil {
		t.Fatalf("网关调用失败：%v", err)
	}

	var text string
	for c := range stream {
		if c.Err != nil {
			t.Fatalf("网关流出错：%v", c.Err)
		}
		text += c.Content
	}

	if len([]rune(text)) < 5 {
		t.Fatalf("回复太短，可疑：%q", text)
	}

	m := gw.Metrics()
	if m.Requests != 1 {
		t.Fatalf("请求计数应为 1，得到 %d", m.Requests)
	}
	if m.TokensUsed <= 0 {
		t.Fatal("Token 用量没有记录")
	}
	if m.AvgLatencyMS <= 0 {
		t.Fatal("延迟没有记录")
	}

	stats := reg.Stats()
	if len(stats) != 1 || stats[0].Success == 0 {
		t.Fatalf("上游健康统计不对：%+v", stats)
	}

	t.Logf("真实链路：%d tokens, %dms, 回复 %q",
		m.TokensUsed, int64(m.AvgLatencyMS), truncRunes(text, 40))
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
