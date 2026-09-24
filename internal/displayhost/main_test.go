package displayhost

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/emulatorgrpc"
)

func TestCheckedFrameBytes(t *testing.T) {
	got, ok := checkedFrameBytes(720, 1280)
	if !ok || got != 720*1280*4 {
		t.Fatalf("checkedFrameBytes = %d,%v", got, ok)
	}
	if _, ok := checkedFrameBytes(0, 10); ok {
		t.Fatal("zero width must be rejected")
	}
	maxInt := int(^uint(0) >> 1)
	if _, ok := checkedFrameBytes(maxInt/2, 4); ok {
		t.Fatal("overflowing dimensions must be rejected")
	}
}

func TestSanitizeName(t *testing.T) {
	if got := sanitizeName("emu-5554_1"); got != "emu-5554_1" {
		t.Fatalf("sanitizeName = %q", got)
	}
	if got := sanitizeName(`emu 5554:/x`); got != "emu-5554--x" {
		t.Fatalf("sanitizeName = %q", got)
	}
	if got := sanitizeName("中文"); got != "device" {
		t.Fatalf("sanitizeName fallback = %q", got)
	}
}

func TestParseHelperConfigRejectsMissingRequiredFields(t *testing.T) {
	if _, err := ParseHelperConfig([]string{"--display-host"}); err == nil {
		t.Fatal("missing instance id must fail")
	}
	if _, err := ParseHelperConfig([]string{"--display-host", "--instance-id", "i1"}); err == nil {
		t.Fatal("missing grpc address must fail")
	}
}

func TestHelperArgsRoundTrip(t *testing.T) {
	want := HelperConfig{
		InstanceID:  "emu-5554-1",
		AVDName:     "MyDevice",
		GRPCAddress: "127.0.0.1:8554",
		Serial:      "emulator-5554",
		ADBPath:     `C:\sdk\platform-tools\adb.exe`,
		StreamWidth: 540,
		WatchParent: true,
	}
	args := HelperArgs(want)
	got, err := ParseHelperConfig(args)
	if err != nil {
		t.Fatalf("ParseHelperConfig(HelperArgs()) failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestStreamWidthLimit(t *testing.T) {
	if got := (HelperConfig{}).streamWidthLimit(); got != defaultStreamWidth {
		t.Fatalf("streamWidthLimit() = %d, want %d", got, defaultStreamWidth)
	}
	if got := (HelperConfig{StreamWidth: 720}).streamWidthLimit(); got != 720 {
		t.Fatalf("streamWidthLimit() = %d, want 720", got)
	}
}

func TestPNGSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 12, 34))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	w, h, err := pngSize(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if w != 12 || h != 34 {
		t.Fatalf("pngSize = %dx%d", w, h)
	}
}

func TestStreamSizeScaledByEmulatorBounds(t *testing.T) {
	cases := []struct {
		name                 string
		width, height, limit int
		wantW, wantH         int
	}{
		{"宽高都在上限内保持原样", 540, 960, 540, 540, 960},
		{"超宽按比例缩放", 1080, 2400, 540, 540, 1200},
		{"窄设备不放大", 320, 640, 540, 320, 640},
		{"尺寸无效返回零值", 0, 640, 540, 0, 0},
	}
	for _, tc := range cases {
		gotW, gotH := streamSize(tc.width, tc.height, tc.limit)
		if gotW != tc.wantW || gotH != tc.wantH {
			t.Fatalf("%s: streamSize(%d,%d,%d) = %dx%d, want %dx%d",
				tc.name, tc.width, tc.height, tc.limit, gotW, gotH, tc.wantW, tc.wantH)
		}
	}
}

func TestWindowSizeFollowsAspectRatio(t *testing.T) {
	w, h := windowSize(720, 1280)
	if w != 405 || h != deviceScreenHeight+deviceTitleBarHeight+deviceToolbarHeight {
		t.Fatalf("windowSize(720,1280) = %dx%d", w, h)
	}
	// 横屏设备：宽度必须随宽高比变大，高度保持窗口骨架高度。
	lw, lh := windowSize(2400, 1080)
	if lw <= w || lh != h {
		t.Fatalf("windowSize(2400,1080) = %dx%d", lw, lh)
	}
	// 未知尺寸时给一个可用的兜底窗口，不能是 0。
	uw, uh := windowSize(0, 0)
	if uw <= 0 || uh <= 0 {
		t.Fatalf("windowSize(0,0) = %dx%d", uw, uh)
	}
}

func TestEncodeJPEGProducesFrame(t *testing.T) {
	pix := []byte{
		255, 0, 0, 255, 0, 255, 0, 255,
		0, 0, 255, 255, 255, 255, 255, 255,
	}
	raw, err := encodeJPEG(Frame{Pix: pix, Width: 2, Height: 2}, 80)
	if err != nil {
		t.Fatalf("encodeJPEG: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.Width != 2 || cfg.Height != 2 {
		t.Fatalf("encoded frame = %dx%d, want 2x2", cfg.Width, cfg.Height)
	}
	if _, err := encodeJPEG(Frame{Pix: pix, Width: 0, Height: 2}, 80); err == nil {
		t.Fatal("zero-sized frame must fail")
	}
	if _, err := encodeJPEG(Frame{Pix: pix[:4], Width: 2, Height: 2}, 80); err == nil {
		t.Fatal("short pixel buffer must fail")
	}
}

func TestFrameSourceCopiesSharedMemory(t *testing.T) {
	region, err := newRegion("test-copy", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = region.Close() }()
	copy(region.Bytes(), []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})

	meta := make(chan emulatorgrpc.FrameMeta, 1)
	meta <- emulatorgrpc.FrameMeta{Width: 2, Height: 2, Seq: 7}
	close(meta)
	src := &frameSource{ctx: context.Background(), region: region, meta: meta, width: 2, height: 2}
	frame, err := src.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if frame.Seq != 7 || frame.Width != 2 || frame.Height != 2 {
		t.Fatalf("frame metadata = %+v", frame)
	}
	// 返回值必须是独立副本：服务端继续写共享内存不能改变已交付帧。
	copy(region.Bytes(), make([]byte, 16))
	if !reflect.DeepEqual(frame.Pix, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}) {
		t.Fatalf("frame was not copied: %v", frame.Pix)
	}
}

func TestDeviceEntryMiddlewareRewritesRootOnly(t *testing.T) {
	var gotPath string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	})
	handler := deviceEntryMiddleware(next)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if gotPath != deviceEntry {
		t.Fatalf(`GET / 转发路径 = %q, want %q`, gotPath, deviceEntry)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/index.js", nil))
	if gotPath != "/assets/index.js" {
		t.Fatalf("静态资源路径不应被改写，得到 %q", gotPath)
	}
}

func TestSendKeyRejectsUnknownKeyWithoutAdb(t *testing.T) {
	err := sendKey(HelperConfig{}, "nope")
	if err == nil {
		t.Fatal("unknown key must fail")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeInvalidArgument {
		t.Fatalf("sendKey error = %v, want invalid argument", err)
	}
	// 已知按键但未配置 adb：必须报工具缺失而不是启动进程。
	err = sendKey(HelperConfig{}, "back")
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeToolMissing {
		t.Fatalf("sendKey(back) error = %v, want tool missing", err)
	}
}
