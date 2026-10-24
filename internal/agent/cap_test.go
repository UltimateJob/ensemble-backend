package agent

import "testing"

// 生图工具只给有 image 能力的角色。
// 之前是「配了渠道就人人能用」，一个写代码的角色突然会画图，行为不可预期。
func TestImageToolGatedByCapability(t *testing.T) {
	tools := ToolsWithImage(true, nil...)
	has := false
	for _, x := range tools {
		if x.Name == ToolImage {
			has = true
		}
	}
	if !has {
		t.Fatal("配了渠道 + 有能力时应该有生图工具")
	}

	tools = ToolsWithImage(false, nil...)
	for _, x := range tools {
		if x.Name == ToolImage {
			t.Fatal("没有能力时不该出现生图工具")
		}
	}
}
