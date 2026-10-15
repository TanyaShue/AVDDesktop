package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/config"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/service"
)

// AppName 与 AppVersion 是应用标识（构建时可用 -ldflags 覆盖）。
var (
	AppName    = "AVDDesktop"
	AppVersion = "0.2.0"
)

// App 是 Wails 绑定根对象：持有各领域 Service 与运行时容器。
//
// 说明：真正的业务在 internal/service 中；App 只负责装配与生命周期。
type App struct {
	ctx context.Context
	rt  *service.Runtime
	log *logging.Logger

	Env      *service.EnvService
	Mirror   *service.MirrorService
	Avd      *service.AvdService
	Emulator *service.EmulatorService
	Display  *service.DisplayService
	Settings *service.SettingsService
	Logs     *service.LogService
	Jobs     *service.JobService
	Window   *service.WindowService
}

// NewApp 装配应用。
func NewApp() (*App, error) {
	appName := AppName
	version := AppVersion

	// 软件自有目录（sdk / avd / config / logs / cache）
	if err := platform.EnsureLayout(); err != nil {
		return nil, fmt.Errorf("无法创建软件目录 %s: %w", platform.Root(), err)
	}

	logger, err := logging.New(logging.Options{
		Dir:      platform.LogDir(),
		Level:    initialLogLevel(),
		KeepDays: 7,
		Stdout:   stdoutIfDev(),
	})
	if err != nil {
		// 日志不可用不应阻塞启动
		log.Printf("警告：日志初始化失败: %v", err)
		logger = logging.Discard()
	}

	settings := config.NewManager(platform.SettingsPath())
	if err := settings.Load(); err != nil {
		logger.Warn("app", "设置加载失败，已使用默认值: %v", err)
	}
	logger.SetLevel(settings.Get().LogLevel)

	rt, err := service.NewRuntime(appName, version, settings, logger)
	if err != nil {
		return nil, err
	}
	rt.SetEmitter(func(event string, payload any) {
		wailsruntime.EventsEmit(rt.Context(), event, payload)
	})

	// 把每条应用日志实时推送前端（底部统一日志区域，与任务日志共享同一控制台）
	logger.SetSink(func(entry logging.Entry) {
		rt.Emit("log:line", service.LogLineFromEntry(entry))
	})

	app := &App{
		rt:       rt,
		log:      logger,
		Env:      service.NewEnvService(rt),
		Mirror:   service.NewMirrorService(rt),
		Avd:      service.NewAvdService(rt),
		Emulator: service.NewEmulatorService(rt),
		Display:  service.NewDisplayService(rt),
		Settings: service.NewSettingsService(rt),
		Logs:     service.NewLogService(rt),
		Jobs:     service.NewJobService(rt),
		Window:   service.NewWindowService(rt),
	}
	app.log.Info("app", "%s %s 启动中，软件目录 %s", appName, version, platform.Root())
	return app, nil
}

// startup 由 Wails 在窗口就绪后调用。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.rt.SetContext(ctx)
	a.log.Info("app", "窗口已就绪（%s/%s）", runtime.GOOS, runtime.GOARCH)

	// 首次运行且软件自带 JDK / SDK 尚未初始化时，自动准备环境（进度显示在底部任务区域）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.log.Error("app", "启动自检 panic: %v", r)
			}
		}()
		report, err := a.Env.Check()
		if err != nil {
			a.log.Warn("app", "启动自检失败: %v", err)
			return
		}
		a.rt.Emit("env:changed", report)
		if report.NeedInit {
			a.log.Info("app", "软件自带环境尚未初始化，开始自动准备（JDK / SDK）")
			if _, err := a.Env.Prepare(); err != nil {
				a.log.Error("app", "自动准备环境失败: %v", err)
			}
		}
	}()
}

// shutdown 由 Wails 在窗口关闭时调用。
func (a *App) shutdown(ctx context.Context) {
	// 先结束设备画面（HTTP 流 + gRPC 连接），再停模拟器实例。
	a.Display.Shutdown()
	a.rt.Shutdown()
	a.log.Info("app", "应用退出")
	_ = a.log.Close()
}

// 退出确认框的按钮文案。
//
// 注意：Wails v2 在 Windows / Linux 上使用原生“是/否”对话框并忽略 Buttons，
// 只有 macOS 会使用自定义文案并原样返回（详见 quitSelectionConfirmed）。
const (
	quitButtonConfirm = "全部关闭并退出"
	quitButtonCancel  = "取消"
)

// beforeClose 用于确认是否存在运行中的模拟器。
func (a *App) beforeClose(ctx context.Context) bool {
	active := activeEmulatorCount(a.Emulator.ListRunning())
	if active == 0 {
		return false // 允许关闭
	}
	selection, err := wailsruntime.MessageDialog(ctx, quitConfirmDialogOptions(active))
	if err != nil {
		a.log.Warn("app", "退出确认框不可用，已取消本次关闭: %v", err)
		return true // 阻止关闭
	}
	if !quitSelectionConfirmed(selection) {
		a.log.Info("app", "退出已取消（确认框返回 %q），保留运行中的模拟器", selection)
		return true // 阻止关闭
	}
	a.log.Info("app", "用户确认退出：停止 %d 个运行中的模拟器实例", active)
	// 同步等待优雅停止（adb emu kill）完成：返回 false 后 Wails 会立即走
	// OnShutdown → Runtime.Shutdown 的强制 StopAll，改成异步会被强杀覆盖。
	a.Emulator.Shutdown(false)
	return false
}

// activeEmulatorCount 统计运行中的实例数量（含启动中与引导中）。
func activeEmulatorCount(instances []domain.EmulatorInstance) int {
	active := 0
	for _, inst := range instances {
		switch inst.State {
		case domain.AvdStarting, domain.AvdBooting, domain.AvdRunning:
			active++
		}
	}
	return active
}

// quitConfirmDialogOptions 构造退出确认框参数。
func quitConfirmDialogOptions(active int) wailsruntime.MessageDialogOptions {
	return wailsruntime.MessageDialogOptions{
		Type:          wailsruntime.QuestionDialog,
		Title:         "仍有模拟器在运行",
		Message:       fmt.Sprintf("当前有 %d 个模拟器实例正在运行。要一并关闭它们并退出吗？", active),
		Buttons:       []string{quitButtonConfirm, quitButtonCancel},
		DefaultButton: quitDialogDefaultButton(),
		CancelButton:  quitButtonCancel,
	}
}

// quitDialogDefaultButton 返回确认框默认（回车）按钮的文案。
//
// Wails 各平台对 DefaultButton 的处理并不一致：Linux 的 GTK 对话框完全忽略该字段；
// macOS 按文案匹配 Buttons 中的按钮；Windows 只识别英文 "No"（忽略大小写与首尾空白）
// 来设置 MB_DEFBUTTON2。因此在 Windows 上必须传 "No"，否则“是”会成为默认按钮，
// 误按一次回车就会关闭全部模拟器；Linux / macOS 继续使用中文文案以与 Buttons 一致。
func quitDialogDefaultButton() string {
	if runtime.GOOS == "windows" {
		return "No"
	}
	return quitButtonCancel
}

// quitSelectionConfirmed 判断确认框返回值是否表示“连同运行中的模拟器一起退出”。
//
// Wails v2.16.0 各平台的 QuestionDialog 返回值不同（已核对 Wails 源码）：
//   - Windows（internal/frontend/desktop/windows/dialog.go）：固定 MB_YESNO，
//     忽略 Buttons，返回 "Yes" / "No"；
//   - Linux（internal/frontend/desktop/linux/window.c）：固定 GTK_BUTTONS_YES_NO，
//     忽略 Buttons，确认返回 "Yes"，取消返回 "No"，Esc / 关闭窗口返回空串；
//   - macOS（internal/frontend/desktop/darwin/dialog.go）：使用自定义 Buttons，
//     原样返回被点击按钮的文案。
//
// 仅把明确的肯定结果视为确认，其余（含空串与未知文案）一律按取消处理，
// 以免在无法确定用户意图时误停模拟器。
func quitSelectionConfirmed(selection string) bool {
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "yes", strings.ToLower(quitButtonConfirm):
		return true
	default:
		return false
	}
}

// GetVersion 返回版本号。
func (a *App) GetVersion() string { return a.rt.Version }

// Greet 是 Wails 模板遗留方法，保留用于本地连通性自测。
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, AVDDesktop is ready.", name)
}

// initialLogLevel 决定启动阶段的日志级别：环境变量 > 默认 info。
//
// 设置加载后会用 settings.logLevel 覆盖。
func initialLogLevel() string {
	if v := os.Getenv("AVDDESKTOP_LOG_LEVEL"); v != "" {
		return v
	}
	return "info"
}

// stdoutIfDev 在开发模式（未打包）下同时把日志输出到标准输出，便于 wails dev 排错。
func stdoutIfDev() *os.File {
	if os.Getenv("AVDDESKTOP_LOG_STDOUT") == "0" {
		return nil
	}
	// Wails dev 模式会设置 WAILS_DEV 相关的环境变量；打包后不输出到控制台
	if os.Getenv("WAILS_DEV") != "" || os.Getenv("AVDDESKTOP_DEV") != "" {
		return os.Stdout
	}
	return nil
}
