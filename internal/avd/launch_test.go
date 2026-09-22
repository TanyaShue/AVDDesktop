package avd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"AVDDesktop/internal/adb"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
)

// newTestLauncher 返回一个不接触真实工具链的启动器。
func newTestLauncher() *Launcher {
	return NewLauncher(platform.Tools{}, nil, nil, nil, nil, logging.Nop())
}

// TestBuildArgs 验证启动参数只包含端口与冷启动/无窗口两个开关的组合。
func TestBuildArgs(t *testing.T) {
	cases := []struct {
		name string
		opts domain.LaunchOptions
		port int
		want []string
	}{
		{"默认", domain.LaunchOptions{}, 5554, []string{"-avd", "Dev1", "-port", "5554"}},
		{"冷启动", domain.LaunchOptions{ColdBoot: true}, 5556, []string{"-avd", "Dev1", "-port", "5556", "-no-snapshot-load"}},
		{"无窗口", domain.LaunchOptions{NoWindow: true}, 5558, []string{"-avd", "Dev1", "-port", "5558", "-no-window"}},
		{"冷启动+无窗口", domain.LaunchOptions{ColdBoot: true, NoWindow: true}, 5560,
			[]string{"-avd", "Dev1", "-port", "5560", "-no-snapshot-load", "-no-window"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildArgs("Dev1", tc.opts, tc.port); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BuildArgs = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// TestAllocatePort 验证端口分配：同时探测 console/adb 端口、不重复、耗尽报错、可归还。
func TestAllocatePort(t *testing.T) {
	first, last := freePortRange(t, 3)
	l := newTestLauncher()
	l.first, l.last = first, last

	seen := map[int]bool{}
	for i := 1; i <= 3; i++ {
		port, err := l.AllocatePort()
		if err != nil {
			t.Fatalf("第 %d 次分配失败（范围 %d-%d）：%v", i, first, last, err)
		}
		if port < first || port > last || port%2 != 0 {
			t.Fatalf("分配的端口 %d 不是范围内的偶数（%d-%d）", port, first, last)
		}
		if seen[port] {
			t.Fatalf("端口 %d 被重复分配", port)
		}
		seen[port] = true
	}
	if _, err := l.AllocatePort(); err == nil {
		t.Fatalf("范围 %d-%d 已耗尽，应返回明确错误", first, last)
	}
	l.releasePort(first)
	if port, err := l.AllocatePort(); err != nil || port != first {
		t.Fatalf("归还 %d 后应能重新分配：port=%d err=%v", first, port, err)
	}

	// console 端口被真实占用时必须跳过
	hold, _ := freePortRange(t, 1)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", hold))
	if err != nil {
		t.Fatalf("无法占用测试端口 %d: %v", hold, err)
	}
	l2 := newTestLauncher()
	l2.first, l2.last = hold, hold
	if _, err := l2.AllocatePort(); err == nil {
		t.Errorf("console 端口 %d 已被占用，不应分配成功", hold)
	}
	_ = ln.Close()
	if port, err := l2.AllocatePort(); err != nil || port != hold {
		t.Errorf("端口释放后应能分配 %d：port=%d err=%v", hold, port, err)
	}
}

// freePortRange 找一段本机可用的连续偶数端口段（含 count 个偶数端口）。
func freePortRange(t *testing.T, count int) (int, int) {
	t.Helper()
	for start := 5700; start <= 5900; start += 2 {
		free := true
		for i := 0; i < count*2; i++ {
			if !isPortFree(start + i) {
				free = false
				break
			}
		}
		if free {
			return start, start + 2*(count-1)
		}
	}
	t.Fatal("本机没有可用的偶数端口段用于测试")
	return 0, 0
}

// TestExplainExit 验证退出原因按关键词给出可操作说明。
func TestExplainExit(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want string
	}{
		{"加速不可用", "x86_64 emulation currently requires hardware acceleration!\nWHPX is not installed and usable", "硬件加速"},
		{"权限不足", "Cannot open AVD config: Access is denied.", "权限"},
		{"端口占用", "Failed to bind to console port 5554: Address already in use", "端口"},
		{"内存不足", "qemu-system-x86_64: cannot allocate memory", "内存"},
		{"其它", "unknown failure", "异常退出"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := explainExit(tc.log, 1)
			if !strings.Contains(got, tc.want) {
				t.Errorf("explainExit(%q) = %q，应包含 %q", tc.log, got, tc.want)
			}
			if !strings.Contains(got, "1") {
				t.Errorf("退出码解释应包含退出码：%q", got)
			}
		})
	}
}

// TestProcessExitCancelsWait 验证「进程提前退出」立刻取消等待，不会空等满超时。
func TestProcessExitCancelsWait(t *testing.T) {
	exit := make(chan struct{})
	waitCtx, cancel := cancelWhenExited(context.Background(), exit)
	defer cancel()

	select {
	case <-waitCtx.Done():
		t.Fatal("进程未退出时不应取消等待")
	case <-time.After(50 * time.Millisecond):
	}
	if processExited(exit) {
		t.Fatal("processExited 误报进程已退出")
	}

	started := time.Now()
	close(exit)
	select {
	case <-waitCtx.Done():
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("进程退出后取消等待耗时 %s，应立即取消", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("进程退出后等待未被取消（会空等满超时）")
	}
	if !processExited(exit) {
		t.Fatal("processExited 未识别已退出的进程")
	}

	// 上层 ctx 取消同样能中断等待
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	waitCtx2, cancel2 := cancelWhenExited(parent, make(chan struct{}))
	defer cancel2()
	parentCancel()
	select {
	case <-waitCtx2.Done():
	case <-time.After(time.Second):
		t.Fatal("上层 ctx 取消未传递到等待上下文")
	}
}

// testInstance 构造一个不涉及真实进程的实例；exit 关闭表示进程已退出。
func testInstance(avdName string, port int) *instance {
	return &instance{exit: make(chan struct{}), info: domain.EmulatorInstance{
		ID: fmt.Sprintf("emu-%d-1", port), AvdName: avdName, Serial: adb.SerialForPort(port),
		Port: port, State: domain.AvdStarting, StartedAt: platform.NowMs(),
	}}
}

// registerInstance 把测试实例登记到启动器，模拟已受理的启动。
func registerInstance(l *Launcher, inst *instance) {
	l.mu.Lock()
	l.instances[inst.info.ID] = inst
	l.mu.Unlock()
}

// TestMonitorConvergesOnPrematureExit 验证进程提前退出时 monitor 立即收敛为 stopped，不空等超时。
func TestMonitorConvergesOnPrematureExit(t *testing.T) {
	port, _ := freePortRange(t, 1)
	l := newTestLauncher()
	inst := testInstance("Dev1", port)
	registerInstance(l, inst)
	close(inst.exit) // 进程已在 monitor 启动前退出（退出码 0）

	started := time.Now()
	l.monitor(context.Background(), inst)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("进程已退出，monitor 仍耗时 %s，应立即收敛", elapsed)
	}
	snap, ok := l.Get(inst.info.ID)
	if !ok {
		t.Fatal("实例未登记到启动器")
	}
	if snap.State != domain.AvdStopped {
		t.Fatalf("进程提前退出后状态 = %s，期望 %s", snap.State, domain.AvdStopped)
	}
}

// TestPortReallocatedAfterExit 验证实例结束后端口被归还，可再次分配。
func TestPortReallocatedAfterExit(t *testing.T) {
	port, _ := freePortRange(t, 1)
	l := newTestLauncher()
	l.first, l.last = port, port
	if got, err := l.AllocatePort(); err != nil || got != port {
		t.Fatalf("准备端口 %d 失败：port=%d err=%v", port, got, err)
	}
	inst := testInstance("Dev1", port)
	registerInstance(l, inst)
	close(inst.exit)
	l.monitor(context.Background(), inst)

	if got, err := l.AllocatePort(); err != nil || got != port {
		t.Fatalf("实例结束后端口 %d 应可再次分配：port=%d err=%v", port, got, err)
	}
}

// TestStartRejectsDuplicateAvd 验证同一 AVD 已有运行实例时拒绝再次启动。
func TestStartRejectsDuplicateAvd(t *testing.T) {
	emulator := filepath.Join(t.TempDir(), "emulator")
	if err := os.WriteFile(emulator, nil, 0o644); err != nil {
		t.Fatalf("创建测试用 emulator 占位文件失败：%v", err)
	}
	l := newTestLauncher()
	l.tools.Emulator = emulator
	registerInstance(l, testInstance("Dev1", 5560))

	_, err := l.Start(context.Background(), "Dev1", domain.LaunchOptions{})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeFileInUse {
		t.Fatalf("同一 AVD 重复启动错误 = %v，期望错误码 %s", err, domain.CodeFileInUse)
	}
}

// TestByAvdIgnoresExitedInstance 验证历史记录即使保留了旧状态，也不会被当作运行实例。
func TestByAvdIgnoresExitedInstance(t *testing.T) {
	port, _ := freePortRange(t, 1)
	l := newTestLauncher()
	inst := testInstance("Dev1", port)
	inst.info.State = domain.AvdRunning
	registerInstance(l, inst)
	close(inst.exit)

	if got, ok := l.ByAvd("Dev1"); ok {
		t.Fatalf("已退出实例不应被返回: %+v", got)
	}
}

// TestByAvdReturnsLiveInstance 验证同名历史记录存在时，仍能找到进程尚存活的实例。
func TestByAvdReturnsLiveInstance(t *testing.T) {
	stalePort, livePort := 5560, 5562
	l := newTestLauncher()
	stale := testInstance("Dev1", stalePort)
	stale.info.State = domain.AvdRunning
	registerInstance(l, stale)
	close(stale.exit)

	live := testInstance("Dev1", livePort)
	live.info.State = domain.AvdRunning
	registerInstance(l, live)

	got, ok := l.ByAvd("Dev1")
	if !ok || got.ID != live.info.ID {
		t.Fatalf("ByAvd 返回了错误实例：got=%+v ok=%v，期望 ID=%s", got, ok, live.info.ID)
	}
}

// TestStopByAvdTargetsLiveInstance 验证按 AVD 停止时不会误命中同名历史记录。
func TestStopByAvdTargetsLiveInstance(t *testing.T) {
	l := newTestLauncher()
	stale := testInstance("Dev1", 5560)
	stale.info.State = domain.AvdStopped
	registerInstance(l, stale)
	close(stale.exit)

	live := testInstance("Dev1", 5562)
	live.info.State = domain.AvdRunning
	registerInstance(l, live)

	done := make(chan error, 1)
	go func() { done <- l.StopByAvd(context.Background(), "Dev1", true) }()

	deadline := time.Now().Add(time.Second)
	for {
		snap, _ := l.Get(live.info.ID)
		if snap.State == domain.AvdStopping {
			close(live.exit)
			break
		}
		select {
		case err := <-done:
			t.Fatalf("StopByAvd 在停止存活实例前提前返回: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			close(live.exit)
			t.Fatal("StopByAvd 未选择存活的同名实例")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StopByAvd 返回错误: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("StopByAvd 未在实例退出后返回")
	}
}

// TestMonitorRequestedStopNonZeroExit 验证用户请求停止后的非 0 退出记为 stopped，只有非请求退出才记 error。
func TestMonitorRequestedStopNonZeroExit(t *testing.T) {
	cases := []struct {
		name          string
		stopRequested bool
		want          domain.AvdState
	}{
		{"已请求停止", true, domain.AvdStopped},
		{"未请求停止", false, domain.AvdError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port, _ := freePortRange(t, 1)
			l := newTestLauncher()
			inst := testInstance("Dev1", port)
			inst.code = 1
			inst.stopRequested = tc.stopRequested
			registerInstance(l, inst)
			close(inst.exit)

			l.monitor(context.Background(), inst)

			snap, ok := l.Get(inst.info.ID)
			if !ok {
				t.Fatal("实例未登记到启动器")
			}
			if snap.State != tc.want {
				t.Fatalf("退出码 1 后状态 = %s，期望 %s", snap.State, tc.want)
			}
			if snap.LastError == "" {
				t.Error("应保留退出原因（LastError）")
			}
		})
	}
}

// TestExplainExitAccelHintMatchesHostPlatform 回归测试：
// 加速不可用的排查建议必须与宿主平台一致（历史上无论哪个平台都提示「Windows 功能」）。
func TestExplainExitAccelHintMatchesHostPlatform(t *testing.T) {
	got := explainExit("x86_64 emulation currently requires hardware acceleration", 1)
	switch runtime.GOOS {
	case "windows":
		if !strings.Contains(got, "Windows 功能") {
			t.Errorf("Windows 上应给出「Windows 功能」建议：%q", got)
		}
	case "linux":
		if !strings.Contains(got, "kvm") || strings.Contains(got, "Windows 功能") {
			t.Errorf("Linux 上应给出 kvm 建议且不提 Windows：%q", got)
		}
	case "darwin":
		if !strings.Contains(got, "Hypervisor.Framework") || strings.Contains(got, "Windows 功能") {
			t.Errorf("macOS 上应给出 Hypervisor.Framework 建议且不提 Windows：%q", got)
		}
	}
}
