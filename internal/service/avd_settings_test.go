package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"AVDDesktop/internal/avd"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
)

// newAvdServiceForTest 组装一个只依赖临时目录的 AvdService。
func newAvdServiceForTest(t *testing.T, home, sdkRoot string) (*AvdService, *Runtime) {
	t.Helper()
	store := avd.New(home, sdkRoot)
	rt := &Runtime{log: logging.Discard(), locks: job.NewKeyedMutex()}
	rt.comp = &components{
		Store:    store,
		Launcher: avd.NewLauncher(platform.Tools{}, nil, store, nil, rt.Emit, logging.Discard()),
	}
	rt.SetContext(context.Background())
	return NewAvdService(rt), rt
}

// TestAvdServiceUpdateSettings 覆盖设置面板的服务层链路：
// 校验 → 写 config.ini → 写 root 标记 → 返回最新详情 → 广播 avd:changed。
func TestAvdServiceUpdateSettings(t *testing.T) {
	home := t.TempDir()
	sdkRoot := t.TempDir()
	config := "AvdId=Dev1\nPlayStore.enabled=no\nabi.type=x86_64\nhw.ramSize=2048\n" +
		"hw.cpu.ncore=4\nhw.lcd.width=1080\nhw.lcd.height=2400\nhw.lcd.density=420\n" +
		"hw.device.name=medium_phone\nimage.sysdir.1=system-images/android-34/google_apis/x86_64/\n" +
		"tag.id=google_apis\ntarget=android-34\n"
	writeTestAvd(t, home, "Dev1", config)
	if err := os.MkdirAll(filepath.Join(sdkRoot, "system-images", "android-34", "google_apis", "x86_64"), 0o755); err != nil {
		t.Fatal(err)
	}

	svc, rt := newAvdServiceForTest(t, home, sdkRoot)
	var events []string
	rt.SetEmitter(func(event string, _ any) { events = append(events, event) })

	keyboard, root := true, true
	detail, err := svc.UpdateSettings(domain.AvdSettingsUpdate{
		Name: "Dev1", RAMMB: 4096, CPUCores: 6,
		LCDWidth: 720, LCDHeight: 1280, LCDDensity: 320, VMHeapMB: 512,
		Keyboard: &keyboard, Root: &root,
	})
	if err != nil {
		t.Fatalf("UpdateSettings 失败: %v", err)
	}
	if detail.Settings.RAMMB != 4096 || detail.Settings.LCDDensity != 320 ||
		detail.Settings.VMHeapMB != 512 || !detail.Settings.Root || !detail.Settings.Keyboard {
		t.Fatalf("返回值未反映新设置: %+v", detail.Settings)
	}
	if detail.Name != "Dev1" || detail.State != domain.AvdStopped {
		t.Fatalf("详情字段错误: %+v", detail)
	}
	if len(events) == 0 || events[len(events)-1] != "avd:changed" {
		t.Fatalf("保存后应广播 avd:changed，实际 %v", events)
	}

	// Detail 与列表读到的是同一份数据
	again, err := svc.Detail("Dev1")
	if err != nil {
		t.Fatalf("Detail 失败: %v", err)
	}
	if again.Settings != detail.Settings {
		t.Fatalf("Detail 与保存结果不一致:\n%+v\n%+v", again.Settings, detail.Settings)
	}
	items, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].RAMMB != 4096 || items[0].LCDDensity != 320 || !items[0].RootEnabled {
		t.Fatalf("设备列表未反映新设置: %+v", items)
	}

	// 非法参数在服务层就被挡下（不写文件、不广播）
	before := len(events)
	if _, err := svc.UpdateSettings(domain.AvdSettingsUpdate{Name: "Dev1", RAMMB: 99999}); err == nil {
		t.Fatal("超范围的内存应被拒绝")
	}
	if len(events) != before {
		t.Fatal("被拒绝的保存不应广播事件")
	}

	// 设备不存在
	if _, err := svc.Detail("Missing"); err == nil {
		t.Fatal("不存在的设备应报错")
	}
}
