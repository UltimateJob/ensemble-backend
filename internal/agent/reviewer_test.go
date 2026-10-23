package agent

import (
	"context"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

type reviewModel struct {
	args string
}

func (m reviewModel) Stream(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) {
	ch := make(chan domain.Chunk, 4)
	go func() {
		defer close(ch)
		if len(req.Tools) == 1 && req.Tools[0].Name == ToolSubmitReview {
			ch <- domain.Chunk{ToolCall: &domain.ToolCallDelta{
				Index: 0, ID: "r1", Name: ToolSubmitReview, Arguments: m.args,
			}}
			ch <- domain.Chunk{Done: true, FinishReason: "tool_calls", Usage: &domain.Usage{PromptTokens: 30, CompletionTokens: 10}}
			return
		}
		ch <- domain.Chunk{Content: "正文里不写结论"}
		ch <- domain.Chunk{Done: true, FinishReason: "stop"}
	}()
	return ch, nil
}

func TestReviewUsesStructuredTool(t *testing.T) {
	m := NewManager()
	res, err := m.Review(context.Background(), reviewModel{args: `{"verdict":"issues","summary":"契约不一致","issues":["POST 返回 200，应为 201"]}`}, ReviewRequest{
		Task: "做接口", Plan: domain.TeamPlan{Objective: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "issues" || len(res.Issues) != 1 {
		t.Fatalf("got %+v", res)
	}
	if res.TokensIn != 30 || res.TokensOut != 10 {
		t.Fatalf("tokens=%d/%d", res.TokensIn, res.TokensOut)
	}
}

func TestReviewRejectsPlaceholderSummary(t *testing.T) {
	m := NewManager()
	if _, err := m.Review(context.Background(), reviewModel{args: `{"verdict":"pass","summary":"占位"}`}, ReviewRequest{Task: "t"}); err == nil {
		t.Fatal("占位说明不应算通过")
	}
}

func TestReviewRejectsInvalidVerdict(t *testing.T) {
	m := NewManager()
	if _, err := m.Review(context.Background(), reviewModel{args: `{"verdict":"maybe","summary":"?"}`}, ReviewRequest{Task: "t"}); err == nil {
		t.Fatal("非法 verdict 应报错")
	}
}

func TestReviewNitsOnlyIsPass(t *testing.T) {
	m := NewManager()
	res, err := m.Review(context.Background(), reviewModel{args: `{"verdict":"issues","summary":"可以更好","nits":["建议补一条日志"]}`}, ReviewRequest{Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	// 只有建议没有 blocker：不该触发返工，按通过处理。
	if res.Verdict != "pass" || len(res.Nits) != 1 {
		t.Fatalf("got %+v", res)
	}
}

func TestReviewBlockersStayIssues(t *testing.T) {
	m := NewManager()
	res, err := m.Review(context.Background(), reviewModel{args: `{"verdict":"issues","summary":"功能缺失","blockers":["DELETE 返回 200，应为 204"]}`}, ReviewRequest{Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "issues" || len(res.Blockers) != 1 {
		t.Fatalf("got %+v", res)
	}
	if got := blockersOf(&domain.TeamReview{Verdict: "issues", Issues: res.Blockers}); len(got) != 1 {
		t.Fatalf("legacy issues should count as blockers: %v", got)
	}
}

func TestReviewFallsBackWhenToolMissing(t *testing.T) {
	m := NewManager()
	if _, err := m.Review(context.Background(), reviewModel{args: `{}`}, ReviewRequest{Task: "t"}); err == nil {
		// reviewModel 永远会调用工具；这里用空参数触发 JSON 校验失败。
		t.Fatal("空参数应报错")
	}
}
