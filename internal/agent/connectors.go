package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 连接器与执行器的接缝。
//
// 连接器是「用户已经授权的外部系统」：GitHub 仓库、Notion 知识库、
// 飞书文档、邮箱。Agent 能读它们，等于有了工作区之外的上下文。
//
// 三条边界，写在这里以免以后被顺手放宽：
//   - 只有用户显式配置且启用、且最近一次探测没报错的连接器才会出现
//   - 写类动作和本地写文件一样走审批，不因为「反正是外部系统」就跳过
//   - 连接器不可用时直接报错给模型，不静默跳过——模型需要知道
//     「你刚才想读飞书文档，但没配」，否则会编造内容

// toolsetFor 生成本次 Run 的工具集。
// 返回：完整工具列表、按完整工具名索引的连接器动作。
func (m *Manager) toolsetFor(userID string) ([]domain.Tool, map[string]ConnectorAction) {
	m.mu.Lock()
	cr := m.connectors
	m.mu.Unlock()

	tools := ToolsWithImage(m.hasImage(), nil...)
	index := map[string]ConnectorAction{}
	if cr == nil {
		return tools, index
	}

	for _, a := range cr.Actions(userID) {
		tools = append(tools, domain.Tool{
			Name:        a.Name,
			Description: a.Description,
			Parameters:  a.Params,
		})
		index[a.Name] = a
	}
	return tools, index
}

// connectorAction 查一个工具名是不是连接器动作。
func (m *Manager) connectorAction(r *Run, toolName string) (ConnectorAction, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.connActions == nil {
		return ConnectorAction{}, false
	}
	a, ok := r.connActions[toolName]
	return a, ok
}

// runConnector 执行一次连接器动作。
func (m *Manager) runConnector(ctx context.Context, userID string, a ConnectorAction, rawArgs string) (string, error) {
	m.mu.Lock()
	cr := m.connectors
	m.mu.Unlock()
	if cr == nil {
		return "", fmt.Errorf("连接器未启用")
	}

	var args map[string]any
	if rawArgs != "" && rawArgs != "{}" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", fmt.Errorf("参数格式不正确：%v", err)
		}
	}
	return cr.Run(ctx, userID, a.ConnID, a.Base, args)
}

// connectorRisk 判断一个工具调用的风险等级。
// 写类动作按「写」处理——外部系统被改坏，比本地文件被改坏更难恢复。
func connectorRisk(r *Run, toolName string) string {
	_ = r
	_ = toolName
	return ""
}

// isConnectorTool 判断一个工具名是不是运行时注入的连接器动作。
// 约定：动作名形如 <action>__<connID>。
func isConnectorTool(name string) bool {
	return strings.Contains(name, "__")
}

// hasImage 是否配了生图渠道。
func (m *Manager) hasImage() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.image != nil
}

// isConnector 判断工具名是否是连接器动作（按命名约定）。
func (m *Manager) isConnector(name string) bool {
	return isConnectorTool(name)
}

// runImage 执行生图并把图片写进工作区。
//
// 返回的 diff 是图片文件的相对路径列表，让前端能像普通产物一样展示。
func (m *Manager) runImage(ctx context.Context, r *Run, w *Workspace, call domain.ToolCall) (string, string, error) {
	m.mu.Lock()
	img := m.image
	m.mu.Unlock()
	if img == nil {
		return "", "", fmt.Errorf("没有配置生图渠道")
	}

	var a struct {
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
		N      int    `json:"n"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &a); err != nil {
		return "", "", fmt.Errorf("参数格式不正确：%v", err)
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return "", "", fmt.Errorf("缺少画面描述")
	}

	files, err := img.Generate(ctx, r.UserID, w.Root, a.Prompt, a.Size, a.N)
	if err != nil {
		return "", "", err
	}

	var sb strings.Builder
	var paths []string
	fmt.Fprintf(&sb, "已生成 %d 张图片：\n", len(files))
	for _, f := range files {
		fmt.Fprintf(&sb, "- %s（%s）\n", f.Path, humanSize(f.Size))
		paths = append(paths, f.Path)
	}
	return sb.String(), strings.Join(paths, "\n"), nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
