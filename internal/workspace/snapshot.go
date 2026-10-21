package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 快照预算：超过就拒绝，而不是悄悄做一半。
const (
	MaxSnapshotFiles = 5000
	MaxSnapshotBytes = 200 << 20 // 200 MiB
	MaxSnapshotFile  = 5 << 20   // 单文件超过 5 MiB 跳过
)

// Snapshot 一次 Run 前的工作区副本。
//
// 用「整目录复制 + 文件清单」而不是 git stash：
// 工作区可能根本不是 git 仓库，也可能有未跟踪文件被覆盖。
type Snapshot struct {
	ID        string    `json:"id"`
	Dir       string    `json:"-"` // 不对外暴露临时目录
	CreatedAt time.Time `json:"createdAt"`
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
	Skipped   int       `json:"skipped"` // 超限被跳过的文件数
}

// Snapshot 创建当前工作区的快照。
func (w *Workspace) Snapshot(id string) (*Snapshot, error) {
	if id == "" {
		id = fmt.Sprintf("snap_%d", time.Now().UnixNano())
	}
	dir := filepath.Join(os.TempDir(), "dsh-snap-"+sanitizeID(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Snapshot{ID: id, Dir: dir, CreatedAt: time.Now()}

	files, err := w.walkFiles()
	if err != nil {
		return nil, err
	}
	if len(files) > MaxSnapshotFiles {
		return nil, fmt.Errorf("workspace: snapshot too large: %d files > %d", len(files), MaxSnapshotFiles)
	}
	for _, rel := range files {
		src := filepath.Join(w.Root, filepath.FromSlash(rel))
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > MaxSnapshotFile {
			s.Skipped++
			continue
		}
		if s.Bytes+info.Size() > MaxSnapshotBytes {
			return nil, fmt.Errorf("workspace: snapshot exceeds %d bytes", int64(MaxSnapshotBytes))
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return nil, err
		}
		if err := copyFile(src, dst); err != nil {
			return nil, err
		}
		s.Files++
		s.Bytes += info.Size()
	}
	return s, nil
}

// Cleanup 删除快照副本。
func (s *Snapshot) Cleanup() error {
	if s == nil || s.Dir == "" {
		return nil
	}
	return os.RemoveAll(s.Dir)
}

// walkFiles 按预算返回工作区内的常规文件相对路径（与 Glob 一致的跳过规则）。
func (w *Workspace) walkFiles() ([]string, error) {
	var out []string
	err := filepath.WalkDir(w.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != w.Root {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := w.Rel(path)
		if relErr != nil {
			return nil
		}
		out = append(out, rel)
		if len(out) > MaxSnapshotFiles {
			return fs.SkipAll
		}
		return nil
	})
	return out, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// FileDiff 一个文件的变更。
type FileDiff struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // added | modified | deleted
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Patch   string `json:"patch,omitempty"`
}

// Diff 对比快照与当前工作区。limit 限制返回文件数（默认 200）。
func (w *Workspace) Diff(s *Snapshot, limit int) ([]FileDiff, int, error) {
	if s == nil {
		return nil, 0, fmt.Errorf("workspace: nil snapshot")
	}
	if limit <= 0 {
		limit = 200
	}
	snapFiles := map[string]bool{}
	var out []FileDiff
	total := 0

	// 快照里有：对比现状 → modified / deleted
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, 0, err
	}
	_ = entries
	err = filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(s.Dir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		snapFiles[rel] = true
		before, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		curPath := filepath.Join(w.Root, filepath.FromSlash(rel))
		after, curErr := os.ReadFile(curPath)
		if curErr != nil {
			total++
			if len(out) < limit {
				out = append(out, FileDiff{Path: rel, Status: "deleted", Removed: countLines(before)})
			}
			return nil
		}
		if strings.HasPrefix(rel, ".dsh-") {
			return nil
		}
		if bytesEqual(before, after) {
			return nil
		}
		total++
		if len(out) < limit {
			added, removed, patch := diffLines(string(before), string(after))
			out = append(out, FileDiff{Path: rel, Status: "modified", Added: added, Removed: removed, Patch: patch})
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	// 当前有、快照没有：added
	files, err := w.walkFiles()
	if err != nil {
		return nil, 0, err
	}
	for _, rel := range files {
		if snapFiles[rel] {
			continue
		}
		total++
		if len(out) >= limit {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(w.Root, filepath.FromSlash(rel)))
		if readErr != nil || isBinary(content) {
			out = append(out, FileDiff{Path: rel, Status: "added"})
			continue
		}
		_, _, patch := diffLines("", string(content))
		out = append(out, FileDiff{Path: rel, Status: "added", Added: countLines(content), Patch: patch})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, total, nil
}

// Revert 把工作区恢复成快照状态：覆盖修改、恢复删除、删掉新增。
func (w *Workspace) Revert(s *Snapshot) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("workspace: nil snapshot")
	}
	changed := 0
	snapFiles := map[string]bool{}
	err := filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(s.Dir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		snapFiles[rel] = true
		dst := filepath.Join(w.Root, filepath.FromSlash(rel))
		before, readErr := os.ReadFile(dst)
		if readErr != nil {
			if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
				return mkErr
			}
			if cpErr := copyFile(path, dst); cpErr != nil {
				return cpErr
			}
			changed++
			return nil
		}
		snapContent, _ := os.ReadFile(path)
		if !bytesEqual(before, snapContent) {
			if cpErr := copyFile(path, dst); cpErr != nil {
				return cpErr
			}
			changed++
		}
		return nil
	})
	if err != nil {
		return changed, err
	}
	// 删除快照里没有、当前存在的新增文件
	files, err := w.walkFiles()
	if err != nil {
		return changed, err
	}
	for _, rel := range files {
		if snapFiles[rel] || strings.HasPrefix(rel, ".dsh-") {
			continue
		}
		if rmErr := os.Remove(filepath.Join(w.Root, filepath.FromSlash(rel))); rmErr == nil {
			changed++
		}
	}
	return changed, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	ha := sha256.Sum256(a)
	hb := sha256.Sum256(b)
	return ha == hb
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return strings.Count(string(b), "\n") + 1
}

// diffLines 生成简化的 unified diff。
//
// 超过 1200 行的文件只报统计，不生成 patch——避免一次 diff 塞爆上下文。
func diffLines(before, after string) (added, removed int, patch string) {
	bl := splitLines(before)
	al := splitLines(after)
	a, r, ok := lcsCounts(bl, al)
	if !ok {
		// 规模过大：退化为「整体替换」统计
		return len(al), len(bl), fmt.Sprintf("--- large diff (%d → %d lines)", len(bl), len(al))
	}
	const maxPatchLines = 400
	var b strings.Builder
	b.WriteString(fmt.Sprintf("@@ -%d +%d @@\n", len(bl), len(al)))
	// 简化输出：先给删除块，再给新增块（不做行级交错，够读）
	removedSet := diffSet(bl, al)
	addedSet := diffSet(al, bl)
	n := 0
	for _, l := range removedSet {
		if n >= maxPatchLines {
			b.WriteString("… (truncated)\n")
			break
		}
		b.WriteString("-" + l + "\n")
		n++
	}
	for _, l := range addedSet {
		if n >= maxPatchLines {
			b.WriteString("… (truncated)\n")
			break
		}
		b.WriteString("+" + l + "\n")
		n++
	}
	return a, r, b.String()
}

// diffSet 返回「在 a 中但不在 b 中」的行（按出现次数配对，避免重复行被全删）。
func diffSet(a, b []string) []string {
	count := map[string]int{}
	for _, l := range b {
		count[l]++
	}
	var out []string
	seen := map[string]int{}
	for _, l := range a {
		if seen[l] < count[l] {
			seen[l]++
			continue
		}
		out = append(out, l)
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// lcsCounts 计算最长公共子序列长度，返回 (相同行数, 差异行数)。
// 规模超过预算时返回 ok=false，由调用方退化处理。
func lcsCounts(a, b []string) (int, int, bool) {
	if len(a) > 1200 || len(b) > 1200 {
		return 0, 0, false
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] > cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
		for k := range cur {
			cur[k] = 0
		}
	}
	common := prev[len(b)]
	diff := (len(a) - common) + (len(b) - common)
	return common, diff, true
}

func sanitizeID(id string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_", " ", "_")
	return repl.Replace(id)
}
