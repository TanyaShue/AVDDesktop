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
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
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
}

// Detector 执行环境自检（带短 TTL 缓存）。
type Detector struct {
	mu       sync.Mutex
	cached   *domain.EnvReport
	cachedAt time.Time
	ttl      time.Duration
}

// NewDetector 创建探测器。
func NewDetector() *Detector {
	return &Detector{ttl: 3 * time.Second}
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
	sdkRes := platform.ResolveSdkRoot(opts.SdkRootOverride)
	avdRes := platform.ResolveAvdHome(opts.AvdHomeOverride)
	paths := platform.NewInstallPaths(sdkRes.Path)
	childEnv := platform.ChildEnv(sdkRes.Path, avdRes.Path, opts.InjectEnv)

	javaPath := platform.FindJava(opts.JdkPathOverride)

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
		Host:      hostInfo(),
		ScannedAt: platform.NowMs(),
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

	wg.Add(4)
	go func() { defer wg.Done(); add(checkJDK(ctx, javaPath, childEnv)) }()
	go func() { defer wg.Done(); add(checkCmdlineTools(ctx, paths, childEnv)) }()
	go func() { defer wg.Done(); add(checkPlatformTools(ctx, paths, childEnv)) }()
	go func() { defer wg.Done(); add(checkEmulator(ctx, paths, childEnv, &report.Accel)) }()

	images, imgStatus := checkSystemImages(paths)
	add(imgStatus)

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
		domain.ToolCmdlineTools: 1,
		domain.ToolJDK:          2,
		domain.ToolEmulator:     3,
		domain.ToolSystemImages: 4,
		domain.ToolPlatformTool: 5,
		domain.ToolAvdHome:      6,
		domain.ToolAcceleration: 7,
		domain.ToolDisk:         8,
	}
	var out []domain.ToolStatus
	for _, it := range items {
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
		domain.ToolEmulator, domain.ToolSystemImages, domain.ToolAcceleration,
		domain.ToolDisk, domain.ToolAvdHome,
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

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

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
