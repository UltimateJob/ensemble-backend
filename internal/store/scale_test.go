package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 这个文件回答一个问题：JSON 分片到底能撑多少条消息？
//
// 换 SQLite 是大工程，且破坏零依赖约束。先量化，再决定要不要做。
// 跑法：go test ./internal/store/ -run TestScale -v -timeout 600s

func makeMsg(convID string, i int) *domain.Message {
	return &domain.Message{
		ConversationID: convID,
		Role:           domain.RoleAssistant,
		Agent:          fmt.Sprintf("角色%d", i%5),
		Content:        fmt.Sprintf("这是第 %d 条消息，用来测试存储规模。内容长度接近真实对话，大约五六十个字左右，包含一些标点和中文。", i),
	}
}

func TestScaleReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("规模测试较慢，-short 跳过")
	}

	sizes := []int{100, 1000, 10000, 50000}

	fmt.Printf("\n%-10s %-14s %-14s %-14s %-14s\n", "消息数", "追加 P95", "最近30条 P95", "全文扫描 P95", "文件大小")
	fmt.Println("--------------------------------------------------------------------------------")

	for _, n := range sizes {
		dir := t.TempDir()
		fs, err := NewFileStore(dir)
		if err != nil {
			t.Fatalf("建存储失败: %v", err)
		}
		conv := &domain.Conversation{ID: "c_scale", UserID: "u1", Title: "规模测试"}
		_ = fs.CreateConv(conv)

		// 灌数据
		seedStart := time.Now()
		for i := 0; i < n; i++ {
			_ = fs.AppendMessage(makeMsg(conv.ID, i))
		}
		_ = fs.Flush()
		seedDur := time.Since(seedStart)

		// 追加延迟（再追加 50 条，取 P95）
		var appends []time.Duration
		for i := 0; i < 50; i++ {
			s := time.Now()
			_ = fs.AppendMessage(makeMsg(conv.ID, n+i))
			appends = append(appends, time.Since(s))
		}

		// 「最近 30 条」延迟
		var recent []time.Duration
		for i := 0; i < 20; i++ {
			s := time.Now()
			_ = fs.ListMessages(conv.ID, 30)
			recent = append(recent, time.Since(s))
		}

		// 全文扫描（搜索用）
		var scan []time.Duration
		for i := 0; i < 5; i++ {
			s := time.Now()
			_ = fs.ListMessages(conv.ID, 0)
			scan = append(scan, time.Since(s))
		}

		size := dirSizeOf(dir)
		fmt.Printf("%-10d %-14s %-14s %-14s %-14s\n",
			n,
			p95(appends).Round(time.Microsecond),
			p95(recent).Round(time.Microsecond),
			p95(scan).Round(time.Microsecond),
			fmt.Sprintf("%.1f MB", float64(size)/1024/1024),
		)
		_ = seedDur

		// 启动加载时间（重新打开）
		s2 := time.Now()
		fs2, err := NewFileStore(dir)
		if err == nil {
			_ = fs2
			loadDur := time.Since(s2)
			fmt.Printf("           启动加载 %.0fms\n", float64(loadDur.Microseconds())/1000)
		}
		_ = fs.Close()
	}
	fmt.Println()
}

func p95(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	cp := make([]time.Duration, len(ds))
	copy(cp, ds)
	for i := 0; i < len(cp); i++ {
		for j := i + 1; j < len(cp); j++ {
			if cp[j] < cp[i] {
				cp[i], cp[j] = cp[j], cp[i]
			}
		}
	}
	idx := int(float64(len(cp)) * 0.95)
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func dirSizeOf(dir string) int64 {
	var total int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
