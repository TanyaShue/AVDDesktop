// Package emulatorgrpc 封装模拟器的 gRPC 控制通道：连接、截图、画面流与触摸注入。
//
// 背景：模拟器以 `-no-window -grpc <port>` 启动后不再有 Qt 窗口，画面只能通过该通道取出。
// 端口被占用时模拟器的 gRPC 服务会静默不启动，因此 Dial 必须自己等待连接就绪，
// 并在上限内返回可判定的错误，而不是把失败推迟到第一次调用。
package emulatorgrpc

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/emulatorgrpc/pb"
)

const (
	// maxMessageSize 放宽单条 gRPC 消息上限：默认 4MB 装不下原生 1080×2400 的
	// RGBA8888 帧（约 10.37MB），会直接以 ResourceExhausted 失败。
	maxMessageSize = 32 << 20

	// dialTimeout 是 Dial 等待连接就绪的上限。地址不可达（端口未监听）时 gRPC 会一直重试，
	// 没有上限就会挂死调用方，因此这里再套一层较短的上限。
	dialTimeout = 3 * time.Second

	// 触点过期语义：按下/移动用 NEVER_EXPIRE(1)，抬起用 EVENT_EXPIRATION_UNSPECIFIED(0)。
	expirationNever = int32(1)
	expirationUp    = int32(0)

	// touchIdentifier 固定为 0：本应用只注入单指。
	touchIdentifier = int32(0)

	// 官方 proto（emulator/lib/emulator_controller.proto）规定：接触期间压力必须非零，
	// 且「结束触点」必须把压力置 0，否则该 identifier 永远不会被注销。
	// 取值 1/10/10 来自真机实测（模拟器 37.1.11.0）：pressure=1、touch_major=touch_minor=10
	// 即可正常下拉通知栏；本 build 的 pressure 是 int32，不要沿用老 float 版的量纲。
	touchPressureDown = int32(1)
	touchPressureUp   = int32(0)
	touchMajor        = int32(10)
	touchMinor        = int32(10)
)

// Frame 是一帧设备画面（RGBA8888，Pix 长度 = Width*Height*4）。
type Frame struct {
	Pix    []byte
	Width  int
	Height int
	Seq    uint32
}

// Client 是模拟器 gRPC 客户端。可并发调用，Close 后不可再使用。
type Client struct {
	conn *grpc.ClientConn
	cc   pb.EmulatorControllerClient
}

// Dial 连接模拟器 gRPC 端点（addr 形如 "127.0.0.1:8556"）；token 为空表示不认证。
//
// 建连非阻塞，但会等待 channel 进入 Ready（上限 dialTimeout 或 ctx 提前结束），
// 使「端口被占用导致服务未启动」这类问题在 Open 阶段就能给出明确的失败。
func Dial(ctx context.Context, addr, token string) (*Client, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMessageSize),
			grpc.MaxCallSendMsgSize(maxMessageSize),
		),
	}
	if token != "" {
		opts = append(opts,
			grpc.WithUnaryInterceptor(bearerUnaryInterceptor(token)),
			grpc.WithStreamInterceptor(bearerStreamInterceptor(token)),
		)
	}
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "无法建立模拟器 gRPC 连接", err.Error()).
			WithHint("请确认模拟器以「自定义 UI」方式启动，或查看应用日志（模块 emulator）")
	}

	waitCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if !conn.WaitForStateChange(waitCtx, state) {
			_ = conn.Close()
			return nil, domain.ErrDetail(domain.CodeProcessFailed,
				"连接模拟器 gRPC 端口超时", addr).
				WithHint("模拟器的 gRPC 端口可能未开启或被占用，请查看应用日志（模块 emulator）")
		}
	}
	return &Client{conn: conn, cc: pb.NewEmulatorControllerClient(conn)}, nil
}

// Close 释放连接。
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// ScreenshotPNG 取一张 PNG 截图（用于探测原生分辨率：模拟器的 Image.width/height 恒为 0，
// 分辨率只能从 PNG 的 IHDR 读出）。
func (c *Client) ScreenshotPNG(ctx context.Context) ([]byte, error) {
	if c == nil || c.cc == nil {
		return nil, domain.Err(domain.CodeProcessFailed, "模拟器 gRPC 客户端未初始化")
	}
	resp, err := c.cc.GetScreenshot(ctx, &pb.ImageFormat{Format: pb.ImageFormat_PNG})
	if err != nil {
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "获取模拟器截图失败", err.Error())
	}
	data := resp.GetImage()
	if len(data) == 0 {
		return nil, domain.Err(domain.CodeProcessFailed, "模拟器返回了空截图")
	}
	return data, nil
}

// StreamScreenshot 打开画面流：请求 width×height（均 > 0）或原生（0）的 RGBA8888 帧。
//
// 返回的 channel 在流结束或 ctx 取消后关闭，且不会泄漏 goroutine；每帧持有独立缓冲，
// 消费方可以安全地保留当前帧直到处理完成（下一帧不会覆写它）。
func (c *Client) StreamScreenshot(ctx context.Context, width, height int) (<-chan Frame, error) {
	if c == nil || c.cc == nil {
		return nil, domain.Err(domain.CodeProcessFailed, "模拟器 gRPC 客户端未初始化")
	}
	req := &pb.ImageFormat{Format: pb.ImageFormat_RGBA8888}
	if width > 0 && height > 0 {
		req.Width = uint32(width)
		req.Height = uint32(height)
	}
	stream, err := c.cc.StreamScreenshot(ctx, req)
	if err != nil {
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "打开模拟器画面流失败", err.Error())
	}

	out := make(chan Frame, 1)
	go func() {
		defer close(out)
		for {
			img, err := stream.Recv()
			if err != nil {
				// ctx 取消或流被服务端结束：正常收尾，由 defer 关闭 channel。
				return
			}
			frame, ok := decodeFrame(img, width, height)
			if !ok {
				continue
			}
			select {
			case out <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// SendTouch 注入触点（单指、identifier 固定 0）。
//
// release=true 表示抬起：该次事件的 expiration 与 pressure 同时归零，模拟器才会把
// identifier 注销并结束手势；按下/移动则用 expiration=NEVER_EXPIRE + 非零压力。
func (c *Client) SendTouch(ctx context.Context, x, y int32, release bool) error {
	if c == nil || c.cc == nil {
		return domain.Err(domain.CodeProcessFailed, "模拟器 gRPC 客户端未初始化")
	}
	expiration := expirationNever
	pressure := touchPressureDown
	if release {
		expiration = expirationUp
		pressure = touchPressureUp
	}
	_, err := c.cc.SendTouch(ctx, &pb.TouchEvent{
		Touches: []*pb.Touch{{
			X:          x,
			Y:          y,
			Identifier: touchIdentifier,
			Pressure:   pressure,
			TouchMajor: touchMajor,
			TouchMinor: touchMinor,
			Expiration: expiration,
		}},
	})
	if err != nil {
		return domain.ErrDetail(domain.CodeProcessFailed, "注入触摸事件失败", err.Error())
	}
	return nil
}

// decodeFrame 把一条流消息转成 Frame。
//
// 实测服务端返回的 Image.width/height 恒为 0，因此尺寸优先取请求值；两者都拿不到时该帧不可用。
func decodeFrame(img *pb.Image, reqWidth, reqHeight int) (Frame, bool) {
	w, h := int(img.GetWidth()), int(img.GetHeight())
	if w <= 0 || h <= 0 {
		w, h = reqWidth, reqHeight
	}
	data := img.GetImage()
	need := w * h * 4
	if w <= 0 || h <= 0 || len(data) < need {
		return Frame{}, false
	}
	pix := make([]byte, need)
	copy(pix, data[:need])
	return Frame{Pix: pix, Width: w, Height: h, Seq: img.GetSeq()}, true
}

// bearerUnaryInterceptor 为每次一元调用附加 `authorization: Bearer <token>` 元数据。
func bearerUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(withBearer(ctx, token), method, req, reply, cc, opts...)
	}
}

// bearerStreamInterceptor 为每次流调用附加 `authorization: Bearer <token>` 元数据。
func bearerStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(withBearer(ctx, token), desc, cc, method, opts...)
	}
}

func withBearer(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
