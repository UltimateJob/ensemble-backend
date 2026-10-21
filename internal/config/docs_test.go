package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 文档里提到的源文件必须存在。
// 写文档时很容易写出一个不存在的路径——上一轮的交接文档就差点这样。
func TestDocsReferenceRealFiles(t *testing.T) {
	docs := []string{"../../README.md", "../../CHANGELOG.md", "../../docs/roadmap.md", "../../docs/handoff.md"}

	for _, path := range docs {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(src), "\n") {
			// 明确标了「新建」「还不存在」的跳过
			if strings.Contains(line, "**新建**") || strings.Contains(line, "还不存在") {
				continue
			}
			for _, m := range regexp.MustCompile("`((?:internal|web|cmd)/[\\w./-]+\\.(?:go|js|css))`").FindAllStringSubmatch(line, -1) {
				rel := filepath.Join("..", "..", m[1])
				if _, err := os.Stat(rel); err != nil {
					t.Errorf("%s 提到 %s，但文件不存在", filepath.Base(path), m[1])
				}
			}
		}
	}
}
