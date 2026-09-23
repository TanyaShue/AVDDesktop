package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/jdk"
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

	activeSource := activeMirrorSource(s.rt)
	activeJDK := activeJDKSource(s.rt)
	report := &domain.EnvReport{
		AppRoot:             platform.Root(),
		JdkRoot:             platform.JdkRoot(),
		SdkRoot:             tools.SdkRoot,
		AvdHome:             platform.AvdHome(),
		JavaPath:            platform.FindJava(),
		MirrorSourceID:      activeSource.ID,
		MirrorSourceName:    activeSource.Name,
		JDKMirrorSourceID:   activeJDK.ID,
		JDKMirrorSourceName: activeJDK.Name,
		Host: domain.HostInfo{
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
			CPUCores: runtime.NumCPU(),
		},
		CheckedAt: platform.NowMs(),
	}

	// 只检测软件自带 JDK：即使系统 JAVA_HOME / PATH 中有 java，也不采用。
	//
	// 各探测互不依赖，并发执行：串行时最坏情况（工具链损坏，每个命令都打满自己的超时）
	// 会阻塞绑定调用约 5 分钟，而故障环境恰恰是用户最常点"环境检查"的场景。
	hasAdb := tools.HasAdb() && sdk.VerifyPackage(tools, "platform-tools") == nil
	hasEmulator := tools.HasEmulator() && sdk.VerifyPackage(tools, "emulator") == nil
	cmdlineOK := tools.HasSdkmanager() && sdk.VerifyPackage(tools, "cmdline-tools;latest") == nil

	var (
		javaVersion, adbVersion, emulatorVersion string
		javaOK                                   bool
		accel                                    domain.AccelInfo
		probes                                   sync.WaitGroup
	)
	probes.Add(4)
	go func() {
		defer probes.Done()
		javaVersion, javaOK = s.probeJava(ctx, report.JavaPath, env)
	}()
	go func() {
		defer probes.Done()
		if hasAdb {
			adbVersion = parseToolVersionLine(sdk.ToolVersion(ctx, tools.Adb, []string{"version"}, env, adbProbe))
		}
	}()
	go func() {
		defer probes.Done()
		if hasEmulator {
			emulatorVersion = parseEmulatorVersion(sdk.ToolOutput(ctx, tools.Emulator, []string{"-version"}, env, emulatorProbe))
		}
	}()
	go func() {
		defer probes.Done()
		if tools.HasEmulator() {
			accel = s.checkAcceleration(ctx, tools, env)
		}
	}()
	probes.Wait()

	report.Components = append(report.Components, toolStatus(domain.ToolJDK, "JDK（软件自带）", javaOK, javaVersion, report.JdkRoot, &domain.ToolFix{
		Kind:  domain.FixPrepare,
		Label: "自动下载 JDK",
	}))

	// sdkmanager / avdmanager 由 JDK 驱动：缺 JDK 时它们即使存在也无法运行。
	// 同时检查包元数据，避免把以前版本留下的空目录/半成品当成可用环境。
	// 这两项依赖 JDK 探测结果，因此在并发探测结束后串行执行。
	sdkmanagerVersion := ""
	if javaOK && cmdlineOK {
		sdkmanagerVersion = parseSdkmanagerVersion(sdk.ToolOutput(ctx, tools.Sdkmanager, []string{"--version"}, env, sdkmanagerProbe))
	}
	sdkmanagerOK := cmdlineOK && sdkmanagerVersion != ""
	avdmanagerOK := cmdlineOK && javaOK
	report.Components = append(report.Components, toolStatus(domain.ToolSdkmanager, "sdkmanager", sdkmanagerOK, sdkmanagerVersion, tools.Sdkmanager, &domain.ToolFix{
		Kind:  domain.FixPrepare,
		Label: "准备 / 修复 SDK",
	}))
	report.Components = append(report.Components, toolStatus(domain.ToolAvdmanager, "avdmanager", avdmanagerOK, "", tools.Avdmanager, &domain.ToolFix{
		Kind:  domain.FixPrepare,
		Label: "准备 / 修复 SDK",
	}))

	// 可用性沿用原判据（存在 + 包元数据完整）：版本只作展示，
	// 解析失败不代表组件不可用，不能因此把用户引向"重装修复"。
	adbOK := hasAdb
	report.Components = append(report.Components, toolStatus(domain.ToolAdb, "adb (platform-tools)", adbOK, adbVersion, tools.Adb, &domain.ToolFix{
		Kind:    domain.FixInstall,
		Label:   "修复 platform-tools",
		Payload: "platform-tools",
	}))

	emulatorOK := hasEmulator
	report.Components = append(report.Components, toolStatus(domain.ToolEmulator, "emulator", emulatorOK, emulatorVersion, tools.Emulator, &domain.ToolFix{
		Kind:    domain.FixInstall,
		Label:   "修复 emulator",
		Payload: "emulator",
	}))
	if tools.HasSdkmanager() && !cmdlineOK {
		setComponentDetail(report, domain.ToolSdkmanager, "检测到旧版本残留或不完整安装，请执行环境修复")
		setComponentDetail(report, domain.ToolAvdmanager, "检测到旧版本残留或不完整安装，请执行环境修复")
	}
	if tools.HasAdb() && !adbOK {
		setComponentDetail(report, domain.ToolAdb, "目录存在但元数据不完整，可能为旧版本残留，请执行环境修复")
	}
	if tools.HasEmulator() && !emulatorOK {
		setComponentDetail(report, domain.ToolEmulator, "目录存在但元数据不完整，可能为旧版本残留，请执行环境修复")
	}

	// 已安装系统镜像（扫描本地 package.xml/source.properties，不联网，也不受 CLI 退出码影响）
	report.NeedInit = !(cmdlineOK && javaOK)
	if !report.NeedInit {
		if images, err := sdk.ListImages(ctx, tools, env, true, nil); err == nil {
			report.Images = len(images)
		} else {
			s.rt.Log().Warn("env", "读取已安装系统镜像失败: %v", err)
		}
	}

	// 硬件加速（并发探测阶段已完成，仅在模拟器存在时才有结果）
	if tools.HasEmulator() {
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

	report.Ready = javaOK && sdkmanagerOK && avdmanagerOK && adbOK && emulatorOK
	report.Issues = s.buildIssues(report, tools, javaOK)
	report.ElapsedMs = time.Since(started).Milliseconds()
	return report, nil
}

// Prepare 使用已保存的镜像自动准备软件自带环境。
//
// 返回 jobID；进度与日志走统一任务区域。
//
// 本方法不会修改镜像源：镜像源由 MirrorService.SetActiveSource / SetActiveJDKSource 单独保存。
func (s *EnvService) Prepare() (string, error) {
	return s.run(false)
}

// Repair 使用已保存的镜像强制校验并重装核心 SDK 工具链。
func (s *EnvService) Repair() (string, error) {
	return s.run(true)
}

// run 自动准备软件自带环境；repair=true 时会强制重装核心工具链并清理旧版缓存。
func (s *EnvService) run(repair bool) (string, error) {
	comp := s.rt.Components()
	tools := comp.Tools
	source, env := mirrorEnv(s.rt)

	if repair {
		if err := guardNoRunningEmulator(comp); err != nil {
			return "", err
		}
	}

	unlock, ok := s.rt.locks.TryLock("sdk:" + tools.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有环境准备/安装任务正在进行").
			WithHint("请等待当前任务完成，或在底部任务区域取消它")
	}

	title := "准备软件自带环境"
	if repair {
		title = "修复软件自带环境"
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobBootstrap,
		Title: title,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		if err := platform.EnsureLayout(); err != nil {
			return domain.Wrap(domain.CodePermissionDenied, "无法创建软件数据目录", err)
		}

		// 1. 软件自带 JDK：sdkmanager / avdmanager 的前置条件。JDK 使用独立的
		// 镜像选择与内置 SHA-256 校验；env 中的 JAVA_HOME 与 PATH 始终指向软件目录，
		// JDK 缺失时不会回退到系统 JDK。
		jdkSource := activeJDKSource(s.rt)
		if _, javaOK := s.probeJava(ctx, platform.FindJava(), env); !javaOK {
			j.SetPhase("下载并解压软件自带 JDK")
			j.Logf("info", "jdk", "从 JDK 镜像 %s（%s）下载 Eclipse Temurin %s 并安装到 %s",
				jdkSource.Name, jdkSource.BaseURL, jdk.Version(), comp.JdkRoot)
			started := time.Now()
			if err := jdk.InstallFromSource(ctx, jdkSource.ID, comp.JdkRoot, comp.JdkHome, platform.CacheDir(), j.SetBytes); err != nil {
				return err
			}
			if _, ok := s.probeJava(ctx, platform.FindJava(), env); !ok {
				return domain.ErrDetail(domain.CodeProcessFailed,
					"JDK 安装后仍不可用", platform.FindJava())
			}
			j.Logf("info", "jdk", "JDK 就绪（耗时 %s）", time.Since(started).Round(time.Second))
		} else {
			j.Logf("info", "jdk", "已存在 JDK：%s", platform.FindJava())
		}

		j.Logf("info", "mirror", "Android SDK 组件使用镜像源：%s（%s）", source.Name, source.BaseURL)
		if repair {
			j.SetPhase("停止旧工具链进程")
			if tools.HasAdb() {
				_, _ = proc.Run(ctx, tools.Adb, []string{"kill-server"}, proc.Options{Env: env, Timeout: 15 * time.Second})
			}
			j.SetPhase("清理旧版本缓存")
			j.Logf("warn", "sdk", "开始强制修复：将重装命令行工具、platform-tools 与 emulator；保留 licenses、系统镜像和 AVD")
			for _, path := range []string{
				filepath.Join(tools.SdkRoot, ".temp"),
				filepath.Join(tools.SdkRoot, ".knownPackages"),
				filepath.Join(tools.SdkRoot, ".sdk"),
			} {
				if err := os.RemoveAll(path); err != nil {
					return domain.Wrap(domain.CodePermissionDenied, "无法清理旧版本缓存: "+path, err)
				}
			}
		}

		// 2. 命令行工具（sdkmanager / avdmanager 的唯一来源）
		cmdlineOK := tools.HasSdkmanager() && sdk.VerifyPackage(tools, "cmdline-tools;latest") == nil
		if repair || !cmdlineOK {
			j.SetPhase("下载 Android 命令行工具")
			if tools.HasSdkmanager() {
				j.Logf("warn", "sdk", "命令行工具元数据不完整，按旧版本残留修复：%s", tools.CmdlineTools)
			}
			j.Logf("info", "sdk", "从 %s 下载命令行工具到 %s", source.Name, tools.CmdlineTools)
			started := time.Now()
			// 速度由任务内部采样估算，这里只上报字节数
			err := sdk.Bootstrap(ctx, tools, platform.CacheDir(), source.BaseURL, j.SetBytes)
			if err != nil {
				return err
			}
			j.Logf("info", "sdk", "命令行工具就绪（耗时 %s）", time.Since(started).Round(time.Second))
		} else {
			j.Logf("info", "sdk", "已存在命令行工具：%s", tools.Sdkmanager)
		}

		// 3. 许可（用 y 回答官方提示，不自行维护许可文件）
		j.SetPhase("接受 SDK 许可")
		if err := sdk.AcceptLicenses(ctx, tools, env, jobLine(j, "sdkmanager")); err != nil {
			return err
		}

		// 4. 基础组件：platform-tools（adb）与 emulator
		missing := make([]string, 0, 2)
		if repair || sdk.VerifyPackage(tools, "platform-tools") != nil {
			missing = append(missing, "platform-tools")
		}
		if repair || sdk.VerifyPackage(tools, "emulator") != nil {
			missing = append(missing, "emulator")
		}
		if len(missing) > 0 {
			// 复查：自举/许可阶段可能耗时数分钟，用户可能在此期间启动了模拟器。
			// 否则 stagePackages 会重命名正在被 emulator.exe 使用的目录（Windows 上
			// 失败并留下 .repair-old 残留，Unix 上直接让运行中的实例崩溃）。
			if slices.Contains(missing, "emulator") {
				if err := guardNoRunningEmulator(comp); err != nil {
					return err
				}
			}
			j.SetPhase("安装 " + strings.Join(missing, "、"))
			j.Logf("info", "sdk", "执行：%s", sdk.InstallCommand(tools, missing))
			stopProgress := watchInstallProgress(ctx, j, tools.SdkRoot, missing, source.BaseURL)
			var err error
			if repair {
				err = sdk.RepairPackages(ctx, tools, env, missing, jobLine(j, "sdkmanager"))
			} else {
				err = sdk.InstallPackages(ctx, tools, env, missing, jobLine(j, "sdkmanager"))
			}
			stopProgress()
			if err != nil {
				return err
			}
		}
		if repair {
			j.Logf("info", "sdk", "软件自带环境修复完成")
		} else {
			j.Logf("info", "sdk", "软件自带环境准备完成")
		}
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
	if err := wailsruntime.ClipboardSetText(s.rt.Context(), text); err != nil {
		return domain.Wrap(domain.CodeProcessFailed, "写入剪贴板失败", err)
	}
	return nil
}

// ---------------------------------------------------------------- 内部辅助

func (s *EnvService) checkAcceleration(ctx context.Context, tools platform.Tools, env []string) domain.AccelInfo {
	res, err := proc.Run(ctx, tools.Emulator, []string{"-accel-check"}, proc.Options{Env: env, Timeout: emulatorProbe})
	raw := strings.TrimSpace(res.Combined())
	parsed := parseAccelCheck(raw)
	// 命令本身失败（超时、被杀、无法启动）时一律视为不可用。
	if err != nil && res.ExitCode != 0 {
		parsed.Available = false
	}
	return domain.AccelInfo{
		Available: parsed.Available,
		Kind:      parsed.Kind,
		Raw:       raw,
		Hints:     accelHints(runtime.GOOS, parsed.Message, parsed.Available),
	}
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

// activeEmulatorName 返回仍占用 emulator 可执行文件的实例（AVD 名）。
//
// error 态同样算「占用」：等待开机超时会把实例置为 error，但进程可能仍然存活，
// 此时替换 <sdk>/emulator 目录同样会让实例崩溃。
func activeEmulatorName(comp *components) (string, bool) {
	for _, instance := range comp.Launcher.List() {
		switch instance.State {
		case domain.AvdStarting, domain.AvdBooting, domain.AvdRunning, domain.AvdStopping, domain.AvdError:
			return instance.AvdName, true
		}
	}
	return "", false
}

// guardNoRunningEmulator 确认没有实例占用 emulator 可执行文件。
//
// 绑定调用与任务体在执行「替换 emulator 目录」之前都必须调用它：只在前端入口检查
// 存在 TOCTOU 窗口（用户在下载/自举的数分钟里启动模拟器）。
func guardNoRunningEmulator(comp *components) error {
	name, busy := activeEmulatorName(comp)
	if !busy {
		return nil
	}
	return domain.ErrDetail(domain.CodeJobBusy,
		"模拟器 "+name+" 正在运行，不能替换 SDK 工具链", name).
		WithHint("请先停止所有模拟器，再执行环境准备/修复")
}

// buildIssues 汇总可操作的环境问题。
func (s *EnvService) buildIssues(report *domain.EnvReport, tools platform.Tools, javaOK bool) []domain.EnvIssue {
	var issues []domain.EnvIssue

	if !javaOK {
		title := "未找到软件自带 JDK"
		if report.JavaPath != "" {
			title = "软件自带 JDK 不可用"
		}
		issues = append(issues, domain.EnvIssue{
			ID:       "jdk-missing",
			Severity: domain.SeverityBlocker,
			Title:    title,
			Detail: "sdkmanager 与 avdmanager 需要软件自带 JDK 17 或更高版本。将从所选 JDK 镜像下载 Eclipse Temurin 21 到 " +
				report.JdkRoot + " 并进行 SHA-256 校验，不读取系统 JAVA_HOME / PATH。",
			FixKind:  domain.FixPrepare,
			FixLabel: "自动下载 JDK",
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
			if strings.TrimSpace(comp.Detail) != "" {
				detail = comp.Detail
			}
			if comp.Fix.Kind == domain.FixInstall {
				detail += "，可通过当前镜像补装：" + comp.Fix.Payload
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

func setComponentDetail(report *domain.EnvReport, id domain.ToolID, detail string) {
	for i := range report.Components {
		if report.Components[i].ID == id {
			report.Components[i].Detail = detail
			return
		}
	}
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

// probeJava 只探测指定路径的 java，并确认主版本 >= 17。
//
// 路径为空或探测失败时返回 false，绝不去 PATH 中回退查找系统 java。
func (s *EnvService) probeJava(ctx context.Context, javaPath string, env []string) (string, bool) {
	if strings.TrimSpace(javaPath) == "" {
		return "", false
	}
	version := parseJavaVersion(sdk.ToolOutput(ctx, javaPath, []string{"-version"}, env, javaTimeout))
	if version == "" {
		return "", false
	}
	return version, javaMajor(version) >= 17
}

// parseJavaVersion 从 `java -version` 的完整输出中取版本号（形如 17.0.6 或 1.8.0_392）。
//
// 只在包含 version "…" 的行上取号：设置了 JAVA_TOOL_OPTIONS / _JAVA_OPTIONS 时，
// JVM 会先输出 "Picked up JAVA_TOOL_OPTIONS: …"，因此不能只看首行；
// 而"取第一对引号"也会被选项里的引号误导。
func parseJavaVersion(out string) string {
	for _, line := range platform.SplitLines(out) {
		i := strings.Index(line, `version "`)
		if i < 0 {
			continue
		}
		rest := line[i+len(`version "`):]
		if j := strings.Index(rest, `"`); j >= 0 {
			rest = rest[:j]
		}
		if v := normalizeJavaVersion(rest); v != "" {
			return v
		}
	}
	return ""
}

// normalizeJavaVersion 去掉引号内的修饰（1.8.0_392 → 8.0，21.0.12.1+1 → 21.0.12.1）。
func normalizeJavaVersion(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "1.") {
		text = strings.TrimPrefix(text, "1.")
	}
	if i := strings.IndexAny(text, "-+_"); i > 0 {
		text = text[:i]
	}
	return text
}

// javaMajor 取 Java 主版本号（8、11、17、21…）。
func javaMajor(version string) int {
	version = strings.TrimSpace(version)
	n, digits := 0, 0
	for _, r := range version {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		digits++
	}
	if digits == 0 {
		return 0
	}
	return n
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
	return ""
}

func parseSdkmanagerVersion(out string) string {
	for _, line := range platform.SplitLines(out) {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || strings.HasPrefix(lower, "warning:") ||
			strings.HasPrefix(lower, "the 'android'") || strings.HasPrefix(lower, "to learn") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		if strings.Contains(trimmed, "Android CLI") || strings.ContainsAny(fields[0], "0123456789") {
			return fields[0]
		}
	}
	return parseToolVersionLine(out)
}
