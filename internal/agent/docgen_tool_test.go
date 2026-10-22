package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 文档工具真的产出可解析的 docx，而不只是一条 JSON 回执。
func TestDocxToolWritesFile(t *testing.T) {
	root := t.TempDir()
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", root, "doc")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: ToolDocx, Arguments: `{"path":"out/report.docx","title":"周报","sections":[{"heading":"进展","bullets":["完成执行器"]}]}`}}},
		{text: "文档已生成。"},
	}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval, Task: "写个周报",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	path := filepath.Join(root, "out", "report.docx")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("docx not written: %v", err)
	}
	if info.Size() < 500 {
		t.Fatalf("docx too small: %d", info.Size())
	}
	raw, _ := os.ReadFile(path)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	var doc string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			buf := new(bytes.Buffer)
			buf.ReadFrom(rc)
			rc.Close()
			doc = buf.String()
		}
	}
	if !strings.Contains(doc, "周报") || !strings.Contains(doc, "完成执行器") {
		t.Fatalf("document.xml missing content")
	}
}

func TestXlsxToolWritesFile(t *testing.T) {
	root := t.TempDir()
	m := NewManager()
	ws, err := m.BindWorkspace("u1", "c1", root, "doc")
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: ToolXlsx, Arguments: `{"path":"data.xlsx","sheets":[{"name":"对照","header":["案例","耗时"],"rows":[["divide","22s"]]}]}`}}},
		{text: "表格已生成。"},
	}}
	r, err := m.Start(context.Background(), fm, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval, Task: "做个表",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, m, r.ID)
	if done.Status != StatusDone {
		t.Fatalf("status=%s err=%s", done.Status, done.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "data.xlsx")); err != nil {
		t.Fatalf("xlsx not written: %v", err)
	}
	// scope 依然生效：文档工具也要受写入边界约束
	fm2 := &fakeModel{steps: []fakeStep{
		{calls: []domain.ToolCall{{ID: "c1", Name: ToolDocx, Arguments: `{"path":"secret.docx","title":"x"}`}}},
		{text: "被拒了"},
	}}
	r2, err := m.Start(context.Background(), fm2, StartRequest{
		WorkspaceID: ws.ID, UserID: "u1", Permission: PermissionApproval, Scope: []string{"docs/"}, Task: "越界写",
	})
	if err != nil {
		t.Fatal(err)
	}
	d2 := waitRun(t, m, r2.ID)
	if len(d2.ToolCalls) == 0 || d2.ToolCalls[0].Status != "denied" {
		t.Fatalf("out-of-scope doc write must be denied: %+v", d2.ToolCalls)
	}
}
