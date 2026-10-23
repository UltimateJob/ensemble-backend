package agent

import "testing"

// 「逐项批准」是最容易被权限矩阵漏掉的一档：
// 漏了会走到 default 分支，连 read 都被拒绝——群里看着像上游挂了。
func TestPermissionAskMatrix(t *testing.T) {
	cases := []struct {
		tool    string
		risk    string
		allowed bool
		needAsk bool
	}{
		{"read", "read", true, false},
		{"list", "read", true, false},
		{"glob", "read", true, false},
		{"grep", "read", true, false},
		{"write", "write", true, true},
		{"edit", "write", true, true},
		{"run", "read", true, true},
		{"run", "write", true, true},
	}
	for _, c := range cases {
		allowed, need, reason := permissionForBase(PermissionAsk, c.tool, c.risk)
		if allowed != c.allowed || need != c.needAsk {
			t.Errorf("%s/%s: got allowed=%v need=%v (%s), want allowed=%v need=%v",
				c.tool, c.risk, allowed, need, reason, c.allowed, c.needAsk)
		}
	}
}

func TestPermissionAskForceApprovalRemote(t *testing.T) {
	// 远程设备开关只能把「写/命令」从自动变成询问，不能把只读也拖下水。
	allowed, need, _ := permissionFor(PermissionAsk, "run", "read", true)
	if !allowed || !need {
		t.Fatalf("ask + forceApproval run: got %v/%v", allowed, need)
	}
	allowed, need, _ = permissionFor(PermissionAsk, "read", "read", true)
	if !allowed || need {
		t.Fatalf("ask + forceApproval read: got %v/%v", allowed, need)
	}
}
