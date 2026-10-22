package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MaxWriteBytes 单次写入上限。超过就拒绝，避免把 artifact 当消息塞。
const MaxWriteBytes = 2 << 20 // 2 MiB

// 搜索/遍历预算。这些数值是「让工具在一次调用里必定结束」的硬边界。
const (
	MaxGlobResults  = 2000
	MaxGrepResults  = 200
	MaxGrepFileSize = 1 << 20 // 单文件 1 MiB
	MaxGrepFiles    = 4000
	maxGrepDepth    = 24
)

// skipDirs 遍历时整体跳过，避免扫描依赖与构建产物。
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".next": true, ".cache": true,
	"__pycache__": true, ".venv": true, "venv": true, ".idea": true,
	".dsh": true, "bin": true,
}

// Write 原子写入一个文件：先写同目录临时文件再 rename。
//
// 父目录不存在时会自动创建（agent 用 write 落新文件是常见情况）。
// 返回写入的字节数与是否为新建。
func (w *Workspace) Write(p string, content []byte) (int, bool, error) {
	if len(content) > MaxWriteBytes {
		return 0, false, fmt.Errorf("%w: %d bytes > %d", ErrTooLarge, len(content), MaxWriteBytes)
	}
	full, err := w.resolve(p, false)
	if err != nil {
		return 0, false, err
	}
	existed := false
	if info, err := os.Stat(full); err == nil {
		if info.IsDir() {
			return 0, false, fmt.Errorf("%w: %s", ErrIsDir, p)
		}
		existed = true
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return 0, false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".dsh-write-*")
	if err != nil {
		return 0, false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后这里是 no-op
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return 0, false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return 0, false, err
	}
	if err := tmp.Close(); err != nil {
		return 0, false, err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return 0, false, err
	}
	if err := os.Rename(tmpName, full); err != nil {
		return 0, false, err
	}
	return len(content), !existed, nil
}

// Edit 一次精确替换。
//
// All=false 时 Old 必须在文件里唯一出现，否则报 ErrEditAmbig——
// 让模型先读文件再改，比「改错一行」便宜得多。
type Edit struct {
	Old string `json:"old"`
	New string `json:"new"`
	All bool   `json:"all,omitempty"`
}

// EditResult 替换结果。
type EditResult struct {
	Path       string `json:"path"`
	Replaced   int    `json:"replaced"`
	Edits      int    `json:"edits"`
	BeforeSize int    `json:"beforeSize"`
	AfterSize  int    `json:"afterSize"`
}

// Apply 在文件内容上做替换，纯函数，便于测试。
func ApplyEdits(src string, edits []Edit) (string, int, error) {
	out := src
	total := 0
	for i, e := range edits {
		if e.Old == "" {
			return src, 0, fmt.Errorf("edit %d: empty anchor", i)
		}
		count := strings.Count(out, e.Old)
		if count == 0 {
			return src, 0, fmt.Errorf("%w: edit %d", ErrEditNoMatch, i)
		}
		if !e.All && count > 1 {
			return src, 0, fmt.Errorf("%w: edit %d matched %d times", ErrEditAmbig, i, count)
		}
		if e.All {
			out = strings.ReplaceAll(out, e.Old, e.New)
			total += count
		} else {
			out = strings.Replace(out, e.Old, e.New, 1)
			total++
		}
	}
	return out, total, nil
}

// Edit 应用一组替换并原子写回。
func (w *Workspace) Edit(p string, edits []Edit) (*EditResult, error) {
	if len(edits) == 0 {
		return nil, errors.New("workspace: no edits")
	}
	full, err := w.resolve(p, true)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrIsDir, p)
	}
	if info.Size() > MaxGetEditBytes {
		return nil, fmt.Errorf("%w: file %d bytes", ErrTooLarge, info.Size())
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	next, replaced, err := ApplyEdits(string(raw), edits)
	if err != nil {
		return nil, err
	}
	if len(next) > MaxWriteBytes {
		return nil, fmt.Errorf("%w: result %d bytes", ErrTooLarge, len(next))
	}
	if _, _, err := w.Write(p, []byte(next)); err != nil {
		return nil, err
	}
	rel, _ := w.Rel(full)
	return &EditResult{
		Path:       rel,
		Replaced:   replaced,
		Edits:      len(edits),
		BeforeSize: len(raw),
		AfterSize:  len(next),
	}, nil
}

// MaxGetEditBytes 允许 edit 的文件上限（比读上限大，但仍有界）。
const MaxGetEditBytes = 8 << 20

// Glob 按 shell 通配符匹配工作区内的文件，返回相对路径。
func (w *Workspace) Glob(pattern string) ([]string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("workspace: empty glob pattern")
	}
	if filepath.IsAbs(pattern) || strings.HasPrefix(pattern, "/") {
		return nil, ErrAbsolute
	}
	if strings.HasPrefix(filepath.Clean(pattern), "..") {
		return nil, ErrEscape
	}
	// 用 WalkDir 自己匹配，避免 filepath.Glob 对深度的语法限制，也便于施加预算。
	re, err := globToRegexp(pattern)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(w.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != w.Root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := w.Rel(path)
		if relErr != nil {
			return nil
		}
		if re.MatchString(rel) {
			out = append(out, rel)
			if len(out) >= MaxGlobResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// globToRegexp 把 `**/*.go` / `src/*.ts` 这类模式编译成正则。
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				// ** 匹配任意层级
				b.WriteString(".*")
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '[', ']', '{', '}', '\\':
			b.WriteString("\\")
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// Match 一条搜索命中。
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep 在工作区内按正则搜索文本文件。
//
// dir 为空表示从 root 开始；结果受 MaxGrepResults 限制。
func (w *Workspace) Grep(pattern, dir string, limit int) ([]Match, error) {
	if pattern == "" {
		return nil, errors.New("workspace: empty grep pattern")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxGrepResults {
		limit = MaxGrepResults
	}
	start, err := w.resolve(dir, true)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(start); statErr != nil {
		return nil, statErr
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotDir, dir)
	}

	var out []Match
	files := 0
	err = filepath.WalkDir(start, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != start {
				return filepath.SkipDir
			}
			if depth := strings.Count(strings.TrimPrefix(path, start), string(filepath.Separator)); depth > maxGrepDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxGrepFileSize {
			return nil
		}
		files++
		if files > MaxGrepFiles {
			return fs.SkipAll
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if isBinary(raw) {
			return nil
		}
		rel, relErr := w.Rel(path)
		if relErr != nil {
			return nil
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if re.MatchString(line) {
				out = append(out, Match{Path: rel, Line: i + 1, Text: clip(line, 400)})
				if len(out) >= limit {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func isBinary(raw []byte) bool {
	n := len(raw)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if raw[i] == 0 {
			return true
		}
	}
	return false
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// Mkdir 创建目录（含父级）。
func (w *Workspace) Mkdir(p string) error {
	full, err := w.resolve(p, false)
	if err != nil {
		return err
	}
	return os.MkdirAll(full, 0o755)
}

// Remove 删除文件或空目录。递归删除只在调用方显式要求时使用。
func (w *Workspace) Remove(p string, recursive bool) error {
	full, err := w.resolve(p, false)
	if err != nil {
		return err
	}
	if full == w.Root {
		return errors.New("workspace: cannot remove root")
	}
	if recursive {
		return os.RemoveAll(full)
	}
	return os.Remove(full)
}
