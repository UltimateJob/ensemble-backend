package docgen

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func zipFiles(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := rc.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		rc.Close()
		out[f.Name] = sb.String()
	}
	return out
}

func TestWriteDocx(t *testing.T) {
	var buf bytes.Buffer
	err := WriteDocx(&buf, Doc{
		Title:    "测试报告",
		Subtitle: "2026-10-22",
		Sections: []Section{
			{Heading: "结论", Paragraphs: []string{"一切正常 & <通过>"}},
			{Heading: "明细", Bullets: []string{"第一项", "第二项"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := zipFiles(t, buf.Bytes())
	doc, ok := files["word/document.xml"]
	if !ok {
		t.Fatalf("missing document.xml, got %v", keys(files))
	}
	for _, want := range []string{"测试报告", "一切正常 &amp; &lt;通过&gt;", "• 第一项", "明细"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document.xml missing %q", want)
		}
	}
	if _, ok := files["[Content_Types].xml"]; !ok {
		t.Fatal("missing content types")
	}
}

func TestWriteXlsx(t *testing.T) {
	var buf bytes.Buffer
	err := WriteXlsx(&buf, []Sheet{{
		Name:   "清单",
		Header: []string{"项目", "状态"},
		Rows:   [][]string{{"接口", "完成"}, {"页面", "进行中"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	files := zipFiles(t, buf.Bytes())
	sheet, ok := files["xl/worksheets/sheet1.xml"]
	if !ok {
		t.Fatalf("missing sheet, got %v", keys(files))
	}
	for _, want := range []string{`r="A1"`, "项目", "进行中", `r="B3"`} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("sheet1.xml missing %q", want)
		}
	}
	if !strings.Contains(files["xl/workbook.xml"], "清单") {
		t.Fatal("workbook missing sheet name")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// 用真正的 Office 解析器读一遍，确认不是「自己觉得对」。
func TestDocxOpensInPythonDocx(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	if err := exec.Command(py, "-c", "import docx").Run(); err != nil {
		t.Skip("python-docx not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "t.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDocx(f, Doc{Title: "标题", Sections: []Section{{Heading: "节", Paragraphs: []string{"正文"}}}}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	out, err := exec.Command(py, "-c", `
import docx,sys
d=docx.Document(sys.argv[1])
print("|".join(p.text for p in d.paragraphs))
`, path).Output()
	if err != nil {
		t.Fatalf("python-docx failed: %v", err)
	}
	if !strings.Contains(string(out), "标题") || !strings.Contains(string(out), "正文") {
		t.Fatalf("python-docx read: %q", out)
	}
}
