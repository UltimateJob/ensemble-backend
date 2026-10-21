package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// 版本号在三处出现：代码、README、CHANGELOG。
// 之前一直是 0.1.0 没人管，这里加个守卫。
func TestVersionConsistency(t *testing.T) {
	code, err := os.ReadFile("../../cmd/server/main.go")
	if err != nil {
		t.Skip("读不到 main.go")
	}
	m := regexp.MustCompile(`const version = "([^"]+)"`).FindStringSubmatch(string(code))
	if m == nil {
		t.Fatal("main.go 里找不到 version 常量")
	}
	ver := m[1]

	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Skip("读不到 README")
	}
	if !strings.Contains(string(readme), ver) {
		t.Fatalf("README 里没有版本号 %s", ver)
	}

	changelog, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Skip("读不到 CHANGELOG")
	}
	if !strings.Contains(string(changelog), "## "+ver) {
		t.Fatalf("CHANGELOG 里没有 %s 这一条", ver)
	}
}
