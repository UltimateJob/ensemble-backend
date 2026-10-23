package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// ToolSubmitPlan 规划第一阶段：只提交谁做什么、改哪里。
// 契约/验证命令放到第二阶段，避免一次生成太长被推理预算截断。
const ToolSubmitPlan = "submit_plan"

// ToolSubmitPlanDetails 规划第二阶段：契约、验收标准、验证命令。
const ToolSubmitPlanDetails = "submit_plan_details"

// PlanLiteTool 第一阶段工具：字段少、输出短，先保证分工能拿到。
func PlanLiteTool(agentKeys []string) domain.Tool {
	enum := agentKeys
	if len(enum) == 0 {
		enum = []string{"arch", "backend", "frontend", "qa"}
	}
	raw := fmt.Sprintf(`{
		"type":"object",
		"properties":{
			"objective":{"type":"string","description":"一句话目标"},
			"contracts":{"type":"string","description":"跨角色接口契约：方法、路径、JSON 字段、状态码"},
			"acceptance":{"type":"array","items":{"type":"string"},"description":"可检查的验收标准"},
			"assignments":{
				"type":"array",
				"description":"分工，只分配给确实需要的人",
				"items":{
					"type":"object",
					"properties":{
						"agentKey":{"type":"string","enum":%s},
						"task":{"type":"string","description":"可独立完成的任务"},
						"scope":{"type":"array","items":{"type":"string"},"description":"只许改的路径，如 backend/"}
					},
					"required":["agentKey","task"]
				}
			},
			"reviewerKey":{"type":"string","description":"验收人 key"}
		},
		"required":["objective","assignments"]
	}`, mustJSON(enum))
	return domain.Tool{
		Name:        ToolSubmitPlan,
		Description: "提交分工。只写目标和分工，契约与验证命令稍后提交。",
		Parameters:  json.RawMessage(raw),
	}
}

// PlanDetailsTool 第二阶段工具：接口契约和验证命令。
func PlanDetailsTool() domain.Tool {
	raw := `{
		"type":"object",
		"properties":{
			"verification":{
				"type":"array",
				"description":"服务端要执行的只读/构建/测试命令（argv，不联网）",
				"items":{
					"type":"object",
					"properties":{
						"name":{"type":"string"},
						"argv":{"type":"array","items":{"type":"string"}},
						"cwd":{"type":"string"}
					},
					"required":["name","argv"]
				}
			},
			"notes":{"type":"string","description":"补充说明"}
		},
		"required":["verification"]
	}`
	return domain.Tool{
		Name:        ToolSubmitPlanDetails,
		Description: "提交服务端验证命令。命令必须只读/构建/测试类，不能联网。",
		Parameters:  json.RawMessage(raw),
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// PlanResult 规划阶段的原始结果。
type PlanResult struct {
	Plan      domain.TeamPlan
	TokensIn  int
	TokensOut int
}

// Plan 让 lead 角色产出一份结构化计划。
//
// 这是一次单独的模型调用，不是 Run：
// - 不需要快照、工具循环、审批；
// - tool_choice=required 强制它调用 submit_plan；
// - 拿不到合法计划就直接失败，不把「猜」当成计划。
func (m *Manager) Plan(ctx context.Context, model Model, req PlanRequest) (*PlanResult, error) {
	if strings.TrimSpace(req.Task) == "" {
		return nil, errors.New("agent: empty planning task")
	}
	system := strings.TrimSpace(req.SystemPrompt)
	if system == "" {
		system = DefaultSystemPrompt(req.LeadName)
	}
	system += `

你是这次任务的团队负责人。用 submit_plan 给出可执行分工：
- 每条分工必须能独立完成，写清交付物和范围；简单任务一个人就够，不要硬拆。
- 前后端并行时写清接口契约；验证命令必须真实可执行，由服务端运行。
- 必须调用 submit_plan 工具，不要在正文里写计划。`

	msgs := []domain.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: req.Task},
	}
	// 只拿分工和契约。验证命令由团队在 worker 并行期间补跑（PlanDetails），
	// 不阻塞开工；这里只给一个兜底，防止调用方拿到空验证。
	lite, err := callPlanStage(ctx, model, req, msgs, PlanLiteTool(req.AgentKeys), ToolSubmitPlan,
		[]int{6000, 12000}, []time.Duration{90 * time.Second, 120 * time.Second})
	if err != nil {
		return nil, err
	}
	res := &PlanResult{}
	if lite.usage != nil {
		res.TokensIn += lite.usage.PromptTokens
		res.TokensOut += lite.usage.CompletionTokens
	}
	plan, err := parsePlanLite(lite.calls)
	if err != nil {
		// 半截 JSON（上游截断但没报 length）也重试一次，别让格式抖动毁掉整个团队。
		log.Printf("[plan] lite parse failed: %v", err)
		lite, rerr := callPlanStage(ctx, model, req, msgs, PlanLiteTool(req.AgentKeys), ToolSubmitPlan,
			[]int{12000}, []time.Duration{120 * time.Second})
		if rerr != nil {
			return nil, rerr
		}
		if lite.usage != nil {
			res.TokensIn += lite.usage.PromptTokens
			res.TokensOut += lite.usage.CompletionTokens
		}
		plan, err = parsePlanLite(lite.calls)
		if err != nil {
			return nil, err
		}
	}
	if len(plan.Verification) == 0 {
		plan.Verification = defaultVerification(req.Files)
	}
	res.Plan = plan
	return res, nil
}

// PlanDetails 只生成服务端验证命令，供团队在 worker 并行期间补跑。
//
// 把它从 Plan 里拆出来，是因为验证命令不影响 worker 开工：
// 规划一拿到分工就该开始执行，验证命令晚几秒到没有关系。
func (m *Manager) PlanDetails(ctx context.Context, model Model, req PlanRequest, plan domain.TeamPlan) ([]domain.TeamCommand, int, int, error) {
	if len(req.AgentKeys) < 2 {
		if len(plan.Verification) > 0 {
			return plan.Verification, 0, 0, nil
		}
		return defaultVerification(req.Files), 0, 0, nil
	}
	// 仓库里已经有 go.mod / package.json 这类明确的模块结构时，
	// 验证命令是确定性的（go test / go vet / node --check），不需要
	// 再花一次模型调用去「发明」它。模型只在结构无法判定时介入。
	if cmds := defaultVerification(req.Files); len(cmds) > 0 && deterministicVerificationOK(req.Files) {
		return cmds, 0, 0, nil
	}
	system := strings.TrimSpace(req.SystemPrompt)
	if system == "" {
		system = DefaultSystemPrompt(req.LeadName)
	}
	dmsgs := []domain.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: detailsPrompt(req.Task, plan)},
	}
	stage, err := callPlanStage(ctx, model, req, dmsgs, PlanDetailsTool(), ToolSubmitPlanDetails,
		[]int{6000, 12000}, []time.Duration{90 * time.Second, 120 * time.Second})
	if err != nil {
		return defaultVerification(req.Files), 0, 0, err
	}
	in, out := 0, 0
	if stage.usage != nil {
		in, out = stage.usage.PromptTokens, stage.usage.CompletionTokens
	}
	var merged domain.TeamPlan
	mergePlanDetails(&merged, stage.calls)
	if len(merged.Verification) == 0 {
		// 第二阶段没给出有效命令时，沿用第一阶段计划里的验证，
		// 再不行才按仓库结构兜底。
		if len(plan.Verification) > 0 {
			return plan.Verification, in, out, nil
		}
		return defaultVerification(req.Files), in, out, nil
	}
	return merged.Verification, in, out, nil
}

// callPlanStage 跑一次结构化规划调用，截断/超时按预算逐级重试。
func callPlanStage(ctx context.Context, model Model, req PlanRequest, msgs []domain.ChatMessage, tool domain.Tool, toolName string, budgets []int, timeouts []time.Duration) (*planStage, error) {
	var lastErr error
	for attempt := 0; attempt < len(budgets); attempt++ {
		started := time.Now()
		attemptCtx, cancel := context.WithTimeout(ctx, timeouts[attempt])
		ch, err := model.Stream(attemptCtx, domain.ChatRequest{
			Model:          req.Model,
			Messages:       msgs,
			Tools:          []domain.Tool{tool},
			ToolChoice:     "required",
			ToolChoiceName: toolName,
			Stream:         true,
			Task:           true,
			UserID:         req.UserID,
			MaxTokens:      budgets[attempt],
		})
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		content, calls, usage, finish, streamErr := collect(ch)
		timedOut := attemptCtx.Err() != nil
		cancel()
		if timedOut && streamErr == nil {
			streamErr = errors.New("规划超时")
		}
		if streamErr != nil {
			log.Printf("[plan] %s attempt=%d elapsed=%s err=%v", toolName, attempt+1, time.Since(started).Round(time.Millisecond), streamErr)
			lastErr = streamErr
			continue
		}
		if finish == "length" {
			log.Printf("[plan] %s attempt=%d elapsed=%s truncated at %d tokens", toolName, attempt+1, time.Since(started).Round(time.Millisecond), budgets[attempt])
			lastErr = errors.New("计划被 max_tokens 截断")
			continue
		}
		log.Printf("[plan] %s ok elapsed=%s calls=%d content=%d", toolName, time.Since(started).Round(time.Millisecond), len(calls), len([]rune(content)))
		return &planStage{content: content, calls: calls, usage: usage}, nil
	}
	return nil, lastErr
}

type planStage struct {
	content string
	calls   []domain.ToolCall
	usage   *domain.Usage
}

func parsePlanLite(calls []domain.ToolCall) (domain.TeamPlan, error) {
	var lastErr error
	for _, c := range calls {
		if c.Name != ToolSubmitPlan {
			continue
		}
		var plan domain.TeamPlan
		if err := json.Unmarshal([]byte(c.Arguments), &plan); err != nil {
			lastErr = fmt.Errorf("计划格式不正确：%w（原文开头：%s）", err, clip(c.Arguments, 240))
			continue
		}
		if strings.TrimSpace(plan.Objective) == "" {
			lastErr = errors.New("计划缺少目标")
			continue
		}
		if len(plan.Assignments) == 0 {
			lastErr = errors.New("计划没有分配任何工作")
			continue
		}
		return plan, nil
	}
	if lastErr == nil {
		lastErr = errors.New("规划没有调用 submit_plan")
	}
	return domain.TeamPlan{}, lastErr
}

func mergePlanDetails(plan *domain.TeamPlan, calls []domain.ToolCall) {
	for _, c := range calls {
		if c.Name != ToolSubmitPlanDetails {
			continue
		}
		var d domain.TeamPlan
		if err := json.Unmarshal([]byte(c.Arguments), &d); err != nil {
			log.Printf("[plan] details format invalid: %v", err)
			continue
		}
		// 第二阶段失败/返回空值时不要清掉第一阶段已有的内容。
		if len(d.Acceptance) > 0 {
			plan.Acceptance = d.Acceptance
		}
		if strings.TrimSpace(d.Contracts) != "" {
			plan.Contracts = d.Contracts
		}
		if len(d.Verification) > 0 {
			plan.Verification = d.Verification
		}
		if strings.TrimSpace(d.Notes) != "" {
			plan.Notes = d.Notes
		}
		return
	}
}

func detailsPrompt(task string, plan domain.TeamPlan) string {
	var b strings.Builder
	b.WriteString("任务：\n")
	b.WriteString(task)
	if len(plan.Assignments) > 0 {
		b.WriteString("\n\n已定分工：\n")
		for _, a := range plan.Assignments {
			fmt.Fprintf(&b, "- %s：%s（scope: %s）\n", a.AgentKey, clip(a.Task, 240), strings.Join(a.Scope, " "))
		}
	}
	b.WriteString(`
只提交服务端验证命令，不要再改分工或契约。
命令必须真实可执行、只读/构建/测试类，不能联网（不要用 curl 访问服务）。`)
	return b.String()
}

// deterministicVerificationOK 仓库结构足够明确时，验证命令可以完全由
// 服务端确定，不必问模型。只认 Go 模块（go.mod）这种单一、无歧义的结构。
func deterministicVerificationOK(files []string) bool {
	hasGo := false
	for _, f := range files {
		f = strings.TrimPrefix(f, "./")
		if strings.HasSuffix(f, "go.mod") {
			hasGo = true
			continue
		}
		// 有 JS 工程文件时，前端可能还需要自己的检查，交给模型补。
		base := f
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		switch base {
		case "package.json", "tsconfig.json", "pyproject.toml", "Cargo.toml":
			return false
		}
	}
	return hasGo
}

// defaultVerification 规划细节失败时的兜底：按仓库结构生成最基本的验证。
// 只做确定性的 Go 测试；不猜测其他语言的命令。
func defaultVerification(files []string) []domain.TeamCommand {
	seen := map[string]bool{}
	var out []domain.TeamCommand
	for _, f := range files {
		f = strings.TrimPrefix(f, "./")
		var dir string
		switch {
		case f == "go.mod":
			dir = ""
		case strings.HasSuffix(f, "/go.mod"):
			dir = strings.TrimSuffix(f, "/go.mod")
		default:
			continue
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		suffix := ""
		if dir != "" {
			suffix = "（" + dir + "）"
		}
		out = append(out,
			domain.TeamCommand{Name: "go test ./..." + suffix, Argv: []string{"go", "test", "./..."}, Cwd: dir},
			domain.TeamCommand{Name: "go vet ./..." + suffix, Argv: []string{"go", "vet", "./..."}, Cwd: dir},
			domain.TeamCommand{Name: "go build ./..." + suffix, Argv: []string{"go", "build", "./..."}, Cwd: dir},
		)
	}
	return out
}

// PlanRequest 规划入参。
type PlanRequest struct {
	// UserID 用于把规划阶段的模型成本归因到发起人。
	UserID       string
	Task         string
	Model        string
	SystemPrompt string
	LeadName     string
	AgentKeys    []string
	// Files 工作区文件列表，用于在规划细节失败时兜底生成验证命令。
	Files []string
}
