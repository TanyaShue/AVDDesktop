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
