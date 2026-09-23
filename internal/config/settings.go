// Package config 管理用户设置（settings.json）的读写。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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
		MirrorSourceID:      "google-cn",
		JDKMirrorSourceID:   "nju",
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
		backup := m.path + ".corrupt"
		if renameErr := os.Rename(m.path, backup); renameErr != nil {
			// 备份失败（Windows 上目标已存在、文件被占用等）时绝不能直接覆盖原文件：
			// 换一个不冲突的名字重试，仍失败就报错，把损坏现场留给用户排查。
			backup = fmt.Sprintf("%s.corrupt-%d", m.path, time.Now().Unix())
			if retryErr := os.Rename(m.path, backup); retryErr != nil {
				return domain.Wrap(domain.CodePermissionDenied,
					"设置文件无法解析且无法备份（未覆盖原文件）", renameErr)
			}
		}
		m.settings = Defaults()
		return m.saveLocked()
	}
	// normalize 会把非法的取值（例如手工改出来的 theme: "blue"）回退为默认值。
	// 必须在这里就修正：否则非法值会一直留在内存里，之后每次 Update 都在 validate
	// 处失败，设置页任何字段都保存不了。
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
	next = fillDefaults(next)
	if err := validate(next); err != nil {
		return m.settings, err
	}
	// 先写盘再提交内存：写盘失败时内存与磁盘必须保持一致，
	// 否则界面按返回值显示"已更新"，重启后却静默回滚。
	saved := m.settings
	m.settings = next
	if err := m.saveLocked(); err != nil {
		m.settings = saved
		return saved, err
	}
	return m.settings, nil
}

// Reset 恢复默认设置。
func (m *Manager) Reset() (domain.AppSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	saved := m.settings
	m.settings = Defaults()
	if err := m.saveLocked(); err != nil {
		m.settings = saved
		return saved, err
	}
	return m.settings, nil
}

func (m *Manager) saveLocked() error {
	return platform.WriteJSON(m.path, m.settings)
}

// fillDefaults 补齐缺失或空字段（不做取值合法性判断：Update 需要在补齐后
// 用 validate 拒绝非法补丁，而不是静默修正）。
func fillDefaults(s domain.AppSettings) domain.AppSettings {
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
	if strings.TrimSpace(s.JDKMirrorSourceID) == "" {
		s.JDKMirrorSourceID = def.JDKMirrorSourceID
	}
	return s
}

// normalize 在 fillDefaults 之上把非法取值回退为默认值。
//
// 不变式：normalize 的返回值一定通过 validate。Load 依赖这一点修复手工编辑
// 出来的非法值（例如 theme: "blue"），否则非法值会一直留在内存里，之后每次
// Update 都在 validate 处失败，设置页任何字段都保存不了。
func normalize(s domain.AppSettings) domain.AppSettings {
	def := Defaults()
	s = fillDefaults(s)
	switch s.Theme {
	case "light", "dark", "system":
	default:
		s.Theme = def.Theme
	}
	switch strings.ToLower(strings.TrimSpace(s.LogLevel)) {
	case "debug", "info", "warn", "error":
	default:
		s.LogLevel = def.LogLevel
	}
	if s.KeepLogDays < 1 || s.KeepLogDays > 90 {
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
