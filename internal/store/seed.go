package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// seedMarker 记录「演示数据已装过」。
//
// 用独立的标记文件而不是「数据目录非空」来判断：
// 用户可能自己删光了所有群，这时不该再给他塞一遍演示数据。
const seedMarker = ".seeded"

// SeedDemo 在空库上装一份演示内容。
//
// 为什么要在后端做：产品第一次打开应该是有内容的，而不是一片空大厅。
// 之前要用户手动跑 `make demo`，那等于把「好不好看」这件事藏在了
// 一个没人会读的 README 段落后面。
//
// 只在两个条件同时成立时装：
//   - 数据目录里没有 .seeded 标记（从没装过）
//   - 当前没有任何会话（真·空库）
//
// 用户自己建的群不会触发，删库重来也会因为标记存在而保持干净。
func (fs *FileStore) SeedDemo(dir string) error {
	marker := filepath.Join(fs.dir, seedMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil // 装过了
	}

	fs.mu.Lock()
	hasConv := len(fs.Memory.convs) > 0
	fs.mu.Unlock()
	if hasConv {
		// 有数据但不是我们装的（老用户升级上来）：补一个标记，别打扰他
		_ = os.WriteFile(marker, []byte("existing\n"), 0o644)
		return nil
	}

	// 演示数据缺失时静默跳过：裁剪过的部署没有 seed/demo 是正常的
	if _, err := os.Stat(filepath.Join(dir, "conversations.json")); err != nil {
		return nil
	}

	var (
		users  []*domain.User
		convs  []*domain.Conversation
		audits []*domain.AuditLog
		events []*domain.Event
	)
	read := func(name string, dst any) error {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if len(b) == 0 {
			return nil
		}
		return json.Unmarshal(b, dst)
	}
	if err := read("users.json", &users); err != nil {
		return err
	}
	if err := read("conversations.json", &convs); err != nil {
		return err
	}
	_ = read("audits.json", &audits)
	_ = read("events.json", &events)

	// 消息按会话分文件
	msgDir := filepath.Join(dir, dirMsgs)
	entries, _ := os.ReadDir(msgDir)
	msgsByConv := map[string][]*domain.Message{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(msgDir, e.Name()))
		if err != nil {
			continue
		}
		var msgs []*domain.Message
		if json.Unmarshal(b, &msgs) != nil {
			continue
		}
		convID := e.Name()[:len(e.Name())-len(".json")]
		msgsByConv[convID] = msgs
	}

	fs.Memory.mu.Lock()
	for _, u := range users {
		fs.Memory.users[u.ID] = u
	}
	for _, c := range convs {
		// 给演示群加「示例」前缀。
		// 「华东医院数据中心投标」这种名字看起来像用户自己的真实数据，
		// 不标清楚会让人以为账号里凭空多出了别人的东西。
		if !strings.HasPrefix(c.Title, "示例 · ") {
			c.Title = "示例 · " + c.Title
		}
		fs.Memory.convs[c.ID] = c
	}
	for _, a := range audits {
		fs.Memory.audits = append(fs.Memory.audits, a)
	}
	for _, ev := range events {
		fs.Memory.events = append(fs.Memory.events, ev)
	}
	total := 0
	for convID, msgs := range msgsByConv {
		// Memory.msgs 是一条扁平切片（全局按时间排），不是 map
		fs.Memory.msgs = append(fs.Memory.msgs, msgs...)
		fs.Memory.msgCount[convID] = len(msgs)
		total += len(msgs)
	}
	// 扁平切片必须按时间排序，分页和「最近 N 条」都依赖这个顺序
	sort.SliceStable(fs.Memory.msgs, func(i, j int) bool {
		return fs.Memory.msgs[i].CreatedAt.Before(fs.Memory.msgs[j].CreatedAt)
	})
	// 全部标脏，落盘时写成正式的 JSON 分片
	fs.dirtyUsers, fs.dirtyConvs = true, true
	fs.dirtyAudits, fs.dirtyEvents = true, true
	for convID := range msgsByConv {
		fs.dirtyMsgs[convID] = true
	}
	fs.Memory.mu.Unlock()

	if err := os.WriteFile(marker, []byte("demo\n"), 0o644); err != nil {
		return err
	}
	logx.L().Info("首次启动，已装载演示内容",
		"群", len(convs), "消息", total,
		"提示", "用演示账号 13800000000 登录即可看到")
	return nil
}
