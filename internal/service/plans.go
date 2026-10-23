package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// planIDPattern 与上游名称保持同一约束：小写、数字、下划线、短横线。
var planIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// PlanStore 套餐配置存储。和上游配置一样落一个独立 JSON 文件，
// 网页里改完即时生效，重启后恢复。
type PlanStore struct {
	mu   sync.RWMutex
	path string
	rows map[string]domain.PlanConfig
}

// NewPlanStore 打开套餐配置。path 为空时使用内存模式（测试/单测）。
// 文件损坏时返回错误，拒绝用空配置覆盖原文件。
func NewPlanStore(path string) (*PlanStore, error) {
	p := &PlanStore{path: path, rows: make(map[string]domain.PlanConfig)}
	if path == "" {
		for _, v := range defaultPlans() {
			p.rows[v.ID] = v
		}
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(b))) > 0 {
		var list []domain.PlanConfig
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("套餐配置解析失败（%s）：%w", path, err)
		}
		for _, v := range list {
			if v.ID == "" {
				continue
			}
			p.rows[v.ID] = v
		}
	}
	if len(p.rows) == 0 {
		for _, v := range defaultPlans() {
			p.rows[v.ID] = v
		}
		if err := p.saveLocked(); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// defaultPlans 给出可直接编辑的起步套餐。免费额度对齐历史默认值 800，
// 避免升级后把存量免费用户误降到更小的额度。
func defaultPlans() []domain.PlanConfig {
	now := time.Now()
	return []domain.PlanConfig{
		{
			ID: "free", Name: "免费版", PriceMonthly: 0, MonthlyQuota: 800,
			Description: "体验核心能力", Features: []string{"每月 800 次对话", "基础模型", "社区支持"},
			Color: "#667085", Enabled: true, Sort: 10, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "pro", Name: "专业版", PriceMonthly: 39, MonthlyQuota: 6000,
			Description: "适合个人高频使用", Features: []string{"每月 6,000 次对话", "全部模型", "优先生效"},
			Color: "#4F46E5", Enabled: true, Sort: 20, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "team", Name: "团队版", PriceMonthly: 199, MonthlyQuota: 30000,
			Description: "适合小团队协作", Features: []string{"每月 30,000 次对话", "团队协作", "优先支持"},
			Color: "#0E9384", Enabled: true, Sort: 30, CreatedAt: now, UpdatedAt: now,
		},
	}
}

// List 按排序值、价格、ID 返回全部套餐。
func (p *PlanStore) List() []domain.PlanConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]domain.PlanConfig, 0, len(p.rows))
	for _, v := range p.rows {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		if out[i].PriceMonthly != out[j].PriceMonthly {
			return out[i].PriceMonthly < out[j].PriceMonthly
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Get 取一个套餐。
func (p *PlanStore) Get(id string) (domain.PlanConfig, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.rows[id]
	return v, ok
}

// Len 套餐数量。
func (p *PlanStore) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.rows)
}

// Upsert 新增或更新套餐。ID 不可修改。
func (p *PlanStore) Upsert(v domain.PlanConfig) error {
	v.ID = strings.TrimSpace(v.ID)
	v.Name = strings.TrimSpace(v.Name)
	v.Description = strings.TrimSpace(v.Description)
	v.Color = strings.TrimSpace(v.Color)
	v.Features = normalizeFeatures(v.Features)
	v.AllowedModels = normalizeModels(v.AllowedModels)
	if !planIDPattern.MatchString(v.ID) {
		return errors.New("套餐 ID 只能用小写字母、数字、- 或 _，且以字母或数字开头")
	}
	if v.Name == "" {
		return errors.New("套餐名称不能为空")
	}
	if len([]rune(v.Name)) > 40 {
		return errors.New("套餐名称最多 40 个字")
	}
	if v.PriceMonthly < 0 {
		return errors.New("月费不能为负数")
	}
	if v.MonthlyQuota < 0 {
		return errors.New("月额度不能为负数")
	}
	if v.RatePerMinute < 0 || v.RatePerMinute > 100000 {
		return errors.New("每分钟请求数需在 0–100000 之间（0 表示使用服务默认值）")
	}
	if v.MaxConcurrency < 0 || v.MaxConcurrency > 10000 {
		return errors.New("并发上限需在 0–10000 之间（0 表示不限）")
	}
	if v.Color == "" {
		v.Color = "#4F46E5"
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	old, exists := p.rows[v.ID]
	if exists {
		v.CreatedAt = old.CreatedAt
	} else {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	p.rows[v.ID] = v
	return p.saveLocked()
}

// Delete 删除套餐。调用方负责检查是否仍有用户在使用。
func (p *PlanStore) Delete(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.rows[id]; !ok {
		return errors.New("套餐不存在")
	}
	if len(p.rows) <= 1 {
		return errors.New("至少保留一个套餐")
	}
	delete(p.rows, id)
	return p.saveLocked()
}

func normalizeFeatures(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		if len([]rune(v)) > 80 {
			v = string([]rune(v)[:80])
		}
		seen[v] = true
		out = append(out, v)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

// normalizeModels 清洗套餐可用模型白名单：去空白、去重、限长。
func normalizeModels(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		if len([]rune(v)) > 64 {
			v = string([]rune(v)[:64])
		}
		seen[v] = true
		out = append(out, v)
		if len(out) >= 50 {
			break
		}
	}
	return out
}

func (p *PlanStore) saveLocked() error {
	if p.path == "" {
		return nil
	}
	list := make([]domain.PlanConfig, 0, len(p.rows))
	for _, v := range p.rows {
		list = append(list, v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Sort != list[j].Sort {
			return list[i].Sort < list[j].Sort
		}
		return list[i].ID < list[j].ID
	})
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}
