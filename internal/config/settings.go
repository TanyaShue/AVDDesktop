// Package config 管理用户设置（settings.json）的读写与迁移。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// CurrentVersion 是设置的 schema 版本，用于未来做迁移。
const CurrentVersion = 1

// Defaults 返回默认设置。
func Defaults() domain.AppSettings {
	return domain.AppSettings{
		Version:                CurrentVersion,
		SdkRoot:                "",
		JdkPath:                "",
		AvdHome:                "",
		InjectEnvForChild:      true,
		ActiveSourceID:         "", // 由 service 层用 mirror.DefaultSourceID 填充
		CustomSources:          nil,
		AutoFallbackToOfficial: true,
		MaxConnectionsPerFile:  4,
		MaxParallelPackages:    2,
		TimeoutSeconds:         30,
		SpeedLimitKBps:         0,
		ProxyMode:              domain.ProxyOff,
		ProxyURL:               "",
		DownloadDir:            "",
		AcceptedLicenseIDs:     nil,
		AutoAcceptLicenses:     false,
		DefaultDeviceProfile:   "medium_phone",
		DefaultRAMMB:           2048,
		DefaultCores:           4,
		DefaultDataPartitionG:  "6G",
		DefaultGPUMode:         "auto",
		Theme:                  "light",
		Language:               "zh-CN",
		DeviceViewMode:         "grid",
		ConfirmBeforeDelete:    true,
		ShowTaskDrawer:         true,
		LogLevel:               "info",
		KeepLogDays:            7,
		AskBeforeDriverInstall: true,
	}
}

// Manager 是设置的线程安全读写器。
type Manager struct {
	mu       sync.RWMutex
	path     string
	settings domain.AppSettings
}

// NewManager 创建设置管理器（不自动加载，由 Load 完成）。
func NewManager(path string) *Manager {
	return &Manager{path: path, settings: Defaults()}
}

// Path 返回配置文件路径。
func (m *Manager) Path() string { return m.path }

// Load 读取设置；文件不存在时写入默认值。
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
		// 配置损坏时不阻塞启动：备份后重建
		_ = os.Rename(m.path, m.path+".corrupt")
		m.settings = Defaults()
		return m.saveLocked()
	}
	m.settings = migrate(settings)
	return nil
}

// Get 返回设置副本。
func (m *Manager) Get() domain.AppSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := m.settings
	cp.CustomSources = append([]domain.MirrorSource(nil), m.settings.CustomSources...)
	cp.AcceptedLicenseIDs = append([]string(nil), m.settings.AcceptedLicenseIDs...)
	return cp
}

// Set 覆盖全部设置并持久化。
func (m *Manager) Set(settings domain.AppSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = settings
	return m.saveLocked()
}

// Update 以浅合并方式更新设置（patch 为 JSON 字段名）。
//
// 只允许已存在的字段被修改，避免前端写入脏数据。
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
			continue // 忽略未知字段
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
	next = migrate(next)
	if err := validate(&next); err != nil {
		return m.settings, err
	}
	m.settings = next
	if err := m.saveLocked(); err != nil {
		return m.settings, err
	}
	return m.settings, nil
}

// ResetSection 重置某一组设置（section 为空表示全部）。
func (m *Manager) ResetSection(section string) (domain.AppSettings, error) {
	def := Defaults()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch section {
	case "download":
		m.settings.MaxConnectionsPerFile = def.MaxConnectionsPerFile
		m.settings.MaxParallelPackages = def.MaxParallelPackages
		m.settings.TimeoutSeconds = def.TimeoutSeconds
		m.settings.SpeedLimitKBps = def.SpeedLimitKBps
		m.settings.ProxyMode = def.ProxyMode
		m.settings.ProxyURL = def.ProxyURL
		m.settings.DownloadDir = def.DownloadDir
	case "mirror":
		m.settings.ActiveSourceID = def.ActiveSourceID
		m.settings.CustomSources = nil
		m.settings.AutoFallbackToOfficial = def.AutoFallbackToOfficial
	case "ui":
		m.settings.Theme = def.Theme
		m.settings.Language = def.Language
		m.settings.DeviceViewMode = def.DeviceViewMode
		m.settings.ShowTaskDrawer = def.ShowTaskDrawer
		m.settings.ConfirmBeforeDelete = def.ConfirmBeforeDelete
	case "avd":
		m.settings.DefaultDeviceProfile = def.DefaultDeviceProfile
		m.settings.DefaultRAMMB = def.DefaultRAMMB
		m.settings.DefaultCores = def.DefaultCores
		m.settings.DefaultDataPartitionG = def.DefaultDataPartitionG
		m.settings.DefaultGPUMode = def.DefaultGPUMode
	default:
		m.settings = def
	}
	if err := m.saveLocked(); err != nil {
		return m.settings, err
	}
	return m.settings, nil
}

// AddCustomSource 添加/更新自定义镜像源。
func (m *Manager) AddCustomSource(src domain.MirrorSource) (domain.AppSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	replaced := false
	for i := range m.settings.CustomSources {
		if m.settings.CustomSources[i].ID == src.ID {
			m.settings.CustomSources[i] = src
			replaced = true
			break
		}
	}
	if !replaced {
		m.settings.CustomSources = append(m.settings.CustomSources, src)
	}
	if err := m.saveLocked(); err != nil {
		return m.settings, err
	}
	return m.settings, nil
}

// RemoveCustomSource 删除自定义镜像源。
func (m *Manager) RemoveCustomSource(id string) (domain.AppSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.settings.CustomSources[:0]
	for _, s := range m.settings.CustomSources {
		if s.ID != id {
			out = append(out, s)
		}
	}
	m.settings.CustomSources = out
	if m.settings.ActiveSourceID == id {
		m.settings.ActiveSourceID = ""
	}
	if err := m.saveLocked(); err != nil {
		return m.settings, err
	}
	return m.settings, nil
}

func (m *Manager) saveLocked() error {
	return platform.WriteJSON(m.path, m.settings)
}

// migrate 处理旧版本设置的字段补齐。
func migrate(s domain.AppSettings) domain.AppSettings {
	def := Defaults()
	if s.Version == 0 {
		s.Version = CurrentVersion
	}
	if s.MaxConnectionsPerFile <= 0 {
		s.MaxConnectionsPerFile = def.MaxConnectionsPerFile
	}
	if s.MaxParallelPackages <= 0 {
		s.MaxParallelPackages = def.MaxParallelPackages
	}
	if s.TimeoutSeconds <= 0 {
		s.TimeoutSeconds = def.TimeoutSeconds
	}
	if s.Theme == "" {
		s.Theme = def.Theme
	}
	if s.Language == "" {
		s.Language = def.Language
	}
	if s.DeviceViewMode == "" {
		s.DeviceViewMode = def.DeviceViewMode
	}
	if s.DefaultRAMMB <= 0 {
		s.DefaultRAMMB = def.DefaultRAMMB
	}
	if s.DefaultCores <= 0 {
		s.DefaultCores = def.DefaultCores
	}
	if s.DefaultDataPartitionG == "" {
		s.DefaultDataPartitionG = def.DefaultDataPartitionG
	}
	if s.DefaultGPUMode == "" {
		s.DefaultGPUMode = def.DefaultGPUMode
	}
	if s.DefaultDeviceProfile == "" {
		s.DefaultDeviceProfile = def.DefaultDeviceProfile
	}
	if s.LogLevel == "" {
		s.LogLevel = def.LogLevel
	}
	return s
}

// validate 做基本合法性检查。
func validate(s *domain.AppSettings) error {
	if s.MaxConnectionsPerFile < 1 || s.MaxConnectionsPerFile > 16 {
		return domain.Err(domain.CodeInvalidArgument, "单文件并发连接数必须在 1-16 之间")
	}
	if s.MaxParallelPackages < 1 || s.MaxParallelPackages > 8 {
		return domain.Err(domain.CodeInvalidArgument, "并行包数量必须在 1-8 之间")
	}
	if s.TimeoutSeconds < 5 || s.TimeoutSeconds > 3600 {
		return domain.Err(domain.CodeInvalidArgument, "超时时间必须在 5-3600 秒之间")
	}
	switch s.ProxyMode {
	case domain.ProxyOff, domain.ProxySystem, domain.ProxyCustom:
	default:
		return domain.Err(domain.CodeInvalidArgument, "代理模式必须是 off/system/custom")
	}
	if s.ProxyMode == domain.ProxyCustom && strings.TrimSpace(s.ProxyURL) == "" {
		return domain.Err(domain.CodeInvalidArgument, "自定义代理模式下必须填写代理地址")
	}
	if s.Theme != "light" && s.Theme != "dark" && s.Theme != "system" {
		return domain.Err(domain.CodeInvalidArgument, "主题必须是 light/dark/system")
	}
	return nil
}

// DiffSettings 返回两个设置的字段差异（用于诊断与审计）。
func DiffSettings(before, after domain.AppSettings) map[string][2]string {
	out := map[string][2]string{}
	bv := reflect.ValueOf(before)
	av := reflect.ValueOf(after)
	t := bv.Type()
	for i := 0; i < t.NumField(); i++ {
		bf := bv.Field(i)
		af := av.Field(i)
		if reflect.DeepEqual(bf.Interface(), af.Interface()) {
			continue
		}
		name := t.Field(i).Tag.Get("json")
		if name == "" {
			name = t.Field(i).Name
		}
		out[name] = [2]string{toStr(bf), toStr(af)}
	}
	return out
}

func toStr(v reflect.Value) string {
	b, err := json.Marshal(v.Interface())
	if err != nil {
		return ""
	}
	return string(b)
}

// DefaultPath 返回默认的 settings.json 路径。
func DefaultPath(appName string) string {
	return filepath.Join(platform.AppDataDir(appName), "settings.json")
}
