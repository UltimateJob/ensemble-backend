package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/workspace"
)

// 持久化格式。刻意只存「能重建出可用状态」的最小集合：
//   - 工作区：路径 + 归属，重启后重新 Open 一次
//   - Run：按时间倒序保留最近 N 个，跑在途的标记为 aborted
//
// 不存的事件流、审批 channel、cancel 函数——那些是运行时对象，
// 反序列化出来也没法用，不如明确当作历史记录。
const (
	maxPersistedRuns = 100
	stateVersion     = 1
)

type persistedState struct {
	Version    int               `json:"version"`
	SavedAt    time.Time         `json:"savedAt"`
	Workspaces []persistedSpace  `json:"workspaces"`
	Runs       []json.RawMessage `json:"runs"`
}

type persistedSpace struct {
	ID             string    `json:"id"`
	UserID         string    `json:"userId"`
	ConversationID string    `json:"conversationId,omitempty"`
	Root           string    `json:"root"`
	Label          string    `json:"label,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// SetPersistence 指定状态文件路径；空字符串表示纯内存。
func (m *Manager) SetPersistence(path string) {
	m.mu.Lock()
	m.statePath = path
	m.mu.Unlock()
}

// Save 把工作区与最近的 Run 写到磁盘。
//
// 调用方是低频变更点（绑工作区、Run 结束），不做 debounce——
// 这些操作本来就不频繁，多一次写换「重启不丢」值得。
func (m *Manager) Save() error {
	m.mu.Lock()
	path := m.statePath
	if path == "" {
		m.mu.Unlock()
		return nil
	}
	st := persistedState{Version: stateVersion, SavedAt: time.Now()}
	for _, w := range m.workspaces {
		st.Workspaces = append(st.Workspaces, persistedSpace{
			ID: w.ID, UserID: w.UserID, ConversationID: w.ConversationID,
			Root: w.Root, Label: w.Label, CreatedAt: w.CreatedAt,
		})
	}
	// 按开始时间倒序，只留最近 N 个
	runs := make([]*Run, 0, len(m.runs))
	for _, r := range m.runs {
		runs = append(runs, r)
	}
	m.mu.Unlock()

	sortRunsDesc(runs)
	if len(runs) > maxPersistedRuns {
		runs = runs[:maxPersistedRuns]
	}
	for _, r := range runs {
		b, err := json.Marshal(r)
		if err != nil {
			continue
		}
		st.Runs = append(st.Runs, b)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	b, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load 读回工作区与 Run。跑在途的 Run 会被标记为 aborted——
// 进程都重启了，它不可能还在跑。
func (m *Manager) Load(path string) error {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var st persistedState
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("agent: 状态文件损坏: %w", err)
	}
	if st.Version != stateVersion {
		return fmt.Errorf("agent: 状态版本不匹配（文件 %d，期望 %d）", st.Version, stateVersion)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pw := range st.Workspaces {
		ws, err := workspace.Open(pw.Root)
		if err != nil {
			continue // 目录没了就跳过，不让整个状态加载失败
		}
		m.workspaces[pw.ID] = &Workspace{
			ID: pw.ID, UserID: pw.UserID, ConversationID: pw.ConversationID,
			Root: ws.Root, Label: pw.Label, CreatedAt: pw.CreatedAt, ws: ws,
		}
	}
	for _, raw := range st.Runs {
		var r Run
		if err := json.Unmarshal(raw, &r); err != nil {
			continue
		}
		if r.Status == StatusRunning || r.Status == StatusWaiting {
			r.Status = StatusAborted
			r.Error = "服务重启，任务中断"
			if r.EndedAt.IsZero() {
				r.EndedAt = time.Now()
			}
		}
		m.runs[r.ID] = &r
	}
	return nil
}

func sortRunsDesc(runs []*Run) {
	for i := 1; i < len(runs); i++ {
		for j := i; j > 0 && runs[j].StartedAt.After(runs[j-1].StartedAt); j-- {
			runs[j], runs[j-1] = runs[j-1], runs[j]
		}
	}
}
