package displayhost

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"AVDDesktop/internal/display"
	"AVDDesktop/internal/emulatorgrpc"
)

// deviceEntry 是设备窗口使用的前端入口。
//
// 辅助进程与主进程共用同一份 embed 资源，用中间件把 "/" 重写到该入口，
// 于是同一个可执行文件可以按启动模式呈现两种完全不同的界面。
const deviceEntry = "/device.html"

// 设备窗口的几何常量：窗口高度 = 画面区 + 自绘标题栏 + 工具栏，
// 数值必须与 frontend/src/styles/device-window.css 中的高度保持一致。
const (
	deviceScreenHeight   = 720
	deviceTitleBarHeight = 40
	deviceToolbarHeight  = 48
	deviceMinWidth       = 220
	deviceMinHeight      = 320
	deviceMaxWidth       = 1200
)

// inputTimeout 是单次触摸注入的上限。
const inputTimeout = 5 * time.Second

// DeviceSession 是设备窗口订阅画面与映射输入所需的全部信息。
type DeviceSession struct {
	InstanceID   string `json:"instanceId"`
	AvdName      string `json:"avdName"`
	Serial       string `json:"serial"`
	URL          string `json:"url"`
	DeviceWidth  int    `json:"deviceWidth"`  // 设备原生分辨率（触摸映射基准）
	DeviceHeight int    `json:"deviceHeight"` // 设备原生分辨率（触摸映射基准）
	StreamWidth  int    `json:"streamWidth"`
	StreamHeight int    `json:"streamHeight"`
	Mode         string `json:"mode"` // webview
}

// DeviceWindow 是独立设备窗口：既是被 Wails 绑定的服务对象，也持有画面与输入的运行时依赖。
//
// 之所以把窗口放在辅助进程里：主进程只做控制面，画面解码/编码与 WebView 渲染都不占用
// 主窗口资源，也不随主窗口一起退出；窗口关闭 = 本进程退出，资源随之回收。
type DeviceWindow struct {
	cfg    HelperConfig
	client *emulatorgrpc.Client
	url    string

	nativeW int
	nativeH int
	streamW int
	streamH int

	base context.Context
	logf func(format string, args ...any)

	// onReady 在「窗口已创建」且「首帧已发布」后调用一次，用于向父进程握手。
	onReady func()

	windowUp   atomic.Bool
	firstFrame atomic.Bool
	readyOnce  sync.Once

	pinned      atomic.Bool
	quitPending atomic.Bool
	quitOnce    sync.Once

	ctxMu sync.RWMutex
	ctx   context.Context
}

// Session 返回画面地址与输入映射所需的信息（前端挂载时读取一次）。
func (d *DeviceWindow) Session() DeviceSession {
	return DeviceSession{
		InstanceID:   d.cfg.InstanceID,
		AvdName:      d.cfg.AVDName,
		Serial:       d.cfg.Serial,
		URL:          d.url,
		DeviceWidth:  d.nativeW,
		DeviceHeight: d.nativeH,
		StreamWidth:  d.streamW,
		StreamHeight: d.streamH,
		Mode:         "webview",
	}
}

// SendTouch 注入触摸：x/y 是画面区域内的归一化坐标（[0,1]）。
func (d *DeviceWindow) SendTouch(x, y float64, release bool) error {
	px, py := display.MapTouch(x, y, d.nativeW, d.nativeH)
	ctx, cancel := context.WithTimeout(d.baseContext(), inputTimeout)
	defer cancel()
	return d.client.SendTouch(ctx, px, py, release)
}

// SendKey 注入按键（back / home / appswitch / 音量 / 方向键等），走 adb。
func (d *DeviceWindow) SendKey(key string) error { return sendKey(d.cfg, key) }

// Screenshot 抓取设备当前画面并保存到软件目录下的 screenshots/，返回本地路径。
func (d *DeviceWindow) Screenshot() (string, error) {
	ctx, cancel := context.WithTimeout(d.baseContext(), screenshotTimeout)
	defer cancel()
	return captureScreenshot(ctx, d.cfg)
}

// Minimise 最小化窗口。
func (d *DeviceWindow) Minimise() {
	if ctx, ok := d.wailsContext(); ok {
		wailsruntime.WindowMinimise(ctx)
	}
}

// ToggleMaximise 切换最大化，返回切换后的状态。
func (d *DeviceWindow) ToggleMaximise() bool {
	ctx, ok := d.wailsContext()
	if !ok {
		return false
	}
	if wailsruntime.WindowIsMaximised(ctx) {
		wailsruntime.WindowUnmaximise(ctx)
		return false
	}
	wailsruntime.WindowMaximise(ctx)
	return true
}

// IsMaximised 返回窗口是否最大化。
func (d *DeviceWindow) IsMaximised() bool {
	ctx, ok := d.wailsContext()
	return ok && wailsruntime.WindowIsMaximised(ctx)
}

// SetAlwaysOnTop 设置窗口置顶，返回设置后的状态。
func (d *DeviceWindow) SetAlwaysOnTop(on bool) bool {
	d.pinned.Store(on)
	if ctx, ok := d.wailsContext(); ok {
		wailsruntime.WindowSetAlwaysOnTop(ctx, on)
	}
	return on
}

// IsAlwaysOnTop 返回窗口当前是否置顶。
func (d *DeviceWindow) IsAlwaysOnTop() bool { return d.pinned.Load() }

// Close 关闭设备窗口（只关画面，不停模拟器）。
func (d *DeviceWindow) Close() { d.RequestQuit("窗口已关闭") }

// Run 创建并运行设备窗口，直到窗口关闭或收到退出请求。
func (d *DeviceWindow) Run(assets fs.FS) error {
	if assets == nil {
		return errors.New("设备窗口缺少前端资源")
	}
	width, height := windowSize(d.nativeW, d.nativeH)
	d.log("创建设备窗口 %dx%d（画面 %dx%d / 流 %dx%d）", width, height, d.nativeW, d.nativeH, d.streamW, d.streamH)
	return wails.Run(&options.App{
		Title:            d.cfg.AVDName,
		Width:            width,
		Height:           height,
		MinWidth:         deviceMinWidth,
		MinHeight:        deviceMinHeight,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 11, G: 13, B: 17, A: 1},
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: deviceEntryMiddleware,
		},
		OnStartup:  d.onStartup,
		OnShutdown: d.onShutdown,
		Bind:       []interface{}{d},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	})
}

// deviceEntryMiddleware 把根路径重写到设备窗口入口。
//
// Wails 按「原始请求路径」判断是否注入运行时脚本（"/" 命中），因此重写只是换掉要读取的
// 文档，绑定能力不受影响；dev 模式下该中间件同样套在 Vite 反向代理之外。
func deviceEntryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || r.URL.Path == "/" {
			r.URL.Path = deviceEntry
		}
		next.ServeHTTP(w, r)
	})
}

// MarkFirstFrame 由帧搬运协程在首帧发布后调用。
func (d *DeviceWindow) MarkFirstFrame() {
	d.firstFrame.Store(true)
	d.maybeReady()
}

// RequestQuit 请求关闭设备窗口；可重复调用，只有第一次生效。
func (d *DeviceWindow) RequestQuit(reason string) {
	d.quitOnce.Do(func() {
		d.log("请求退出设备窗口：%s", reason)
		if ctx, ok := d.wailsContext(); ok {
			wailsruntime.Quit(ctx)
			return
		}
		// 窗口还没起来：先记下意图，OnStartup 会立刻关闭窗口。
		d.quitPending.Store(true)
	})
}

func (d *DeviceWindow) onStartup(ctx context.Context) {
	d.ctxMu.Lock()
	d.ctx = ctx
	d.ctxMu.Unlock()
	d.windowUp.Store(true)
	d.log("设备窗口已就绪")

	if d.quitPending.Load() {
		d.log("启动期间已收到退出请求，立即关闭窗口")
		wailsruntime.Quit(ctx)
		return
	}
	go d.ensureVisible(ctx)
	d.maybeReady()
}

// ensureVisible 补偿「隐藏方式启动」吞掉首次显示的问题。
//
// 父进程用 CREATE_NO_WINDOW + STARTUPINFO(SW_HIDE) 启动本进程时，Windows 只会把进程的
// 第一条 ShowWindow 换成 SW_HIDE，Wails 自己的首次显示因此不生效（窗口存在但不可见）。
// 这里再显式显示几次：无论两条调用谁在前，只要有一次落在首次之后窗口就会真正出现。
func (d *DeviceWindow) ensureVisible(ctx context.Context) {
	for _, delay := range []time.Duration{250 * time.Millisecond, 800 * time.Millisecond} {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		wailsruntime.WindowShow(ctx)
	}
}

func (d *DeviceWindow) onShutdown(context.Context) { d.log("设备窗口已关闭") }

func (d *DeviceWindow) maybeReady() {
	if !d.windowUp.Load() || !d.firstFrame.Load() {
		return
	}
	d.readyOnce.Do(func() {
		if d.onReady != nil {
			d.onReady()
		}
	})
}

func (d *DeviceWindow) wailsContext() (context.Context, bool) {
	d.ctxMu.RLock()
	defer d.ctxMu.RUnlock()
	return d.ctx, d.ctx != nil
}

func (d *DeviceWindow) baseContext() context.Context {
	if d.base != nil {
		return d.base
	}
	return context.Background()
}

func (d *DeviceWindow) log(format string, args ...any) {
	if d.logf == nil {
		return
	}
	d.logf(format, args...)
}

// windowSize 按设备宽高比给出窗口初始尺寸：画面区高度固定，宽度随宽高比变化，
// 再加上自绘标题栏与工具栏。
func windowSize(deviceWidth, deviceHeight int) (int, int) {
	chrome := deviceTitleBarHeight + deviceToolbarHeight
	if deviceWidth <= 0 || deviceHeight <= 0 {
		return deviceMinWidth * 2, deviceScreenHeight + chrome
	}
	screenWidth := int(math.Round(float64(deviceScreenHeight) * float64(deviceWidth) / float64(deviceHeight)))
	switch {
	case screenWidth < deviceMinWidth:
		screenWidth = deviceMinWidth
	case screenWidth > deviceMaxWidth:
		screenWidth = deviceMaxWidth
	}
	return screenWidth, deviceScreenHeight + chrome
}
