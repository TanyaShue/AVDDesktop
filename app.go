package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/config"
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

	Env         *service.EnvService
	Mirror      *service.MirrorService
	Sdk         *service.SdkService
	Avd         *service.AvdService
	Emulator    *service.EmulatorService
	Adb         *service.AdbService
	Settings    *service.SettingsService
	Diagnostics *service.DiagnosticsService
	Jobs        *service.JobService
	Window      *service.WindowService
}

// NewApp 装配应用。
func NewApp() (*App, error) {
	appName := AppName
	version := AppVersion

	appDir := platform.AppDataDir(appName)
	if err := platform.EnsureDir(appDir); err != nil {
		return nil, fmt.Errorf("无法创建应用数据目录 %s: %w", appDir, err)
	}

	// 日志：排序为 环境变量 > 配置（在 settings 加载后修正）> 默认
	logger, err := logging.New(logging.Options{
		Dir:      platform.SubDir(appName, "logs"),
		Level:    initialLogLevel(),
		KeepDays: 7,
		Stdout:   stdoutIfDev(),
	})
	if err != nil {
		// 日志不可用不应阻塞启动
		log.Printf("警告：日志初始化失败: %v", err)
		logger = logging.Discard()
	}

	settings := config.NewManager(filepath.Join(appDir, "settings.json"))
	if err := settings.Load(); err != nil {
		logger.Warn("app", "设置加载失败，已使用默认值: %v", err)
	} else {
		// 应用设置中的日志级别与保留天数
		current := settings.Get()
		logger.SetLevel(current.LogLevel)
	}

	rt := service.NewRuntime(appName, version, settings, platform.SubDir(appName, "cache"), logger)
	rt.SetEmitter(func(event string, payload any) {
		// Runtime.Emit 已保证上下文就绪，此处可直接调用
		wailsruntime.EventsEmit(rt.Context(), event, payload)
	})

	// 把每条日志实时推送前端（设置页的日志面板按需订阅）
	logger.SetSink(func(entry logging.Entry) {
		rt.Emit("log:line", entry)
	})

	app := &App{
		rt:          rt,
		log:         logger,
		Env:         service.NewEnvService(rt),
		Mirror:      service.NewMirrorService(rt),
		Sdk:         service.NewSdkService(rt),
		Avd:         service.NewAvdService(rt),
		Emulator:    service.NewEmulatorService(rt),
		Adb:         service.NewAdbService(rt),
		Settings:    service.NewSettingsService(rt),
		Diagnostics: service.NewDiagnosticsService(rt),
		Jobs:        service.NewJobService(rt),
		Window:      service.NewWindowService(rt),
	}
	app.log.Info("app", "%s %s 启动中，数据目录 %s", appName, version, appDir)
	return app, nil
}

// startup 由 Wails 在窗口就绪后调用。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.rt.SetContext(ctx)
	a.log.Info("app", "窗口已就绪（%s/%s）", runtime.GOOS, runtime.GOARCH)

	// 首帧渲染后推一次环境快照，避免前端白屏等待
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.log.Error("app", "启动自检 panic: %v", r)
			}
		}()
		time.Sleep(300 * time.Millisecond)
		report, err := a.Env.Detect(service.DetectRequest{Force: true})
		if err != nil {
			a.log.Warn("app", "启动自检失败: %v", err)
			return
		}
		a.rt.Emit("env:changed", report)
	}()
}

// shutdown 由 Wails 在窗口关闭时调用。
func (a *App) shutdown(ctx context.Context) {
	a.rt.Shutdown()
	a.log.Info("app", "应用退出")
	_ = a.log.Close()
}
// beforeClose 用于确认是否存在运行中的模拟器。
func (a *App) beforeClose(ctx context.Context) bool {
	running := a.Emulator.ListRunning()
	active := 0
	for _, inst := range running {
		if inst.State == "starting" || inst.State == "booting" || inst.State == "running" {
			active++
		}
	}
	if active == 0 {
		return false // 允许关闭
	}
	selection, err := wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{
		Type:          wailsruntime.QuestionDialog,
		Title:         "仍有模拟器在运行",
		Message:       fmt.Sprintf("当前有 %d 个模拟器实例正在运行。要一并关闭它们并退出吗？", active),
		Buttons:       []string{"全部关闭并退出", "取消"},
		DefaultButton: "取消",
		CancelButton:  "取消",
	})
	if err != nil || selection != "全部关闭并退出" {
		return true // 阻止关闭
	}
	a.Emulator.Shutdown(false)
	return false
}

// GetVersion 返回版本号（前端"关于"页使用）。
func (a *App) GetVersion() string { return a.rt.Version }

// GetAppInfo 返回应用级信息。
func (a *App) GetAppInfo() map[string]string {
	resolved := a.rt.Resolved()
	return map[string]string{
		"appName":  a.rt.AppName,
		"version":  a.rt.Version,
		"dataDir":  platform.AppDataDir(a.rt.AppName),
		"logDir":   resolved.LogDir,
		"cacheDir": resolved.CacheDir,
	}
}

// OpenDataDir 打开应用数据目录（诊断用）。
func (a *App) OpenDataDir() error {
	return a.Env.OpenInExplorer(platform.AppDataDir(a.rt.AppName))
}

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
