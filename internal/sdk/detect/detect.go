// Package detect 是首页环境自检的实现（M1）。
//
// 设计要点（见 ARCHITECTURE.md §6.1）：
//   - 路径解析优先于一切（SDK 根 / AVD 主目录），结果附来源说明
//   - 组件探测并行执行，先渲染骨架屏再逐张填充卡片
//   - "存在文件"不等于"可用"：关键工具要实跑一次（--version / -accel-check）
//   - 加速判定以 `emulator -accel-check` 退出码为权威依据
package detect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sdk/licenses"
	"AVDDesktop/internal/sdk/query"
)

// MinJDKVersion 是 cmdline-tools 12+ 要求的 JDK 版本。
const MinJDKVersion = "17"

// Options 是自检输入。
type Options struct {
	SdkRootOverride string
	JdkPathOverride string
	AvdHomeOverride string
	InjectEnv       bool

	// RunningInstances 由 service 层注入（探测器不感知进程管理器）
	RunningInstances int
	// AvdIssues 是 service 层汇总的 AVD 健康问题（配置损坏、镜像丢失等）
	AvdIssues []string
}

// Detector 执行环境自检（带短 TTL 缓存）。
type Detector struct {
	log logging.Interface

	mu       sync.Mutex
	cached   *domain.EnvReport
	cachedAt time.Time
	ttl      time.Duration
}

// NewDetector 创建探测器。log 为 nil 时使用空日志器。
func NewDetector(log logging.Interface) *Detector {
	return &Detector{ttl: 3 * time.Second, log: logging.Or(log)}
}

// Detect 执行全量自检。
func (d *Detector) Detect(ctx context.Context, opts Options) (*domain.EnvReport, error) {
	d.mu.Lock()
	if d.cached != nil && time.Since(d.cachedAt) < d.ttl {
		rep := d.cached
		d.mu.Unlock()
		return rep, nil
	}
	d.mu.Unlock()

	report := d.scan(ctx, opts)

	d.mu.Lock()
	d.cached = report
	d.cachedAt = time.Now()
	d.mu.Unlock()
	return report, nil
}

// Invalidate 使缓存失效（安装/设置变更后调用）。
func (d *Detector) Invalidate() {
	d.mu.Lock()
	d.cached = nil
	d.mu.Unlock()
}

func (d *Detector) scan(ctx context.Context, opts Options) *domain.EnvReport {
	started := time.Now()
	sdkRes := platform.ResolveSdkRoot(opts.SdkRootOverride)
	avdRes := platform.ResolveAvdHome(opts.AvdHomeOverride)
	paths := platform.NewInstallPaths(sdkRes.Path)
	childEnv := platform.ChildEnv(sdkRes.Path, avdRes.Path, opts.InjectEnv)

	javaPath := platform.FindJava(opts.JdkPathOverride)

	d.log.Info("detect", "开始环境自检：SDK=%s（%s） AVD=%s（%s） JDK=%s",
		sdkRes.Path, sdkRes.Source, avdRes.Path, avdRes.Source, nonEmptyOr(javaPath, "未找到"))

	report := &domain.EnvReport{
		SdkRoot:       sdkRes.Path,
		SdkRootSource: sdkRes.Source,
		JdkPath:       javaPath,
		AvdHome: domain.AvdHomeInfo{
			Path:     avdRes.Path,
			Source:   avdRes.Source,
			Exists:   platform.DirExists(avdRes.Path),
			Writable: platform.IsWritable(avdRes.Path),
			Count:    countAvds(avdRes.Path),
		},
		Host:             hostInfo(),
		ScannedAt:        platform.NowMs(),
		RunningInstances: opts.RunningInstances,
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		items []domain.ToolStatus
	)
	add := func(t domain.ToolStatus) {
		mu.Lock()
		items = append(items, t)
		mu.Unlock()
	}

	// Windows 开关与虚拟化能力：CIM 查询约 3s（内部 10 分钟缓存），放到并行段里跑
	if runtime.GOOS == "windows" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := platform.VirtualizationInfoCached(ctx)
			mu.Lock()
			report.Windows = &domain.WindowsInfo{
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
			report.Host.Windows = strings.TrimSpace(v.Caption + " " + v.Build)
			report.Host.CPUModel = v.CPU
			mu.Unlock()
		}()
	}

	wg.Add(4)
	go func() { defer wg.Done(); add(checkJDK(ctx, javaPath, childEnv)) }()
	go func() { defer wg.Done(); add(checkCmdlineTools(ctx, paths, childEnv)) }()
	go func() { defer wg.Done(); add(checkPlatformTools(ctx, paths, childEnv)) }()
	go func() { defer wg.Done(); add(checkEmulator(ctx, paths, childEnv, &report.Accel)) }()

	images, imgStatus := checkSystemImages(paths)
	add(imgStatus)
	add(checkLicenses(paths))
	add(checkAvdHealth(opts.AvdIssues, report.AvdHome))
	add(checkRunningInstances(opts.RunningInstances))

	disks := []domain.DiskInfo{platform.DiskSpace(sdkRes.Path)}
	if avdRes.Path != "" && !platform.SameVolume(sdkRes.Path, avdRes.Path) {
		disks = append(disks, platform.DiskSpace(avdRes.Path))
	}
	report.Disks = disks
	add(checkDisk(disks))
	add(checkAvdHome(report.AvdHome))

	wg.Wait()

	sort.SliceStable(items, func(i, j int) bool { return orderOf(items[i].ID) < orderOf(items[j].ID) })
	report.Components = items
	report.Blockers = collectBlockers(items, len(images))
	report.Ready = len(report.Blockers) == 0
	report.AcceptedLicenses = licenses.AcceptedIDs(paths.Licenses)
	report.ScanMs = time.Since(started).Milliseconds()

	// 跨组件关联分析：加速引导、镜像与模拟器版本兼容性、长路径、运行中实例
	report.Issues = buildIssues(report, images, opts)

	// 逐个组件写日志：正常项用 debug，异常项用 warn，便于用户按级别排查
	for _, c := range items {
		fields := fmt.Sprintf("state=%s version=%q path=%q", c.State, c.Version, c.Path)
		switch c.State {
		case domain.StatePresent:
			d.log.Debug("detect", "%s：正常（%s）", c.ID, fields)
		case domain.StateMissing:
			d.log.Warn("detect", "%s：未安装（%s）%s", c.ID, fields, c.Detail)
		default:
			d.log.Warn("detect", "%s：%s（%s）%s", c.ID, c.State, fields, c.Detail)
		}
	}
	d.log.Info("detect", "自检完成：ready=%v 阻塞项=%d 问题=%d 组件=%d 耗时=%s",
		report.Ready, len(report.Blockers), len(report.Issues), len(items), time.Since(started).Round(time.Millisecond))
	for _, issue := range report.Issues {
		switch issue.Severity {
		case domain.SeverityBlocker, domain.SeverityWarning:
			d.log.Warn("detect", "问题[%s] %s：%s", issue.Severity, issue.Title, issue.Detail)
		default:
			d.log.Debug("detect", "提示[%s] %s：%s", issue.Severity, issue.Title, issue.Detail)
		}
	}
	return report
}

// ---------------------------------------------------------------- 各组件探测

func checkJDK(ctx context.Context, javaPath string, env []string) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolJDK, Name: "Java 运行环境 (JDK)"}
	if javaPath == "" {
		st.State = domain.StateMissing
		st.Detail = "未找到 java。命令行工具（sdkmanager / avdmanager）需要 JDK " + MinJDKVersion + "+"
		st.Fix = &domain.ToolFix{Kind: domain.FixSetJDK, Label: "指定 JDK"}
		st.Meta = map[string]string{"requirement": "JDK " + MinJDKVersion + "+"}
		return st
	}
	st.Path = javaPath
	res, err := proc.Run(ctx, javaPath, []string{"-version"}, proc.Options{Env: env, Timeout: 20 * time.Second})
	if err != nil && res.Combined() == "" {
		st.State = domain.StateBroken
		st.Detail = "无法执行 java：" + err.Error()
		st.Fix = &domain.ToolFix{Kind: domain.FixSetJDK, Label: "重新指定 JDK"}
		return st
	}
	version := ParseJavaVersion(res.Combined())
	st.Version = version
	switch {
	case version == "":
		st.State = domain.StateBroken
		st.Detail = "无法识别 java 版本"
		st.Fix = &domain.ToolFix{Kind: domain.FixSetJDK, Label: "重新指定 JDK"}
	case !platform.VersionAtLeast(version, MinJDKVersion):
		st.State = domain.StateIncompatible
		st.Detail = "当前 JDK " + version + " 版本过低，命令行工具需要 " + MinJDKVersion + " 及以上"
		st.Fix = &domain.ToolFix{Kind: domain.FixSetJDK, Label: "更换 JDK"}
	default:
		st.State = domain.StatePresent
		st.Detail = "版本满足要求（需要 JDK " + MinJDKVersion + "+）"
	}
	return st
}

func checkCmdlineTools(ctx context.Context, paths platform.InstallPaths, env []string) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolCmdlineTools, Name: "命令行工具 (sdkmanager / avdmanager)"}
	hasSM := platform.FileExists(paths.Sdkmanager)
	hasAVD := platform.FileExists(paths.Avdmanager)
	switch {
	case !hasSM && !hasAVD:
		st.State = domain.StateMissing
		st.Path = paths.CmdlineTools
		st.Detail = "未安装。它是创建与管理 AVD 的必需组件"
		st.Fix = &domain.ToolFix{Kind: domain.FixInstall, Label: "从镜像安装", Payload: "cmdline-tools;latest"}
		return st
	}
	if !hasAVD {
		st.State = domain.StateBroken
		st.Path = paths.CmdlineTools
		st.Detail = "缺少 avdmanager，安装包可能不完整"
		st.Fix = &domain.ToolFix{Kind: domain.FixRepair, Label: "修复安装", Payload: "cmdline-tools;latest"}
		return st
	}

	// 实跑校验：avdmanager 需要 JDK，缺 JDK 时会失败
	res, err := proc.Run(ctx, paths.Avdmanager, []string{"list", "device", "-c"}, proc.Options{
		Env: env, Timeout: 60 * time.Second,
	})
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		st.Path = paths.CmdlineTools
		st.State = domain.StateBroken
		st.Detail = "avdmanager 无法正常运行（通常是缺少或无法使用 JDK）"
		if detail := strings.TrimSpace(res.Combined()); detail != "" {
			st.Meta = map[string]string{"raw": firstLines(detail, 6)}
		}
		st.Fix = &domain.ToolFix{Kind: domain.FixSetJDK, Label: "指定 JDK"}
		return st
	}

	st.State = domain.StatePresent
	st.Path = paths.CmdlineTools
	st.Version = readRevision(paths.CmdlineTools)
	st.Detail = "sdkmanager 与 avdmanager 均可正常调用"
	st.Meta = map[string]string{
		"sdkmanager":  paths.Sdkmanager,
		"avdmanager":  paths.Avdmanager,
		"deviceCount": itoa(len(platform.SplitLines(res.Stdout))),
	}
	return st
}

func checkPlatformTools(ctx context.Context, paths platform.InstallPaths, env []string) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolPlatformTool, Name: "平台工具 (adb)"}
	if !platform.FileExists(paths.Adb) {
		st.State = domain.StateMissing
		st.Path = paths.PlatformTools
		st.Detail = "未安装 adb，无法安装应用/查看设备状态/截图"
		st.Fix = &domain.ToolFix{Kind: domain.FixInstall, Label: "安装 platform-tools", Payload: "platform-tools"}
		return st
	}
	st.Path = paths.Adb
	st.Version = readRevision(paths.PlatformTools)
	res, err := proc.Run(ctx, paths.Adb, []string{"--version"}, proc.Options{Env: env, Timeout: 20 * time.Second})
	if err != nil || res.ExitCode != 0 {
		st.State = domain.StateBroken
		st.Detail = "adb 无法执行"
		st.Fix = &domain.ToolFix{Kind: domain.FixRepair, Label: "重新安装", Payload: "platform-tools"}
		return st
	}
	if v := ParseAdbVersion(res.Stdout); v != "" && st.Version == "" {
		st.Version = v
	}
	st.State = domain.StatePresent
	st.Detail = "adb 可用"
	return st
}

func checkEmulator(ctx context.Context, paths platform.InstallPaths, env []string, accel *domain.AccelInfo) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolEmulator, Name: "模拟器 (emulator)"}
	if !platform.FileExists(paths.EmulatorExe) {
		st.State = domain.StateMissing
		st.Path = paths.Emulator
		st.Detail = "未安装模拟器引擎，无法启动 AVD"
		st.Fix = &domain.ToolFix{Kind: domain.FixInstall, Label: "从镜像安装", Payload: "emulator"}
		if accel != nil {
			*accel = domain.AccelInfo{Kind: "unknown", Hints: []string{"安装模拟器后可检测硬件加速"}}
		}
		return st
	}
	st.Path = paths.EmulatorExe
	st.Version = readRevision(paths.Emulator)
	res, err := proc.Run(ctx, paths.EmulatorExe, []string{"-version"}, proc.Options{Env: env, Timeout: 30 * time.Second})
	if err == nil && res.Combined() != "" {
		if v := ParseEmulatorVersion(res.Combined()); v != "" {
			st.Version = v
		}
	} else {
		st.State = domain.StateBroken
		st.Detail = "emulator 无法执行"
		st.Fix = &domain.ToolFix{Kind: domain.FixRepair, Label: "重新安装", Payload: "emulator"}
		return st
	}

	// 硬件加速（权威判定）
	info := CheckAcceleration(ctx, paths.EmulatorExe, env)
	if accel != nil {
		*accel = info
	}
	st.State = domain.StatePresent
	if info.Available {
		st.Detail = "已安装，硬件加速可用（" + strings.ToUpper(info.Kind) + "）"
	} else {
		st.Detail = "已安装，但缺少可用的硬件加速，启动会很慢"
		st.Fix = &domain.ToolFix{Kind: domain.FixEnableWHPX, Label: "开启加速", Payload: info.Kind}
	}
	st.Meta = map[string]string{"accel": info.Kind, "accelAvailable": boolStr(info.Available)}
	return st
}

func checkSystemImages(paths platform.InstallPaths) ([]domain.SystemImage, domain.ToolStatus) {
	st := domain.ToolStatus{ID: domain.ToolSystemImages, Name: "系统镜像"}
	images := query.NewScanner(paths.SdkRoot).SystemImages()
	if len(images) == 0 {
		st.State = domain.StateMissing
		st.Path = paths.SystemImages
		st.Detail = "还没有任何系统镜像，创建 AVD 前需要至少安装一个"
		st.Fix = &domain.ToolFix{Kind: domain.FixDownloadImage, Label: "下载镜像", Payload: ""}
		return images, st
	}
	st.State = domain.StatePresent
	st.Path = paths.SystemImages
	st.Version = itoa(len(images)) + " 个"
	apis := map[string]bool{}
	playstore := 0
	for _, img := range images {
		apis[img.APILevel] = true
		if img.IsPlaystore {
			playstore++
		}
	}
	keys := make([]string, 0, len(apis))
	for k := range apis {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	st.Detail = "已安装 " + itoa(len(images)) + " 个镜像（API " + strings.Join(keys, ", ") + "）"
	st.Meta = map[string]string{"count": itoa(len(images)), "playstoreCount": itoa(playstore)}
	return images, st
}

func checkDisk(disks []domain.DiskInfo) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolDisk, Name: "磁盘空间"}
	if len(disks) == 0 {
		st.State = domain.StateUnknown
		return st
	}
	st.Path = disks[0].Path
	st.Version = itoa64(disks[0].FreeGB) + " GB 可用"
	if disks[0].Sufficient {
		st.State = domain.StatePresent
		st.Detail = "空间充足（建议保留 30 GB 以上以便安装多个系统镜像）"
	} else {
		st.State = domain.StateIncompatible
		st.Detail = "剩余空间不足 12 GB，安装系统镜像可能失败"
		st.Fix = &domain.ToolFix{Kind: domain.FixChoosePath, Label: "更换目录"}
	}
	st.Meta = map[string]string{"freeGB": itoa64(disks[0].FreeGB), "totalGB": itoa64(disks[0].TotalGB)}
	return st
}

func checkAvdHome(info domain.AvdHomeInfo) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolAvdHome, Name: "AVD 存放目录"}
	st.Path = info.Path
	if info.Path == "" {
		st.State = domain.StateBroken
		st.Detail = "无法解析 AVD 目录"
		return st
	}
	if !info.Writable {
		st.State = domain.StateBroken
		st.Detail = "目录不可写，无法创建 AVD（" + info.Source + "）"
		st.Fix = &domain.ToolFix{Kind: domain.FixChoosePath, Label: "更换目录"}
		return st
	}
	st.State = domain.StatePresent
	st.Detail = "解析来源：" + info.Source
	st.Version = itoa(info.Count) + " 个设备"
	st.Meta = map[string]string{"source": info.Source, "count": itoa(info.Count)}
	return st
}

// ---------------------------------------------------------------- 解析工具

// CheckAcceleration 运行 `emulator -accel-check` 并解析结果。
func CheckAcceleration(ctx context.Context, emulatorExe string, env []string) domain.AccelInfo {
	info := domain.AccelInfo{Kind: "unknown"}
	if emulatorExe == "" || !platform.FileExists(emulatorExe) {
		info.Kind = "none"
		info.Hints = []string{"未安装 emulator，无法检测硬件加速"}
		return info
	}
	res, _ := proc.Run(ctx, emulatorExe, []string{"-accel-check"}, proc.Options{Env: env, Timeout: 40 * time.Second})
	info.Raw = strings.TrimSpace(res.Combined())
	kind, available := ParseAccelCheck(res.ExitCode, info.Raw)
	info.Kind = kind
	info.Available = available
	if !available {
		info.Hints = accelHints(kind)
	}
	return info
}

// ParseAccelCheck 解析 `emulator -accel-check` 的输出。
//
// 实测输出形如：
//
//	accel:
//	0
//	WHPX(10.0.26200) is installed and usable.
//	accel
func ParseAccelCheck(exitCode int, out string) (kind string, available bool) {
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "whpx"):
		kind = "whpx"
	case strings.Contains(lower, "aehd") || strings.Contains(lower, "android emulator hypervisor driver"):
		// 实测 AEHD 的自描述文案是 "Android Emulator hypervisor driver is installed and usable."，
		// 不含 "aehd" 字串，因此必须匹配全称。
		kind = "aehd"
	case strings.Contains(lower, "haxm"):
		kind = "haxm"
	case strings.Contains(lower, "gvm"):
		kind = "gvm"
	case strings.Contains(lower, "kvm"):
		kind = "kvm"
	default:
		kind = "none"
	}
	available = exitCode == 0 && (strings.Contains(lower, "is installed and usable") ||
		strings.Contains(lower, "is usable") || strings.Contains(lower, "ok"))
	if strings.Contains(lower, "not installed") || strings.Contains(lower, "not usable") ||
		strings.Contains(lower, "failed") || strings.Contains(lower, "not found") {
		available = false
	}
	return kind, available
}

// ParseJavaVersion 从 `java -version` 输出解析版本号。
func ParseJavaVersion(out string) string {
	// openjdk version "17.0.6" 2023-01-17 / java version "1.8.0_291"
	re := regexp.MustCompile(`version "([^"]+)"`)
	if m := re.FindStringSubmatch(out); len(m) == 2 {
		v := m[1]
		if strings.HasPrefix(v, "1.") { // 1.8.0_291 → 8.0.291
			parts := strings.SplitN(strings.TrimPrefix(v, "1."), ".", 2)
			major := parts[0]
			minor := "0"
			patch := "0"
			if len(parts) == 2 {
				rest := strings.ReplaceAll(parts[1], "_", ".")
				rest = strings.TrimSuffix(rest, ".0")
				sub := strings.Split(rest, ".")
				if len(sub) > 0 {
					minor = sub[0]
				}
				if len(sub) > 1 {
					patch = sub[1]
				}
			}
			return major + "." + minor + "." + patch
		}
		return strings.Trim(v, ".")
	}
	return ""
}

// ParseEmulatorVersion 从 `emulator -version` 输出解析版本号。
func ParseEmulatorVersion(out string) string {
	re := regexp.MustCompile(`(?i)Android emulator version\s+([0-9][0-9.]*)`)
	if m := re.FindStringSubmatch(out); len(m) == 2 {
		return strings.TrimSuffix(m[1], ".")
	}
	return ""
}

// ParseAdbVersion 从 `adb --version` 输出解析版本号。
//
// 实测输出（注意第一行的 1.0.41 是协议版本，不是 adb 版本）：
//
//	Android Debug Bridge version 1.0.41
//	Version 36.0.2-12345678
//	Installed as C:\...\adb.exe
func ParseAdbVersion(out string) string {
	reLine := regexp.MustCompile(`(?m)^\s*Version\s+([0-9][0-9.]*)`)
	if m := reLine.FindStringSubmatch(out); len(m) == 2 {
		return m[1]
	}
	re := regexp.MustCompile(`(?i)version\s+([0-9][0-9.]*)`)
	if m := re.FindStringSubmatch(out); len(m) == 2 {
		return m[1]
	}
	return ""
}

func accelHints(kind string) []string {
	switch kind {
	case "aehd":
		return []string{
			"检测到 AEHD 驱动但当前不可用：常见原因是同时启用了 Hyper-V",
			"AEHD 官方将于 2026-12-31 停止支持，推荐改用 WHPX",
		}
	case "whpx":
		return []string{
			"WHPX 已安装但不可用：请在「启用或关闭 Windows 功能」中确认已勾选「Windows 虚拟机监控程序平台」",
			"启用后需要重启电脑",
		}
	default:
		return []string{
			"未检测到可用的硬件加速",
			"推荐：开启 Windows 功能「虚拟机监控程序平台（HypervisorPlatform）」与「虚拟机平台（VirtualMachinePlatform）」，重启后生效",
			"备选：安装 AEHD 驱动（需管理员权限，且不能与 Hyper-V 同时启用）",
			"如果没有加速，模拟器仍可启动但会非常慢",
		}
	}
}

// ---------------------------------------------------------------- 其它工具函数

func readRevision(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "source.properties"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Pkg.Revision=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Pkg.Revision="))
		}
	}
	return ""
}

func countAvds(avdHome string) int {
	entries, err := os.ReadDir(avdHome)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".ini") {
			n++
		}
	}
	return n
}

func hostInfo() domain.HostInfo {
	return domain.HostInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUCores: runtime.NumCPU(),
	}
}

func collectBlockers(items []domain.ToolStatus, imageCount int) []domain.ToolStatus {
	prio := map[domain.ToolID]int{
		domain.ToolCmdlineTools:     1,
		domain.ToolJDK:              2,
		domain.ToolEmulator:         3,
		domain.ToolSystemImages:     4,
		domain.ToolPlatformTool:     5,
		domain.ToolAvdHome:          6,
		domain.ToolLicenses:         7,
		domain.ToolAcceleration:     8,
		domain.ToolDisk:             9,
		domain.ToolAvdHealth:        10,
		domain.ToolRunningInstances: 11,
	}
	var out []domain.ToolStatus
	for _, it := range items {
		// 许可缺失不算阻塞（用户可能不使用 sdkmanager），但会在问题清单里提醒
		if it.ID == domain.ToolLicenses {
			continue
		}
		switch it.State {
		case domain.StateMissing, domain.StateBroken, domain.StateIncompatible:
			out = append(out, it)
		}
	}
	if imageCount == 0 {
		// 系统镜像缺失已在组件里体现，这里只保证排序稳定
		_ = imageCount
	}
	sort.SliceStable(out, func(i, j int) bool { return prio[out[i].ID] < prio[out[j].ID] })
	return out
}

func orderOf(id domain.ToolID) int {
	order := []domain.ToolID{
		domain.ToolJDK, domain.ToolCmdlineTools, domain.ToolPlatformTool,
		domain.ToolEmulator, domain.ToolSystemImages, domain.ToolLicenses,
		domain.ToolAcceleration, domain.ToolDisk, domain.ToolAvdHome,
		domain.ToolAvdHealth, domain.ToolRunningInstances,
	}
	for i, v := range order {
		if v == id {
			return i
		}
	}
	return len(order)
}

func firstLines(s string, n int) string {
	lines := platform.SplitLines(s)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// checkLicenses 检查 SDK 许可是否已接受（sdkmanager/avdmanager 需要它才能工作）。
func checkLicenses(paths platform.InstallPaths) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolLicenses, Name: "SDK 许可协议"}
	accepted := licenses.AcceptedIDs(paths.Licenses)
	st.Path = paths.Licenses
	if len(accepted) == 0 {
		st.State = domain.StateMissing
		st.Detail = "尚未接受任何 SDK 许可协议，sdkmanager 与部分安装流程会失败"
		st.Fix = &domain.ToolFix{Kind: domain.FixInstall, Label: "查看并接受许可"}
		return st
	}
	st.State = domain.StatePresent
	st.Version = itoa(len(accepted)) + " 项"
	st.Detail = "已接受：" + strings.Join(accepted, "、")
	st.Meta = map[string]string{"count": itoa(len(accepted))}
	return st
}

// checkAvdHealth 汇总由 service 层传入的 AVD 健康问题。
func checkAvdHealth(issues []string, home domain.AvdHomeInfo) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolAvdHealth, Name: "设备配置健康度"}
	st.Path = home.Path
	if home.Count == 0 {
		st.State = domain.StatePresent
		st.Version = "0 个设备"
		st.Detail = "还没有创建设备，创建后会自动校验配置"
		return st
	}
	if len(issues) == 0 {
		st.State = domain.StatePresent
		st.Version = itoa(home.Count) + " 个设备均正常"
		st.Detail = "所有设备的配置与系统镜像均可用"
		return st
	}
	st.State = domain.StateBroken
	st.Version = itoa(len(issues)) + " 个问题"
	st.Detail = strings.Join(limitStrings(issues, 5), "；")
	st.Fix = &domain.ToolFix{Kind: domain.FixChoosePath, Label: "去设备页处理"}
	st.Meta = map[string]string{"count": itoa(len(issues))}
	return st
}

// checkRunningInstances 报告当前运行中的模拟器实例数量。
func checkRunningInstances(count int) domain.ToolStatus {
	st := domain.ToolStatus{ID: domain.ToolRunningInstances, Name: "运行中的模拟器"}
	if count == 0 {
		st.State = domain.StatePresent
		st.Version = "0 个"
		st.Detail = "当前没有运行中的实例"
		return st
	}
	st.State = domain.StatePresent
	st.Version = itoa(count) + " 个"
	st.Detail = "有实例正在运行：更新组件与修改设备配置会受限，请先停止它们"
	st.Meta = map[string]string{"count": itoa(count)}
	return st
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// nonEmptyOr 返回非空值或替代文本（仅供日志可读性使用）。
func nonEmptyOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func limitStrings(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	out := append([]string(nil), values[:n]...)
	return append(out, fmt.Sprintf("等 %d 项", len(values)))
}

// buildIssues 做跨组件关联分析，输出带修复命令的可操作问题清单。
func buildIssues(report *domain.EnvReport, images []domain.SystemImage, opts Options) []domain.EnvIssue {
	var issues []domain.EnvIssue
	find := func(id domain.ToolID) (domain.ToolStatus, bool) {
		for _, c := range report.Components {
			if c.ID == id {
				return c, true
			}
		}
		return domain.ToolStatus{}, false
	}

	// ① 硬件加速：区分“未开启”与“已开启但不可用”
	if !report.Accel.Available {
		issue := domain.EnvIssue{
			ID:       "accel-unavailable",
			Severity: domain.SeverityWarning,
			Title:    "未检测到可用的硬件加速",
			Detail:   "模拟器仍可启动，但速度会明显变慢。",
			DocsURL:  "https://developer.android.com/studio/run/emulator-acceleration",
		}
		win := report.Windows
		switch {
		case win == nil || !win.Available:
			issue.Detail += "无法读取 Windows 开关状态，请手动确认已启用「Windows 虚拟机监控程序平台」。"
			issue.FixLabel = "复制开启命令"
			issue.FixCommand = hypervisorPlatformCommand
		case win.HypervisorPresent:
			issue.Detail += "机器已有 hypervisor 在运行（Hyper-V/WHPX/VBS），但模拟器未能使用它。" +
				"常见原因是「Windows 虚拟机监控程序平台」功能未勾选，或同时安装了 AEHD 驱动造成冲突。"
			issue.FixLabel = "复制开启命令"
			issue.FixCommand = hypervisorPlatformCommand
		case win.VirtFirmware || win.VMMonitor:
			issue.Detail += "CPU 已支持虚拟化，但未启用任何 hypervisor。建议开启 Windows 功能或安装 AEHD 驱动。"
			issue.FixLabel = "复制开启命令"
			issue.FixCommand = hypervisorPlatformCommand
		default:
			issue.Detail += "主板 BIOS/UEFI 中似乎未开启 CPU 虚拟化（VT-x/AMD-V），需要先在固件设置里打开。"
			issue.FixLabel = "查看官方说明"
		}
		issues = append(issues, issue)
	}

	// ② 系统镜像与模拟器版本不兼容
	if emu, ok := find(domain.ToolEmulator); ok && emu.State == domain.StatePresent {
		for _, img := range images {
			if img.RequiresEmulator == "" || platform.VersionAtLeast(emu.Version, img.RequiresEmulator) {
				continue
			}
			issues = append(issues, domain.EnvIssue{
				ID:       "image-requires-newer-emulator:" + img.Path,
				Severity: domain.SeverityWarning,
				Title:    fmt.Sprintf("镜像 Android %s 需要更新的模拟器", img.APILevel),
				Detail: fmt.Sprintf("该镜像要求 emulator ≥ %s，当前为 %s。升级模拟器后再使用该镜像。",
					img.RequiresEmulator, emu.Version),
				FixLabel:   "升级模拟器",
				FixKind:    domain.FixUpdate,
				FixPayload: "emulator",
			})
			break
		}
	}

	// ③ 长路径支持（Android 系统镜像目录很容易超过 260 字符）
	if report.Windows != nil && report.Windows.Available && !report.Windows.LongPathsEnabled {
		issues = append(issues, domain.EnvIssue{
			ID:       "long-paths-disabled",
			Severity: domain.SeverityInfo,
			Title:    "未开启 Windows 长路径支持",
			Detail: "SDK 目录层级较深时，解压系统镜像可能因 260 字符限制失败。" +
				"如果安装过程中出现路径过长错误，可用下面的命令开启（需管理员 + 重启）。",
			FixLabel:   "复制开启命令",
			FixCommand: longPathsCommand,
		})
	}

	// ④ 运行中实例影响更新
	if opts.RunningInstances > 0 {
		if _, ok := find(domain.ToolEmulator); ok {
			issues = append(issues, domain.EnvIssue{
				ID:       "instances-running",
				Severity: domain.SeverityInfo,
				Title:    fmt.Sprintf("有 %d 个模拟器实例正在运行", opts.RunningInstances),
				Detail:   "在实例运行时无法更新 emulator（文件被占用），也无法修改设备配置。",
				FixLabel: "去设备页停止",
			})
		}
	}

	// ⑤ 没有系统镜像 → 无法创建 AVD
	if imgStatus, ok := find(domain.ToolSystemImages); ok && imgStatus.State == domain.StateMissing {
		issues = append(issues, domain.EnvIssue{
			ID:       "no-system-image",
			Severity: domain.SeverityBlocker,
			Title:    "还没有安装任何系统镜像",
			Detail:   "创建 AVD 需要至少一个系统镜像（推荐 Google APIs，约 1.5-2 GB）。",
			FixLabel: "去下载镜像",
			FixKind:  domain.FixDownloadImage,
		})
	}

	// ⑥ AVD 配置健康问题（由 service 层传入）
	for i, msg := range limitStrings(opts.AvdIssues, 5) {
		issues = append(issues, domain.EnvIssue{
			ID:       fmt.Sprintf("avd-issue-%d", i),
			Severity: domain.SeverityWarning,
			Title:    "设备配置存在问题",
			Detail:   msg,
			FixLabel: "去设备页处理",
		})
	}

	return issues
}

const (
	hypervisorPlatformCommand = `dism /online /enable-feature /featurename:HypervisorPlatform /all /norestart`
	longPathsCommand          = `reg add "HKLM\SYSTEM\CurrentControlSet\Control\FileSystem" /v LongPathsEnabled /t REG_DWORD /d 1 /f`
)

func itoa(v int) string { return itoa64(int64(v)) }

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
