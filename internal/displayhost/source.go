package displayhost

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"io"
	"strconv"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/emulatorgrpc"
	"AVDDesktop/internal/presenter"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sharedmem"
)

const (
	grpcWaitTimeout = 3 * time.Minute
	grpcRetryEvery  = time.Second
	dialAttempt     = 3 * time.Second
	adbKeyTimeout   = 15 * time.Second
)

// frameSource 把共享内存中的最新 RGBA 帧复制成 Presenter 可独立持有的帧。
//
// 复制看起来多余，但它是保证撕裂安全的关键：emulator 可以在客户端读取时开始写下一帧，
// Presenter 不应直接持有仍可能被服务端改写的共享内存。
type frameSource struct {
	ctx    context.Context
	region *sharedmem.Region
	meta   <-chan emulatorgrpc.FrameMeta
	width  int
	height int
}

// Next 等待下一帧并复制像素。
func (s *frameSource) Next(ctx context.Context) (presenter.Frame, error) {
	if s == nil || s.region == nil {
		return presenter.Frame{}, fmt.Errorf("frame source not initialized")
	}
	select {
	case <-ctx.Done():
		return presenter.Frame{}, ctx.Err()
	case <-s.ctx.Done():
		return presenter.Frame{}, s.ctx.Err()
	case meta, ok := <-s.meta:
		if !ok {
			return presenter.Frame{}, io.EOF
		}
		w, h := meta.Width, meta.Height
		if w <= 0 || h <= 0 {
			w, h = s.width, s.height
		}
		need, ok := checkedFrameBytes(w, h)
		if !ok {
			return presenter.Frame{}, fmt.Errorf("模拟器返回了无效帧尺寸 %dx%d", w, h)
		}
		raw := s.region.Bytes()
		if len(raw) < need {
			return presenter.Frame{}, fmt.Errorf("共享内存过小：need=%d size=%d", need, len(raw))
		}
		pix := make([]byte, need)
		copy(pix, raw[:need])
		return presenter.Frame{
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
	name := "presenter-" + sanitizeName(instanceID)
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

func sendNavigationKey(ctx context.Context, cfg HelperConfig, key string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	codes := map[string]int{"back": 4, "home": 3, "appswitch": 187}
	code, ok := codes[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "不支持的导航键")
	}
	if strings.TrimSpace(cfg.ADBPath) == "" {
		return domain.Err(domain.CodeToolMissing, "未配置 ADB 路径")
	}
	keyCtx, cancel := context.WithTimeout(context.Background(), adbKeyTimeout)
	defer cancel()
	_, err := runADB(keyCtx, cfg.ADBPath, cfg.Serial, code)
	return err
}

func runADB(ctx context.Context, adbPath, serial string, code int) (string, error) {
	args := []string{"-s", serial, "shell", "input", "keyevent", strconv.Itoa(code)}
	res, err := proc.Run(ctx, adbPath, args, proc.Options{Timeout: adbKeyTimeout})
	if err != nil {
		return res.Combined(), err
	}
	if res.ExitCode != 0 {
		return res.Combined(), domain.ErrDetail(domain.CodeProcessFailed,
			fmt.Sprintf("adb 发送导航键失败（退出码 %d）", res.ExitCode), res.Combined())
	}
	return res.Combined(), nil
}
