package service

import (
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// ResolveAgentPermission 解析一个角色最终生效的执行权限。
//
// 优先级：会话里按角色覆盖 → 会话默认 → 角色自身设置 → 代码场景兜底 ask。
// 会话默认排在角色自身之前：用户在群设置里选的档位必须压过内置角色的默认值，
// 否则「逐项批准」会被角色的 approval 静默盖掉。
// 返回空字符串表示「这个角色不参与执行」。
func ResolveAgentPermission(conv *domain.Conversation, a Agent) string {
	if conv == nil {
		return ""
	}
	if p := strings.TrimSpace(conv.AgentPermissions[a.Key]); p != "" {
		return p
	}
	if p := strings.TrimSpace(conv.DefaultPermission); p != "" {
		return p
	}
	if p := strings.TrimSpace(a.Permission); p != "" {
		return p
	}
	if conv.Workspace != nil || conv.Studio == "code" {
		// 代码工作室默认最安全的「逐项批准」，用户可在创建时/群设置里改。
		return "ask"
	}
	return ""
}

// IsExecutablePermission 该权限是否允许角色真的改文件。
func IsExecutablePermission(p string) bool {
	switch strings.TrimSpace(p) {
	case "ask", "approval", "full":
		return true
	}
	return false
}

// AgentByKey 按 key 或名字找角色。
func AgentByKey(agents []Agent, keyOrName string) *Agent {
	keyOrName = strings.TrimSpace(keyOrName)
	if keyOrName == "" {
		return nil
	}
	for i := range agents {
		if agents[i].Key == keyOrName || agents[i].Name == keyOrName {
			return &agents[i]
		}
	}
	return nil
}

// WorkspaceBound 会话是否已经绑定工作区。
func WorkspaceBound(conv *domain.Conversation) bool {
	return conv != nil && conv.Workspace != nil && strings.TrimSpace(conv.Workspace.Root) != ""
}
