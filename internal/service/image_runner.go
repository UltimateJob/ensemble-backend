package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/gateway"
)

// ImageRunner 让生图渠道满足 agent.ImageRunner。
//
// Agent 走同步路径：它需要拿到文件路径才能继续（比如把图放进文档）。
// 前台用户走异步路径（ImageJobs），因为界面等得起、Agent 等不起。
// 两条路复用同一个 gateway.ImageGen。

// Generate 生成图片并写进工作区。
//
// 落盘位置是工作区下的 generated/images/。选这个目录而不是根目录，
// 是为了让 Agent 之后的 list/glob 不会把图片和源码混在一起。
func (j *ImageJobs) Generate(ctx context.Context, userID, workspaceRoot, prompt, size string, n int) ([]agent.ImageFile, error) {
	if !j.Enabled() {
		return nil, errors.New("没有配置生图渠道")
	}
	outDir := filepath.Join(workspaceRoot, "generated", "images")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	res, err := j.gen.Generate(ctx, gateway.ImageRequest{
		Prompt: prompt, Size: size, N: n,
	}, outDir)
	if err != nil {
		return nil, err
	}

	out := make([]agent.ImageFile, 0, len(res.Files))
	for _, f := range res.Files {
		// 相对工作区路径：Agent 和前端都按这个口径引用文件
		rel, rerr := filepath.Rel(workspaceRoot, f.Path)
		if rerr != nil {
			rel = f.Path
		}
		out = append(out, agent.ImageFile{
			Path: strings.TrimPrefix(rel, "./"),
			Size: f.Size,
		})
	}
	return out, nil
}
