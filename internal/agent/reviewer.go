package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 验收阶段的结构化出口。
//
// 之前让验收 Agent 在正文里写「结论：通过」——真实模型会一头扎进工具循环，
// 读完全部文件、重跑测试，用满步数也没写结论（基准里两次发生）。
// 结论是流程的最后一环，不能靠模型自觉：这里用一次强制工具调用把
// 验收结果变成结构化数据，前面那轮审计只负责收集证据（Notes）。
const ToolSubmitReview = "submit_review"

func ReviewTool() domain.Tool {
	raw := `{
		"type":"object",
		"properties":{
			"verdict":{"type":"string","enum":["pass","issues"],"description":"pass 表示验收通过；issues 表示存在阻塞验收的问题"},
			"summary":{"type":"string","description":"一两句话结论"},
			"blockers":{"type":"array","items":{"type":"string"},"description":"阻塞验收的问题，必须能指出具体文件/行为并说明如何复现；verdict=issues 时至少一条"},
			"nits":{"type":"array","items":{"type":"string"},"description":"不阻塞验收的改进建议；不要放进 blockers"}
		},
		"required":["verdict","summary"]
	}`
	return domain.Tool{
		Name:        ToolSubmitReview,
		Description: "提交验收结论。必须调用这个工具，不要在正文里写结论。只有阻塞验收的问题才放 blockers。",
		Parameters:  json.RawMessage(raw),
	}
}

// ReviewRequest 结构化验收的入参。
type ReviewRequest struct {
	UserID       string
	Model        string
	SystemPrompt string
	Task         string
	Plan         domain.TeamPlan
	Checks       []domain.TeamCheck
	// Notes 审计阶段的可见说明（assistant 正文），作为证据。
	Notes string
	// Files 工作区文件列表，帮助判断交付是否齐全。
	Files []string
	// Evidence 关键文件的正文片段。审计 Run 超时/失败时用它兜底，
	// 让验收仍然基于真实代码，而不是只看到文件名就猜。
	Evidence string
	// AllChecksPassed 确定性验证是否全部通过。全通过时验收只拦功能性问题，
	// 不因为个人偏好再返工一轮。
	AllChecksPassed bool
}

// ReviewResult 结构化验收结果。
type ReviewResult struct {
	Verdict  string   `json:"verdict"`
	Summary  string   `json:"summary"`
	Blockers []string `json:"blockers,omitempty"`
	Nits     []string `json:"nits,omitempty"`
	// Issues 兼容旧模型可能返回的字段名。
	Issues    []string `json:"issues,omitempty"`
	TokensIn  int
	TokensOut int
}

// Review 用一次强制工具调用产出验收结论。
//
// 它不跑 Run、不带工具：证据来自服务端确定性检查和上一轮审计的 Notes。
// 这样即使审计阶段用满步数，流程也一定有一个可执行的结论。
func (m *Manager) Review(ctx context.Context, model Model, req ReviewRequest) (*ReviewResult, error) {
	system := strings.TrimSpace(req.SystemPrompt)
	if system == "" {
		system = "你是独立验收人。只判断交付是否满足验收标准，不改代码。"
	}
	system += `

你是这次团队任务的独立验收人。服务端已经跑过确定性验证，审计阶段也看过代码。
- 只基于下面的证据判断，不要假设没看到的功能存在。
- 结论必须通过 submit_review 工具提交，不要在正文里写结论。
- verdict=issues 时 blockers 至少给一条可执行的问题；verdict=pass 时 blockers 留空。
- 只有「直接违背任务要求」或「功能不可用/会坏」的问题才算 blocker。风格、额外校验、契约措辞的偏好放 nits，不影响 verdict。
- 不要把「验证命令通过」当成「验收通过」：检查交付是否齐全、契约是否一致。`
	if req.AllChecksPassed {
		system += `
- 服务端确定性验证已全部通过。这种情况下默认 verdict=pass；只有你指出明确的功能性缺陷时才判 issues。`
	}

	var b strings.Builder
	b.WriteString("任务：\n" + req.Task + "\n")
	if req.Plan.Objective != "" {
		b.WriteString("\n计划目标：" + req.Plan.Objective + "\n")
	}
	if len(req.Plan.Acceptance) > 0 {
		b.WriteString("\n验收标准：\n")
		for _, s := range req.Plan.Acceptance {
			b.WriteString("- " + s + "\n")
		}
	}
	if strings.TrimSpace(req.Plan.Contracts) != "" {
		b.WriteString("\n接口契约：\n" + clip(req.Plan.Contracts, 1500) + "\n")
	}
	if len(req.Plan.Assignments) > 0 {
		b.WriteString("\n分工：\n")
		for _, a := range req.Plan.Assignments {
			name := a.AgentName
			if name == "" {
				name = a.AgentKey
			}
			fmt.Fprintf(&b, "- %s：%s（scope: %s）\n", name, clip(a.Task, 160), strings.Join(a.Scope, " "))
		}
	}
	if len(req.Files) > 0 {
		b.WriteString("\n工作区文件：\n" + clip(strings.Join(req.Files, "\n"), 1500) + "\n")
	}
	if len(req.Checks) > 0 {
		b.WriteString("\n服务端验证：\n")
		for _, c := range req.Checks {
			status := "通过"
			switch {
			case c.Skipped:
				status = "跳过（" + c.Reason + "）"
			case !c.Passed:
				status = "失败"
			}
			fmt.Fprintf(&b, "- %s：%s\n", c.Name, status)
			if !c.Passed && c.Output != "" {
				b.WriteString("  输出：" + clip(strings.ReplaceAll(c.Output, "\n", " "), 500) + "\n")
			}
		}
	}
	if strings.TrimSpace(req.Notes) != "" {
		b.WriteString("\n审计阶段的发现：\n" + clip(req.Notes, 5000) + "\n")
	}
	if strings.TrimSpace(req.Evidence) != "" {
		b.WriteString("\n关键文件内容：\n" + clip(req.Evidence, 14000) + "\n")
	}

	msgs := []domain.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
	// 验收只产出一个短 JSON，不能像普通执行一样无限等。
	// 第一次超时/截断时再试一次：验收失败会让整个团队停在 reviewing，
	// 返工也拿不到结论。两次都失败才如实标 error。
	var content string
	var calls []domain.ToolCall
	var usage *domain.Usage
	var lastErr error
	// 推理型模型同样会把 max_tokens 花在思考上；从 6k 起，截断再放大。
	budgets := []int{6000, 12000}
	for attempt := 0; attempt < len(budgets); attempt++ {
		reviewCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		ch, err := model.Stream(reviewCtx, domain.ChatRequest{
			Model:          req.Model,
			Messages:       msgs,
			Tools:          []domain.Tool{ReviewTool()},
			ToolChoice:     "required",
			ToolChoiceName: ToolSubmitReview,
			Stream:         true,
			Task:           true,
			UserID:         req.UserID,
			MaxTokens:      budgets[attempt],
		})
		if err != nil {
			cancel()
			lastErr = err
			log.Printf("[review] attempt=%d stream err=%v", attempt+1, err)
			continue
		}
		var finish string
		var streamErr error
		content, calls, usage, finish, streamErr = collect(ch)
		cancel()
		if streamErr != nil {
			lastErr = streamErr
			log.Printf("[review] attempt=%d elapsed timeout err=%v", attempt+1, streamErr)
			continue
		}
		if finish == "length" {
			lastErr = errors.New("验收结论被 max_tokens 截断")
			log.Printf("[review] attempt=%d truncated at %d", attempt+1, budgets[attempt])
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, lastErr
	}
	res := &ReviewResult{}
	if usage != nil {
		res.TokensIn = usage.PromptTokens
		res.TokensOut = usage.CompletionTokens
	}
	for _, c := range calls {
		if c.Name != ToolSubmitReview {
			continue
		}
		if err := json.Unmarshal([]byte(c.Arguments), res); err != nil {
			return nil, fmt.Errorf("验收结论格式不正确：%w（原文开头：%s）", err, clip(c.Arguments, 240))
		}
		res.Verdict = strings.ToLower(strings.TrimSpace(res.Verdict))
		if res.Verdict != "pass" && res.Verdict != "issues" {
			return nil, fmt.Errorf("验收结论不合法：%q", res.Verdict)
		}
		summary := strings.TrimSpace(res.Summary)
		if utf8.RuneCountInString(summary) < 4 || strings.Contains(summary, "占位") {
			return nil, fmt.Errorf("验收结论缺少有效说明：%q", summary)
		}
		if res.Verdict == "issues" {
			// 兼容旧字段：模型把阻塞问题写进 issues 也认。
			if len(res.Blockers) == 0 && len(res.Issues) > 0 {
				res.Blockers = res.Issues
			}
			// 只有 nit 没有 blocker 时，不应该拖着一轮返工：
			// 直接按通过处理，建议仍保留给用户看。
			if len(res.Blockers) == 0 {
				if len(res.Nits) > 0 || len(res.Issues) > 0 || strings.TrimSpace(res.Summary) != "" {
					res.Verdict = "pass"
				} else {
					return nil, errors.New("验收判为有问题，但没有给出问题")
				}
			}
		}
		return res, nil
	}
	// 上游/模型偶尔不调用工具，而是在正文里写结论。
	// 结构化结果优先，但正文里有明确结论时也要接住——这比判 error、
	// 让整个团队停在 reviewing 要好。
	pass, hasIssues := parseReviewVerdict(strings.ToLower(content))
	if pass || hasIssues {
		res := &ReviewResult{Summary: clip(content, 1200)}
		if usage != nil {
			res.TokensIn = usage.PromptTokens
			res.TokensOut = usage.CompletionTokens
		}
		if pass {
			res.Verdict = "pass"
		} else {
			res.Verdict = "issues"
			res.Issues = []string{clip(content, 1200)}
		}
		return res, nil
	}
	return nil, fmt.Errorf("验收没有调用 submit_review（正文：%s）", clip(content, 200))
}
