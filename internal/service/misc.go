package service

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk/detect"
)

// ---------------------------------------------------------------- SettingsService

// SettingsService 读写用户设置。
type SettingsService struct{ rt *Runtime }

// NewSettingsService 创建 SettingsService。
func NewSettingsService(rt *Runtime) *SettingsService { return &SettingsService{rt: rt} }

// Get 返回当前设置。
func (s *SettingsService) Get() domain.AppSettings { return s.rt.settings.Get() }

// Update 以浅合并方式更新设置。
func (s *SettingsService) Update(patch map[string]any) (*domain.AppSettings, error) {
	before := s.rt.settings.Get()
	after, err := s.rt.settings.Update(patch)
	if err != nil {
		return nil, err
	}
	// SDK 根 / AVD 目录变化 → 重建工具链句柄并重新自检
	if after.SdkRoot != before.SdkRoot || after.AvdHome != before.AvdHome ||
		after.JdkPath != before.JdkPath || after.InjectEnvForChild != before.InjectEnvForChild {
		s.rt.InvalidateComponents()
	}
	// 日志级别变化 → 立即生效
	if after.LogLevel != before.LogLevel {
		s.rt.Log().SetLevel(after.LogLevel)
		s.rt.Log().Info("settings", "日志级别已切换为 %s", after.LogLevel)
	}
	if after.ProxyMode != before.ProxyMode || after.ProxyURL != before.ProxyURL {
		s.rt.Log().Info("settings", "代理设置已更新：mode=%s url=%s", after.ProxyMode, after.ProxyURL)
	}
	out := after
	return &out, nil
}

// Reset 重置某一组设置（section 为空表示全部）。
func (s *SettingsService) Reset(section string) (*domain.AppSettings, error) {
	out, err := s.rt.settings.ResetSection(section)
	if err != nil {
		return nil, err
	}
	s.rt.InvalidateComponents()
	return &out, nil
}

// Path 返回设置文件路径。
func (s *SettingsService) Path() string { return s.rt.settings.Path() }

// ---------------------------------------------------------------- DiagnosticsService

// DiagnosticsService 提供诊断与自检。
type DiagnosticsService struct{ rt *Runtime }

// NewDiagnosticsService 创建 DiagnosticsService。
func NewDiagnosticsService(rt *Runtime) *DiagnosticsService { return &DiagnosticsService{rt: rt} }

// EnvSummary 返回诊断摘要（环境 + 设置 + 检查项）。
func (s *DiagnosticsService) EnvSummary() (*domain.DiagnosticReport, error) {
	report, err := s.rt.detector.Detect(s.rt.Context(), s.rt.detectOptions())
	if err != nil {
		return nil, err
	}
	out := &domain.DiagnosticReport{
		GeneratedAt: platform.NowMs(),
		AppVersion:  s.rt.Version,
		Env:         *report,
		Settings:    s.rt.settings.Get(),
	}
	return out, nil
}

// RunSelfCheck 执行端到端冒烟检查（本地文件 → 网络 → 进程 → 磁盘）。
func (s *DiagnosticsService) RunSelfCheck() (string, error) {
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:       domain.JobDiagnostics,
		Title:      "运行自检",
		Subtitle:   "环境诊断",
		ItemsTotal: 5,
	}, func(ctx context.Context, j *job.Job) error {
		comp := s.rt.Components()
		checks := []domain.CheckResult{}

		// 1) 目录可写
		start := time.Now()
		err := platform.EnsureDir(comp.Paths.SdkRoot)
		checks = append(checks, domain.CheckResult{
			Name: "SDK 目录可写", OK: err == nil && platform.IsWritable(comp.Paths.SdkRoot),
			Detail: nonEmpty(errString(err), comp.Paths.SdkRoot), ElapsedMs: time.Since(start).Milliseconds(),
		})

		// 2) 镜像索引可获取
		start = time.Now()
		source := s.rt.ActiveSource()
		_, idxErr := comp.Installer.ResolveIndex(ctx, source, true)
		checks = append(checks, domain.CheckResult{
			Name: "镜像索引可获取（" + source.Name + "）", OK: idxErr == nil,
			Detail: nonEmpty(errString(idxErr), source.BaseURL), ElapsedMs: time.Since(start).Milliseconds(),
		})

		// 3) 外部工具可用
		start = time.Now()
		adbOK := comp.Adb.Available()
		checks = append(checks, domain.CheckResult{
			Name: "adb 可用", OK: adbOK, Detail: comp.Paths.Adb,
			ElapsedMs: time.Since(start).Milliseconds(),
		})

		// 4) 加速可用
		start = time.Now()
		accel := detect.CheckAcceleration(ctx, comp.Paths.EmulatorExe, comp.Env)
		checks = append(checks, domain.CheckResult{
			Name: "硬件加速可用", OK: accel.Available,
			Detail: nonEmpty(accel.Kind, "未检测到"), ElapsedMs: time.Since(start).Milliseconds(),
		})

		// 5) 磁盘空间
		disk := platform.DiskSpace(comp.Paths.SdkRoot)
		checks = append(checks, domain.CheckResult{
			Name: "磁盘空间 ≥ 12 GB", OK: disk.Sufficient,
			Detail: itoa(int(disk.FreeGB)) + " GB 可用", ElapsedMs: 0,
		})

		for _, c := range checks {
			level := "info"
			if !c.OK {
				level = "warn"
			}
			j.Logf(level, "selfcheck", "%s：%s（%s）", c.Name, boolText(c.OK), c.Detail)
		}
		failed := 0
		for _, c := range checks {
			if !c.OK {
				failed++
			}
		}
		s.rt.Log().Info("diagnostics", "自检完成：%d/%d 项通过", len(checks)-failed, len(checks))
		s.rt.Emit("diagnostics:checks", checks)
		return nil
	})
	return j.ID(), nil
}

// ExportReport 导出诊断包（zip：设置 + 环境 + 日志）。
func (s *DiagnosticsService) ExportReport() (string, error) {
	report, err := s.EnvSummary()
	if err != nil {
		return "", err
	}
	target := filepath.Join(platform.SubDir(s.rt.AppName, "logs"),
		"avddesktop-diagnostics-"+time.Now().Format("20060102-150405")+".zip")

	file, err := os.Create(target)
	if err != nil {
		return "", domain.Wrap(domain.CodePermissionDenied, "无法创建诊断包", err)
	}
	defer func() { _ = file.Close() }()

	zw := zip.NewWriter(file)
	defer func() { _ = zw.Close() }()

	if err := writeZipJSON(zw, "environment.json", report); err != nil {
		return "", err
	}
	if err := writeZipJSON(zw, "settings.json", s.rt.settings.Get()); err != nil {
		return "", err
	}
	if err := writeZipJSON(zw, "resolved-paths.json", s.rt.Resolved()); err != nil {
		return "", err
	}
	if err := appendLogs(zw, platform.SubDir(s.rt.AppName, "logs"), 3); err != nil {
		return "", err
	}
	return target, nil
}

// ReadAppLog 读取日志尾部：优先内存环形缓冲（实时、跨文件），缓存未命中时回退读文件。
func (s *DiagnosticsService) ReadAppLog(tail int) ([]domain.LogLine, error) {
	if tail <= 0 {
		tail = 200
	}
	if entries := s.rt.Log().Tail(tail); len(entries) > 0 {
		return entriesToLines(entries), nil
	}
	return s.readLogFile(tail)
}

// maxReadBytes 限制一次性读取的日志文件大小，避免超大文件把 UI 卡死。
const maxReadBytes = 4 << 20

func (s *DiagnosticsService) readLogFile(tail int) ([]domain.LogLine, error) {
	dir := s.rt.Log().Dir()
	if dir == "" {
		dir = platform.SubDir(s.rt.AppName, "logs")
	}
	files := s.rt.Log().Files()
	if len(files) == 0 {
		return nil, nil
	}
	newest := files[0]
	data, err := os.ReadFile(newest)
	if err != nil {
		return nil, domain.Wrap(domain.CodePermissionDenied, "无法读取日志文件", err)
	}
	if len(data) > maxReadBytes {
		data = data[len(data)-maxReadBytes:]
	}
	lines := platform.SplitLines(string(data))
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	out := make([]domain.LogLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, domain.LogLine{At: 0, Level: "info", Source: "app", Message: l})
	}
	return out, nil
}

// SetLogLevel 切换日志级别（设置页控制）。
func (s *DiagnosticsService) SetLogLevel(level string) error {
	switch level {
	case "debug", "info", "warn", "error":
	default:
		return domain.Err(domain.CodeInvalidArgument, "日志级别必须是 debug/info/warn/error")
	}
	if _, err := s.rt.settings.Update(map[string]any{"logLevel": level}); err != nil {
		return err
	}
	s.rt.Log().SetLevel(level)
	s.rt.Log().Info("logging", "日志级别已切换为 %s", level)
	return nil
}

// LogLevel 返回当前日志级别。
func (s *DiagnosticsService) LogLevel() string { return s.rt.Log().Level() }

// LogDir 返回日志目录。
func (s *DiagnosticsService) LogDir() string {
	if dir := s.rt.Log().Dir(); dir != "" {
		return dir
	}
	return platform.SubDir(s.rt.AppName, "logs")
}

// LogFiles 返回保留的日志文件列表（新→旧）。
func (s *DiagnosticsService) LogFiles() []string { return domain.NonNil(s.rt.Log().Files()) }

// TestLog 向日志写入一条测试消息（验证日志链路与实时推送）。
func (s *DiagnosticsService) TestLog(message string) string {
	if message == "" {
		message = "这是一条测试日志"
	}
	s.rt.Log().Info("diagnostics", "%s", message)
	return s.LogLevel()
}

func entriesToLines(entries []logging.Entry) []domain.LogLine {
	out := make([]domain.LogLine, 0, len(entries))
	for _, e := range entries {
		out = append(out, domain.LogLine{At: e.At, Level: e.Level, Source: e.Module, Message: e.Message})
	}
	return domain.NonNil(out)
}

// ClearCache 清理缓存与临时下载文件。
func (s *DiagnosticsService) ClearCache() error {
	freed := int64(0)
	for _, dir := range []string{
		platform.SubDir(s.rt.AppName, "cache"),
		s.rt.downloadDir(),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			freed += platform.DirSize(path)
			_ = os.RemoveAll(path)
		}
	}
	s.rt.Log().Info("diagnostics", "缓存已清理：释放约 %s", platform.HumanSize(freed))
	return nil
}

// OpenLogFolder 打开日志目录。
func (s *DiagnosticsService) OpenLogFolder() error {
	dir := s.LogDir()
	if err := platform.EnsureDir(dir); err != nil {
		return domain.Wrap(domain.CodePathNotFound, "无法创建日志目录", err)
	}
	if err := platform.OpenPath(dir); err != nil {
		return domain.Wrap(domain.CodeUnknown, "无法打开日志目录", err)
	}
	return nil
}

// AppVersion 返回版本号。
func (s *DiagnosticsService) AppVersion() string { return s.rt.Version }

// ---------------------------------------------------------------- WindowService

// WindowService 提供无边框窗口的窗口控制（自绘标题栏使用）。
type WindowService struct{ rt *Runtime }

// NewWindowService 创建 WindowService。
func NewWindowService(rt *Runtime) *WindowService { return &WindowService{rt: rt} }

// Minimise 最小化窗口。
func (s *WindowService) Minimise() { wailsruntime.WindowMinimise(s.rt.Context()) }

// ToggleMaximise 切换最大化。
func (s *WindowService) ToggleMaximise() { wailsruntime.WindowToggleMaximise(s.rt.Context()) }

// IsMaximised 返回是否最大化。
func (s *WindowService) IsMaximised() bool { return wailsruntime.WindowIsMaximised(s.rt.Context()) }

// Close 退出应用。
func (s *WindowService) Close() { wailsruntime.Quit(s.rt.Context()) }

// ---------------------------------------------------------------- 辅助

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func boolText(v bool) string {
	if v {
		return "通过"
	}
	return "未通过"
}

func writeZipJSON(zw *zip.Writer, name string, v any) error {
	w, err := zw.Create(name)
	if err != nil {
		return domain.Wrap(domain.CodeUnknown, "无法写入诊断包", err)
	}
	data, err := jsonMarshalIndent(v)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func appendLogs(zw *zip.Writer, dir string, maxFiles int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	count := 0
	for i := len(entries) - 1; i >= 0 && count < maxFiles; i-- {
		e := entries[i]
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		src, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		w, err := zw.Create("logs/" + e.Name())
		if err != nil {
			_ = src.Close()
			continue
		}
		_, _ = io.Copy(w, src)
		_ = src.Close()
		count++
	}
	return nil
}
