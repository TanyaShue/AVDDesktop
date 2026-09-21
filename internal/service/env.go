package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/avd/store"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk/detect"
)

// EnvService 提供环境自检与宿主交互。
type EnvService struct{ rt *Runtime }

// NewEnvService 创建 EnvService。
func NewEnvService(rt *Runtime) *EnvService { return &EnvService{rt: rt} }

// DetectRequest 是自检请求。
type DetectRequest struct {
	Force           bool   `json:"force"`
	SdkRootOverride string `json:"sdkRootOverride,omitempty"`
}

// Detect 执行全量环境自检。
func (s *EnvService) Detect(req DetectRequest) (*domain.EnvReport, error) {
	if req.Force {
		s.rt.detector.Invalidate()
	}
	settings := s.rt.settings.Get()
	sdkRoot := req.SdkRootOverride
	if sdkRoot == "" {
		sdkRoot = settings.SdkRoot
	}

	comp := s.rt.Components()
	report, err := s.rt.detector.Detect(s.rt.Context(), detect.Options{
		SdkRootOverride:  sdkRoot,
		JdkPathOverride:  settings.JdkPath,
		AvdHomeOverride:  settings.AvdHome,
		InjectEnv:        settings.InjectEnvForChild,
		RunningInstances: s.runningInstanceCount(),
		AvdIssues:        s.avdHealthIssues(comp),
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

// runningInstanceCount 统计处于活动状态的实例数。
func (s *EnvService) runningInstanceCount() int {
	count := 0
	for _, inst := range s.rt.Components().Launcher.List() {
		switch inst.State {
		case domain.AvdStarting, domain.AvdBooting, domain.AvdRunning, domain.AvdStopping:
			count++
		}
	}
	return count
}

// avdHealthIssues 汇总设备配置问题（损坏配置、缺失镜像、.ini 缺失等）。
//
// 使用 WithSize=false 的快路径：遍历数 GB 的设备数据目录会让自检变慢数秒。
func (s *EnvService) avdHealthIssues(comp *components) []string {
	items, err := comp.Store.ListWith(store.ListOptions{WithSize: false})
	if err != nil || len(items) == 0 {
		return nil
	}
	var issues []string
	for _, it := range items {
		if strings.TrimSpace(it.Broken) != "" {
			issues = append(issues, fmt.Sprintf("%s：%s", it.DisplayName, it.Broken))
			continue
		}
		// 缺少 .ini 的 AVD 在重启后会被工具忽略，属于隐性问题
		layout := comp.Store.Resolve(it.Name)
		if layout.Exists && !platform.FileExists(layout.IniPath) {
			if err := comp.Store.EnsureIni(it.Name); err == nil {
				s.rt.Log().Warn("avd", "设备 %s 缺少 .ini，已自动补写", it.Name)
			} else {
				issues = append(issues, it.DisplayName+"：缺少 .ini 配置文件且无法自动修复")
			}
		}
	}
	return issues
}

// EnabledWindowsFeatures 返回 Windows 关键开关（虚拟化/长路径），供加速引导面板使用。
func (s *EnvService) EnabledWindowsFeatures() (*domain.WindowsInfo, error) {
	v := platform.VirtualizationInfoCached(s.rt.Context())
	info := &domain.WindowsInfo{
		Available:         v.Available,
		HypervisorPresent: v.HypervisorPresent,
		VirtFirmware:      v.VirtFirmware,
		SLAT:              v.SLAT,
		VMMonitor:         v.VMMonitor,
		LongPathsEnabled:  v.LongPathsEnabled,
		HyperVHostService: v.HyperVHostService,
		VMComputeService:  v.VMComputeService,
		CPU:               v.CPU,
		ProductName:       v.ProductName,
		Caption:           v.Caption,
		Version:           v.Version,
		Build:             v.Build,
		Source:            v.Source,
		Error:             v.Error,
	}
	return info, nil
}

// DetectSdkRoots 返回候选 SDK 根目录。
func (s *EnvService) DetectSdkRoots() []domain.SdkRootCandidate {
	return platform.DiscoverSdkRoots("")
}

// DetectJava 只检测 JDK。
func (s *EnvService) DetectJava() (*domain.ToolStatus, error) {
	settings := s.rt.settings.Get()
	javaPath := platform.FindJava(settings.JdkPath)
	env := platform.ChildEnv(settings.SdkRoot, "", settings.InjectEnvForChild)
	status := detectJavaStatus(s.rt, javaPath, env)
	return &status, nil
}

// CheckAcceleration 只检测硬件加速。
func (s *EnvService) CheckAcceleration() (*domain.AccelInfo, error) {
	comp := s.rt.Components()
	info := detect.CheckAcceleration(s.rt.Context(), comp.Paths.EmulatorExe, comp.Env)
	// 用 Windows 开关信息补充“为什么不可用”的可操作建议
	if !info.Available {
		win := platform.VirtualizationInfoCached(s.rt.Context())
		if win.Available && win.HypervisorPresent {
			info.Hints = append(info.Hints,
				"检测到机器已有 hypervisor 运行：请确认「Windows 虚拟机监控程序平台」已启用",
				"开启命令：dism /online /enable-feature /featurename:HypervisorPlatform /all /norestart")
		}
		if win.Available && !win.HypervisorPresent && !win.VirtFirmware && !win.VMMonitor {
			info.Hints = append(info.Hints,
				"未检测到任何 hypervisor，且 CPU 虚拟化能力未上报：请先在 BIOS/UEFI 中开启 VT-x/AMD-V")
		}
	}
	return &info, nil
}

// DiskSpace 查询指定路径所在磁盘空间。
func (s *EnvService) DiskSpace(path string) (*domain.DiskInfo, error) {
	if strings.TrimSpace(path) == "" {
		path = s.rt.Components().Paths.SdkRoot
	}
	info := platform.DiskSpace(path)
	return &info, nil
}

// ResolveAvdHome 解析 AVD 主目录（含来源说明）。
func (s *EnvService) ResolveAvdHome() (*domain.AvdHomeInfo, error) {
	settings := s.rt.settings.Get()
	res := platform.ResolveAvdHome(settings.AvdHome)
	info := domain.AvdHomeInfo{
		Path:     res.Path,
		Source:   res.Source,
		Exists:   platform.DirExists(res.Path),
		Writable: platform.IsWritable(res.Path),
	}
	if entries, err := os.ReadDir(res.Path); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".ini") {
				info.Count++
			}
		}
	}
	return &info, nil
}

// ValidateSdkRoot 校验用户手动指定的 SDK 路径。
func (s *EnvService) ValidateSdkRoot(path string) (*domain.SdkRootValidation, error) {
	out := &domain.SdkRootValidation{Path: path}
	if strings.TrimSpace(path) == "" {
		out.Message = "路径为空"
		return out, nil
	}
	if !platform.DirExists(path) {
		out.Message = "目录不存在（可以是新目录，安装时会自动创建）"
		out.Writable = platform.IsWritable(path)
		out.OK = out.Writable
		return out, nil
	}
	out.Writable = platform.IsWritable(path)
	checks := []struct {
		name string
		dir  string
	}{
		{"cmdline-tools", filepath.Join(path, "cmdline-tools")},
		{"platform-tools", filepath.Join(path, "platform-tools")},
		{"emulator", filepath.Join(path, "emulator")},
		{"licenses", filepath.Join(path, "licenses")},
		{"system-images", filepath.Join(path, "system-images")},
	}
	for _, c := range checks {
		if platform.DirExists(c.dir) {
			out.Found = append(out.Found, c.name)
		}
	}
	for _, c := range []string{"cmdline-tools", "platform-tools", "emulator"} {
		if !contains(out.Found, c) {
			out.Missing = append(out.Missing, c)
		}
	}
	out.OK = out.Writable
	if !out.Writable {
		out.Message = "目录不可写，请选择其它位置或使用管理员权限"
	} else if len(out.Missing) == 0 {
		out.Message = "看起来是一个完整的 Android SDK 目录"
	} else {
		out.Message = "目录可写，但缺少部分组件，可通过安装补齐"
	}
	return out, nil
}

// ResolvedPaths 返回当前解析出的关键路径。
func (s *EnvService) ResolvedPaths() *ResolvedPaths {
	out := s.rt.Resolved()
	return &out
}

// ---------------------------------------------------------------- 宿主交互

// PickRequest 是文件/目录选择请求。
type PickRequest struct {
	Title            string `json:"title"`
	DefaultDirectory string `json:"defaultDirectory,omitempty"`
	DefaultFilename  string `json:"defaultFilename,omitempty"`
	FilterDisplay    string `json:"filterDisplay,omitempty"`
	FilterPattern    string `json:"filterPattern,omitempty"`
}

// PickDirectory 打开原生目录选择对话框。
func (s *EnvService) PickDirectory(req PickRequest) (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(s.rt.Context(), wailsruntime.OpenDialogOptions{
		Title:            orDefault(req.Title, "选择目录"),
		DefaultDirectory: req.DefaultDirectory,
	})
	if err != nil {
		return "", domain.Wrap(domain.CodeUnknown, "无法打开目录选择对话框", err)
	}
	return dir, nil
}

// PickFile 打开原生文件选择对话框。
func (s *EnvService) PickFile(req PickRequest) (string, error) {
	opts := wailsruntime.OpenDialogOptions{
		Title:            orDefault(req.Title, "选择文件"),
		DefaultDirectory: req.DefaultDirectory,
		DefaultFilename:  req.DefaultFilename,
	}
	if req.FilterPattern != "" {
		opts.Filters = []wailsruntime.FileFilter{{
			DisplayName: orDefault(req.FilterDisplay, req.FilterPattern),
			Pattern:     req.FilterPattern,
		}}
	}
	file, err := wailsruntime.OpenFileDialog(s.rt.Context(), opts)
	if err != nil {
		return "", domain.Wrap(domain.CodeUnknown, "无法打开文件选择对话框", err)
	}
	return file, nil
}

// SaveFile 打开保存对话框并返回路径。
func (s *EnvService) SaveFile(req PickRequest) (string, error) {
	opts := wailsruntime.SaveDialogOptions{
		Title:           orDefault(req.Title, "保存文件"),
		DefaultFilename: req.DefaultFilename,
	}
	if req.FilterPattern != "" {
		opts.Filters = []wailsruntime.FileFilter{{
			DisplayName: orDefault(req.FilterDisplay, req.FilterPattern),
			Pattern:     req.FilterPattern,
		}}
	}
	path, err := wailsruntime.SaveFileDialog(s.rt.Context(), opts)
	if err != nil {
		return "", domain.Wrap(domain.CodeUnknown, "无法打开保存对话框", err)
	}
	return path, nil
}

// OpenInExplorer 在资源管理器中打开路径（Windows）/ 文件管理器中打开（其它平台）。
func (s *EnvService) OpenInExplorer(path string) error {
	if path == "" {
		return domain.Err(domain.CodeInvalidArgument, "路径为空")
	}
	if !platform.DirExists(path) && !platform.FileExists(path) {
		if err := platform.EnsureDir(path); err != nil {
			return domain.Err(domain.CodePathNotFound, "路径不存在: "+path)
		}
	}
	wailsruntime.BrowserOpenURL(s.rt.Context(), "file://"+filepath.ToSlash(path))
	return nil
}

// OpenExternalURL 用系统浏览器打开链接。
func (s *EnvService) OpenExternalURL(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return domain.Err(domain.CodeInvalidArgument, "只允许打开 http/https 链接")
	}
	wailsruntime.BrowserOpenURL(s.rt.Context(), url)
	return nil
}

// CopyToClipboard 复制文本到剪贴板。
func (s *EnvService) CopyToClipboard(text string) error {
	wailsruntime.ClipboardSetText(s.rt.Context(), text)
	return nil
}

// OpenTerminal 打开终端并切换到指定目录（复制命令供用户粘贴）。
type TerminalRequest struct {
	Directory string `json:"directory"`
	Command   string `json:"command,omitempty"`
}

// OpenTerminal 在目标目录打开系统终端；命令会复制到剪贴板。
func (s *EnvService) OpenTerminal(req TerminalRequest) error {
	dir := req.Directory
	if dir == "" {
		dir = s.rt.Components().Paths.SdkRoot
	}
	if !platform.DirExists(dir) {
		if err := platform.EnsureDir(dir); err != nil {
			return domain.Err(domain.CodePathNotFound, "目录不存在: "+dir)
		}
	}
	if req.Command != "" {
		wailsruntime.ClipboardSetText(s.rt.Context(), req.Command)
	}
	switch runtime.GOOS {
	case "windows":
		wailsruntime.BrowserOpenURL(s.rt.Context(), "cmd://"+dir)
	default:
		wailsruntime.BrowserOpenURL(s.rt.Context(), "file://"+filepath.ToSlash(dir))
	}
	return nil
}

// HostInfo 返回宿主信息摘要（标题栏/诊断用）。
func (s *EnvService) HostInfo() map[string]string {
	return map[string]string{
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
		"version": s.rt.Version,
		"app":     s.rt.AppName,
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// detectJavaStatus 复用探测器里 JDK 那一项的逻辑，避免导出 detect 包内部函数。
func detectJavaStatus(rt *Runtime, javaPath string, env []string) domain.ToolStatus {
	report, err := rt.detector.Detect(rt.Context(), rt.detectOptions())
	if err == nil {
		for _, c := range report.Components {
			if c.ID == domain.ToolJDK {
				return c
			}
		}
	}
	st := domain.ToolStatus{ID: domain.ToolJDK, Name: "Java 运行环境 (JDK)", Path: javaPath}
	if javaPath == "" {
		st.State = domain.StateMissing
		st.Detail = "未找到 java，命令行工具需要 JDK " + detect.MinJDKVersion + "+"
		st.Fix = &domain.ToolFix{Kind: domain.FixSetJDK, Label: "指定 JDK"}
		return st
	}
	st.State = domain.StateUnknown
	st.Detail = "检测超时，请重试"
	_ = env
	return st
}
