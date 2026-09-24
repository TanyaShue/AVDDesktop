//go:build e2e && windows

package e2e

import (
	"os"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/service"
)

// TestE2E_DeviceWindow 验证主程序控制面能够启动真实设备窗口辅助进程（WebView 承载），
// 并能在 Close 时回收它。需要预先构建主程序，并通过环境变量指向该二进制：
//
//	$env:AVDDESKTOP_E2E_HOME='D:\...\build\bin'
//	$env:AVDDESKTOP_NATIVE_BINARY='D:\...\build\bin\AVDDesktop.exe'
//	go test -tags e2e -run TestE2E_DeviceWindow -timeout 20m -v ./internal/e2e
func TestE2E_DeviceWindow(t *testing.T) {
	binary := os.Getenv("AVDDESKTOP_NATIVE_BINARY")
	if binary == "" {
		t.Skip("set AVDDESKTOP_NATIVE_BINARY to a built AVDDesktop.exe")
	}
	t.Setenv("AVDDESKTOP_NATIVE_BINARY", binary)

	rt := newRuntime(t)
	emuSvc := service.NewEmulatorService(rt)
	name, created := ensureAvdForBoot(t, rt, service.NewAvdService(rt))
	t.Logf("device window e2e AVD=%s created=%v", name, created)

	inst, err := emuSvc.Start(service.StartRequest{AvdName: name, CustomUI: true})
	if err != nil {
		t.Fatalf("start emulator: %v", err)
	}
	t.Cleanup(func() {
		_ = emuSvc.Stop(inst.ID, true)
	})

	running, err := waitInstanceState(emuSvc, inst.ID, domain.AvdRunning, 10*time.Minute)
	if err != nil {
		t.Fatalf("wait emulator running: %v", err)
	}

	display := service.NewDisplayService(rt)
	t.Cleanup(display.Shutdown)
	session, err := display.OpenWindow(running.ID)
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	if session.Mode != "webview" {
		t.Fatalf("session mode = %q, want webview", session.Mode)
	}
	if session.URL == "" {
		t.Fatal("session url is empty")
	}

	// 辅助进程完全异步；OpenWindow 返回即代表窗口已创建且首帧已发布，
	// 这里再观察一段时间确认没有快速退出。
	stableUntil := time.Now().Add(8 * time.Second)
	for time.Now().Before(stableUntil) {
		active := display.Active()
		if len(active) == 0 {
			t.Fatal("device window exited before becoming active")
		}
		time.Sleep(250 * time.Millisecond)
	}
	if active := display.Active(); len(active) != 1 || active[0] != running.ID {
		t.Fatalf("active sessions = %v, want [%s]", active, running.ID)
	}

	if err := display.Close(running.ID); err != nil {
		t.Fatalf("Close device window: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(display.Active()) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("device window remained active after Close: %v", display.Active())
}
