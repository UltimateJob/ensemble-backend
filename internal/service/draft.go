package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
)

// Drafter 用模型把一个名字/关键词扩写成完整角色设定。
type Drafter struct {
	gw *gateway.Gateway
}

func NewDrafter(gw *gateway.Gateway) *Drafter { return &Drafter{gw: gw} }

// Draft 生成角色。返回可直接加入场景的 Agent。
func (d *Drafter) Draft(ctx context.Context, name, keywords string) (Agent, error) {
	if strings.TrimSpace(name) == "" {
		return Agent{}, fmt.Errorf("请先给角色起个名字")
	}

	sys := `你是角色设定师。根据用户给的名字和关键词，产出一个可以直接放进群聊的角色。
只输出 JSON，不要 markdown 代码块，不要解释。字段：
{"title":"8 字以内的身份描述","prompt":"这个角色的说话方式与性格，60 字以内，用第二人称写","emoji":"一个字"}
要求：prompt 必须具体到能指导对话——写清语气、句子长短、口头习惯、绝不做的事。`

	user := fmt.Sprintf("名字：%s\n关键词：%s", name, firstOr(keywords, "未指定，你来定"))

	stream, err := d.gw.Stream(ctx, domain.ChatRequest{
		Messages: []domain.ChatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		Stream: true,
	})
	if err != nil {
		return Agent{}, err
	}

	var buf strings.Builder
	for c := range stream {
		if c.Err != nil {
			return Agent{}, c.Err
		}
		buf.WriteString(c.Content)
		if c.Done {
			break
		}
	}

	raw := strings.TrimSpace(buf.String())
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var out struct {
		Title  string `json:"title"`
		Prompt string `json:"prompt"`
		Emoji  string `json:"emoji"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		// 模型偶尔不按格式（演示上游就是），给一段能用的兜底人设。
		// 这里不能太短——存进角色库有 10 字门槛，太短会被拒。
		out.Prompt = fallbackPrompt(name, keywords)
		out.Title = "自定义角色"
	}
	if len([]rune(out.Prompt)) < 10 {
		out.Prompt = fallbackPrompt(name, keywords)
	}

	key := "custom_" + fmt.Sprint(len(name)) + "_" + name
	return Agent{
		Key:    key,
		Name:   name,
		Title:  firstOr(out.Title, "自定义角色"),
		Emoji:  firstOr(out.Emoji, string([]rune(name)[0])),
		Color:  pickColor(name),
		Prompt: firstOr(out.Prompt, "说话自然、简短。"),
	}, nil
}

var palette = []string{
	"#B45309", "#0369A1", "#6D28D9", "#B91C1C", "#047857",
	"#BE185D", "#7C3AED", "#0F766E", "#C2410C", "#1D4ED8",
}

func pickColor(seed string) string {
	h := 0
	for _, r := range seed {
		h = (h*31 + int(r)) % 1000003
	}
	if h < 0 {
		h = -h
	}
	return palette[h%len(palette)]
}

// fallbackPrompt 演示上游（以及模型不按格式时）用的兜底人设。
// 必须够长够具体：要有语气、句子长短、口头习惯，
// 否则存进角色库时会被 10 字门槛拒掉。
func fallbackPrompt(name, keywords string) string {
	kw := strings.TrimSpace(keywords)
	if kw == "" {
		kw = "日常琐事"
	}
	return fmt.Sprintf(
		"你是%s，说话简短直接，不绕弯子。话题围绕%s展开，回答常在一两句内，偶尔反问一句。不主动说教，也不长篇大论。",
		name, kw,
	)
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
