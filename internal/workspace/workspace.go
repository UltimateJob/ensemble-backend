// Package workspace 实现本地工作区：路径守卫、文件工具、命令执行、快照。
//
// 设计约束（见 docs/agent-executor.md）：
//   - 工作区是一个本机绝对目录，所有文件操作都必须落在 root 内；
//   - 拒绝绝对路径、`..` 逃逸、symlink 逃逸；
//   - 这是「根目录约束」，不是 OS 沙箱：命令执行以当前用户权限运行，
//     只有文件工具受 root 限制。这个边界必须在 API 与 UI 上如实标注。
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 哨兵错误。上层据此映射 HTTP 状态码，不做字符串匹配。
var (
	ErrEscape      = errors.New("workspace: path escapes root")
	ErrAbsolute    = errors.New("workspace: absolute path not allowed")
	ErrNotFound    = errors.New("workspace: not found")
	ErrIsDir       = errors.New("workspace: is a directory")
	ErrNotDir      = errors.New("workspace: not a directory")
	ErrTooLarge    = errors.New("workspace: file too large")
	ErrEmptyRoot   = errors.New("workspace: empty root")
	ErrRootGone    = errors.New("workspace: root does not exist")
	ErrEditNoMatch = errors.New("workspace: edit anchor not found")
	ErrEditAmbig   = errors.New("workspace: edit anchor is not unique")
)

// 单次读取上限，超过按分页处理。
const MaxReadBytes = 1 << 20 // 1 MiB

// Workspace 一个受根目录约束的工作区。
//
// 零值不可用，必须经 Open/New 构造。
type Workspace struct {
	Root string // 已 EvalSymlinks 的绝对路径
}

// Open 校验并打开一个本机目录作为工作区。
//
// 目录必须存在且是目录；路径会被 EvalSymlinks 规范化，
// 这样后续包含性检查不受 symlink 影响（macOS 的 /var → /private/var 是常见坑）。
func Open(root string) (*Workspace, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrEmptyRoot
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrRootGone, root)
		}
		return nil, err
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotDir, root)
	}
	return &Workspace{Root: real}, nil
}

// Rel 把 root 内的绝对路径转成工作区相对路径（正斜杠）。
func (w *Workspace) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(w.Root, abs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return ".", nil
	}
	return filepath.ToSlash(rel), nil
}

// resolve 把调用方给的相对路径解析成 root 内的绝对路径。
//
// mustExist=false 时允许目标不存在（写新文件），但会规范化最近的已存在祖先，
// 避免通过 `link/new.txt` 这种路径绕过 symlink 检查。
func (w *Workspace) resolve(p string, mustExist bool) (string, error) {
	if w == nil || w.Root == "" {
		return "", ErrEmptyRoot
	}
	p = strings.TrimSpace(p)
	if p == "" {
		p = "."
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return "", ErrAbsolute
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrEscape
	}
	full := filepath.Join(w.Root, clean)
	if !w.contains(full) {
		return "", ErrEscape
	}
	if mustExist {
		real, err := filepath.EvalSymlinks(full)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("%w: %s", ErrNotFound, p)
			}
			return "", err
		}
		if !w.contains(real) {
			return "", ErrEscape
		}
		return real, nil
	}
	// 目标可以不存在：找到最深的已存在祖先做 symlink 解析，再拼回剩余部分。
	probe := full
	var tail []string
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			resolved := real
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			if !w.contains(resolved) {
				return "", ErrEscape
			}
			return resolved, nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("%w: %s", ErrEscape, p)
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
}

func (w *Workspace) contains(abs string) bool {
	if abs == w.Root {
		return true
	}
	return strings.HasPrefix(abs, w.Root+string(filepath.Separator))
}

// Stat 返回 root 内某个路径的信息。
func (w *Workspace) Stat(p string) (os.FileInfo, error) {
	full, err := w.resolve(p, true)
	if err != nil {
		return nil, err
	}
	return os.Stat(full)
}

// Exists 判断路径是否存在且不逃逸。
func (w *Workspace) Exists(p string) bool {
	_, err := w.resolve(p, true)
	return err == nil
}

// Abs 返回 root 内的绝对路径（只做守卫，不保证存在）。
func (w *Workspace) Abs(p string) (string, error) {
	return w.resolve(p, false)
}

// FileInfo 列表项。
type FileInfo struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // Unix 毫秒
}

// List 列出目录下的一层条目，按名字排序，目录在前。
func (w *Workspace) List(p string) ([]FileInfo, error) {
	full, err := w.resolve(p, true)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotDir, p)
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		// 跳过 .git 这类巨型目录的展开成本由调用方控制，这里仍如实返回。
		fi, err := e.Info()
		if err != nil {
			continue
		}
		rel, err := w.Rel(filepath.Join(full, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, FileInfo{
			Path:    rel,
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    fi.Size(),
			ModTime: fi.ModTime().UnixMilli(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// FileChunk 一段文件内容（按行分页）。
type FileChunk struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	StartLine  int    `json:"startLine"` // 1-based
	TotalLines int    `json:"totalLines"`
	Truncated  bool   `json:"truncated"`
	Size       int64  `json:"size"`
}

// Read 读取文件的一段。offset 为 0 表示从头；limit 为 0 表示读到末尾（受 MaxReadBytes 限制）。
func (w *Workspace) Read(p string, offset, limit int) (*FileChunk, error) {
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
	if info.Size() > MaxReadBytes && limit == 0 {
		limit = 2000 // 大文件默认只给前 2000 行，避免一次塞爆上下文
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lines, err := readLines(f, info.Size(), offset, limit)
	if err != nil {
		return nil, err
	}
	rel, _ := w.Rel(full)
	return &FileChunk{
		Path:       rel,
		Content:    strings.Join(lines.Body, "\n"),
		StartLine:  lines.Start,
		TotalLines: lines.Total,
		Truncated:  lines.Truncated,
		Size:       info.Size(),
	}, nil
}

type lineResult struct {
	Body      []string
	Start     int
	Total     int
	Truncated bool
}

func readLines(f *os.File, size int64, offset, limit int) (*lineResult, error) {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 32*1024)
	var read int64
	for read < size {
		n, err := f.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			read += int64(n)
			if len(buf) >= MaxReadBytes {
				break
			}
		}
		if err != nil {
			break
		}
	}
	all := strings.Split(string(buf), "\n")
	// 结尾换行会产生一个空元素，去掉但不影响行号语义。
	if n := len(all); n > 0 && all[n-1] == "" {
		all = all[:n-1]
	}
	total := len(all)
	start := offset
	if start > total {
		start = total
	}
	end := total
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return &lineResult{
		Body:      all[start:end],
		Start:     start + 1,
		Total:     total,
		Truncated: end < total,
	}, nil
}
