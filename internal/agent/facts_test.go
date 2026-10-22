package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func TestUnsourcedNumbers(t *testing.T) {
	outputs := []string{`{"count":12,"files":["a","b"]}`, "耗时 173ms，覆盖 8 条用例"}
	cases := []struct {
		text string
		want int
	}{
		{"覆盖 12 条用例", 0},              // 有来源
		{"耗时 173 ms", 0},              // 空格归一化后有来源
		{"覆盖 50 条用例", 1},              // 编的
		{"改了 17 个文件", 1},              // 编的
		{"2026-10-22 上线，端口 :8099", 0}, // 日期/端口不是统计量，不查
		{"正确率 98%", 1},                // 编的
	}
	for _, c := range cases {
		got := UnsourcedNumbers(c.text, outputs)
		if len(got) != c.want {
			t.Errorf("%q => %v, want %d", c.text, got, c.want)
		}
	}
}

// 编造数字的文档会被拒绝，并且提示要先去取真实数据。
func TestDocxRejectsFabricatedNumbers(t *testing.T) {
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", t.TempDir(), "doc")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: ToolDocx, Arguments: `{"path":"r.docx","title":"周报","sections":[{"heading":"数据","bullets":["完成 42 个任务"]}]}`}}},
		{calls: []domain.ToolCall{{ID: "c2", Name: ToolDocx, Arguments: `{"path":"r.docx","title":"周报","sections":[{"heading":"数据","bullets":["本周推进了执行器"]}]}`}}},
		{text: "改好了"},
	}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval, Task: "写周报",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s", done.Status)
	}
	if len(done.ToolCalls) != 2 {
		t.Fatalf("tools=%d", len(done.ToolCalls))
	}
	if done.ToolCalls[0].Status != "denied" {
		t.Fatalf("fabricated number should be denied: %+v", done.ToolCalls[0])
	}
	if !strings.Contains(done.ToolCalls[0].Output, "没有来源") {
		t.Fatalf("denial message: %s", done.ToolCalls[0].Output)
	}
	if done.ToolCalls[1].Status != "ok" {
		t.Fatalf("clean text should pass: %+v", done.ToolCalls[1])
	}
}
