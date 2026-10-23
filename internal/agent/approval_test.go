package agent

import (
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/workspace"
)

// 远程设备要求审批时，full 权限也要先问一句。
func TestForceApprovalOnFullPermission(t *testing.T) {
	// 写文件：full 本来直接放行，设备要求审批后必须审批
	allowed, need, reason := permissionFor(PermissionFull, ToolWrite, workspace.RiskWrite, true)
	if !allowed {
		t.Fatal("要求审批不等于拒绝")
	}
	if !need {
		t.Fatal("设备要求审批时，写文件应该需要审批")
	}
	if reason == "" {
		t.Fatal("应该说明为什么需要审批")
	}

	// 跑命令同理
	_, need, _ = permissionFor(PermissionFull, ToolRun, workspace.RiskWrite, true)
	if !need {
		t.Fatal("设备要求审批时，跑命令应该需要审批")
	}

	// 读操作不该被打扰——远程看文件不需要审批，
	// 否则手机上每翻一个文件点一次，产品就没法用了
	_, need, _ = permissionFor(PermissionFull, ToolRead, workspace.RiskRead, true)
	if need {
		t.Fatal("读操作用户不该被审批打扰")
	}
}

// 关闭审批开关后恢复原来的行为。
func TestForceApprovalOffKeepsFullBehavior(t *testing.T) {
	_, need, _ := permissionFor(PermissionFull, ToolWrite, workspace.RiskWrite, false)
	if need {
		t.Fatal("关掉设备审批后，full 权限写文件不该再问")
	}
	_, need, _ = permissionFor(PermissionFull, ToolRun, workspace.RiskWrite, false)
	if need {
		t.Fatal("关掉设备审批后，full 权限跑命令不该再问")
	}
}

// 审批开关不能放宽 readonly：设备策略只加限制，不加权限。
func TestForceApprovalNeverWidensReadonly(t *testing.T) {
	allowed, _, _ := permissionFor(PermissionReadonly, ToolWrite, workspace.RiskWrite, true)
	if allowed {
		t.Fatal("只读权限 + 要求审批，写文件仍然应该被拒")
	}
}

// 关键安全边界：设备策略不能绕过 critical 黑名单。
func TestForceApprovalCannotBypassCriticalBlacklist(t *testing.T) {
	allowed, _, _ := permissionFor(PermissionFull, ToolRun, workspace.RiskCritical, false)
	if allowed {
		t.Fatal("critical 命令在 full 权限下就应该被拒")
	}
	allowed, _, _ = permissionFor(PermissionFull, ToolRun, workspace.RiskCritical, true)
	if allowed {
		t.Fatal("设备策略不该让 critical 命令放行")
	}
}

// approval 权限下的既有行为不受设备开关影响（本来就要审批）。
func TestForceApprovalIdempotentOnApprovalPermission(t *testing.T) {
	_, need1, _ := permissionFor(PermissionApproval, ToolRun, workspace.RiskWrite, false)
	_, need2, _ := permissionFor(PermissionApproval, ToolRun, workspace.RiskWrite, true)
	if !need1 || !need2 {
		t.Fatal("approval 权限跑命令本来就该审批，开关前后都应为 true")
	}
}
