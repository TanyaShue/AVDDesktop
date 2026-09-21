package service

import (
	"context"
	"runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sdk"
)

// EnvService 提供环境检查与自动准备（全局唯一入口）。
type EnvService struct{ rt *Runtime }

// NewEnvService 创建 EnvService。
func NewEnvService(rt *Runtime) *EnvService { return &EnvService{rt: rt} }

// 各类命令的检测超时。
const (
	javaTimeout     = 30 * time.Second
	sdkmanagerProbe = 60 * time.Second
	adbProbe        = 30 * time.Second
	emulatorProbe   = 90 * time.Second
)

// Check 执行唯一的环境检查：确认软件自带的 sdkmanager / avdmanager / emulator / adb 与 JDK 是否可用。
func (s *EnvService) Check() (*domain.EnvReport, error) {
	started := time.Now()
	comp := s.rt.Components()
	tools := comp.Tools
	env := comp.Env
	ctx := s.rt.Context()

	report := &domain.EnvReport{
		AppRoot:  platform.Root(),
		SdkRoot:  tools.SdkRoot,
		AvdHome:  platform.AvdHome(),
		JavaPath: platform.FindJava(),
		Host: domain.HostInfo{
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
			CPUCores: runtime.NumCPU(),
		},
		CheckedAt: platform.NowMs(),
	}

	javaVersion := ""
	if report.JavaPath != "" {
		javaVersion = parseJavaVersion(sdk.ToolVersion(ctx, report.JavaPath, []string{"-version"}, env, javaTimeout))
	} else {
		javaVersion = sdk.ToolVersion(ctx, "java", []string{"-version"}, env, javaTimeout)
	}

	javaOK := report.JavaPath != "" && javaVersion != ""
	report.Components = append(report.Components, toolStatus(domain.ToolJDK, "JDK", javaOK, javaVersion, report.JavaPath, &domain.ToolFix{
		Label:   "查看说明",
		Command: "安装 JDK 17 或更高版本，并设置 JAVA_HOME（sdkmanager 与 avdmanager 依赖 JDK）",
	}))

	// sdkmanager / avdmanager 由 JDK 驱动：缺 JDK 时它们即使存在也无法运行。
	sdkmanagerVersion := ""
	if javaOK {
		sdkmanagerVersion = sdk.ToolVersion(ctx, tools.Sdkmanager, []string{"--version"}, env, sdkmanagerProbe)
	}
	report.Components = append(report.Components, toolStatus(domain.ToolSdkmanager, "sdkmanager", tools.HasSdkmanager() && sdkmanagerVersion != "", sdkmanagerVersion, tools.Sdkmanager, &domain.ToolFix{
		Kind:  domain.FixPrepare,
		Label: "自动准备 SDK",
	}))
	report.Components = append(report.Components, toolStatus(domain.ToolAvdmanager, "avdmanager", tools.HasAvdmanager() && javaOK, "", tools.Avdmanager, &domain.ToolFix{
		Kind:  domain.FixPrepare,
		Label: "自动准备 SDK",
	}))

	adbVersion := ""
	if tools.HasAdb() {
		adbVersion = parseToolVersionLine(sdk.ToolVersion(ctx, tools.Adb, []string{"version"}, env, adbProbe))
	}
	report.Components = append(report.Components, toolStatus(domain.ToolAdb, "adb (platform-tools)", tools.HasAdb(), adbVersion, tools.Adb, &domain.ToolFix{
		Kind:    domain.FixInstall,
		Label:   "安装 platform-tools",
		Payload: "platform-tools",
	}))

	emulatorVersion := ""
	if tools.HasEmulator() {
		emulatorVersion = parseEmulatorVersion(sdk.ToolVersion(ctx, tools.Emulator, []string{"-version"}, env, emulatorProbe))
	}
	report.Components = append(report.Components, toolStatus(domain.ToolEmulator, "emulator", tools.HasEmulator(), emulatorVersion, tools.Emulator, &domain.ToolFix{
		Kind:    domain.FixInstall,
		Label:   "安装 emulator",
		Payload: "emulator",
	}))

	// 已安装系统镜像（读官方 sdkmanager 的本地列表，不联网）
	report.NeedInit = !(tools.HasSdkmanager() && javaOK)
	if !report.NeedInit {
		if images, err := sdk.ListImages(ctx, tools, env, true, nil); err == nil {
			report.Images = len(images)
		} else {
			s.rt.Log().Warn("env", "读取已安装系统镜像失败: %v", err)
		}
	}

	// 硬件加速（仅在模拟器存在时检测，命令本身较慢）
	if tools.HasEmulator() {
		accel := s.checkAcceleration(ctx, tools, env)
		report.Accel = &accel
		report.Components = append(report.Components, domain.ToolStatus{
			ID:      domain.ToolAcceleration,
			Name:    "硬件加速",
			State:   domain.StatePresent,
			Version: accel.Kind,
			Detail:  accel.Raw,
		})
	}

	// 磁盘空间（系统镜像约 1.5-2 GB）
	report.Disk = platform.DiskSpace(tools.SdkRoot)
	report.Avds = len(s.avdNames(comp))

	report.Ready = javaOK && tools.HasSdkmanager() && tools.HasAvdmanager() && tools.HasAdb() && tools.HasEmulator()
	report.Issues = s.buildIssues(report, tools)
	report.ElapsedMs = time.Since(started).Milliseconds()
	return report, nil
}

// Prepare 自动准备 SDK 环境：下载命令行工具 → 接受许可 → 安装基础组件。
//
// 返回 jobID；进度与日志走统一任务区域。
func (s *EnvService) Prepare() (string, error) {
	comp := s.rt.Components()
	tools := comp.Tools
	env := comp.Env

	unlock, ok := s.rt.locks.TryLock("sdk:" + tools.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有 SDK 准备/安装任务正在进行").
			WithHint("请等待当前任务完成，或在底部任务区域取消它")
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobBootstrap,
		Title: "准备软件 SDK 环境",
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		if err := platform.EnsureLayout(); err != nil {
			return domain.Wrap(domain.CodePermissionDenied, "无法创建软件数据目录", err)
		}

		// 1. 命令行工具（sdkmanager / avdmanager 的唯一来源）
		if !tools.HasSdkmanager() {
			j.SetPhase("下载 Android 命令行工具")
			j.Logf("info", "sdk", "从官方地址下载命令行工具到 %s", tools.CmdlineTools)
			started := time.Now()
			lastBytes := int64(0)
			lastAt := started
			err := sdk.Bootstrap(ctx, tools, platform.CacheDir(), func(done, total int64) {
				speed := int64(0)
				if elapsed := time.Since(lastAt).Seconds(); elapsed > 0.5 {
					speed = int64(float64(done-lastBytes) / elapsed)
					lastBytes, lastAt = done, time.Now()
				}
				j.SetBytes(done, total, speed)
			})
			if err != nil {
				return err
			}
			j.Logf("info", "sdk", "命令行工具就绪（耗时 %s）", time.Since(started).Round(time.Second))
		} else {
			j.Logf("info", "sdk", "已存在命令行工具：%s", tools.Sdkmanager)
		}

		// 2. 许可（用 y 回答官方提示，不自行维护许可文件）
		j.SetPhase("接受 SDK 许可")
		if err := sdk.AcceptLicenses(ctx, tools, env, jobLine(j, "sdkmanager")); err != nil {
			return err
		}

		// 3. 基础组件：platform-tools（adb）与 emulator
		missing := make([]string, 0, 2)
		if !tools.HasAdb() {
			missing = append(missing, "platform-tools")
		}
		if !tools.HasEmulator() {
			missing = append(missing, "emulator")
		}
		if len(missing) > 0 {
			j.SetPhase("安装 " + strings.Join(missing, "、"))
			j.Logf("info", "sdk", "执行：%s", sdk.InstallCommand(tools, missing))
			if err := sdk.InstallPackages(ctx, tools, env, missing, jobLine(j, "sdkmanager")); err != nil {
				return err
			}
		}
		j.Logf("info", "sdk", "SDK 环境准备完成")
		s.rt.Emit("env:changed", nil)
		return nil
	})
	return j.ID(), nil
}

// Resolved 返回软件自有目录的解析结果。
func (s *EnvService) Resolved() *ResolvedPaths {
	res := s.rt.Resolved()
	return &res
}

// CopyToClipboard 把文本写入系统剪贴板（供界面复制命令/路径）。
func (s *EnvService) CopyToClipboard(text string) error {
	if strings.TrimSpace(text) == "" {
		return domain.Err(domain.CodeInvalidArgument, "复制内容为空")
	}
	if !s.rt.ContextReady() {
		return domain.Err(domain.CodeUnknown, "应用尚未就绪")
	}
	wailsruntime.ClipboardSetText(s.rt.Context(), text)
	return nil
}

// ---------------------------------------------------------------- 内部辅助

func (s *EnvService) checkAcceleration(ctx context.Context, tools platform.Tools, env []string) domain.AccelInfo {
	res, err := proc.Run(ctx, tools.Emulator, []string{"-accel-check"}, proc.Options{Env: env, Timeout: emulatorProbe})
	raw := strings.TrimSpace(res.Combined())
	info := domain.AccelInfo{Raw: raw}
	lower := strings.ToLower(raw)
	switch {
	case err == nil && strings.Contains(lower, "is installed and usable"):
		info.Available = true
	case strings.Contains(lower, "is not installed"), strings.Contains(lower, "not installed"):
		info.Hints = append(info.Hints, "未安装可用的硬件加速（Windows 可启用「Windows 虚拟机监控程序平台」或安装 AEHD）")
	case strings.Contains(lower, "not supported"), strings.Contains(lower, "not enabled"):
		info.Hints = append(info.Hints, "CPU 虚拟化未开启：请在 BIOS/UEFI 中启用 VT-x / AMD-V")
	}
	for _, kind := range []string{"WHPX", "HAXM", "AEHD", "GVM", "KVM"} {
		if strings.Contains(strings.ToUpper(raw), kind) {
			info.Kind = strings.ToLower(kind)
			break
		}
	}
	if info.Kind == "" {
		info.Kind = "none"
	}
	if !info.Available {
		info.Hints = append(info.Hints, "没有硬件加速时模拟器可以启动，但会明显变慢")
	}
	return info
}

func (s *EnvService) avdNames(comp *components) []string {
	items, err := comp.Store.List()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

// buildIssues 汇总可操作的环境问题。
func (s *EnvService) buildIssues(report *domain.EnvReport, tools platform.Tools) []domain.EnvIssue {
	var issues []domain.EnvIssue

	if report.JavaPath == "" {
		issues = append(issues, domain.EnvIssue{
			ID:       "jdk-missing",
			Severity: domain.SeverityBlocker,
			Title:    "未找到 JDK",
			Detail:   "sdkmanager 与 avdmanager 需要 JDK 17 或更高版本。请安装 JDK 并设置 JAVA_HOME，或把 java 加入 PATH。",
		})
	}
	if report.NeedInit {
		issues = append(issues, domain.EnvIssue{
			ID:       "sdk-not-initialized",
			Severity: domain.SeverityWarning,
			Title:    "软件自带 SDK 尚未初始化",
			Detail:   "将下载官方命令行工具到 " + tools.CmdlineTools + "，并安装 platform-tools 与 emulator。",
			FixKind:  domain.FixPrepare,
			FixLabel: "立即准备",
		})
	} else {
		for _, comp := range report.Components {
			if comp.State == domain.StatePresent || comp.Fix == nil {
				continue
			}
			if comp.ID == domain.ToolJDK {
				continue
			}
			detail := "缺少 " + comp.Name
			if comp.Fix.Kind == domain.FixInstall {
				detail += "，可通过 sdkmanager 安装：" + comp.Fix.Payload
			}
			issues = append(issues, domain.EnvIssue{
				ID:         "missing-" + string(comp.ID),
				Severity:   domain.SeverityWarning,
				Title:      comp.Name + " 不可用",
				Detail:     detail,
				FixKind:    comp.Fix.Kind,
				FixLabel:   comp.Fix.Label,
				FixPayload: comp.Fix.Payload,
				FixCommand: comp.Fix.Command,
			})
		}
	}
	if !report.Disk.Sufficient {
		issues = append(issues, domain.EnvIssue{
			ID:       "disk-low",
			Severity: domain.SeverityWarning,
			Title:    "SDK 所在磁盘空间不足",
			Detail:   report.Disk.Path + " 剩余 " + platform.HumanSize(report.Disk.FreeGB<<30) + "，建议至少保留 12 GB 用于系统镜像",
		})
	}
	if report.Accel != nil && !report.Accel.Available {
		issues = append(issues, domain.EnvIssue{
			ID:       "accel-unavailable",
			Severity: domain.SeverityInfo,
			Title:    "硬件加速不可用",
			Detail:   strings.Join(report.Accel.Hints, "；"),
		})
	}
	return domain.NonNil(issues)
}

// toolStatus 构造组件状态，未就绪时附带修复动作。
func toolStatus(id domain.ToolID, name string, ok bool, version, path string, fix *domain.ToolFix) domain.ToolStatus {
	state := domain.StateMissing
	if ok {
		state = domain.StatePresent
		fix = nil
	}
	return domain.ToolStatus{ID: id, Name: name, State: state, Version: version, Path: path, Fix: fix}
}

// jobLine 把命令输出写进任务日志。
func jobLine(j *job.Job, source string) func(stream, line string) {
	return func(stream, line string) {
		level := "info"
		if stream == "stderr" {
			level = "warn"
		}
		j.Log(level, source, line)
	}
}

// parseJavaVersion 从 `java -version` 输出中取版本号（形如 17.0.6 或 1.8.0_392）。
func parseJavaVersion(out string) string {
	text := out
	if i := strings.Index(text, "\""); i >= 0 {
		if j := strings.Index(text[i+1:], "\""); j >= 0 {
			text = text[i+1 : i+1+j]
		}
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "1.") {
		text = strings.TrimPrefix(text, "1.")
	}
	if i := strings.IndexAny(text, "-+_"); i > 0 {
		text = text[:i]
	}
	return text
}

// parseEmulatorVersion 从 `emulator -version` 输出中取版本号。
func parseEmulatorVersion(out string) string {
	const marker = "Android emulator version "
	if i := strings.Index(out, marker); i >= 0 {
		rest := out[i+len(marker):]
		if j := strings.IndexAny(rest, " \r\n"); j > 0 {
			return rest[:j]
		}
		return strings.TrimSpace(rest)
	}
	return parseToolVersionLine(out)
}

// parseToolVersionLine 取 "… version 1.2.3" 形式的版本号。
func parseToolVersionLine(out string) string {
	fields := strings.Fields(out)
	for i, f := range fields {
		if strings.EqualFold(f, "version") && i+1 < len(fields) {
			return strings.TrimSpace(fields[i+1])
		}
	}
	return firstLineOrEmpty(out)
}

func firstLineOrEmpty(out string) string {
	lines := platform.SplitLines(out)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
