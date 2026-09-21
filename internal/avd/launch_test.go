package avd

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

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
