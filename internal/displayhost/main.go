// Package displayhost 运行独立设备窗口辅助进程（同一个可执行文件的 --display-host 模式）。
//
// 数据面完全在本进程内完成：gRPC MMAP -> 共享内存 -> JPEG -> 本机 MJPEG 会话 -> WebView。
// 主进程只负责启动、监测与回收，不接触任何像素。
package displayhost

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"AVDDesktop/internal/display"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/emulatorgrpc"
)

const (
	helperFlag = "--display-host"
	// readyMarker 是给父进程的私有握手行：窗口与首帧都就绪后才会打印，
	// 父进程据此把 Open 视为成功，避免窗口还没画面就报“已打开”。
	readyMarker = "__AVDDESKTOP_WINDOW_READY__"
)

// IsHelperInvocation 判断当前进程是否应以设备窗口辅助进程启动。
func IsHelperInvocation(args []string) bool {
	for _, arg := range args {
		if arg == helperFlag || arg == "--display-host=true" {
			return true
		}
	}
	return false
}

// HelperConfig 是主进程传给辅助进程的启动参数。
type HelperConfig struct {
	InstanceID   string `json:"instanceId"`
	AVDName      string `json:"avdName"`
	GRPCAddress  string `json:"grpcAddress"`
	Serial       string `json:"serial"`
	ADBPath      string `json:"adbPath"`
	DeviceWidth  int    `json:"deviceWidth"`
	DeviceHeight int    `json:"deviceHeight"`
	// StreamWidth 是画面流宽度上限；0 表示使用 defaultStreamWidth。
	StreamWidth int `json:"streamWidth"`
	// WatchParent 为真时，父进程关闭 stdin（正常退出、崩溃或被强杀）即视为退出请求。
	WatchParent bool `json:"watchParent"`
}

// streamWidthLimit 返回本次会话请求的画面宽度上限。
func (c HelperConfig) streamWidthLimit() int {
	if c.StreamWidth > 0 {
		return c.StreamWidth
	}
	return defaultStreamWidth
}

// pumpDrainTimeout 是收尾时等待帧搬运协程退出的上限。
//
// 有界等待是必须的：搬运协程可能正拿着共享内存的切片做拷贝，直接 unmap 会读到非法内存。
const pumpDrainTimeout = 2 * time.Second

// RunHelper 解析参数并阻塞运行设备窗口，直到窗口关闭、设备退出或进程被终止。
func RunHelper(args []string, assets fs.FS) error {
	cfg, err := ParseHelperConfig(args)
	if err != nil {
		return err
	}
	if assets == nil {
		return domain.Err(domain.CodeProcessFailed, "设备窗口缺少前端资源")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, args ...any) { _, _ = fmt.Fprintf(os.Stdout, "[device] "+format+"\n", args...) }

	if cfg.WatchParent {
		go watchParentExit(stop, logf)
	}

	client, err := dialWithRetry(ctx, cfg.GRPCAddress)
	if err != nil {
		return err
	}

	width, height := cfg.DeviceWidth, cfg.DeviceHeight
	if width <= 0 || height <= 0 {
		png, shotErr := probeScreenshot(ctx, client, grpcWaitTimeout)
		if shotErr != nil {
			return shotErr
		}
		width, height, err = pngSize(png)
		if err != nil {
			return err
		}
	}

	streamW, streamH := streamSize(width, height, cfg.streamWidthLimit())

	frames, err := display.NewServer(nil)
	if err != nil {
		_ = client.Close()
		return err
	}

	region, err := newRegion(cfg.InstanceID, streamW, streamH)
	if err != nil {
		_ = frames.Close()
		_ = client.Close()
		return err
	}

	// pumpDone 在帧搬运协程退出后关闭；收尾时要等它，见下面的清理顺序。
	pumpDone := make(chan struct{})

	// 收尾顺序很关键：先取消上下文并等帧搬运协程退出（否则它可能在 unmap 之后读取映射内存），
	// 再断开 gRPC 让模拟器释放映射，最后删除映射文件。反过来会在 Windows 上留下 %TEMP% 残留文件。
	defer func() {
		stop()
		select {
		case <-pumpDone:
		case <-time.After(pumpDrainTimeout):
		}
		_ = client.Close()
		_ = frames.Close()
		_ = region.Close()
	}()

	stream, err := client.StreamScreenshotMMAP(ctx, streamW, streamH, region.Handle())
	if err != nil {
		return err
	}

	session := frames.Session(cfg.InstanceID)
	logf("设备 %dx%d → 画面流 %dx%d，监听 %s", width, height, streamW, streamH, frames.Addr())

	window := &DeviceWindow{
		cfg:     cfg,
		client:  client,
		url:     frames.URL(cfg.InstanceID),
		nativeW: width,
		nativeH: height,
		streamW: streamW,
		streamH: streamH,
		base:    ctx,
		logf:    logf,
	}
	window.onReady = func() {
		// stdout 是给父进程的私有握手通道：这一行同时承担「就绪」与「会话信息上报」，
		// 父进程据此把 URL / 分辨率补进 DisplaySession（主进程拿不到子进程的随机端口）。
		if payload, err := json.Marshal(window.Session()); err == nil {
			_, _ = fmt.Fprintf(os.Stdout, "%s %s\n", readyMarker, payload)
			return
		}
		_, _ = fmt.Fprintln(os.Stdout, readyMarker)
	}

	pump := &framePump{
		source: &frameSource{
			ctx:    ctx,
			region: region,
			meta:   stream,
			width:  streamW,
			height: streamH,
		},
		sink: func(jpeg []byte) {
			session.Publish(jpeg)
			window.MarkFirstFrame()
		},
		quality: jpegQuality,
		logf:    logf,
		quit:    window.RequestQuit,
	}
	go func() {
		defer close(pumpDone)
		pump.run(ctx)
	}()

	// 进程级退出（父进程断连 / SIGINT / 画面流结束）统一收敛到关窗口：
	// wails.Run 返回后 defer 会释放共享内存与映射文件。
	go func() {
		<-ctx.Done()
		window.RequestQuit("会话结束")
	}()

	return window.Run(assets)
}

// watchParentExit 在父进程关闭 stdin 管道后触发退出。
//
// 父进程正常退出、崩溃或被强杀都会关闭管道，因此设备窗口不会变成孤儿进程；
// 同时让本进程走正常收尾路径（关闭窗口、释放共享内存、删除映射文件）。
func watchParentExit(cancel context.CancelFunc, logf func(format string, args ...any)) {
	_, _ = io.Copy(io.Discard, os.Stdin)
	logf("父进程已退出，设备窗口关闭")
	cancel()
}

// ParseHelperConfig 解析辅助进程命令行参数。
func ParseHelperConfig(args []string) (HelperConfig, error) {
	var (
		cfg    HelperConfig
		rawCfg string
	)
	fs := flag.NewFlagSet("display-host", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&rawCfg, "config-json", "", "device window helper config as JSON")
	fs.StringVar(&cfg.InstanceID, "instance-id", "", "emulator instance id")
	fs.StringVar(&cfg.AVDName, "avd-name", "", "AVD display name")
	fs.StringVar(&cfg.GRPCAddress, "grpc", "", "emulator gRPC address")
	fs.StringVar(&cfg.Serial, "serial", "", "adb serial")
	fs.StringVar(&cfg.ADBPath, "adb", "", "adb executable path")
	fs.IntVar(&cfg.DeviceWidth, "device-width", 0, "device native width")
	fs.IntVar(&cfg.DeviceHeight, "device-height", 0, "device native height")
	fs.IntVar(&cfg.StreamWidth, "stream-width", 0, "max stream width (0 = default)")
	fs.BoolVar(&cfg.WatchParent, "watch-parent", false, "quit when the parent closes stdin")
	if err := fs.Parse(stripHelperFlag(args)); err != nil {
		return HelperConfig{}, err
	}
	if strings.TrimSpace(rawCfg) != "" {
		if err := json.Unmarshal([]byte(rawCfg), &cfg); err != nil {
			return HelperConfig{}, fmt.Errorf("解析设备窗口配置失败: %w", err)
		}
	}
	if strings.TrimSpace(cfg.InstanceID) == "" {
		return HelperConfig{}, fmt.Errorf("缺少 instance-id")
	}
	if strings.TrimSpace(cfg.GRPCAddress) == "" {
		return HelperConfig{}, fmt.Errorf("缺少 grpc 地址")
	}
	if cfg.DeviceWidth < 0 || cfg.DeviceHeight < 0 {
		return HelperConfig{}, fmt.Errorf("设备尺寸不能为负数")
	}
	if cfg.StreamWidth < 0 {
		return HelperConfig{}, fmt.Errorf("画面流宽度不能为负数")
	}
	return cfg, nil
}

func stripHelperFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == helperFlag || arg == "--display-host=true" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func dialWithRetry(ctx context.Context, addr string) (*emulatorgrpc.Client, error) {
	return dialWithRetryBounds(ctx, addr, grpcWaitTimeout, grpcRetryEvery)
}

// HelperArgs 把配置编码为同一可执行文件可解析的参数。
func HelperArgs(cfg HelperConfig) []string {
	raw, _ := json.Marshal(cfg)
	return []string{
		helperFlag,
		"--config-json", string(raw),
		"--instance-id", cfg.InstanceID,
		"--avd-name", cfg.AVDName,
		"--grpc", cfg.GRPCAddress,
		"--serial", cfg.Serial,
		"--adb", cfg.ADBPath,
		"--device-width", strconv.Itoa(cfg.DeviceWidth),
		"--device-height", strconv.Itoa(cfg.DeviceHeight),
		"--stream-width", strconv.Itoa(cfg.StreamWidth),
		"--watch-parent=" + strconv.FormatBool(cfg.WatchParent),
	}
}
