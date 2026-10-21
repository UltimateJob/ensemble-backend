package agent

import (
	"path"
	"strings"
)

// Scope 一个 Agent 的写入边界：只允许改这些路径。
//
// 为什么要有它：上一轮实测里，负责前端页面的 agent 顺手把后端 Go 文件也改了。
// 它在总结里如实说明了，但这说明「职责边界」只写在提示词里——
// 模型可以被说服，代码不会。写在提示词里的规矩，执行器必须再兜一次。
//
// 语义：
//   - 空 Scope 表示不限制（兼容旧数据）；
//   - 以 "/" 结尾的写法是目录前缀，如 "web/"；
//   - 含 "/" 的写法按整条相对路径匹配 glob，如 "web/**/*.html"；
//   - 不含 "/" 的写法只匹配文件名，如 "*.go"。
type Scope []string

// Allows 判断某个工作区相对路径是否在范围内。
func (s Scope) Allows(p string) bool {
	if len(s) == 0 {
		return true
	}
	p = strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
	for _, raw := range s {
		pat := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, "/") {
			if strings.HasPrefix(p, pat) {
				return true
			}
			continue
		}
		if strings.Contains(pat, "/") {
			if globMatch(pat, p) {
				return true
			}
			continue
		}
		// 只有文件名：匹配任意目录下的同名文件
		if globMatch(pat, path.Base(p)) {
			return true
		}
	}
	return false
}

// String 给错误信息用。
func (s Scope) String() string {
	if len(s) == 0 {
		return "不限"
	}
	return strings.Join(s, ", ")
}

// globMatch 支持 ** 的 glob。
//
// 标准库 path.Match 不支持 **，但执行器里 "web/**/*.ts" 这种写法很常见，
// 所以把 ** 先展开成层级通配再交给 path.Match。
func globMatch(pattern, name string) bool {
	if !strings.Contains(pattern, "**") {
		ok, err := path.Match(pattern, name)
		return err == nil && ok
	}
	// 把 **/ 变成「任意层级」：逐段尝试
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		ok, err := path.Match(strings.ReplaceAll(pattern, "**", "*"), name)
		return err == nil && ok
	}
	prefix := strings.TrimSuffix(parts[0], "/")
	suffix := strings.TrimPrefix(parts[1], "/")
	if prefix != "" && !strings.HasPrefix(name, prefix) {
		// 允许 ** 匹配零层时 prefix 只是目录名的一部分
		if !strings.HasPrefix(name, prefix+"/") {
			return false
		}
	}
	rest := name
	if prefix != "" {
		rest = strings.TrimPrefix(name, prefix)
		rest = strings.TrimPrefix(rest, "/")
	}
	if suffix == "" {
		return true
	}
	// 在剩余路径的任意层级上尝试匹配 suffix
	segs := strings.Split(rest, "/")
	for i := range segs {
		cand := strings.Join(segs[i:], "/")
		if ok, err := path.Match(suffix, cand); err == nil && ok {
			return true
		}
	}
	return false
}
