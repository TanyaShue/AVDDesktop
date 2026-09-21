// Package config 管理用户设置（settings.json）的读写。
package config

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// Version 是设置的 schema 版本。
const Version = 1

// Defaults 返回默认设置。
func Defaults() domain.AppSettings {
	return domain.AppSettings{
		Version:             Version,
		Theme:               "light",
		ConfirmBeforeDelete: true,
		ShowTaskDrawer:      true,
		LogLevel:            "info",
		KeepLogDays:         7,
	}
}

// Manager 是设置的线程安全读写器。
type Manager struct {
	mu       sync.RWMutex
	path     string
	settings domain.AppSettings
}

// NewManager 创建设置管理器（由 Load 完成加载）。
func NewManager(path string) *Manager {
	return &Manager{path: path, settings: Defaults()}
}

// Path 返回配置文件路径。
func (m *Manager) Path() string { return m.path }

// Load 读取设置；文件不存在时写入默认值，内容损坏时备份后重建。
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	raw, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return m.saveLocked()
		}
		return domain.Wrap(domain.CodePermissionDenied, "无法读取设置文件", err)
	}
	settings := Defaults()
	if err := json.Unmarshal(raw, &settings); err != nil {
		_ = os.Rename(m.path, m.path+".corrupt")
		m.settings = Defaults()
		return m.saveLocked()
	}
	m.settings = normalize(settings)
	return nil
}

// Get 返回设置副本。
func (m *Manager) Get() domain.AppSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

// Update 以补丁方式更新设置（key 为 JSON 字段名，未知字段忽略）。
func (m *Manager) Update(patch map[string]any) (domain.AppSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	base, err := json.Marshal(m.settings)
	if err != nil {
		return m.settings, domain.Wrap(domain.CodeUnknown, "无法序列化设置", err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(base, &asMap); err != nil {
		return m.settings, domain.Wrap(domain.CodeUnknown, "无法反序列化设置", err)
	}
	for k, v := range patch {
		if _, ok := asMap[k]; !ok {
			continue
		}
		asMap[k] = v
	}
	merged, err := json.Marshal(asMap)
	if err != nil {
		return m.settings, domain.Wrap(domain.CodeUnknown, "无法合并设置", err)
	}
	next := Defaults()
	if err := json.Unmarshal(merged, &next); err != nil {
		return m.settings, domain.Wrap(domain.CodeInvalidArgument, "设置值类型不匹配", err)
	}
	next = normalize(next)
	if err := validate(next); err != nil {
		return m.settings, err
	}
	m.settings = next
	if err := m.saveLocked(); err != nil {
		return m.settings, err
	}
	return m.settings, nil
}

// Reset 恢复默认设置。
func (m *Manager) Reset() (domain.AppSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = Defaults()
	if err := m.saveLocked(); err != nil {
		return m.settings, err
	}
	return m.settings, nil
}

func (m *Manager) saveLocked() error {
	return platform.WriteJSON(m.path, m.settings)
}

// normalize 补齐缺失或非法的字段值。
func normalize(s domain.AppSettings) domain.AppSettings {
	def := Defaults()
	if s.Version == 0 {
		s.Version = def.Version
	}
	if s.Theme == "" {
		s.Theme = def.Theme
	}
	if strings.TrimSpace(s.LogLevel) == "" {
		s.LogLevel = def.LogLevel
	}
	if s.KeepLogDays <= 0 {
		s.KeepLogDays = def.KeepLogDays
	}
	return s
}

func validate(s domain.AppSettings) error {
	switch s.Theme {
	case "light", "dark", "system":
	default:
		return domain.Err(domain.CodeInvalidArgument, "主题必须是 light/dark/system")
	}
	switch strings.ToLower(s.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return domain.Err(domain.CodeInvalidArgument, "日志级别必须是 debug/info/warn/error")
	}
	if s.KeepLogDays < 1 || s.KeepLogDays > 90 {
		return domain.Err(domain.CodeInvalidArgument, "日志保留天数必须在 1-90 之间")
	}
	return nil
}
