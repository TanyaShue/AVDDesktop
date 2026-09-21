//go:build e2e

// Package e2e 是走真实服务层 + 真网络 + 真磁盘的端到端测试。
//
// 运行方式（默认不参与 `go test ./...`）：
//
//	go test -tags e2e -timeout 60m ./internal/e2e/ -run TestE2E_EnvPrepare -v
//
// 首次运行会真实下载官方命令行工具（约 150 MB）与 platform-tools / emulator，
// 缓存在 AVDDESKTOP_E2E_HOME 指定的软件根目录里（未指定时使用临时目录）。
package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"AVDDesktop/internal/config"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sdk"
	"AVDDesktop/internal/service"
)

func TestMain(m *testing.M) {
	home := strings.TrimSpace(os.Getenv("AVDDESKTOP_E2E_HOME"))
	if home == "" {
		dir, err := os.MkdirTemp("", "avddesktop-e2e-")
		if err != nil {
			panic(err)
		}
		home = dir
	}
	// 必须在任何 platform.Root() 调用之前设置
	if err := os.Setenv("AVDDESKTOP_HOME", home); err != nil {
		panic(err)
	}
	if err := platform.EnsureLayout(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// TestE2E_EnvPrepare 验证 Phase 1 的验收链路：
// 软件自带 SDK 不存在 → 自动初始化（下载命令行工具 / 接受许可 / 安装基础组件）
// → 四个工具全部来自软件自己的目录。
func TestE2E_EnvPrepare(t *testing.T) {
	rt := newRuntime(t)
	env := service.NewEnvService(rt)
	tools := rt.Components().Tools

	before, err := env.Check()
	if err != nil {
		t.Fatalf("环境检查失败: %v", err)
	}
	t.Logf("初始化前：needInit=%v ready=%v sdk=%s", before.NeedInit, before.Ready, before.SdkRoot)
	if before.SdkRoot != platform.SdkRoot() {
		t.Fatalf("SDK 目录必须位于软件根目录下: %s", before.SdkRoot)
	}
	if before.JavaPath == "" {
		t.Fatalf("未找到 JDK：sdkmanager 无法运行（安装 JDK 17+ 并设置 JAVA_HOME 后重跑）")
	}

	if before.NeedInit || !tools.HasAdb() || !tools.HasEmulator() {
		jobID, err := env.Prepare()
		if err != nil {
			t.Fatalf("启动自动准备失败: %v", err)
		}
		if err := waitJob(rt, jobID, 45*time.Minute); err != nil {
			t.Fatalf("自动准备失败: %v", err)
		}
	} else {
		t.Log("SDK 已就绪，跳过初始化（复用上次缓存）")
	}

	// 工具链全部来自软件自有目录
	for name, path := range map[string]string{
		"sdkmanager": tools.Sdkmanager,
		"avdmanager": tools.Avdmanager,
		"adb":        tools.Adb,
		"emulator":   tools.Emulator,
	} {
		if !platform.FileExists(path) {
			t.Errorf("%s 未就位: %s", name, path)
		}
		if !strings.HasPrefix(path, platform.Root()) {
			t.Errorf("%s 不在软件目录内: %s", name, path)
		}
	}

	after, err := env.Check()
	if err != nil {
		t.Fatalf("初始化后环境检查失败: %v", err)
	}
	if !after.Ready {
		t.Fatalf("环境未就绪，问题：%+v", after.Issues)
	}
	for _, c := range after.Components {
		if c.State != domain.StatePresent {
			t.Errorf("组件 %s 状态异常: %s %s", c.Name, c.State, c.Detail)
		}
	}
	t.Logf("初始化完成：sdkmanager/avdmanager/adb/emulator 均来自 %s（耗时 %dms）",
		after.SdkRoot, after.ElapsedMs)

	// 官方 sdkmanager 可用：能列出已安装包（离线）
	images, err := sdk.ListImages(rt.Context(), tools, rt.Components().Env, true, nil)
	if err != nil {
		t.Fatalf("sdkmanager 无法列出已安装包: %v", err)
	}
	t.Logf("本机已安装系统镜像 %d 个", len(images))
}

// newRuntime 组装一个真实的运行时（不接前端事件）。
func newRuntime(t *testing.T) *service.Runtime {
	t.Helper()
	settings := config.NewManager(platform.SettingsPath())
	if err := settings.Load(); err != nil {
		t.Fatalf("设置加载失败: %v", err)
	}
	logger, err := logging.New(logging.Options{Dir: platform.LogDir(), Level: "info", KeepDays: 1})
	if err != nil {
		t.Fatalf("日志初始化失败: %v", err)
	}
	rt, err := service.NewRuntime("AVDDesktopE2E", "e2e", settings, logger)
	if err != nil {
		t.Fatalf("运行时初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return rt
}

// waitJob 等待任务结束；任务失败时返回其错误。
func waitJob(rt *service.Runtime, jobID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		j, ok := rt.Jobs().Get(jobID)
		if !ok {
			return testError("任务不存在: " + jobID)
		}
		info := j.Info()
		switch info.Status {
		case domain.JobSucceeded:
			return nil
		case domain.JobFailed:
			if info.Error != nil {
				return info.Error
			}
			return testError("任务失败: " + info.Title)
		case domain.JobCanceled:
			return testError("任务被取消: " + info.Title)
		}
		if time.Now().After(deadline) {
			return testError("任务超时: " + info.Title + "（阶段：" + info.Phase + "）")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

type testError string

func (e testError) Error() string { return string(e) }

// TestE2E_AvdCreate 验证 Phase 2 的验收链路：
// 选择 System Image（不存在时由 sdkmanager 自动安装）→ avdmanager 创建 AVD
// → 用官方 avdmanager 确认设备存在 → 删除后确认消失。
func TestE2E_AvdCreate(t *testing.T) {
	rt := newRuntime(t)
	env := service.NewEnvService(rt)
	avdSvc := service.NewAvdService(rt)
	tools := rt.Components().Tools

	// 1. 环境必须就绪（缺则先自动准备）
	report, err := env.Check()
	if err != nil {
		t.Fatalf("环境检查失败: %v", err)
	}
	if !report.Ready {
		t.Logf("环境未就绪，先自动准备：%+v", report.Issues)
		jobID, err := env.Prepare()
		if err != nil {
			t.Fatalf("启动自动准备失败: %v", err)
		}
		if err := waitJob(rt, jobID, 45*time.Minute); err != nil {
			t.Fatalf("自动准备失败: %v", err)
		}
		if report, err = env.Check(); err != nil || !report.Ready {
			t.Fatalf("准备后环境仍未就绪：err=%v issues=%+v", err, report.Issues)
		}
	}

	// 2. 选镜像：宿主 ABI 的 google_apis，已安装优先，否则 API 最低（体积最小）
	images, err := avdSvc.ListImages(false)
	if err != nil {
		t.Fatalf("读取系统镜像列表失败: %v", err)
	}
	image := pickImage(t, images)
	t.Logf("选用镜像：%s（已安装=%v，ABI=%s，API=%s）", image.Path, image.Installed, image.ABI, image.API)

	// 3. 建设备档案（medium_phone 不存在时退回第一个）
	profileID := "medium_phone"
	profiles, err := avdSvc.ListProfiles(false)
	if err != nil {
		t.Fatalf("读取设备档案失败: %v", err)
	}
	found := false
	for _, p := range profiles {
		if p.ID == profileID {
			found = true
			break
		}
	}
	if !found {
		if len(profiles) == 0 {
			t.Fatal("avdmanager 未返回任何设备档案")
		}
		profileID = profiles[0].ID
		t.Logf("未找到 medium_phone，改用 %s", profileID)
	}
	t.Logf("avdmanager 提供 %d 个设备档案（首选 %s）", len(profiles), profileID)

	// 4. 创建 AVD（镜像缺失时由 sdkmanager 自动安装）
	name := "E2E_Avd_" + time.Now().Format("20060102_150405")
	started := time.Now()
	jobID, err := avdSvc.Create(domain.AvdSpec{
		Name:            name,
		SystemImagePath: image.Path,
		ProfileID:       profileID,
	})
	if err != nil {
		t.Fatalf("启动创建任务失败: %v", err)
	}
	if err := waitJob(rt, jobID, 60*time.Minute); err != nil {
		t.Fatalf("创建 AVD 失败: %v", err)
	}
	t.Logf("创建完成，耗时 %s", time.Since(started).Round(time.Second))

	// 5. 确认设备存在：软件自己的列表 + 官方 avdmanager 双重确认
	items, err := avdSvc.List()
	if err != nil {
		t.Fatalf("列出设备失败: %v", err)
	}
	var created *domain.AvdSummary
	for i := range items {
		if items[i].Name == name {
			created = &items[i]
			break
		}
	}
	if created == nil {
		t.Fatalf("软件列表里没有刚创建的设备 %s：%+v", name, items)
	}
	if created.API == "" || created.ABI == "" {
		t.Errorf("设备摘要缺少 target/abi：%+v", created)
	}
	if created.Broken != "" {
		t.Errorf("新建设备不应是损坏状态：%s", created.Broken)
	}

	official := avdmanagerListAvd(t, tools)
	if !strings.Contains(official, name) {
		t.Fatalf("官方 avdmanager list avd 未列出 %s：\n%s", name, official)
	}
	t.Logf("官方 avdmanager 已确认设备存在：%s", firstLineContaining(official, name))

	// 6. 删除并确认消失
	jobID, err = avdSvc.Delete(name)
	if err != nil {
		t.Fatalf("启动删除任务失败: %v", err)
	}
	if err := waitJob(rt, jobID, 5*time.Minute); err != nil {
		t.Fatalf("删除 AVD 失败: %v", err)
	}
	items, err = avdSvc.List()
	if err != nil {
		t.Fatalf("删除后列出设备失败: %v", err)
	}
	for _, it := range items {
		if it.Name == name {
			t.Fatalf("设备 %s 删除后仍然存在", name)
		}
	}
	if official := avdmanagerListAvd(t, tools); strings.Contains(official, name) {
		t.Errorf("官方 avdmanager 仍能看到已删除的设备 %s", name)
	}
	t.Logf("删除完成，设备已从软件列表与官方 avdmanager 中消失")
}

// pickImage 选一个宿主 ABI 的 google_apis 镜像：已安装优先，否则 API 最低（下载最小）。
func pickImage(t *testing.T, images []domain.SystemImage) domain.SystemImage {
	t.Helper()
	host := sdk.HostABI()
	var candidates []domain.SystemImage
	for _, img := range images {
		if img.ABI != host || img.Tag != "google_apis" {
			continue
		}
		if !apiAtLeast(img.API, 30) {
			continue
		}
		candidates = append(candidates, img)
	}
	if len(candidates) == 0 {
		t.Fatalf("没有找到宿主 ABI(%s) 的 google_apis 镜像（API>=30），共 %d 个镜像", host, len(images))
	}
	best := candidates[0]
	for _, img := range candidates[1:] {
		switch {
		case img.Installed && !best.Installed:
			best = img
		case img.Installed == best.Installed && apiLess(img.API, best.API):
			best = img
		}
	}
	return best
}

func apiAtLeast(api string, min int) bool {
	return apiNumber(api) >= min
}

func apiLess(a, b string) bool { return apiNumber(a) < apiNumber(b) }

func apiNumber(api string) int {
	api = strings.TrimPrefix(strings.TrimSpace(api), "android-")
	n, digits := 0, 0
	for _, r := range api {
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

// avdmanagerListAvd 通过软件自带的 avdmanager 列出设备（官方工具确认）。
func avdmanagerListAvd(t *testing.T, tools platform.Tools) string {
	t.Helper()
	rt := newRuntime(t)
	res, err := proc.Run(rt.Context(), tools.Avdmanager, []string{"list", "avd"}, proc.Options{
		Env:     rt.Components().Env,
		Timeout: 90 * time.Second,
	})
	out := res.Combined()
	if err != nil {
		t.Fatalf("avdmanager list avd 执行失败: %v\n%s", err, out)
	}
	return out
}

func firstLineContaining(text, sub string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, sub) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
