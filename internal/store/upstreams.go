package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// 上游渠道（provider）的持久化与凭据加密。
//
// 为什么要放进管理台：之前渠道只能写在 EXTRA_PROVIDERS 环境变量里，
// 换一个 Key 就要改 .env 并重启服务。真实运维里「中转挂了，填官方 Key
// 顶上」是分钟级操作，不该依赖登录机器改文件。
//
// 安全边界与连接器一致：Key 用 JWT_SECRET 派生的 AES-GCM 加密后落盘，
// 对外接口只返回「已配置」和末四位，绝不回传明文。

// UpstreamRecord 一个可路由的上游渠道。
type UpstreamRecord struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// Sealed 加密后的 API Key；空表示没配置。
	Sealed string `json:"sealed,omitempty"`
	// KeyHint 末四位，仅用于界面辨认，不能还原 Key。
	KeyHint string `json:"keyHint,omitempty"`
	Model   string `json:"model,omitempty"`
	Source  string `json:"source,omitempty"` // env | admin
	Enabled bool   `json:"enabled"`
	// Kind 渠道类型：chat（对话/补全）| image（生图）。
	// 空视为 chat——升级前写入的记录没有这个字段。
	//
	// 为什么不复用 DealJSON 那套判断：对话接口和生图接口是两种协议
	// （/chat/completions vs /images/generations），混在一个列表里
	// 会让「这个 Key 能不能生图」变得不可判断。
	Kind      string    `json:"kind,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Key 只在写入时使用，不落盘。
	Key string `json:"-"`
}

type upstreamFile struct {
	Items []UpstreamRecord `json:"items"`
}

// Upstreams 渠道存储。进程内单例，由 main 注入路径。
type Upstreams struct {
	mu   sync.Mutex
	path string
	rows map[string]UpstreamRecord
}

// NewUpstreams 加载 data/upstreams.json。文件不存在视为空表。
func NewUpstreams(path string) (*Upstreams, error) {
	u := &Upstreams{path: path, rows: make(map[string]UpstreamRecord)}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return u, nil
		}
		return nil, err
	}
	var f upstreamFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for _, r := range f.Items {
		u.rows[r.Name] = r
	}
	return u, nil
}

// List 返回全部渠道，按名称排序；Key 不回传。
func (u *Upstreams) List() []UpstreamRecord {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]UpstreamRecord, 0, len(u.rows))
	for _, r := range u.rows {
		// 密文也不外传：能解密的人拿到它就能还原 Key。
		r.Key = ""
		r.Sealed = ""
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Key 返回解密后的明文 Key（仅内部调用）。
func (u *Upstreams) Key(name string) (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok := u.rows[name]
	if !ok || r.Sealed == "" {
		return "", false
	}
	m, err := sealer().open(r.Sealed)
	if err != nil {
		return "", false
	}
	return m["apiKey"], true
}

// Upsert 新增或更新渠道。Key 为空表示沿用原 Key。
func (u *Upstreams) Upsert(in UpstreamRecord) (UpstreamRecord, error) {
	if in.Name == "" || in.BaseURL == "" {
		return UpstreamRecord{}, errors.New("name 与 baseUrl 必填")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	cur, exists := u.rows[in.Name]
	now := time.Now()
	if !exists {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	if in.Key != "" {
		sealed, err := sealer().seal(map[string]string{"apiKey": in.Key})
		if err != nil {
			return UpstreamRecord{}, err
		}
		in.Sealed = sealed
		in.KeyHint = hint(in.Key)
	} else {
		in.Sealed = cur.Sealed
		in.KeyHint = cur.KeyHint
	}
	in.Key = ""
	if in.Model == "" {
		in.Model = cur.Model
	}
	if in.Source == "" {
		in.Source = cur.Source
	}
	u.rows[in.Name] = in
	if err := u.persistLocked(); err != nil {
		return UpstreamRecord{}, err
	}
	return in, nil
}

// Delete 删除渠道。
func (u *Upstreams) Delete(name string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.rows[name]; !ok {
		return false
	}
	delete(u.rows, name)
	_ = u.persistLocked()
	return true
}

// SetEnabled 启停渠道。
func (u *Upstreams) SetEnabled(name string, on bool) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok := u.rows[name]
	if !ok {
		return false
	}
	r.Enabled = on
	r.UpdatedAt = time.Now()
	u.rows[name] = r
	_ = u.persistLocked()
	return true
}

func (u *Upstreams) persistLocked() error {
	f := upstreamFile{Items: make([]UpstreamRecord, 0, len(u.rows))}
	for _, r := range u.rows {
		f.Items = append(f.Items, r)
	}
	sort.Slice(f.Items, func(i, j int) bool { return f.Items[i].Name < f.Items[j].Name })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(u.path), 0o755); err != nil {
		return err
	}
	tmp := u.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, u.path)
}

// hint 返回 Key 的末四位，用于界面辨认。
func hint(k string) string {
	if len(k) <= 4 {
		return "****"
	}
	return "…" + k[len(k)-4:]
}
