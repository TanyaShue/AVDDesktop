package service

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"AVDDesktop/internal/emulatorgrpc"
)

// 帧编码是「模拟器 RGBA 原生帧 → WebView 能显示的 JPEG」的关键一步，
// 尺寸、行距或像素顺序写错都会在真机上表现为花屏，因此单独验证。
func TestEncodeFrame_ProducesDecodableJPEG(t *testing.T) {
	const (
		w = 16
		h = 16
	)
	pix := make([]byte, w*h*4)
	// 帧是正向（top-down）的——真机实测与同屏 PNG 逐行比对 diff=0.00，无需翻转。
	// 造一帧「画面顶部纯红、底部纯蓝」，JPEG 有损，取色时避开交界。
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			if y < h/2 {
				copy(pix[off:off+4], []byte{255, 0, 0, 255}) // 画面顶部：红
			} else {
				copy(pix[off:off+4], []byte{0, 0, 255, 255}) // 画面底部：蓝
			}
		}
	}

	data, err := encodeFrame(emulatorgrpc.Frame{Pix: pix, Width: w, Height: h})
	if err != nil {
		t.Fatalf("encodeFrame 失败: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("编码结果不是合法 JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("JPEG 尺寸 = %dx%d，want %dx%d", b.Dx(), b.Dy(), w, h)
	}

	if r, g, bl, _ := img.At(2, 2).RGBA(); r < 0x8000 || g > 0x4000 || bl > 0x4000 {
		t.Fatalf("上半屏像素 = (%d,%d,%d)，want 红", r>>8, g>>8, bl>>8)
	}
	if r, g, bl, _ := img.At(w-3, h-3).RGBA(); bl < 0x8000 || r > 0x4000 || g > 0x4000 {
		t.Fatalf("下半屏像素 = (%d,%d,%d)，want 蓝", r>>8, g>>8, bl>>8)
	}
}

func TestPNGSize_ReadsIHDR(t *testing.T) {
	var buf bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 1080, 2400))
	src.Set(0, 0, color.RGBA{R: 1, A: 255})
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("构造测试 PNG 失败: %v", err)
	}

	w, h, err := pngSize(buf.Bytes())
	if err != nil {
		t.Fatalf("pngSize 失败: %v", err)
	}
	if w != 1080 || h != 2400 {
		t.Fatalf("pngSize = %dx%d，want 1080x2400", w, h)
	}

	if _, _, err := pngSize([]byte("not a png")); err == nil {
		t.Fatal("非法 PNG 应当报错")
	}
}

// 连接中（等待 gRPC 就绪可达数分钟）用户关掉设备窗口时，Open 必须放弃登记会话，
// 否则会留下一个无人订阅的画面流：gRPC 连接与帧循环会一直跑到模拟器退出，界面上完全无感。
//
// 判定用「关闭计数」而不是布尔标记：连接期间可能同时有多个 Open 在飞（React StrictMode
// 会重复执行挂载副作用），布尔标记被其中一个消费掉就会放过另一个。
func TestAbandonedReason_CloseDuringConnect(t *testing.T) {
	s := &DisplayService{items: map[string]*displayItem{}, closeEpoch: map[string]uint64{}}

	first := s.closeEpoch["i1"]  // Open#1 开始连接
	second := s.closeEpoch["i1"] // Open#2（并发/StrictMode 兄弟）开始连接

	if got := s.abandonedReason("i1", first); got != "" {
		t.Fatalf("未关闭时不应作废，实际 %q", got)
	}

	if err := s.Close("i1"); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if got := s.abandonedReason("i1", first); got == "" {
		t.Fatal("关闭后正在连接的 Open 必须作废")
	}
	if got := s.abandonedReason("i1", second); got == "" {
		t.Fatal("关闭后并发的另一个 Open 也必须作废（布尔标记会在这里漏掉）")
	}

	// 关闭之后重新打开：新的连接不应被上一次关闭作废。
	third := s.closeEpoch["i1"]
	if got := s.abandonedReason("i1", third); got != "" {
		t.Fatalf("重新打开不应被旧的关闭作废，实际 %q", got)
	}

	// 应用退出：任何连接都必须作废。
	s.closed = true
	if got := s.abandonedReason("i1", third); got == "" {
		t.Fatal("应用退出后 Open 必须作废")
	}
}
