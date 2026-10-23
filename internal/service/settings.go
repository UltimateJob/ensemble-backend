package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/UltimateJob/ensemble-backend/internal/pkg/logx"
)

// Settings 运营开关。与套餐、上游一样单独落一个 JSON 文件，
// 管理台改完即时生效，重启后恢复。
//
// 这里只有「怎么运营」的参数，不放任何密钥；默认值是保守的：
// 注册开放、维护关闭、不自动扣余额、风控阈值只告警不拦请求。
type Settings struct {
	// 注册与新手
	RegistrationOpen bool   `json:"registrationOpen"`
	DefaultPlan      string `json:"defaultPlan"`
	TrialQuota       int    `json:"trialQuota"`

	// 公告与维护
	Announcement       string    `json:"announcement,omitempty"`
	AnnouncementLevel  string    `json:"announcementLevel,omitempty"` // info | warning | critical
	AnnouncementAt     time.Time `json:"announcementAt,omitempty"`
	MaintenanceMode    bool      `json:"maintenanceMode"`
	MaintenanceMessage string    `json:"maintenanceMessage,omitempty"`

	// 计费
	// AutoDeduct 为真时，每次上游调用按实际成本从用户钱包扣款。
	AutoDeduct bool `json:"autoDeduct"`
	// EnforceBalance 为真时，余额不足的用户不能再发起消耗额度的请求。
	// 默认关闭：先看清账，再决定要不要拦。
	EnforceBalance bool `json:"enforceBalance"`

	// 风控阈值（只告警，不改变业务行为）
	RiskUserCostCNY   float64 `json:"riskUserCostCny"`
	RiskMarginPct     float64 `json:"riskMarginPct"`
	RiskErrorRatePct  float64 `json:"riskErrorRatePct"`
	RiskDailySpendCNY float64 `json:"riskDailySpendCny"`

	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// DefaultSettings 新装与坏文件回退时使用。
func DefaultSettings() Settings {
	return Settings{
		RegistrationOpen:   true,
		DefaultPlan:        "free",
		TrialQuota:         800,
		AnnouncementLevel:  "info",
		MaintenanceMessage: "系统维护中，请稍后再试。",
		RiskUserCostCNY:    50,
		RiskMarginPct:      0,
		RiskErrorRatePct:   5,
		RiskDailySpendCNY:  200,
	}
}

// SettingsStore 运营设置的读写。
type SettingsStore struct {
	mu   sync.RWMutex
	path string
	val  Settings
}

// OpenSettings 打开设置文件。path 为空时使用内存默认值。
// 文件损坏时隔离坏文件并回退默认值：运营开关坏了不该让服务起不来。
func OpenSettings(path string) (*SettingsStore, error) {
	s := &SettingsStore{path: path, val: DefaultSettings()}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := s.save(); err != nil {
				return nil, err
			}
			return s, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		var loaded Settings
		if err := json.Unmarshal(data, &loaded); err != nil {
			quarantine := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
			_ = os.Rename(path, quarantine)
			logx.L().Warn("运营设置损坏，已隔离并使用默认值",
				"file", path, "隔离为", quarantine, "err", err.Error())
		} else {
			s.val = normalizeSettings(loaded)
		}
	}
	return s, nil
}

// Get 返回当前设置的副本。
func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.val
}

// Update 校验并保存设置。
func (s *SettingsStore) Update(in Settings) (Settings, error) {
	in = normalizeSettings(in)
	if err := validateSettings(in); err != nil {
		return s.Get(), err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Announcement != s.val.Announcement {
		if in.Announcement == "" {
			in.AnnouncementAt = time.Time{}
		} else {
			in.AnnouncementAt = time.Now()
		}
	} else {
		in.AnnouncementAt = s.val.AnnouncementAt
	}
	in.UpdatedAt = time.Now()
	s.val = in
	if err := s.saveLocked(); err != nil {
		return s.val, err
	}
	return s.val, nil
}

func normalizeSettings(v Settings) Settings {
	v.DefaultPlan = strings.TrimSpace(v.DefaultPlan)
	v.Announcement = strings.TrimSpace(v.Announcement)
	v.AnnouncementLevel = strings.ToLower(strings.TrimSpace(v.AnnouncementLevel))
	v.MaintenanceMessage = strings.TrimSpace(v.MaintenanceMessage)
	if v.DefaultPlan == "" {
		v.DefaultPlan = "free"
	}
	switch v.AnnouncementLevel {
	case "info", "warning", "critical":
	default:
		v.AnnouncementLevel = "info"
	}
	if v.MaintenanceMessage == "" {
		v.MaintenanceMessage = DefaultSettings().MaintenanceMessage
	}
	if v.TrialQuota < 0 {
		v.TrialQuota = 0
	}
	if v.RiskUserCostCNY < 0 {
		v.RiskUserCostCNY = 0
	}
	if v.RiskDailySpendCNY < 0 {
		v.RiskDailySpendCNY = 0
	}
	return v
}

func validateSettings(v Settings) error {
	if utf8.RuneCountInString(v.Announcement) > 500 {
		return errors.New("公告最多 500 个字")
	}
	if utf8.RuneCountInString(v.MaintenanceMessage) > 200 {
		return errors.New("维护提示最多 200 个字")
	}
	if v.TrialQuota > 10000000 {
		return errors.New("试用额度不能超过 10000000 次")
	}
	if v.RiskMarginPct < -1000 || v.RiskMarginPct > 1000 {
		return errors.New("毛利率告警阈值需在 -1000%–1000% 之间")
	}
	if v.RiskErrorRatePct < 0 || v.RiskErrorRatePct > 100 {
		return errors.New("错误率告警阈值需在 0–100% 之间")
	}
	if v.RiskUserCostCNY > 1e9 || v.RiskDailySpendCNY > 1e9 {
		return errors.New("风控金额阈值过大")
	}
	return nil
}

func (s *SettingsStore) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *SettingsStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.val, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
