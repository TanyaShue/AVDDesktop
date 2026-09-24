package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/display"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/emulatorgrpc"
)

// 设备画面（自定义 UI）相关常量。
const (
	// maxStreamWidth 是推给 WebView 的最大宽度：540 宽实测约 28fps，
	// 再宽（原生 1080）只有约 10fps，且 JPEG 体积成倍增长。
	maxStreamWidth = 540
	// jpegQuality 是 MJPEG 的编码质量。
	jpegQuality = 80

	// grpcWaitTimeout / grpcRetryEvery 是等待模拟器 gRPC 就绪的上限与间隔。
	// 端口被占用时模拟器的 gRPC 服务会静默不启动，因此必须自行轮询并在上限内失败。
	//
	// 上限取 3 分钟：冷启动（含快照失效后重新开机）时模拟器要几十秒才交出第一张可截图的画面，
	// 实测首次 Open 在 30s 上限下会误报「等待画面通道就绪超时」，而同一实例再过约 10s 就能出图。
	// 实例提前退出时循环会立刻结束，因此放宽上限不会让失败场景变慢。
	grpcWaitTimeout = 3 * time.Minute
	grpcRetryEvery  = time.Second
	// dialAttempt 是单次拨号的上限（失败后按 grpcRetryEvery 重试）。
	dialAttempt = 3 * time.Second
	// probeTimeout 是取 PNG 截图（探测原生分辨率）的上限。
	probeTimeout = 10 * time.Second
	// touchTimeout / navKeyTimeout 是单次触摸与导航键的上限。
	touchTimeout  = 5 * time.Second
	navKeyTimeout = 15 * time.Second
	// aliveCheckEvery 是帧循环检查实例是否仍在运行的间隔。
	//
	// 画面是脏帧驱动的：静止时几乎不推帧，因此不能只依赖「流结束」来判断实例退出，
	// 必须额外按时间轮询实例状态。
	aliveCheckEvery = time.Second
)

// 导航键映射：无窗口模式下模拟器的 gRPC 键盘注入无效，改走 adb 的 keyevent。
var navKeys = map[string]int{
	"back":      4,   // KEYCODE_BACK
	"home":      3,   // KEYCODE_HOME
	"appswitch": 187, // KEYCODE_APP_SWITCH
}

// DisplayService 管理自定义 UI 的两条显示路径：
//   - 独立设备窗口：辅助进程取 MMAP 帧、编码 JPEG、由 WebView 承载窗口与工具栏（native_display.go）；
//   - 应用内浮层：MJPEG + 前端 <img>，作为设备窗口不可用时的兼容路径（本文件主体）。
//
// 两条路径共用同一份 MJPEG 服务与触摸映射实现。
type DisplayService struct {
	rt *Runtime

	mu            sync.Mutex
	nativeStartMu sync.Mutex
	srv           *display.Server
	items         map[string]*displayItem
	natives       map[string]*nativeSession
	// closeEpoch 记录每个实例「被要求关闭」的次数：Open 在开始连接前记下当时的值，连接完成
	// （可达数分钟）后若发现计数变了，说明用户在这期间关掉了设备窗口，于是直接拆掉刚建好的会话。
	//
	// 用计数而不是布尔标记：连接期间可能同时有多个 Open 在飞（React StrictMode 下开发模式会
	// 重复挂载、以及设备页卸载兜底调用），布尔标记会被其中一个消费掉而放过另一个，仍会留下
	// 无人订阅的画面会话（gRPC 流 + 帧循环一直跑到模拟器退出），而界面上完全无感。
	closeEpoch map[string]uint64
	base       context.Context
	cancel     context.CancelFunc
	closed     bool
}

// displayItem 是一个实例的画面会话。
type displayItem struct {
	info    DisplaySession
	client  *emulatorgrpc.Client
	session *display.Session
	cancel  context.CancelFunc
}

// DisplaySession 是设备窗口订阅画面所需的全部信息。
type DisplaySession struct {
	InstanceID   string `json:"instanceId"`
	AvdName      string `json:"avdName"`
	Serial       string `json:"serial"`
	URL          string `json:"url"`
	DeviceWidth  int    `json:"deviceWidth"`  // 设备原生分辨率（输入映射基准）
	DeviceHeight int    `json:"deviceHeight"` // 设备原生分辨率（输入映射基准）
	StreamWidth  int    `json:"streamWidth"`
	StreamHeight int    `json:"streamHeight"`
	Mode         string `json:"mode"` // webview | web
}

// NewDisplayService 创建 DisplayService。
func NewDisplayService(rt *Runtime) *DisplayService {
	ctx, cancel := context.WithCancel(context.Background())
	return &DisplayService{
		rt:         rt,
		items:      make(map[string]*displayItem),
		natives:    make(map[string]*nativeSession),
		closeEpoch: make(map[string]uint64),
		base:       ctx,
		cancel:     cancel,
	}
}

// Open 打开实例的设备画面：幂等，内部会等待模拟器 gRPC 就绪（上限 3 分钟，冷启动可能几十秒）。
func (s *DisplayService) Open(instanceID string) (*DisplaySession, error) {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return nil, domain.Err(domain.CodeInvalidArgument, "缺少模拟器实例标识")
	}

	// 幂等：同一实例重复打开直接返回已有会话。
	if item := s.item(id); item != nil {
		info := item.info
		return &info, nil
	}

	// 同一实例的并发 Open 串行化（等待 gRPC 就绪最长 3 分钟，不能并发拨号）。
	defer s.rt.locks.Lock("display:" + id)()

	// 本次连接开始时的「关闭计数」：连接期间用户若关掉设备窗口，计数会变，注册前据此放弃。
	s.mu.Lock()
	epoch := s.closeEpoch[id]
	s.mu.Unlock()

	if item := s.item(id); item != nil {
		info := item.info
		return &info, nil
	}

	inst, ok := s.instance(id)
	if !ok {
		return nil, domain.ErrDetail(domain.CodeInvalidArgument, "未找到模拟器实例", "instanceID="+id).
			WithHint("请刷新设备列表后重试")
	}
	if !inst.CustomUI || inst.GrpcPort <= 0 {
		return nil, domain.Err(domain.CodeInvalidArgument, "该实例不是以「自定义 UI」方式启动的").
			WithHint("请使用「更多操作 → 使用自定义 UI 启动」")
	}

	client, png, err := s.connect(id, inst.GrpcPort)
	if err != nil {
		return nil, err
	}

	nativeW, nativeH, err := pngSize(png)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	streamW := nativeW
	if streamW > maxStreamWidth {
		streamW = maxStreamWidth
	}
	streamH := int(math.Round(float64(streamW) * float64(nativeH) / float64(nativeW)))

	srv, err := s.ensureServer()
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(s.base)
	item := &displayItem{
		info: DisplaySession{
			InstanceID:   id,
			AvdName:      inst.AvdName,
			Serial:       inst.Serial,
			URL:          srv.URL(id),
			DeviceWidth:  nativeW,
			DeviceHeight: nativeH,
			StreamWidth:  streamW,
			StreamHeight: streamH,
			Mode:         "web",
		},
		client:  client,
		session: srv.Session(id),
		cancel:  cancel,
	}

	// 会话登记与「是否已被作废」的判断必须在同一临界区内完成，否则 Close 可能插在中间：
	// 它看不到未登记的会话，只能推进 closeEpoch，而登记后的会话就再也没人拆了。
	s.mu.Lock()
	reason := s.abandonedReason(id, epoch)
	if reason == "" {
		s.items[id] = item
	}
	s.mu.Unlock()
	if reason != "" {
		s.teardown(item, "")
		return nil, domain.Err(domain.CodeJobCanceled, reason)
	}

	s.rt.Log().Info("display", "已打开设备画面：%s（设备 %dx%d → 画面流 %dx%d）",
		inst.AvdName, nativeW, nativeH, streamW, streamH)
	go s.stream(ctx, item)

	info := item.info
	return &info, nil
}

// Close 关闭实例的设备画面（不停模拟器）；未打开时为空操作。
//
// 连接中（Open 还在等 gRPC 就绪，最长 3 分钟）点关闭同样有效：这里记下关闭意图，
// Open 完成连接后会立即自行拆掉，不会留下无人订阅的画面会话。
func (s *DisplayService) Close(instanceID string) error {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return domain.Err(domain.CodeInvalidArgument, "缺少模拟器实例标识")
	}
	if native := s.takeNative(id); native != nil {
		s.stopNativeSession(native, "")
		s.rt.Log().Info("display", "已关闭 %s 的设备窗口", native.info.AvdName)
		return nil
	}
	s.mu.Lock()
	// 无论会话是否已登记都要计数：连接中的 Open 靠它发现「用户已经关掉了窗口」。
	s.closeEpoch[id]++
	item := s.items[id]
	if item != nil {
		delete(s.items, id)
	}
	s.mu.Unlock()
	if item == nil {
		return nil
	}
	s.teardown(item, "")
	s.rt.Log().Info("display", "已关闭 %s 的设备画面", item.info.AvdName)
	return nil
}

// SendTouch 注入触摸：x/y 是设备窗口内的归一化坐标（[0,1]）。
func (s *DisplayService) SendTouch(instanceID string, x, y float64, release bool) error {
	item, err := s.require(instanceID)
	if err != nil {
		return err
	}
	px, py := display.MapTouch(x, y, item.info.DeviceWidth, item.info.DeviceHeight)
	ctx, cancel := context.WithTimeout(s.base, touchTimeout)
	defer cancel()
	return item.client.SendTouch(ctx, px, py, release)
}

// SendKey 发送导航键（back / home / appswitch），走 adb（无窗口模式下 gRPC 键盘注入无效）。
func (s *DisplayService) SendKey(instanceID string, key string) error {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return domain.Err(domain.CodeInvalidArgument, "缺少模拟器实例标识")
	}
	name := strings.ToLower(strings.TrimSpace(key))
	code, ok := navKeys[name]
	if !ok {
		return domain.ErrDetail(domain.CodeInvalidArgument, "不支持的导航键", "key="+key).
			WithHint("可用值：back / home / appswitch")
	}
	inst, ok := s.instance(id)
	if !ok {
		return domain.ErrDetail(domain.CodeInvalidArgument, "未找到模拟器实例", "instanceID="+id).
			WithHint("请刷新设备列表后重试")
	}
	ctx, cancel := context.WithTimeout(s.base, navKeyTimeout)
	defer cancel()
	if _, err := s.rt.Components().Adb.Shell(ctx, inst.Serial, "input", "keyevent", strconv.Itoa(code)); err != nil {
		return keyError(err)
	}
	s.rt.Log().Debug("display", "已发送导航键 %s（serial=%s）", name, inst.Serial)
	return nil
}

// Active 返回当前打开了设备画面的实例 ID（升序）。
func (s *DisplayService) Active() []string {
	s.mu.Lock()
	out := make([]string, 0, len(s.items)+len(s.natives))
	for id := range s.items {
		out = append(out, id)
	}
	for id := range s.natives {
		out = append(out, id)
	}
	s.mu.Unlock()
	sort.Strings(out)
	return domain.NonNil(out)
}

// Shutdown 结束所有会话并释放画面服务（应用退出时调用）。
func (s *DisplayService) Shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	items := make([]*displayItem, 0, len(s.items))
	for id, item := range s.items {
		items = append(items, item)
		delete(s.items, id)
	}
	natives := make([]*nativeSession, 0, len(s.natives))
	for id, item := range s.natives {
		natives = append(natives, item)
		delete(s.natives, id)
	}
	srv := s.srv
	s.mu.Unlock()

	// 先取消基上下文，让帧循环与后续调用立即失败。
	s.cancel()
	for _, item := range items {
		s.teardown(item, "")
	}
	for _, item := range natives {
		s.stopNativeSession(item, "")
	}
	if srv != nil {
		_ = srv.Close()
	}
}

// stream 是帧循环：gRPC 画面流 → JPEG → 发布到 MJPEG 会话。
func (s *DisplayService) stream(ctx context.Context, item *displayItem) {
	frames, err := item.client.StreamScreenshot(ctx, item.info.StreamWidth, item.info.StreamHeight)
	if err != nil {
		s.rt.Log().Warn("display", "%s 的画面流打开失败：%v", item.info.AvdName, err)
		s.finish(item, "无法打开模拟器画面流")
		return
	}

	ticker := time.NewTicker(aliveCheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// 会话已被 Close/Shutdown 清理，直接退出。
			return
		case frame, ok := <-frames:
			if !ok {
				s.finish(item, "模拟器画面流已结束")
				return
			}
			jpeg, err := encodeFrame(frame)
			if err != nil {
				s.rt.Log().Warn("display", "%s 的帧编码失败：%v", item.info.AvdName, err)
				continue
			}
			item.session.Publish(jpeg)
		case <-ticker.C:
			if !s.alive(item.info.InstanceID) {
				s.finish(item, "模拟器已停止")
				return
			}
		}
	}
}

// connect 有界等待 gRPC 就绪并探测原生分辨率（PNG 截图的 IHDR）。
func (s *DisplayService) connect(instanceID string, grpcPort int) (*emulatorgrpc.Client, []byte, error) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(grpcPort))
	deadline := time.Now().Add(grpcWaitTimeout)
	var lastErr error
	for {
		if !s.alive(instanceID) {
			return nil, nil, domain.ErrDetail(domain.CodeProcessFailed,
				"模拟器实例已退出，无法连接画面通道", "instanceID="+instanceID).
				WithHint("请重新使用「更多操作 → 使用自定义 UI 启动」")
		}

		dialCtx, cancel := context.WithTimeout(s.base, dialAttempt)
		client, err := emulatorgrpc.Dial(dialCtx, addr, "")
		cancel()
		if err == nil {
			probeCtx, probeCancel := context.WithTimeout(s.base, probeTimeout)
			png, shotErr := client.ScreenshotPNG(probeCtx)
			probeCancel()
			if shotErr == nil {
				return client, png, nil
			}
			err = shotErr
			_ = client.Close()
		}
		lastErr = err

		if time.Now().After(deadline) {
			return nil, nil, domain.ErrDetail(domain.CodeProcessFailed,
				"等待模拟器画面通道就绪超时", lastErr.Error()).
				WithHint("请确认模拟器已正常启动（gRPC 端口可能被占用），并查看应用日志（模块 display）")
		}
		select {
		case <-s.base.Done():
			return nil, nil, domain.Err(domain.CodeJobCanceled, "应用正在退出，等待画面通道已取消")
		case <-time.After(grpcRetryEvery):
		}
	}
}

// ensureServer 惰性启动 MJPEG 服务（只监听 127.0.0.1 随机端口）。
func (s *DisplayService) ensureServer() (*display.Server, error) {
	if srv := s.server(); srv != nil {
		return srv, nil
	}
	srv, err := display.NewServer(s.rt.Log())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = srv.Close()
		return nil, domain.Err(domain.CodeJobCanceled, "应用正在退出，无法启动画面服务")
	}
	if s.srv == nil {
		s.srv = srv
		s.mu.Unlock()
		return srv, nil
	}
	existing := s.srv
	s.mu.Unlock()
	_ = srv.Close()
	return existing, nil
}

func (s *DisplayService) server() *display.Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srv
}

func (s *DisplayService) instance(id string) (domain.EmulatorInstance, bool) {
	return s.rt.Components().Launcher.Get(id)
}

// alive 判断实例是否仍在运行（决定设备窗口是否该自动关闭）。
//
// 判定必须与前端 DevicesPage 的 instanceAlive 一致：error 只有在进程真的退出（ExitCode 非空）
// 之后才算结束。进程仍活着时画面可能继续更新，提前拆会话会让设备窗口停在最后一帧且没有任何提示。
func (s *DisplayService) alive(id string) bool {
	inst, ok := s.instance(id)
	if !ok {
		return false
	}
	switch inst.State {
	case domain.AvdStarting, domain.AvdBooting, domain.AvdRunning, domain.AvdStopping:
		return true
	case domain.AvdError:
		return inst.ExitCode == nil
	default:
		return false
	}
}

func (s *DisplayService) item(id string) *displayItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[id]
}

// abandonedReason 判断一次 Open（开始连接时记录了 epoch）在登记会话前是否已被作废：
// 应用退出，或连接期间用户关掉了设备窗口（Close 会推进 closeEpoch）。
// 返回空串表示可以登记。调用方必须持有 s.mu。
func (s *DisplayService) abandonedReason(id string, epoch uint64) string {
	switch {
	case s.closed:
		return "应用正在退出，无法打开设备画面"
	case s.closeEpoch[id] != epoch:
		return "设备窗口已关闭"
	default:
		return ""
	}
}

// require 取出已打开的会话，未打开时给出可操作错误。
func (s *DisplayService) require(instanceID string) (*displayItem, error) {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return nil, domain.Err(domain.CodeInvalidArgument, "缺少模拟器实例标识")
	}
	item := s.item(id)
	if item == nil {
		return nil, domain.Err(domain.CodeInvalidArgument, "设备窗口未打开").
			WithHint("请先使用「更多操作 → 使用自定义 UI 启动」打开设备窗口")
	}
	return item, nil
}

// finish 由帧循环在异常结束（实例退出 / 流结束）时调用：清理并通知前端关闭窗口。
func (s *DisplayService) finish(item *displayItem, reason string) {
	s.mu.Lock()
	current, ok := s.items[item.info.InstanceID]
	if ok && current == item {
		delete(s.items, item.info.InstanceID)
	}
	s.mu.Unlock()
	if !ok || current != item {
		return // 已被 Close/Shutdown 清理
	}
	s.teardown(item, reason)
}

// teardown 释放一个会话的全部资源（帧循环、gRPC 连接、MJPEG 会话）。
func (s *DisplayService) teardown(item *displayItem, reason string) {
	item.cancel()
	_ = item.client.Close()
	item.session.Close()
	if srv := s.server(); srv != nil {
		srv.DropSession(item.info.InstanceID)
	}
	if reason != "" {
		// 前端通过 emulator:state / avd:changed 感知实例退出并自行关闭设备窗口，
		// 这里只留日志便于排查（契约中没有 display 事件）。
		s.rt.Log().Info("display", "%s 的设备画面已结束：%s", item.info.AvdName, reason)
	}
}

// pngSize 从 PNG 的 IHDR 读出画面尺寸。
//
// 必须解析 PNG 头：模拟器返回的 Image.width/height 恒为 0，接口层拿不到分辨率。
func pngSize(data []byte) (int, int, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, domain.ErrDetail(domain.CodeProcessFailed, "无法解析模拟器截图尺寸", err.Error())
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, domain.ErrDetail(domain.CodeProcessFailed, "模拟器截图的尺寸无效",
			fmt.Sprintf("%dx%d", cfg.Width, cfg.Height))
	}
	return cfg.Width, cfg.Height, nil
}

// encodeFrame 把一帧 RGBA8888 编码成 JPEG。
//
// 帧是正向（top-down）的：官方 proto 里 Image.image 注释写的 "bottom up" 在本 build
// 实测不成立——真机 37.1.11 上把 RGBA 帧与同屏 PNG 按原顺序逐行比对 diff=0.00，
// 上下翻转后 diff≈12，因此这里不做翻转。
//
// 每帧使用独立缓冲：帧会被发布到 MJPEG 会话并保留到下一帧到来，
// 复用缓冲会让订阅者读到正在被改写的像素。
func encodeFrame(f emulatorgrpc.Frame) ([]byte, error) {
	img := &image.RGBA{
		Pix:    f.Pix,
		Stride: f.Width * 4,
		Rect:   image.Rect(0, 0, f.Width, f.Height),
	}
	var buf bytes.Buffer
	buf.Grow(len(f.Pix) / 8)
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// keyError 保留 adb 层给出的错误码（例如 TOOL_MISSING），只补充「导航键」语境。
func keyError(err error) error {
	var appErr *domain.AppError
	if errors.As(err, &appErr) && appErr.Code != "" {
		return domain.ErrDetail(appErr.Code, "发送导航键失败", err.Error())
	}
	return domain.Wrap(domain.CodeProcessFailed, "发送导航键失败", err)
}
