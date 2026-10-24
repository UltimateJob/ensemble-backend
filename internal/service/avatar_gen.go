package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/gateway"
)

// 角色头像生成。
//
// 为什么提示词是固定的：用户填的是「名字 + 一句简介」，直接拼进
// 提示词里生成，风格会随简介的措辞漂移——一会儿写实、一会儿三维、
// 一会儿插画，一排头像放在群里像从五个 app 里各抓了一个。
//
// 所以平台定死风格描述，只把「名字 + 简介」当作主体线索注入。
// 用户想换一个，就重新生成（每次随机种子）。

const (
	// avatarStyle 平台统一的头像风格。
	//
	// 这套描述是实测定下来的：3D 黏土质感 + 柔和正面光 + 纯色背景，
	// 在 32-64px 的小尺寸下仍然能认出是谁，同时和纸团现有的预生成
	// 头像（黏土小人）风格一致。
	avatarStyle = "A soft 3D clay-render character portrait, head and shoulders, " +
		"centered, facing the camera, gentle natural expression, warm neutral seamless background, " +
		"muted but natural colors in hair and clothing (not monochrome, not plastic), " +
		"soft diffused studio lighting, subtle matte clay texture, clean minimal composition, " +
		"no text, no logo, no border. Square 1:1 framing with comfortable headroom."
)

// AvatarPrompt 由角色名与人设拼出完整的头像提示词。
//
// 简介只取前 120 字：太长会把风格描述淹掉，模型会开始按简介里的
// 具体情节构图（比如简介写「热爱登山」，就给你一个在山顶的人）。
// 我们要的是「这个人长什么样」，不是「这个人在干什么」。
func AvatarPrompt(name, title, prompt string) string {
	name = strings.TrimSpace(name)
	title = strings.TrimSpace(title)
	brief := strings.TrimSpace(prompt)
	if len([]rune(brief)) > 120 {
		brief = string([]rune(brief)[:120])
	}

	var b strings.Builder
	b.WriteString("Subject: a character named ")
	if name != "" {
		b.WriteString(name)
	} else {
		b.WriteString("(unnamed)")
	}
	b.WriteString(".")
	if title != "" {
		b.WriteString(" Role: " + title + ".")
	}
	if brief != "" {
		// 明确告诉模型「只用来提示气质，不要画场景」——
		// 不加这句时，简介里的动词会被当成情节。
		b.WriteString(" Personality notes (use only to inform expression and styling, do NOT depict any scene or action): ")
		b.WriteString(brief)
		b.WriteString(".")
	}
	b.WriteString(" ")
	b.WriteString(avatarStyle)
	return b.String()
}

// GenerateAvatar 生成一张头像并返回落盘路径与提示词。
//
// 用会话/角色默认渠道（轮询）。头像体积小、不需要最贵的模型，
// 走默认路由即可。
func (j *ImageJobs) GenerateAvatar(ctx context.Context, userID, name, title, prompt, outDir string) (*gateway.ImageResult, string, error) {
	if !j.Enabled() {
		return nil, "", fmt.Errorf("没有配置生图渠道")
	}
	gen := j.pickGen("")
	if gen == nil {
		return nil, "", fmt.Errorf("没有配置生图渠道")
	}
	full := AvatarPrompt(name, title, prompt)
	res, err := gen.Generate(ctx, gateway.ImageRequest{
		Prompt: full,
		Size:   "1024x1024",
		N:      1,
	}, outDir)
	if err != nil {
		return nil, "", err
	}
	return res, full, nil
}
