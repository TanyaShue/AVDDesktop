package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"AVDDesktop/internal/displayhost"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// nativeExecutablePath 返回用于启动设备窗口的可执行文件。
// 开发/端到端测试可通过 AVDDESKTOP_NATIVE_BINARY 覆盖；正常运行时使用当前主程序。
var nativeExecutablePath = func() (string, error) {
	if override := strings.TrimSpace(os.Getenv("AVDDESKTOP_NATIVE_BINARY")); override != "" {
		return override, nil
	}
	return os.Executable()
}

const (
	nativeReadyMarker = "__AVDDESKTOP_WINDOW_READY__"

	// gracefulWindowExitWait 是「请求优雅退出」后等待辅助进程自行收尾的上限。
	// 辅助进程读到 stdin EOF 会关窗口、等帧协程退出、释放共享内存并删除映射文件；超时才强杀。
	// 上限取 5s：辅助进程内部的收尾本身是有界的（等帧协程 + 删除重试约 2.6s 上限），
	// 留出余量可以避免在临界情况下误判为超时并强杀，从而漏删映射文件。
	gracefulWindowExitWait = 5 * time.Second
	// windowKillWait 是强杀后等待进程消失的上限。
	windowKillWait = 3 * time.Second
)

// nativeSession 是一个独立设备窗口辅助进程。
type nativeSession struct {
	info DisplaySession
	cmd  *exec.Cmd
	done chan struct{}

	// stdin 是优雅退出通道：关闭它等于通知辅助进程「该收尾了」。
	stdin io.WriteCloser

	ready     chan struct{}
	readyOnce sync.Once
	stopOnce  sync.Once

	intentional atomic.Bool
	waitErr     error // done 关闭后读取
}

// OpenWindow 打开独立设备窗口（WebView 承载）。
//
// 辅助进程与主进程使用同一个可执行文件，但会以 --display-host 模式启动，
// 因此不会初始化 Wails 主程序，也不会抢单实例锁。
func (s *DisplayService) OpenWindow(instanceID string) (*DisplaySession, error) {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return nil, domain.Err(domain.CodeInvalidArgument, "缺少模拟器实例标识")
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

	// 启动序列整体串行化，避免 React StrictMode/重复点击创建两个窗口。
	s.nativeStartMu.Lock()
	defer s.nativeStartMu.Unlock()

	if existing := s.native(id); existing != nil {
		if !existing.exited() {
			info := existing.info
			return &info, nil
		}
		_ = s.removeNative(id, existing)
	}

	exe, err := nativeExecutablePath()
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法定位 AVDDesktop 可执行文件", err)
	}
	comp := s.rt.Components()
	cfg := displayhost.HelperConfig{
		InstanceID:  id,
		AVDName:     inst.AvdName,
		GRPCAddress: fmt.Sprintf("127.0.0.1:%d", inst.GrpcPort),
		Serial:      inst.Serial,
		ADBPath:     comp.Tools.Adb,
		// 原生分辨率由辅助进程用 PNG 截图探测，主进程不重复连接 gRPC。
		DeviceWidth:  0,
		DeviceHeight: 0,
		// 画面宽度由辅助进程按默认上限决定（模拟器侧缩放）。
		StreamWidth: 0,
		// 父进程（本进程）关闭 stdin 即请求辅助进程收尾，避免残留窗口与映射文件。
		WatchParent: true,
	}
	cmd := exec.Command(exe, displayhost.HelperArgs(cfg)...)
	cmd.Dir = platform.Root()
	cmd.Env = comp.Env
	proc.Prepare(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法创建设备窗口 stdin 管道", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法创建设备窗口 stdout 管道", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法创建设备窗口 stderr 管道", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法启动设备窗口", err)
	}

	native := &nativeSession{
		info: DisplaySession{
			InstanceID:   id,
			AvdName:      inst.AvdName,
			Serial:       inst.Serial,
			DeviceWidth:  0,
			DeviceHeight: 0,
			Mode:         "webview",
		},
		cmd:   cmd,
		done:  make(chan struct{}),
		stdin: stdin,
		ready: make(chan struct{}),
	}

	// 先挂上收尾协程再登记：即使进程立刻退出也不会留下无人 Wait 的子进程。
	go native.copyOutput("stdout", stdout, s.rt.Log().Debug)
	go native.copyOutput("stderr", stderr, s.rt.Log().Warn)
	go s.watchNative(native)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		native.intentional.Store(true)
		_ = proc.KillTree(context.Background(), cmd.Process.Pid)
		return nil, domain.Err(domain.CodeJobCanceled, "应用正在退出，无法打开设备窗口")
	}
	s.natives[id] = native
	s.mu.Unlock()

	// 不能把子进程 Start 成功当成窗口可用：辅助进程只有在窗口创建且首帧已发布后才打印
	// ready marker，因此这里等待它就绪（冷启动上限与旧路径一致），避免前端提前显示"已打开"。
	select {
	case <-native.ready:
	case <-native.done:
		_ = s.removeNative(id, native)
		exitErr := native.waitErr
		if exitErr == nil {
			exitErr = fmt.Errorf("设备窗口进程在就绪前退出")
		}
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "设备窗口启动失败", exitErr.Error())
	case <-time.After(grpcWaitTimeout + 15*time.Second):
		s.stopNativeSession(native, "等待设备窗口就绪超时")
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "等待设备窗口就绪超时",
			"ready marker not received")
	case <-s.base.Done():
		s.stopNativeSession(native, "应用退出")
		return nil, domain.Err(domain.CodeJobCanceled, "应用正在退出，等待设备窗口已取消")
	}

	s.rt.Log().Info("display", "已打开 %s 的设备窗口（pid=%d）", inst.AvdName, cmd.Process.Pid)
	info := native.info
	return &info, nil
}

// WindowSupported 返回当前平台是否支持独立设备窗口。
//
// 设备窗口由 WebView 承载（Wails 在 Windows/macOS/Linux 都提供），因此始终可用。
func (s *DisplayService) WindowSupported() bool { return true }

func (s *DisplayService) native(id string) *nativeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.natives[id]
}

func (s *DisplayService) takeNative(id string) *nativeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.natives[id]
	if item != nil {
		delete(s.natives, id)
	}
	return item
}

func (s *DisplayService) removeNative(id string, item *nativeSession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.natives[id] != item {
		return false
	}
	delete(s.natives, id)
	return true
}

func (s *DisplayService) watchNative(item *nativeSession) {
	err := item.cmd.Wait()
	item.waitErr = err
	close(item.done)

	removed := s.removeNative(item.info.InstanceID, item)
	if !removed {
		return
	}
	if item.intentional.Load() {
		s.rt.Log().Debug("display", "%s 的设备窗口已关闭", item.info.AvdName)
		return
	}
	if err != nil {
		s.rt.Log().Warn("display", "%s 的设备窗口异常退出：%v", item.info.AvdName, err)
	} else {
		s.rt.Log().Info("display", "%s 的设备窗口已关闭", item.info.AvdName)
	}
	s.rt.Emit("display:native-closed", map[string]string{
		"instanceId": item.info.InstanceID,
		"reason":     nativeExitReason(err),
	})
}

// stopNativeSession 结束一个设备窗口进程：先请求优雅退出（关 stdin），超时才强杀。
//
// 优雅退出是有意义的：辅助进程只有走正常收尾路径才会删除共享内存映射文件。
func (s *DisplayService) stopNativeSession(item *nativeSession, reason string) {
	if item == nil {
		return
	}
	if !s.removeNative(item.info.InstanceID, item) {
		// 已经由 watchNative 移除时仍需确保进程已结束。
		if item.exited() {
			return
		}
	}
	item.stopOnce.Do(func() {
		item.intentional.Store(true)
		item.requestQuit()
	})
	if !waitClosed(item.done, gracefulWindowExitWait) {
		s.rt.Log().Warn("display", "%s 的设备窗口未在期限内优雅退出，改为强制结束", item.info.AvdName)
		if item.cmd != nil && item.cmd.Process != nil {
			_ = proc.KillTree(context.Background(), item.cmd.Process.Pid)
		}
		if !waitClosed(item.done, windowKillWait) {
			s.rt.Log().Warn("display", "%s 的设备窗口进程仍未退出", item.info.AvdName)
		}
	}
	if reason != "" {
		s.rt.Log().Debug("display", "设备窗口已结束：%s", reason)
	}
}

// requestQuit 请求辅助进程自行收尾；没有优雅通道时直接强杀。
func (n *nativeSession) requestQuit() {
	if n.stdin != nil {
		_ = n.stdin.Close()
		return
	}
	if n.cmd != nil && n.cmd.Process != nil {
		_ = proc.KillTree(context.Background(), n.cmd.Process.Pid)
	}
}

func (n *nativeSession) exited() bool {
	select {
	case <-n.done:
		return true
	default:
		return false
	}
}

func (n *nativeSession) copyOutput(stream string, r io.Reader, sink func(string, string, ...any)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if stream == "stdout" {
			if payload, ok := readyLinePayload(line); ok {
				if payload != nil {
					n.applyReadyInfo(payload)
				}
				n.readyOnce.Do(func() { close(n.ready) })
				continue
			}
		}
		sink("display", "[window:%s] %s", stream, line)
	}
}

// readyLinePayload 判断一行是否是辅助进程的握手行，并取出其中的会话信息。
//
// 握手行有两种形态：只有 marker（兼容旧格式），或 marker + 一段 JSON（当前格式）。
// 关 ready 之前先写入会话信息，OpenWindow 在收到 ready 之后读取就是安全的。
func readyLinePayload(line string) (payload []byte, ok bool) {
	if line == nativeReadyMarker {
		return nil, true
	}
	if rest, cut := strings.CutPrefix(line, nativeReadyMarker+" "); cut {
		return []byte(rest), true
	}
	return nil, false
}

// applyReadyInfo 记录辅助进程上报的画面地址与分辨率。
func (n *nativeSession) applyReadyInfo(raw []byte) {
	var info struct {
		URL          string `json:"url"`
		DeviceWidth  int    `json:"deviceWidth"`
		DeviceHeight int    `json:"deviceHeight"`
		StreamWidth  int    `json:"streamWidth"`
		StreamHeight int    `json:"streamHeight"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return
	}
	n.info.URL = info.URL
	n.info.DeviceWidth = info.DeviceWidth
	n.info.DeviceHeight = info.DeviceHeight
	n.info.StreamWidth = info.StreamWidth
	n.info.StreamHeight = info.StreamHeight
}

// waitClosed 等待 done 关闭，返回是否在期限内关闭。
func waitClosed(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func nativeExitReason(err error) string {
	if err == nil {
		return "window-closed"
	}
	return err.Error()
}
