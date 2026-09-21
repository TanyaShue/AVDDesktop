package service

import (
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/domain"
)

// SettingsService 暴露用户设置的读写。
type SettingsService struct{ rt *Runtime }

// NewSettingsService 创建 SettingsService。
func NewSettingsService(rt *Runtime) *SettingsService { return &SettingsService{rt: rt} }

// Get 返回当前设置。
func (s *SettingsService) Get() domain.AppSettings { return s.rt.settings.Get() }

// Update 以补丁方式更新设置。
func (s *SettingsService) Update(patch map[string]any) (domain.AppSettings, error) {
	next, err := s.rt.settings.Update(patch)
	if err != nil {
		return next, err
	}
	// 日志级别与保留天数需要立即生效
	s.rt.Log().SetLevel(next.LogLevel)
	return next, nil
}

// Reset 恢复默认设置。
func (s *SettingsService) Reset() (domain.AppSettings, error) {
	next, err := s.rt.settings.Reset()
	if err != nil {
		return next, err
	}
	s.rt.Log().SetLevel(next.LogLevel)
	return next, nil
}

// Path 返回设置文件路径。
func (s *SettingsService) Path() string { return s.rt.settings.Path() }

// LogService 暴露应用日志（底部统一日志区域的初始内容）。
type LogService struct{ rt *Runtime }

// NewLogService 创建 LogService。
func NewLogService(rt *Runtime) *LogService { return &LogService{rt: rt} }

// Tail 返回最近 n 条应用日志。
func (s *LogService) Tail(n int) []domain.LogLine {
	entries := s.rt.Log().Tail(n)
	out := make([]domain.LogLine, 0, len(entries))
	for _, e := range entries {
		out = append(out, domain.LogLine{At: e.At, Level: e.Level, Source: e.Module, Message: e.Message})
	}
	return domain.NonNil(out)
}

// WindowService 提供无边框窗口的控制。
type WindowService struct{ rt *Runtime }

// NewWindowService 创建 WindowService。
func NewWindowService(rt *Runtime) *WindowService { return &WindowService{rt: rt} }

// Minimise 最小化窗口。
func (s *WindowService) Minimise() { wailsruntime.WindowMinimise(s.rt.Context()) }

// ToggleMaximise 切换最大化。
func (s *WindowService) ToggleMaximise() {
	ctx := s.rt.Context()
	if wailsruntime.WindowIsMaximised(ctx) {
		wailsruntime.WindowUnmaximise(ctx)
		return
	}
	wailsruntime.WindowMaximise(ctx)
}

// IsMaximised 返回窗口是否最大化。
func (s *WindowService) IsMaximised() bool { return wailsruntime.WindowIsMaximised(s.rt.Context()) }

// Close 关闭窗口。
func (s *WindowService) Close() { wailsruntime.Quit(s.rt.Context()) }
