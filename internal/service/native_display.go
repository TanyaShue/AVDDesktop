package service

import (
	"bufio"
	"context"
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
	"AVDDesktop/internal/presenter"
	"AVDDesktop/internal/proc"
)

// nativeExecutablePath 返回用于启动 Presenter 模式的可执行文件。
// 开发/端到端测试可通过 AVDDESKTOP_NATIVE_BINARY 覆盖；正常运行时使用当前主程序。
var nativeExecutablePath = func() (string, error) {
	if override := strings.TrimSpace(os.Getenv("AVDDESKTOP_NATIVE_BINARY")); override != "" {
		return override, nil
	}
	return os.Executable()
}

const nativeReadyMarker = "__AVDDESKTOP_PRESENTER_READY__"

// nativeSession 是一个独立 Presenter 辅助进程。
type nativeSession struct {
	info DisplaySession
	cmd  *exec.Cmd
	done chan struct{}

	ready     chan struct{}
	readyOnce sync.Once
	stopOnce  sync.Once

	intentional atomic.Bool
	waitErr     error // done 关闭后读取
}

// OpenNative 打开独立原生设备窗口。
//
// 辅助进程与主进程使用同一个可执行文件，但会以 --display-host 模式启动，
// 因此不会初始化 Wails，也不会抢单实例锁。
func (s *DisplayService) OpenNative(instanceID string) (*DisplaySession, error) {
	if !presenter.Supported() {
		return nil, domain.Err(domain.CodeUnsupported, "当前平台暂不支持独立原生设备窗口").
			WithHint("继续使用应用内设备窗口")
	}

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

	// 启动序列整体串行化，避免 React StrictMode/重复点击创建两个 Presenter。
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
		InstanceID:   id,
		AVDName:      inst.AvdName,
		GRPCAddress:  fmt.Sprintf("127.0.0.1:%d", inst.GrpcPort),
		Serial:       inst.Serial,
		ADBPath:      comp.Tools.Adb,
		DeviceWidth:  0,
		DeviceHeight: 0,
	}
	cmd := exec.Command(exe, displayhost.HelperArgs(cfg)...)
	cmd.Dir = platform.Root()
	cmd.Env = comp.Env
	proc.Prepare(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法创建 Presenter stdout 管道", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法创建 Presenter stderr 管道", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法启动原生设备窗口", err)
	}

	native := &nativeSession{
		info: DisplaySession{
			InstanceID:   id,
			AvdName:      inst.AvdName,
			Serial:       inst.Serial,
			DeviceWidth:  0,
			DeviceHeight: 0,
			Mode:         "native",
		},
		cmd:   cmd,
		done:  make(chan struct{}),
		ready: make(chan struct{}),
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		native.intentional.Store(true)
		_ = proc.KillTree(context.Background(), cmd.Process.Pid)
		return nil, domain.Err(domain.CodeJobCanceled, "应用正在退出，无法打开原生设备窗口")
	}
	s.natives[id] = native
	s.mu.Unlock()

	go native.copyOutput("stdout", stdout, s.rt.Log().Debug)
	go native.copyOutput("stderr", stderr, s.rt.Log().Warn)
	go s.watchNative(native)

	// 不能把子进程 Start 成功当成窗口可用：等待 presenter.OnReady，冷启动等待上限与
	// 旧 WebView 路径的 gRPC 就绪上限一致，避免前端提前显示"已打开"。
	select {
	case <-native.ready:
	case <-native.done:
		exitErr := native.waitErr
		if exitErr == nil {
			exitErr = fmt.Errorf("presenter exited before ready")
		}
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "原生设备窗口启动失败", exitErr.Error())
	case <-time.After(grpcWaitTimeout + 15*time.Second):
		s.stopNativeSession(native, "等待原生窗口就绪超时")
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "等待原生设备窗口就绪超时",
			"presenter ready marker not received")
	case <-s.base.Done():
		s.stopNativeSession(native, "应用退出")
		return nil, domain.Err(domain.CodeJobCanceled, "应用正在退出，等待原生设备窗口已取消")
	}

	s.rt.Log().Info("display", "已启动 %s 的原生设备窗口（pid=%d）", inst.AvdName, cmd.Process.Pid)
	info := native.info
	return &info, nil
}

// NativeSupported 返回当前平台是否有原生 Presenter 实现。
func (s *DisplayService) NativeSupported() bool { return presenter.Supported() }

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
		s.rt.Log().Debug("display", "%s 的原生设备窗口已关闭", item.info.AvdName)
		return
	}
	if err != nil {
		s.rt.Log().Warn("display", "%s 的原生设备窗口异常退出：%v", item.info.AvdName, err)
	} else {
		s.rt.Log().Info("display", "%s 的原生设备窗口已关闭", item.info.AvdName)
	}
	s.rt.Emit("display:native-closed", map[string]string{
		"instanceId": item.info.InstanceID,
		"reason":     nativeExitReason(err),
	})
}

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
		if item.cmd != nil && item.cmd.Process != nil {
			_ = proc.KillTree(context.Background(), item.cmd.Process.Pid)
		}
	})
	select {
	case <-item.done:
	case <-time.After(3 * time.Second):
		s.rt.Log().Warn("display", "%s 的原生设备窗口未在期限内退出", item.info.AvdName)
	}
	if reason != "" {
		s.rt.Log().Debug("display", "原生设备窗口已结束：%s", reason)
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
		if stream == "stdout" && line == nativeReadyMarker {
			n.readyOnce.Do(func() { close(n.ready) })
			continue
		}
		sink("display", "[presenter:%s] %s", stream, line)
	}
}

func nativeExitReason(err error) string {
	if err == nil {
		return "window-closed"
	}
	return err.Error()
}
