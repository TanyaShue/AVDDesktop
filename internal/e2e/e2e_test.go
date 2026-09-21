//go:build e2e

// Package e2e 是端到端回归测试套件。
//
// 运行方式（必须显式开启，避免常规 CI 拉取 2GB 镜像）：
//
//	go test -tags e2e ./internal/e2e/ -v -timeout 60m
//
// 环境变量开关：
//
//	AVDDESKTOP_E2E_HEAVY=1   启用下载安装测试（会从镜像下载约 160MB）
//	AVDDESKTOP_E2E_BOOT=1    启用模拟器启动测试（需要本机已装系统镜像，会真的开机）
//	AVDDESKTOP_E2E_SOURCE    指定镜像源 base URL（默认腾讯云）
//
// 设计原则：
//   - 所有测试都使用临时目录，绝不改动用户真实的 SDK / AVD 目录
//   - 通过 service 层调用（走真实的 Job、事件、锁与领域实现），而不是绕过抽象直连
//   - 需要外部资源（网络、镜像、JDK）的用例用 require* 辅助函数显式跳过并说明原因
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"AVDDesktop/internal/avd/store"
	"AVDDesktop/internal/config"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/mirror"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sdk/install"
	"AVDDesktop/internal/sdk/licenses"
	"AVDDesktop/internal/sdk/query"
	"AVDDesktop/internal/service"
)

const (
	defaultSource = "https://mirrors.cloud.tencent.com/AndroidSDK/"
	testAppName   = "AVDDesktopE2E"
	bootTimeout   = 8 * time.Minute
)

// env 是测试环境快照。
type env struct {
	ctx        context.Context
	sdkRoot    string
	avdHome    string
	tmp        string
	hasSDK     bool
	hasJDK     bool
	hasImage   bool
	imagePath  string
	jdkPath    string
	source     domain.MirrorSource
	hasNetwork bool
}

// setup 构造测试环境：解析真实 SDK/JDK（只读使用），并把 AVD 目录指向临时目录。
func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()

	sdkRes := platform.ResolveSdkRoot("")
	jdk := platform.FindJava("")
	e := &env{
		ctx:     ctx,
		sdkRoot: sdkRes.Path,
		tmp:     t.TempDir(),
		jdkPath: jdk,
		hasJDK:  jdk != "",
		source: domain.MirrorSource{
			ID: "e2e-source", Name: "E2E 镜像源", BaseURL: sourceURL(),
			Kind: domain.MirrorMirror,
		},
	}
	e.avdHome = filepath.Join(e.tmp, "avd")
	if err := platform.EnsureDir(e.avdHome); err != nil {
		t.Fatalf("无法创建临时 AVD 目录: %v", err)
	}

	// SDK 目录必须存在 cmdline-tools 才算“有 SDK”
	e.hasSDK = platform.FileExists(platform.NewInstallPaths(e.sdkRoot).Avdmanager)
	if e.hasSDK {
		images := query.NewScanner(e.sdkRoot).SystemImages()
		if len(images) > 0 {
			// 优先选非 Play 镜像（可 root，便于 adb 验证）
			for _, img := range images {
				if !img.IsPlaystore {
					e.imagePath, e.hasImage = img.Path, true
					break
				}
			}
			if !e.hasImage {
				e.imagePath, e.hasImage = images[0].Path, true
			}
		}
	}

	// 一次轻量网络探测：决定后续网络用例是否跳过
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	engine := mirror.NewEngine(t.TempDir(), logging.Nop())
	res := engine.Test(probeCtx, e.source, mirror.TestOptions{SkipThroughput: true}.WithDefaults())
	e.hasNetwork = res.XMLOK
	if !e.hasNetwork {
		t.Logf("镜像源不可用（%s），网络相关用例将被跳过：%s", e.source.BaseURL, res.Error)
	}

	t.Logf("E2E 环境：SDK=%s（可用=%v）JDK=%s（可用=%v）系统镜像=%s（可用=%v）网络=%v",
		e.sdkRoot, e.hasSDK, e.jdkPath, e.hasJDK, e.imagePath, e.hasImage, e.hasNetwork)
	return e
}

func sourceURL() string {
	if v := strings.TrimSpace(os.Getenv("AVDDESKTOP_E2E_SOURCE")); v != "" {
		return v
	}
	return defaultSource
}

func (e *env) requireNetwork(t *testing.T) {
	t.Helper()
	if !e.hasNetwork {
		t.Skip("镜像源不可达，跳过网络用例（可用 AVDDESKTOP_E2E_SOURCE 指定其它源）")
	}
}

func (e *env) requireSDK(t *testing.T) {
	t.Helper()
	if !e.hasSDK {
		t.Skip("本机没有可用的 Android SDK（缺 cmdline-tools），跳过")
	}
}

func (e *env) requireImage(t *testing.T) {
	t.Helper()
	if !e.hasImage {
		t.Skip("本机没有已安装的系统镜像，跳过（可用 AVDDESKTOP_E2E_HEAVY=1 先下载一个）")
	}
}

func (e *env) requireJDK(t *testing.T) {
	t.Helper()
	if !e.hasJDK {
		t.Skip("本机没有可用的 JDK，跳过需要 avdmanager 的用例")
	}
}

func requireHeavy(t *testing.T) {
	t.Helper()
	if os.Getenv("AVDDESKTOP_E2E_HEAVY") == "" {
		t.Skip("需要 AVDDESKTOP_E2E_HEAVY=1 才会执行下载安装用例")
	}
}

func requireBoot(t *testing.T) {
	t.Helper()
	if os.Getenv("AVDDESKTOP_E2E_BOOT") == "" {
		t.Skip("需要 AVDDESKTOP_E2E_BOOT=1 才会执行模拟器启动用例")
	}
}

// newRuntime 构造一个隔离的 Runtime（临时设置文件 + 临时 AVD 目录 + 真实 SDK）。
func (e *env) newRuntime(t *testing.T) *service.Runtime {
	t.Helper()
	settings := config.NewManager(filepath.Join(e.tmp, "settings.json"))
	if err := settings.Load(); err != nil {
		t.Fatalf("加载临时设置失败: %v", err)
	}
	if _, err := settings.Update(map[string]any{
		"sdkRoot":            e.sdkRoot,
		"avdHome":            e.avdHome,
		"activeSourceId":     e.source.ID,
		"autoAcceptLicenses": true,
		"logLevel":           "debug",
	}); err != nil {
		t.Fatalf("写入临时设置失败: %v", err)
	}
	if _, err := settings.AddCustomSource(e.source); err != nil {
		t.Fatalf("写入镜像源失败: %v", err)
	}
	rt := service.NewRuntime(testAppName, "e2e", settings, filepath.Join(e.tmp, "cache"), logging.Discard())
	t.Cleanup(rt.Shutdown)
	return rt
}

// waitJob 等待任务结束并返回最终快照。
func waitJob(t *testing.T, rt *service.Runtime, id string, timeout time.Duration) domain.JobInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, ok := rt.Jobs().Get(id)
		if !ok {
			t.Fatalf("任务不存在: %s", id)
		}
		info := job.Info()
		switch info.Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCanceled:
			return info
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("任务 %s 超过 %s 未结束", id, timeout)
	return domain.JobInfo{}
}

// ---------------------------------------------------------------- 1. 环境自检

func TestE2E_EnvironmentDetect(t *testing.T) {
	e := setup(t)
	rt := e.newRuntime(t)
	envSvc := service.NewEnvService(rt)

	report, err := envSvc.Detect(service.DetectRequest{Force: true})
	if err != nil {
		t.Fatalf("自检失败: %v", err)
	}
	if report.SdkRoot == "" {
		t.Error("SDK 根目录为空")
	}
	if report.AvdHome.Path != e.avdHome {
		t.Errorf("AVD 目录未按设置生效：got=%s want=%s", report.AvdHome.Path, e.avdHome)
	}
	if len(report.Components) < 8 {
		t.Errorf("组件数量偏少：%d", len(report.Components))
	}
	for _, c := range report.Components {
		t.Logf("  [%-17s] %-8s %-12s %s", c.ID, c.State, c.Version, c.Name)
	}
	for _, issue := range report.Issues {
		t.Logf("  问题[%s] %s（修复：%s %s）", issue.Severity, issue.Title, issue.FixLabel, issue.FixCommand)
	}
	if report.ScanMs <= 0 {
		t.Error("未记录扫描耗时")
	}
	t.Logf("自检完成：ready=%v 阻塞=%d 问题=%d 耗时=%dms", report.Ready, len(report.Blockers), len(report.Issues), report.ScanMs)

	// 二次调用必须命中缓存（TTL 内不应重复扫描）
	start := time.Now()
	if _, err := envSvc.Detect(service.DetectRequest{}); err != nil {
		t.Fatalf("二次自检失败: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("二次自检未命中缓存，耗时 %s", elapsed)
	}
}

// ---------------------------------------------------------------- 2. 仓库索引与安装计划

func TestE2E_RepoIndexAndPlan(t *testing.T) {
	e := setup(t)
	e.requireNetwork(t)

	installer := install.New(platform.NewInstallPaths(filepath.Join(e.tmp, "sdk")), filepath.Join(e.tmp, "cache"), logging.Nop())
	idx, err := installer.ResolveIndex(e.ctx, e.source, true)
	if err != nil {
		t.Fatalf("解析索引失败: %v", err)
	}
	if len(idx.Packages) < 100 {
		t.Errorf("索引包数量异常：%d", len(idx.Packages))
	}
	for _, want := range []string{"cmdline-tools;latest", "platform-tools", "emulator"} {
		pkg, ok := idx.Find(want)
		if !ok {
			t.Fatalf("索引缺少关键包 %s", want)
		}
		archive, err := pkg.PickArchive("windows", "amd64")
		if err != nil {
			t.Fatalf("%s 选择归档失败: %v", want, err)
		}
		url := idx.PackageURL(archive)
		if !strings.HasPrefix(url, "http") || archive.Size <= 0 || archive.SHA1 == "" {
			t.Errorf("%s 归档信息不完整：url=%s size=%d sha1=%s", want, url, archive.Size, archive.SHA1)
		}
		t.Logf("  %-22s rev=%-10s size=%-12s %s", want, pkg.Revision, platform.HumanSize(archive.Size), url)
		if ref := pkg.LicenseID(); ref != "" && !licenses.IsKnown(ref) {
			t.Errorf("%s 声明了未知许可 %s", want, ref)
		}
	}

	// 系统镜像索引（验证相对 URL 基准 = 索引所在目录）
	sysIdx, err := installer.ResolveSysImgIndex(e.ctx, e.source, "google_apis", false)
	if err != nil {
		t.Fatalf("解析系统镜像索引失败: %v", err)
	}
	if len(sysIdx.Packages) == 0 {
		t.Fatal("系统镜像索引为空")
	}
	var checked bool
	for _, pkg := range sysIdx.Packages {
		archive, aErr := pkg.PickArchive("windows", "amd64")
		if aErr != nil {
			continue
		}
		url := sysIdx.PackageURL(archive)
		if !strings.Contains(url, "/sys-img/google_apis/") {
			t.Errorf("系统镜像 URL 基准错误：%s", url)
		}
		t.Logf("  系统镜像样例：%s → %s（%s）", pkg.Path, filepath.Base(url), platform.HumanSize(archive.Size))
		checked = true
		break
	}
	if !checked {
		t.Error("未能从系统镜像索引中选出可用归档")
	}

	// 安装计划
	plan, err := installer.Plan(e.ctx, e.source, []string{"cmdline-tools;latest", "platform-tools", "emulator"})
	if err != nil {
		t.Fatalf("生成安装计划失败: %v", err)
	}
	if len(plan.Steps) == 0 {
		t.Fatal("安装计划为空")
	}
	if plan.TotalBytes <= 0 {
		t.Error("安装计划总大小应大于 0")
	}
	if len(plan.Licenses) == 0 {
		t.Error("安装计划应包含需要接受的许可")
	}
	for _, step := range plan.Steps {
		t.Logf("  计划：%s %s %s（%s）", step.Path, step.Action, platform.HumanSize(step.SizeBytes), step.Reason)
	}
}

// ---------------------------------------------------------------- 3. 镜像测速

func TestE2E_SpeedTest(t *testing.T) {
	e := setup(t)
	e.requireNetwork(t)

	rt := e.newRuntime(t)
	mirrorSvc := service.NewMirrorService(rt)

	jobID, err := mirrorSvc.TestSources(service.TestRequest{Quick: true})
	if err != nil {
		t.Fatalf("启动测速失败: %v", err)
	}
	info := waitJob(t, rt, jobID, 3*time.Minute)
	if info.Status != domain.JobSucceeded {
		t.Fatalf("测速任务失败：%s %v", info.Status, info.Error)
	}

	results := mirrorSvc.GetCachedResults()
	if len(results) == 0 {
		t.Fatal("没有测速结果")
	}
	usable := 0
	for _, r := range results {
		t.Logf("  %-16s 等级=%-12s 评分=%-3d TTFB=%-6dms Range=%-5v 错误=%s",
			r.SourceID, r.Grade, r.Score, r.TTFBMs, r.RangeSupported, r.Error)
		if r.XMLOK {
			usable++
		}
	}
	if usable == 0 {
		t.Error("至少应有一个镜像源的索引可用")
	}

	// 自定义源增删
	src, err := mirrorSvc.AddSource(service.AddSourceRequest{Name: "E2E 自定义源", BaseURL: e.source.BaseURL, Probe: true})
	if err != nil {
		t.Fatalf("添加自定义源失败: %v", err)
	}
	if src.BaseURL != e.source.BaseURL {
		t.Errorf("镜像地址未规范化：%s", src.BaseURL)
	}
	if err := mirrorSvc.RemoveSource(src.ID); err != nil {
		t.Fatalf("删除自定义源失败: %v", err)
	}
	if err := mirrorSvc.SetActiveSource(e.source.ID); err != nil {
		t.Fatalf("切换镜像源失败: %v", err)
	}
}

// ---------------------------------------------------------------- 4. 下载 / 解压 / 校验（重）

func TestE2E_InstallPlatformTools(t *testing.T) {
	requireHeavy(t)
	e := setup(t)
	e.requireNetwork(t)

	targetSdk := filepath.Join(e.tmp, "sdk")
	paths := platform.NewInstallPaths(targetSdk)
	installer := install.New(paths, filepath.Join(e.tmp, "cache"), logging.Nop())

	ctx, cancel := context.WithTimeout(e.ctx, 15*time.Minute)
	defer cancel()
	result, err := installer.Install(ctx, e.source, []string{"platform-tools"}, install.Options{
		AutoAcceptLicenses:      true,
		AllowFallbackToOfficial: true,
		Concurrency:             4,
		DownloadDir:             filepath.Join(e.tmp, "downloads"),
	}, nil)
	if err != nil {
		t.Fatalf("安装 platform-tools 失败: %v", err)
	}
	if len(result.Installed) != 1 {
		t.Fatalf("期望安装 1 个包，实际 %+v", result)
	}
	if !platform.FileExists(paths.Adb) {
		t.Fatalf("adb 未出现在预期位置: %s", paths.Adb)
	}
	if !platform.FileExists(filepath.Join(paths.PlatformTools, "source.properties")) {
		t.Error("缺少 source.properties")
	}
	if !licenses.IsAccepted(paths.Licenses, "android-sdk-license") {
		t.Error("许可文件未写入")
	}

	// 本地扫描应能识别
	installed := query.NewScanner(targetSdk).Installed()
	if len(installed) == 0 || installed[0].Path != "platform-tools" {
		t.Errorf("本地扫描未识别到 platform-tools：%+v", installed)
	}
	t.Logf("platform-tools 安装成功：version=%s size=%s", installed[0].InstalledRevision, platform.HumanSize(installed[0].SizeBytes))

	// 幂等：再次安装应跳过
	again, err := installer.Install(ctx, e.source, []string{"platform-tools"}, install.Options{
		AutoAcceptLicenses: true, Concurrency: 2, DownloadDir: filepath.Join(e.tmp, "downloads"),
	}, nil)
	if err != nil {
		t.Fatalf("重复安装报错: %v", err)
	}
	if len(again.Skipped) != 1 {
		t.Errorf("重复安装应跳过，实际 %+v", again)
	}
}

// TestE2E_BootstrapCmdlineTools 验证“零基础一键安装命令行工具”（重）。
func TestE2E_BootstrapCmdlineTools(t *testing.T) {
	requireHeavy(t)
	e := setup(t)
	e.requireNetwork(t)
	e.requireJDK(t)

	targetSdk := filepath.Join(e.tmp, "sdk")
	paths := platform.NewInstallPaths(targetSdk)
	installer := install.New(paths, filepath.Join(e.tmp, "cache"), logging.Nop())

	ctx, cancel := context.WithTimeout(e.ctx, 20*time.Minute)
	defer cancel()
	res, err := installer.Bootstrap(ctx, e.source, true, false, install.Options{
		AutoAcceptLicenses:      true,
		AllowFallbackToOfficial: true,
		Concurrency:             4,
		DownloadDir:             filepath.Join(e.tmp, "downloads"),
	}, nil)
	if err != nil {
		t.Fatalf("bootstrap 失败: %v", err)
	}
	t.Logf("已安装：%v", res.Installed)

	for _, tool := range []string{paths.Sdkmanager, paths.Avdmanager} {
		if !platform.FileExists(tool) {
			t.Fatalf("缺少工具: %s", tool)
		}
	}
	if !platform.FileExists(filepath.Join(paths.CmdlineTools, "source.properties")) {
		t.Error("cmdline-tools 缺少 source.properties（解压层级可能不对）")
	}

	// 真的执行一次 sdkmanager（验证解压结构与 JDK 依赖都正确）
	out := runTool(t, e, paths.Sdkmanager, "--version")
	if !strings.Contains(strings.ToLower(out), "version") && !strings.Contains(out, ".") {
		t.Errorf("sdkmanager --version 输出异常：%s", truncate(out, 200))
	}
	// avdmanager 能否列出设备档案
	devices := runTool(t, e, paths.Avdmanager, "list", "device", "-c")
	lines := platform.SplitLines(devices)
	if len(lines) < 10 {
		t.Errorf("avdmanager 列出的设备档案过少：%d", len(lines))
	}
	t.Logf("bootstrap 校验通过：sdkmanager 与 avdmanager 均可运行（设备档案 %d 项）", len(lines))
}

// ---------------------------------------------------------------- 5. AVD 生命周期

func TestE2E_AvdLifecycle(t *testing.T) {
	e := setup(t)
	e.requireSDK(t)
	e.requireImage(t)

	rt := e.newRuntime(t)
	avdSvc := service.NewAvdService(rt)

	// 档案列表（优先使用 avdmanager，缺 JDK 时回退内置）
	profiles, err := avdSvc.ListProfiles(false)
	if err != nil {
		t.Fatalf("读取设备档案失败: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("设备档案为空")
	}
	t.Logf("设备档案 %d 项，示例：%s / %s", len(profiles), profiles[0].ID, profiles[0].Name)

	// schema 由服务层提供
	schema := avdSvc.ListConfigSchema()
	if len(schema) < 50 {
		t.Errorf("硬件配置 schema 项数偏少：%d", len(schema))
	}

	name := "E2E_Device"
	spec := domain.AvdSpec{
		Name:            name,
		DisplayName:     "E2E 测试设备",
		ProfileID:       pickProfile(profiles),
		SystemImagePath: e.imagePath,
		SDCardSize:      "256M",
		HW: map[string]string{
			"hw.ramSize":   "1536",
			"hw.cpu.ncore": "2",
			"hw.keyboard":  "yes",
		},
		CreateWithAvdManager: false, // 直写后端：不依赖 JDK，验证降级路径
		Tags:                 []string{"e2e"},
		Note:                 "由端到端测试创建",
	}

	// 名称校验
	if v, err := avdSvc.ValidateName(name); err != nil || !v.Valid {
		t.Fatalf("名称应合法：%+v %v", v, err)
	}
	if v, _ := avdSvc.ValidateName("bad name!"); v.Valid {
		t.Error("非法名称未被拒绝")
	}

	// 创建
	jobID, err := avdSvc.Create(spec)
	if err != nil {
		t.Fatalf("创建 AVD 失败: %v", err)
	}
	info := waitJob(t, rt, jobID, 3*time.Minute)
	if info.Status != domain.JobSucceeded {
		t.Fatalf("创建任务失败：%s %v", info.Status, info.Error)
	}

	// 校验磁盘结构
	layout := store.New(e.avdHome).Resolve(name)
	for _, path := range []string{layout.IniPath, layout.ConfigIni, layout.MetaPath} {
		if !platform.FileExists(path) {
			t.Fatalf("缺少文件: %s", path)
		}
	}

	// 读回配置
	detail, err := avdSvc.Get(name)
	if err != nil {
		t.Fatalf("读取详情失败: %v", err)
	}
	if detail.Config["hw.ramSize"] != "1536" || detail.Config["hw.cpu.ncore"] != "2" {
		t.Errorf("硬件覆盖未写入：ram=%s cores=%s", detail.Config["hw.ramSize"], detail.Config["hw.cpu.ncore"])
	}
	if detail.Summary.RAMMB != 1536 {
		t.Errorf("摘要内存解析错误：%d", detail.Summary.RAMMB)
	}
	if detail.Summary.APILevel == "" || detail.Summary.ABI == "" {
		t.Errorf("摘要缺少系统信息：%+v", detail.Summary)
	}
	if detail.Summary.Broken != "" {
		t.Errorf("新建设备不应是损坏状态：%s", detail.Summary.Broken)
	}
	t.Logf("设备创建成功：display=%q api=%s abi=%s ram=%dMB cores=%d size=%s",
		detail.Summary.DisplayName, detail.Summary.APILevel, detail.Summary.ABI,
		detail.Summary.RAMMB, detail.Summary.Cores, platform.HumanSize(detail.Summary.SizeBytes))

	// 修改配置
	newName := "E2E 改名后"
	if err := avdSvc.Update(name, domain.AvdPatch{
		DisplayName: &newName,
		HW:          map[string]string{"hw.ramSize": "3072", "hw.lcd.density": "320"},
	}); err != nil {
		t.Fatalf("修改配置失败: %v", err)
	}
	updated, _ := avdSvc.Get(name)
	if updated.Summary.RAMMB != 3072 {
		t.Errorf("修改后内存应为 3072，实际 %d", updated.Summary.RAMMB)
	}
	if updated.Summary.DisplayName != newName {
		t.Errorf("显示名未更新：%q", updated.Summary.DisplayName)
	}

	// config.ini 直编 + diff
	before, err := avdSvc.ReadConfigRaw(name)
	if err != nil {
		t.Fatalf("读取原始配置失败: %v", err)
	}
	edited := before + "e2e.custom.key=hello\n"
	diff, err := avdSvc.WriteConfigRaw(service.WriteConfigRawRequest{Name: name, Content: edited, DryRun: true})
	if err != nil {
		t.Fatalf("预演保存失败: %v", err)
	}
	if diff.Added["e2e.custom.key"] != "hello" {
		t.Errorf("diff 未识别新增项：%+v", diff)
	}
	if len(diff.Warnings) == 0 {
		t.Error("未知配置项应给出警告")
	}
	if _, err := avdSvc.WriteConfigRaw(service.WriteConfigRawRequest{Name: name, Content: edited}); err != nil {
		t.Fatalf("保存原始配置失败: %v", err)
	}

	// 克隆
	cloneJob, err := avdSvc.Clone(service.CloneRequest{SourceName: name, NewName: "E2E_Clone"})
	if err != nil {
		t.Fatalf("克隆失败: %v", err)
	}
	if info := waitJob(t, rt, cloneJob, 2*time.Minute); info.Status != domain.JobSucceeded {
		t.Fatalf("克隆任务失败：%v", info.Error)
	}
	clone, err := avdSvc.Get("E2E_Clone")
	if err != nil {
		t.Fatalf("读取克隆结果失败: %v", err)
	}
	if clone.Config["AvdId"] != "E2E_Clone" {
		t.Errorf("克隆后 AvdId 未更新：%s", clone.Config["AvdId"])
	}
	if clone.Summary.RAMMB != 3072 {
		t.Errorf("克隆应保留配置，实际 ram=%d", clone.Summary.RAMMB)
	}

	// 重命名
	if err := avdSvc.Rename("E2E_Clone", "E2E_Renamed"); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	renamed, err := avdSvc.Get("E2E_Renamed")
	if err != nil {
		t.Fatalf("重命名后读取失败: %v", err)
	}
	if renamed.Config["AvdId"] != "E2E_Renamed" {
		t.Errorf("重命名后 AvdId 未更新：%s", renamed.Config["AvdId"])
	}
	if _, err := avdSvc.Get("E2E_Clone"); err == nil {
		t.Error("旧名称仍可读取，重命名未生效")
	}

	// 列表
	list, err := avdSvc.List()
	if err != nil {
		t.Fatalf("列出设备失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("应有 2 个设备，实际 %d：%+v", len(list), names(list))
	}
	t.Logf("设备列表：%v", names(list))

	// 清除数据（无运行实例时允许）
	wipeJob, err := avdSvc.WipeData(name)
	if err != nil {
		t.Fatalf("清除数据失败: %v", err)
	}
	if info := waitJob(t, rt, wipeJob, time.Minute); info.Status != domain.JobSucceeded {
		t.Fatalf("清除数据任务失败：%v", info.Error)
	}

	// 删除
	for _, target := range []string{name, "E2E_Renamed"} {
		delJob, err := avdSvc.Delete(service.DeleteRequest{Name: target, DeleteFiles: true})
		if err != nil {
			t.Fatalf("删除 %s 失败: %v", target, err)
		}
		if info := waitJob(t, rt, delJob, time.Minute); info.Status != domain.JobSucceeded {
			t.Fatalf("删除任务失败：%v", info.Error)
		}
		if platform.DirExists(store.New(e.avdHome).Resolve(target).Dir) {
			t.Errorf("%s 的目录未被删除", target)
		}
	}
	remaining, _ := avdSvc.List()
	if len(remaining) != 0 {
		t.Errorf("删除后仍有设备：%v", names(remaining))
	}
}

// ---------------------------------------------------------------- 6. 模拟器启动（重）

func TestE2E_EmulatorBootAndAdb(t *testing.T) {
	requireBoot(t)
	e := setup(t)
	e.requireSDK(t)
	e.requireImage(t)

	rt := e.newRuntime(t)
	avdSvc := service.NewAvdService(rt)
	emuSvc := service.NewEmulatorService(rt)
	adbSvc := service.NewAdbService(rt)

	profiles, err := avdSvc.ListProfiles(false)
	if err != nil {
		t.Fatalf("读取档案失败: %v", err)
	}
	name := "E2E_Boot"
	spec := domain.AvdSpec{
		Name:            name,
		DisplayName:     "E2E 启动测试",
		ProfileID:       pickProfile(profiles),
		SystemImagePath: e.imagePath,
		HW:              map[string]string{"hw.ramSize": "2048", "hw.cpu.ncore": "2"},
	}
	jobID, err := avdSvc.Create(spec)
	if err != nil {
		t.Fatalf("创建 AVD 失败: %v", err)
	}
	if info := waitJob(t, rt, jobID, 3*time.Minute); info.Status != domain.JobSucceeded {
		t.Fatalf("创建任务失败：%v", info.Error)
	}
	t.Cleanup(func() {
		_ = emuSvc.StopByAvd(name, true)
		if jobID, err := avdSvc.Delete(service.DeleteRequest{Name: name, DeleteFiles: true}); err == nil {
			waitJob(t, rt, jobID, time.Minute)
		}
	})

	inst, err := emuSvc.Start(service.StartRequest{
		AvdName: name,
		Options: domain.LaunchOptions{
			NoWindow:   true, // 无窗口：CI 与自动化友好
			NoAudio:    true,
			NoBootAnim: true,
			ColdBoot:   true,
			WipeData:   false,
			GPUMode:    "swiftshader_indirect", // 无 GPU 环境下最稳
			MemoryMB:   2048,
			Cores:      2,
		},
	})
	if err != nil {
		t.Fatalf("启动模拟器失败: %v", err)
	}
	t.Logf("实例已启动：id=%s serial=%s port=%d pid=%d", inst.ID, inst.Serial, inst.Port, inst.PID)

	// 轮询等待开机完成
	deadline := time.Now().Add(bootTimeout)
	var lastState domain.AvdState
	booted := false
	for time.Now().Before(deadline) {
		it, ok := findInstance(emuSvc, inst.ID)
		if !ok {
			t.Fatal("实例从列表中消失")
		}
		if it.State != lastState {
			t.Logf("  状态：%s（%s）", it.State, it.LastError)
			lastState = it.State
		}
		switch it.State {
		case domain.AvdRunning:
			booted = true
		case domain.AvdError, domain.AvdStopped:
			for _, l := range mustLogs(emuSvc, it.ID) {
				t.Logf("  [模拟器] %s", l.Message)
			}
			t.Fatalf("模拟器启动失败：%s", it.LastError)
		}
		if booted {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if !booted {
		t.Fatal("等待开机完成超时")
	}
	t.Logf("模拟器已开机完成（%s）", inst.Serial)

	// adb 集成
	devices, err := adbSvc.Devices()
	if err != nil {
		t.Fatalf("adb devices 失败: %v", err)
	}
	found := false
	for _, d := range devices {
		if d.Serial == inst.Serial {
			found = true
			t.Logf("  adb 设备：%s state=%s model=%s", d.Serial, d.State, d.Model)
		}
	}
	if !found {
		t.Fatalf("adb 未看到实例 %s（共 %d 个设备）", inst.Serial, len(devices))
	}

	out, err := adbSvc.Shell(inst.Serial, "getprop sys.boot_completed")
	if err != nil {
		t.Fatalf("adb shell 失败: %v", err)
	}
	if strings.TrimSpace(out) != "1" {
		t.Errorf("sys.boot_completed = %q，期望 1", out)
	}
	if sdk, err := adbSvc.Shell(inst.Serial, "getprop ro.build.version.sdk"); err == nil {
		t.Logf("  系统 API 版本：%s", strings.TrimSpace(sdk))
	}

	// 截图（验证二进制输出通道）
	if dataURL, err := emuSvc.Screenshot(inst.ID); err != nil {
		t.Errorf("截图失败: %v", err)
	} else if len(dataURL) < 1000 {
		t.Errorf("截图数据异常小：%d 字节", len(dataURL))
	} else {
		t.Logf("  截图成功：base64 长度 %d", len(dataURL))
	}

	// 按键与旋转
	if err := emuSvc.SendKey(inst.ID, "HOME"); err != nil {
		t.Errorf("发送 HOME 键失败: %v", err)
	}
	if err := emuSvc.Rotate(inst.ID, "landscape"); err != nil {
		t.Errorf("旋转失败: %v", err)
	}

	// 停止
	if err := emuSvc.StopByAvd(name, false); err != nil {
		t.Fatalf("停止实例失败: %v", err)
	}
	t.Log("实例已正常停止")
}

// ---------------------------------------------------------------- 7. 包管理

func TestE2E_SdkListingAndVersions(t *testing.T) {
	e := setup(t)
	e.requireSDK(t)

	rt := e.newRuntime(t)
	sdkSvc := service.NewSdkService(rt)

	installed := sdkSvc.ListInstalled()
	if len(installed) == 0 {
		t.Fatal("未读取到已安装包")
	}
	t.Logf("已安装 %d 个包：", len(installed))
	for _, p := range installed {
		t.Logf("  %-46s rev=%-10s %s", p.Path, p.InstalledRevision, platform.HumanSize(p.SizeBytes))
		if p.InstalledRevision == "" {
			t.Errorf("%s 缺少版本信息", p.Path)
		}
	}

	// 校验完整性
	for _, p := range installed {
		res, err := sdkSvc.VerifyPackage(p.Path)
		if err != nil {
			t.Errorf("校验 %s 报错: %v", p.Path, err)
			continue
		}
		if !res.OK {
			t.Logf("  警告：%s 校验未通过：%v", p.Path, res.Details)
		}
	}

	// 系统镜像视图（本地）
	images, err := sdkSvc.ListSystemImages(service.ListSystemImagesRequest{OnlyInstalled: true})
	if err != nil {
		t.Fatalf("读取已安装镜像失败: %v", err)
	}
	for _, img := range images {
		t.Logf("  镜像：%s api=%s tag=%s abi=%s play=%v requiresEmulator=%s",
			img.Path, img.APILevel, img.TagID, img.ABI, img.IsPlaystore, img.RequiresEmulator)
		if img.APILevel == "" || img.ABI == "" {
			t.Errorf("镜像信息不完整：%+v", img)
		}
	}
}

// ---------------------------------------------------------------- 辅助

type jobLogSource interface {
	GetLog(instanceID string, tail int) ([]domain.LogLine, error)
	ListRunning() []domain.EmulatorInstance
}

func findInstance(src jobLogSource, id string) (domain.EmulatorInstance, bool) {
	for _, it := range src.ListRunning() {
		if it.ID == id {
			return it, true
		}
	}
	return domain.EmulatorInstance{}, false
}

func mustLogs(src jobLogSource, id string) []domain.LogLine {
	logs, _ := src.GetLog(id, 60)
	return logs
}

// toolEnv 构造外部工具所需环境变量：cmdline-tools 是 Java 程序，必须能找到 java。
func toolEnv(jdkPath string) []string {
	env := os.Environ()
	if jdkPath != "" {
		// <jdk>/bin/java.exe → <jdk>
		root := filepath.Dir(filepath.Dir(jdkPath))
		env = append(env, "JAVA_HOME="+root)
	}
	return env
}

// run 执行外部工具并捕获输出（带超时）。
func run(ctx context.Context, tool string, args []string, jdkPath string) (proc.Result, error) {
	res, err := proc.Run(ctx, tool, args, proc.Options{Env: toolEnv(jdkPath), Timeout: 5 * time.Minute})
	return res, err
}

func runTool(t *testing.T, e *env, tool string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, 3*time.Minute)
	defer cancel()
	res, err := run(ctx, tool, args, e.jdkPath)
	if err != nil && strings.TrimSpace(res.Stdout) == "" {
		t.Fatalf("执行 %s %v 失败: %v\n%s", filepath.Base(tool), args, err, res.Combined())
	}
	return res.Stdout
}

func pickProfile(profiles []domain.DeviceProfile) string {
	// 优先选通用手机档案，保证各种 API 级别都可用
	for _, want := range []string{"medium_phone", "pixel_7", "pixel_6", "Nexus 5X"} {
		for _, p := range profiles {
			if p.ID == want {
				return p.ID
			}
		}
	}
	return profiles[0].ID
}

func names(items []domain.AvdSummary) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
