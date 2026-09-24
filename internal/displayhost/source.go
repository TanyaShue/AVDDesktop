package displayhost

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/emulatorgrpc"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sharedmem"
)

const (
	grpcWaitTimeout = 3 * time.Minute
	grpcRetryEvery  = time.Second
	dialAttempt     = 3 * time.Second
	adbKeyTimeout   = 15 * time.Second

	// defaultStreamWidth 是向模拟器请求的画面宽度上限。
	//
	// 模拟器会按 ImageFormat.width/height 自行缩放（见官方 proto：返回尺寸不会超过请求值），
	// 所以这里请求多大就只传输多大：540 宽实测约 28fps，更大分辨率下 gRPC 传输与 JPEG
	// 编码开销成倍增长，而设备窗口只需要几百像素宽就能看清内容。
	defaultStreamWidth = 540

	// jpegQuality 是发布给设备窗口的 MJPEG 编码质量。
	jpegQuality = 80
)

// keyCodes 是设备窗口可以注入的按键（adb keyevent 码）。
//
// 无窗口模式下模拟器的 gRPC 键盘注入无效，因此按键统一走 adb。
var keyCodes = map[string]int{
	"back":       4,   // KEYCODE_BACK
	"home":       3,   // KEYCODE_HOME
	"appswitch":  187, // KEYCODE_APP_SWITCH
	"volumeup":   24,  // KEYCODE_VOLUME_UP
	"volumedown": 25,  // KEYCODE_VOLUME_DOWN
	"dpadup":     19,
	"dpaddown":   20,
	"dpadleft":   21,
	"dpadright":  22,
	"enter":      66,
	"delete":     67,
	"power":      26,
}

// Frame 是一帧已经复制出共享内存的 top-down RGBA8888 画面。
type Frame struct {
	Pix    []byte
	Width  int
	Height int
	Seq    uint32
}

// frameSource 把共享内存中的最新 RGBA 帧复制成调用方可独立持有的帧。
//
// 复制看起来多余，但它是保证撕裂安全的关键：emulator 可以在客户端读取时开始写下一帧，
// 消费方不应直接持有仍可能被服务端改写的共享内存。
type frameSource struct {
	ctx    context.Context
	region *sharedmem.Region
	meta   <-chan emulatorgrpc.FrameMeta
	width  int
	height int
}

// Next 等待下一帧并复制像素。
func (s *frameSource) Next(ctx context.Context) (Frame, error) {
	if s == nil || s.region == nil {
		return Frame{}, fmt.Errorf("frame source not initialized")
	}
	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-s.ctx.Done():
		return Frame{}, s.ctx.Err()
	case meta, ok := <-s.meta:
		if !ok {
			return Frame{}, io.EOF
		}
		w, h := meta.Width, meta.Height
		if w <= 0 || h <= 0 {
			w, h = s.width, s.height
		}
		need, ok := checkedFrameBytes(w, h)
		if !ok {
			return Frame{}, fmt.Errorf("模拟器返回了无效帧尺寸 %dx%d", w, h)
		}
		raw := s.region.Bytes()
		if len(raw) < need {
			return Frame{}, fmt.Errorf("共享内存过小：need=%d size=%d", need, len(raw))
		}
		pix := make([]byte, need)
		copy(pix, raw[:need])
		return Frame{
			Pix:    pix,
			Width:  w,
			Height: h,
			Seq:    meta.Seq,
		}, nil
	}
}

func newRegion(instanceID string, width, height int) (*sharedmem.Region, error) {
	if width <= 0 || height <= 0 {
		return nil, domain.Err(domain.CodeInvalidArgument, "设备尺寸无效")
	}
	size, ok := checkedFrameBytes(width, height)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "设备帧尺寸溢出")
	}
	name := "device-window-" + sanitizeName(instanceID)
	return sharedmem.New(name, size)
}

func checkedFrameBytes(width, height int) (int, bool) {
	if width <= 0 || height <= 0 {
		return 0, false
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/4 || width*4 > maxInt/height {
		return 0, false
	}
	return width * height * 4, true
}

func sanitizeName(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 || strings.Trim(b.String(), "-") == "" {
		return "device"
	}
	return b.String()
}

// streamSize 把设备原生分辨率缩放到不超过 limit 的请求尺寸。
//
// 宽高都显式请求：官方 proto 保证返回尺寸不会超过请求值，因此共享内存可以按请求尺寸
// 一次性分配；设备旋转（返回更小的帧）也不会越界。
func streamSize(width, height, limit int) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	if limit <= 0 || width <= limit {
		return width, height
	}
	scaled := int(math.Round(float64(limit) * float64(height) / float64(width)))
	if scaled < 1 {
		scaled = 1
	}
	return limit, scaled
}

func pngSize(data []byte) (int, int, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, domain.ErrDetail(domain.CodeProcessFailed, "无法解析模拟器截图尺寸", err.Error())
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, domain.Err(domain.CodeProcessFailed, "模拟器截图尺寸无效")
	}
	return cfg.Width, cfg.Height, nil
}

func probeScreenshot(ctx context.Context, client *emulatorgrpc.Client, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		png, err := client.ScreenshotPNG(ctx)
		if err == nil {
			return png, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, domain.ErrDetail(domain.CodeProcessFailed, "等待模拟器首帧超时", lastErr.Error())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(grpcRetryEvery):
		}
	}
}

func dialWithRetryBounds(ctx context.Context, addr string, timeout, interval time.Duration) (*emulatorgrpc.Client, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		dialCtx, cancel := context.WithTimeout(ctx, dialAttempt)
		client, err := emulatorgrpc.Dial(dialCtx, addr, "")
		cancel()
		if err == nil {
			return client, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, domain.ErrDetail(domain.CodeProcessFailed, "连接模拟器画面通道超时", lastErr.Error())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// sendKey 把设备窗口的按键请求转成 adb keyevent。
//
// 刻意不继承调用方 context：按键是用户当下的一次操作，辅助进程正在退出时也应尽力送达。
func sendKey(cfg HelperConfig, key string) error {
	name := strings.ToLower(strings.TrimSpace(key))
	code, ok := keyCodes[name]
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "不支持的按键").
			WithHint("可用值：" + strings.Join(sortedKeyNames(), " / "))
	}
	ctx, cancel := context.WithTimeout(context.Background(), adbKeyTimeout)
	defer cancel()
	_, err := runADB(ctx, cfg, "shell", "input", "keyevent", strconv.Itoa(code))
	return err
}

func sortedKeyNames() []string {
	names := make([]string, 0, len(keyCodes))
	for name := range keyCodes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// runADB 执行一次带设备序列号的 adb 调用。
func runADB(ctx context.Context, cfg HelperConfig, args ...string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if strings.TrimSpace(cfg.ADBPath) == "" {
		return "", domain.Err(domain.CodeToolMissing, "未配置 ADB 路径")
	}
	if strings.TrimSpace(cfg.Serial) == "" {
		return "", domain.Err(domain.CodeInvalidArgument, "缺少设备序列号")
	}
	if len(args) == 0 {
		return "", domain.Err(domain.CodeInvalidArgument, "缺少 adb 参数")
	}
	full := append([]string{"-s", cfg.Serial}, args...)
	res, err := proc.Run(ctx, cfg.ADBPath, full, proc.Options{Timeout: adbKeyTimeout})
	if err != nil {
		return res.Combined(), err
	}
	if res.ExitCode != 0 {
		return res.Combined(), domain.ErrDetail(domain.CodeProcessFailed,
			fmt.Sprintf("adb %s 失败（退出码 %d）", args[0], res.ExitCode), res.Combined())
	}
	return res.Combined(), nil
}
