package displayhost

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"reflect"
	"testing"

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
