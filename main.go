package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"AVDDesktop/internal/displayhost"
	"AVDDesktop/internal/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 同一可执行文件同时承载设备窗口辅助进程（WebView 版独立窗口）；必须在 NewApp 与
	// 单实例锁之前分流，否则辅助进程会和主进程抢同一个实例锁。
	if displayhost.IsHelperInvocation(os.Args[1:]) {
		if err := displayhost.RunHelper(os.Args[1:], assets); err != nil {
			log.Fatalf("设备窗口退出: %v", err)
		}
		return
	}

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
			app.Avd,
			app.Emulator,
			app.Display,
			app.Settings,
			app.Logs,
			app.Jobs,
			app.Window,
		},
	})

	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}
}
