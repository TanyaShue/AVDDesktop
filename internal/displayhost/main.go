// Package displayhost 运行独立原生 Presenter 辅助进程。
//
// 主进程只负责启动、监测和回收本进程；设备画面数据面完全在辅助进程内完成：
// gRPC MMAP -> 共享内存 -> RGBA 帧 -> 原生 Presenter 窗口。
package displayhost

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/emulatorgrpc"
	"AVDDesktop/internal/presenter"
)

const (
	helperFlag           = "--display-host"
	presenterReadyMarker = "__AVDDESKTOP_PRESENTER_READY__"
)

// IsHelperInvocation 判断当前进程是否应以 native presenter 辅助进程启动。
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
}

// RunHelper 解析参数并阻塞运行 Presenter，直到窗口关闭、设备退出或进程被终止。
func RunHelper(args []string) error {
	cfg, err := ParseHelperConfig(args)
	if err != nil {
		return err
	}
	if !presenter.Supported() {
		return domain.Err(domain.CodeProcessFailed, "当前平台不支持原生设备窗口").
			WithHint("将使用应用内设备窗口作为兼容路径")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := dialWithRetry(ctx, cfg.GRPCAddress)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

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

	region, err := newRegion(cfg.InstanceID, width, height)
	if err != nil {
		return err
	}
	defer func() { _ = region.Close() }()

	stream, err := client.StreamScreenshotMMAP(ctx, width, height, region.Handle())
	if err != nil {
		return err
	}
	source := &frameSource{
		ctx:    ctx,
		region: region,
		meta:   stream,
		width:  width,
		height: height,
	}

	return presenter.Run(ctx, presenter.Config{
		Title: cfg.AVDName,
		OnReady: func() {
			// stdout 是给父进程的私有握手通道；父进程只识别这一行，不展示给用户。
			_, _ = fmt.Fprintln(os.Stdout, presenterReadyMarker)
		},
		InstanceID:   cfg.InstanceID,
		DeviceWidth:  width,
		DeviceHeight: height,
		Source:       source,
		SendTouch: func(x, y int32, release bool) error {
			return client.SendTouch(ctx, x, y, release)
		},
		SendKey: func(key string) error {
			return sendNavigationKey(ctx, cfg, key)
		},
	})
}

// ParseHelperConfig 解析辅助进程命令行参数。
func ParseHelperConfig(args []string) (HelperConfig, error) {
	var (
		cfg    HelperConfig
		rawCfg string
	)
	fs := flag.NewFlagSet("display-host", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&rawCfg, "config-json", "", "presenter helper config as JSON")
	fs.StringVar(&cfg.InstanceID, "instance-id", "", "emulator instance id")
	fs.StringVar(&cfg.AVDName, "avd-name", "", "AVD display name")
	fs.StringVar(&cfg.GRPCAddress, "grpc", "", "emulator gRPC address")
	fs.StringVar(&cfg.Serial, "serial", "", "adb serial")
	fs.StringVar(&cfg.ADBPath, "adb", "", "adb executable path")
	fs.IntVar(&cfg.DeviceWidth, "device-width", 0, "device native width")
	fs.IntVar(&cfg.DeviceHeight, "device-height", 0, "device native height")
	if err := fs.Parse(stripHelperFlag(args)); err != nil {
		return HelperConfig{}, err
	}
	if strings.TrimSpace(rawCfg) != "" {
		if err := json.Unmarshal([]byte(rawCfg), &cfg); err != nil {
			return HelperConfig{}, fmt.Errorf("解析 presenter 配置失败: %w", err)
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
	}
}
