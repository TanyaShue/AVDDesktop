package displayhost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
)

// framePump 把 MMAP 共享内存里的 RGBA 帧编码成 JPEG，并发布到本机 MJPEG 会话。
//
// 设备窗口是 WebView：它消费的是 JPEG（MJPEG），因此这里沿用主程序兼容路径同一套
// 「最新帧覆盖、永不阻塞生产者」语义——慢客户端只丢帧，不会拖慢 gRPC 流。
type framePump struct {
	source  *frameSource
	sink    func(jpeg []byte)
	quality int
	logf    func(format string, args ...any)
	quit    func(reason string)
}

// run 持续搬运画面，直到上下文结束或画面流断开。
func (p *framePump) run(ctx context.Context) {
	if p == nil || p.source == nil || p.sink == nil {
		return
	}
	for {
		frame, err := p.source.Next(ctx)
		if err != nil {
			p.finish(ctx, err)
			return
		}
		encoded, err := encodeJPEG(frame, p.quality)
		if err != nil {
			p.log("画面编码失败：%v", err)
			continue
		}
		p.sink(encoded)
	}
}

func (p *framePump) finish(ctx context.Context, err error) {
	switch {
	case ctx.Err() != nil, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// 进程正在退出，不需要任何提示。
	case errors.Is(err, io.EOF):
		p.log("模拟器画面流已结束")
		p.requestQuit("模拟器已停止")
	default:
		p.log("读取画面失败：%v", err)
		p.requestQuit("画面流中断")
	}
}

func (p *framePump) log(format string, args ...any) {
	if p.logf == nil {
		return
	}
	p.logf(format, args...)
}

func (p *framePump) requestQuit(reason string) {
	if p.quit == nil {
		return
	}
	p.quit(reason)
}

// encodeJPEG 把一帧 RGBA8888 编码成 JPEG。
//
// 帧像素由 frameSource 复制而来，编码过程不会与模拟器写入共享内存冲突；
// 这里按需裁剪，避免模拟器返回的尺寸大于请求尺寸时越界。
func encodeJPEG(frame Frame, quality int) ([]byte, error) {
	if frame.Width <= 0 || frame.Height <= 0 {
		return nil, fmt.Errorf("帧尺寸无效：%dx%d", frame.Width, frame.Height)
	}
	need, ok := checkedFrameBytes(frame.Width, frame.Height)
	if !ok || len(frame.Pix) < need {
		return nil, fmt.Errorf("帧像素不足：need=%d size=%d", need, len(frame.Pix))
	}
	if quality <= 0 || quality > 100 {
		quality = jpegQuality
	}
	img := &image.RGBA{
		Pix:    frame.Pix[:need],
		Stride: frame.Width * 4,
		Rect:   image.Rect(0, 0, frame.Width, frame.Height),
	}
	var buf bytes.Buffer
	buf.Grow(need / 8)
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
