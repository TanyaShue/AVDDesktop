package emulatorgrpc

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"AVDDesktop/internal/emulatorgrpc/pb"
)

// fakeController 是进程内的 EmulatorController：行为对齐真机实测（
// 流消息里的 Image.width/height 恒为 0，帧内容按 seq 填充便于校验顺序）。
type fakeController struct {
	pb.UnimplementedEmulatorControllerServer

	png    []byte
	frames int

	mu      sync.Mutex
	touches []*pb.TouchEvent
	auths   []string
}

func (f *fakeController) GetScreenshot(_ context.Context, req *pb.ImageFormat) (*pb.Image, error) {
	if req.GetFormat() != pb.ImageFormat_PNG {
		return nil, status.Error(codes.InvalidArgument, "只支持 PNG 截图")
	}
	return &pb.Image{Image: f.png}, nil
}

func (f *fakeController) StreamScreenshot(req *pb.ImageFormat, stream pb.EmulatorController_StreamScreenshotServer) error {
	if req.GetFormat() != pb.ImageFormat_RGBA8888 {
		return status.Error(codes.InvalidArgument, "只支持 RGBA8888 流")
	}
	w, h := int(req.GetWidth()), int(req.GetHeight())
	for i := 0; i < f.frames; i++ {
		frame := make([]byte, w*h*4)
		for j := range frame {
			frame[j] = byte(i + 1)
		}
		if err := stream.Send(&pb.Image{Image: frame, Seq: uint32(i)}); err != nil {
			return err
		}
	}
	// 真机是脏帧驱动：没有新帧时流一直挂着，直到客户端断开。
	<-stream.Context().Done()
	return nil
}

func (f *fakeController) SendTouch(ctx context.Context, ev *pb.TouchEvent) (*emptypb.Empty, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	f.touches = append(f.touches, ev)
	f.auths = append(f.auths, strings.Join(md.Get("authorization"), ","))
	f.mu.Unlock()
	return &emptypb.Empty{}, nil
}

func (f *fakeController) snapshot() ([]*pb.TouchEvent, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*pb.TouchEvent(nil), f.touches...), append([]string(nil), f.auths...)
}

// startFake 启动进程内的假模拟器（只监听 127.0.0.1 的随机端口）。
func startFake(t *testing.T, f *fakeController) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听本地端口失败: %v", err)
	}
	srv := grpc.NewServer(grpc.MaxRecvMsgSize(maxMessageSize))
	pb.RegisterEmulatorControllerServer(srv, f)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

func TestDial_UnreachablePortFailsFast(t *testing.T) {
	// 拿一个刚释放的端口，确保没有服务在监听。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听本地端口失败: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	started := time.Now()
	client, err := Dial(context.Background(), addr, "")
	if err == nil {
		_ = client.Close()
		t.Fatal("端口未监听时 Dial 应当失败")
	}
	if elapsed := time.Since(started); elapsed > dialTimeout+2*time.Second {
		t.Fatalf("Dial 失败耗时 %s，超过上限", elapsed)
	}
}

func TestScreenshotPNG_ReturnsBytesVerbatim(t *testing.T) {
	want := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")
	addr := startFake(t, &fakeController{png: want})

	client, err := Dial(context.Background(), addr, "")
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer func() { _ = client.Close() }()

	got, err := client.ScreenshotPNG(context.Background())
	if err != nil {
		t.Fatalf("ScreenshotPNG 失败: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("截图字节不一致：got %q want %q", got, want)
	}
}

func TestStreamScreenshot_DeliversFramesInOrderThenCloses(t *testing.T) {
	const (
		width  = 4
		height = 3
		frames = 3
	)
	addr := startFake(t, &fakeController{frames: frames})

	client, err := Dial(context.Background(), addr, "")
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.StreamScreenshot(ctx, width, height)
	if err != nil {
		cancel()
		t.Fatalf("StreamScreenshot 失败: %v", err)
	}

	for i := 0; i < frames; i++ {
		select {
		case frame, ok := <-stream:
			if !ok {
				t.Fatalf("第 %d 帧之前 channel 已关闭", i)
			}
			if frame.Width != width || frame.Height != height {
				t.Fatalf("第 %d 帧尺寸 = %dx%d，want %dx%d", i, frame.Width, frame.Height, width, height)
			}
			if frame.Seq != uint32(i) {
				t.Fatalf("第 %d 帧序号 = %d", i, frame.Seq)
			}
			if len(frame.Pix) != width*height*4 {
				t.Fatalf("第 %d 帧字节数 = %d", i, len(frame.Pix))
			}
			for _, b := range frame.Pix {
				if b != byte(i+1) {
					t.Fatalf("第 %d 帧内容错误：出现字节 %d", i, b)
				}
			}
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatalf("等待第 %d 帧超时", i)
		}
	}

	// ctx 取消后 channel 必须关闭（且 goroutine 收尾）。
	cancel()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-stream:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("ctx 取消后 channel 未关闭")
		}
	}
}

// 原生 1080×2400 的 RGBA8888 帧约 10.37MB，超过 gRPC 默认的 4MB 接收上限；
// 上限没放宽的话这里会直接以 ResourceExhausted 失败。
func TestStreamScreenshot_AcceptsNativeSizeFrames(t *testing.T) {
	const (
		width  = 1080
		height = 2400
	)
	addr := startFake(t, &fakeController{frames: 1})

	client, err := Dial(context.Background(), addr, "")
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.StreamScreenshot(ctx, width, height)
	if err != nil {
		t.Fatalf("StreamScreenshot 失败: %v", err)
	}

	select {
	case frame, ok := <-stream:
		if !ok {
			t.Fatal("未收到原生尺寸帧")
		}
		if want := width * height * 4; len(frame.Pix) != want {
			t.Fatalf("帧字节数 = %d，want %d", len(frame.Pix), want)
		}
		if frame.Width != width || frame.Height != height {
			t.Fatalf("帧尺寸 = %dx%d，want %dx%d", frame.Width, frame.Height, width, height)
		}
		if frame.Pix[0] != 1 || frame.Pix[len(frame.Pix)-1] != 1 {
			t.Fatal("原生尺寸帧内容不完整")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("等待原生尺寸帧超时")
	}
}

func TestSendTouch_RecordsEvents(t *testing.T) {
	fake := &fakeController{}
	addr := startFake(t, fake)
	client, err := Dial(context.Background(), addr, "")
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.SendTouch(ctx, 540, 5, false); err != nil {
		t.Fatalf("按下失败: %v", err)
	}
	if err := client.SendTouch(ctx, 540, 1200, true); err != nil {
		t.Fatalf("抬起失败: %v", err)
	}

	events, _ := fake.snapshot()
	if len(events) != 2 {
		t.Fatalf("服务端收到 %d 个触摸事件，want 2", len(events))
	}
	down, up := events[0], events[1]

	if len(down.GetTouches()) != 1 || len(up.GetTouches()) != 1 {
		t.Fatalf("触点数量不符：%d / %d", len(down.GetTouches()), len(up.GetTouches()))
	}
	dt, ut := down.GetTouches()[0], up.GetTouches()[0]

	if dt.GetX() != 540 || dt.GetY() != 5 {
		t.Fatalf("按下坐标 = (%d,%d)，want (540,5)", dt.GetX(), dt.GetY())
	}
	if ut.GetX() != 540 || ut.GetY() != 1200 {
		t.Fatalf("抬起坐标 = (%d,%d)，want (540,1200)", ut.GetX(), ut.GetY())
	}
	if dt.GetIdentifier() != 0 || ut.GetIdentifier() != 0 {
		t.Fatalf("identifier = %d / %d，want 0", dt.GetIdentifier(), ut.GetIdentifier())
	}
	if dt.GetExpiration() != 1 {
		t.Fatalf("按下 expiration = %d，want 1（NEVER_EXPIRE）", dt.GetExpiration())
	}
	if ut.GetExpiration() != 0 {
		t.Fatalf("抬起 expiration = %d，want 0", ut.GetExpiration())
	}
	// 抬起必须同时把压力归零，否则模拟器不会注销该 identifier。
	if dt.GetPressure() == 0 {
		t.Fatal("按下压力不能为 0")
	}
	if ut.GetPressure() != 0 {
		t.Fatalf("抬起压力 = %d，want 0", ut.GetPressure())
	}
}

func TestDial_SendsBearerToken(t *testing.T) {
	fake := &fakeController{}
	addr := startFake(t, fake)
	client, err := Dial(context.Background(), addr, "s3cret")
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.SendTouch(context.Background(), 1, 2, false); err != nil {
		t.Fatalf("SendTouch 失败: %v", err)
	}
	_, auths := fake.snapshot()
	if len(auths) != 1 || auths[0] != "Bearer s3cret" {
		t.Fatalf("服务端收到的 authorization = %v，want [Bearer s3cret]", auths)
	}
}
