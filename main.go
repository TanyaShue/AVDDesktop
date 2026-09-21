package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"AVDDesktop/internal/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	// 单实例：避免两个进程同时写同一个 SDK/AVD 目录
	release, alreadyRunning, lockErr := platform.AcquireSingleInstance(AppName)
	if lockErr != nil {
		app.log.Warn("app", "单实例锁创建失败（不影响使用）: %v", lockErr)
	} else if alreadyRunning {
		app.log.Warn("app", "已有实例在运行，本次启动退出")
		platform.FocusExistingInstance("AVDDesktop")
		platform.NotifyAlreadyRunning("AVDDesktop",
			"AVDDesktop 已经在运行。\n\n请使用已打开的窗口；若窗口被隐藏，请在任务栏中恢复。")
		return
	} else {
		defer release()
	}

	err = wails.Run(&options.App{
		Title:     "AVDDesktop",
		Width:     1180,
		Height:    760,
		MinWidth:  960,
		MinHeight: 640,
		// 无边框窗口 + 自绘标题栏（对齐 assets/ 参考设计）
		Frameless: true,
		// 浅色主题（与设计规范一致，深色主题由前端 data-theme 切换）
		BackgroundColour: &options.RGBA{R: 245, G: 246, B: 248, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:     app.startup,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
		Bind: []interface{}{
			app,
			app.Env,
			app.Mirror,
			app.Sdk,
			app.Avd,
			app.Emulator,
			app.Adb,
			app.Settings,
			app.Diagnostics,
			app.Jobs,
			app.Window,
		},
	})

	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}
}
