package avd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
)

// settingsStore 建一个带 fullConfig 的设备，返回 store 与配置目录。
//
// 同时按配置里的 image.sysdir.1 在 SDK 目录下建出镜像目录，使设备被视为「配置完好」。
func settingsStore(t *testing.T, name, config string) (*Store, string) {
	t.Helper()
	s, home, sdkRoot := newTestStore(t)
	writeAvd(t, home, name, "", config)
	for _, line := range strings.Split(config, "\n") {
		sysdir, ok := strings.CutPrefix(strings.TrimSpace(line), "image.sysdir.1=")
		if !ok {
			continue
		}
		if err := os.MkdirAll(filepath.Join(sdkRoot, filepath.FromSlash(strings.TrimSpace(sysdir))), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return s, filepath.Join(home, name+".avd")
}

func TestDetailReadsSettingsAndRootSupport(t *testing.T) {
	s, _ := settingsStore(t, "Dev1", fullConfig)

	detail, err := s.Detail("Dev1")
	if err != nil {
		t.Fatalf("Detail 失败: %v", err)
	}
	want := domain.AvdSettings{
		RAMMB: 2048, CPUCores: 4, LCDWidth: 1080, LCDHeight: 2400, LCDDensity: 420, VMHeapMB: 256,
		Keyboard: false, Root: false,
	}
	if detail.Settings != want {
		t.Fatalf("设置读取错误:\n实际 %+v\n期望 %+v", detail.Settings, want)
	}
	if !detail.RootSupported || detail.RootReason != "" {
		t.Fatalf("google_apis 镜像应支持 root：supported=%v reason=%q", detail.RootSupported, detail.RootReason)
	}
	if detail.API != "34" || detail.ABI != "x86_64" || detail.DeviceProfileID != "medium_phone" {
		t.Fatalf("只读信息错误: %+v", detail)
	}
	if detail.SystemImagePath != "system-images/android-34/google_apis/x86_64/" {
		t.Fatalf("系统镜像路径错误: %q", detail.SystemImagePath)
	}
	if detail.SkinPath != "_no_skin" {
		t.Fatalf("皮肤信息错误: %q", detail.SkinPath)
	}
	if detail.Broken != "" {
		t.Fatalf("配置损坏原因应为空: %q", detail.Broken)
	}
	if detail.State != domain.AvdStopped {
		t.Fatalf("未运行设备的状态应为 stopped，实际 %q", detail.State)
	}

	// 设备列表也要带出密度与 root 标记（设备卡片直接用）
	items, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].LCDDensity != 420 || items[0].RootEnabled {
		t.Fatalf("列表字段错误: %+v", items)
	}
}

func TestUpdateSettingsWritesConfigAndState(t *testing.T) {
	s, dir := settingsStore(t, "Dev1", fullConfig)
	keyboard := true
	root := true

	detail, err := s.UpdateSettings(domain.AvdSettingsUpdate{
		Name: "Dev1", RAMMB: 6144, CPUCores: 8,
		LCDWidth: 720, LCDHeight: 1600, LCDDensity: 320, VMHeapMB: 512,
		Keyboard: &keyboard, Root: &root,
	})
	if err != nil {
		t.Fatalf("UpdateSettings 失败: %v", err)
	}
	for k, want := range map[string]string{
		"hw.ramSize":     "6144",
		"hw.cpu.ncore":   "8",
		"hw.lcd.width":   "720",
		"hw.lcd.height":  "1600",
		"hw.lcd.density": "320",
		"vm.heapSize":    "512",
		"hw.keyboard":    "yes",
		// 分辨率变化时同步皮肤名（原有 _no_skin 保持不变，hw.lcd.* 才会生效）
		"skin.name":         "720x1600",
		"skin.path":         "_no_skin",
		"target":            "android-34", // 未涉及的键原样保留
		"PlayStore.enabled": "no",
	} {
		if got := mustReadConfig(t, s, "Dev1")[k]; got != want {
			t.Errorf("config[%q] = %q，期望 %q", k, got, want)
		}
	}
	if !detail.Settings.Root || !detail.Settings.Keyboard {
		t.Fatalf("返回值应反映新设置: %+v", detail.Settings)
	}
	if detail.Settings.RAMMB != 6144 || detail.Settings.LCDDensity != 320 {
		t.Fatalf("返回值设置错误: %+v", detail.Settings)
	}

	// root 标记写在软件自己的状态文件里，不污染 config.ini
	if !s.RootEnabled("Dev1") {
		t.Fatal("RootEnabled 应为 true")
	}
	if _, ok := mustReadConfig(t, s, "Dev1")["root"]; ok {
		t.Fatal("config.ini 里不应出现 root 键")
	}
	if _, err := os.Stat(filepath.Join(dir, appStateFile)); err != nil {
		t.Fatalf("状态文件未写入: %v", err)
	}

	// 注释与键顺序保留、结尾换行规范
	raw, err := os.ReadFile(filepath.Join(dir, "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, "# 由 avdmanager 生成的配置\n") {
		t.Errorf("注释应保留在文件开头:\n%s", text)
	}
	if strings.Contains(text, "\r") || strings.HasSuffix(text, "\n\n") {
		t.Errorf("换行格式不正确: %q", text)
	}

	// 幂等：同样输入再写一次，内容不变
	if _, err := s.UpdateSettings(domain.AvdSettingsUpdate{
		Name: "Dev1", RAMMB: 6144, CPUCores: 8,
		LCDWidth: 720, LCDHeight: 1600, LCDDensity: 320, VMHeapMB: 512,
		Keyboard: &keyboard, Root: &root,
	}); err != nil {
		t.Fatalf("第二次 UpdateSettings 失败: %v", err)
	}
	again, err := os.ReadFile(filepath.Join(dir, "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != text {
		t.Errorf("重复保存不应改变 config.ini:\n第一次:\n%s\n第二次:\n%s", text, string(again))
	}
}

// TestUpdateSettingsPartial 只提交改动项时不应影响其它配置。
func TestUpdateSettingsPartial(t *testing.T) {
	s, _ := settingsStore(t, "Dev1", fullConfig)

	if _, err := s.UpdateSettings(domain.AvdSettingsUpdate{Name: "Dev1", RAMMB: 3072}); err != nil {
		t.Fatalf("UpdateSettings 失败: %v", err)
	}
	config := mustReadConfig(t, s, "Dev1")
	for k, want := range map[string]string{
		"hw.ramSize":     "3072",
		"hw.cpu.ncore":   "4",    // 未提交 → 不变
		"hw.lcd.width":   "1080", // 未提交 → 不变
		"hw.lcd.density": "420",
		"vm.heapSize":    "256",
	} {
		if config[k] != want {
			t.Errorf("config[%q] = %q，期望 %q", k, config[k], want)
		}
	}
	// 只改内存时不应写 resolved 皮肤键
	if _, ok := config["skin.name"]; !ok || config["skin.name"] != "1080x2400" {
		t.Errorf("未改分辨率时皮肤名不应变化: %q", config["skin.name"])
	}

	// 关闭键盘：显式 false 也要写进 config.ini
	off := false
	if _, err := s.UpdateSettings(domain.AvdSettingsUpdate{Name: "Dev1", Keyboard: &off}); err != nil {
		t.Fatalf("UpdateSettings 失败: %v", err)
	}
	if got := mustReadConfig(t, s, "Dev1")["hw.keyboard"]; got != "no" {
		t.Errorf("hw.keyboard = %q，期望 no", got)
	}
}

func TestUpdateSettingsValidationAndErrors(t *testing.T) {
	s, _ := settingsStore(t, "Dev1", fullConfig)

	cases := []struct {
		name string
		req  domain.AvdSettingsUpdate
		want string
	}{
		{"内存过小", domain.AvdSettingsUpdate{Name: "Dev1", RAMMB: 128}, "内存"},
		{"内存过大", domain.AvdSettingsUpdate{Name: "Dev1", RAMMB: 99999}, "内存"},
		{"核心过多", domain.AvdSettingsUpdate{Name: "Dev1", CPUCores: 64}, "CPU 核心"},
		{"密度过小", domain.AvdSettingsUpdate{Name: "Dev1", LCDDensity: 10}, "像素密度"},
		{"密度过大", domain.AvdSettingsUpdate{Name: "Dev1", LCDDensity: 1200}, "像素密度"},
		{"堆过大", domain.AvdSettingsUpdate{Name: "Dev1", VMHeapMB: 8192}, "虚拟机堆上限"},
		{"宽度过小", domain.AvdSettingsUpdate{Name: "Dev1", LCDWidth: 100, LCDHeight: 800}, "屏幕宽度"},
		{"只有宽度", domain.AvdSettingsUpdate{Name: "Dev1", LCDWidth: 720}, "同时给出宽度和高度"},
		{"只有高度", domain.AvdSettingsUpdate{Name: "Dev1", LCDHeight: 1280}, "同时给出宽度和高度"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.UpdateSettings(c.req)
			var appErr *domain.AppError
			if !errors.As(err, &appErr) || appErr.Code != domain.CodeInvalidArgument {
				t.Fatalf("应返回 INVALID_ARGUMENT，实际 %v", err)
			}
			if !strings.Contains(appErr.Message, c.want) {
				t.Fatalf("错误信息 %q 应包含 %q", appErr.Message, c.want)
			}
		})
	}

	// 未指定名称
	if _, err := s.UpdateSettings(domain.AvdSettingsUpdate{RAMMB: 2048}); err == nil {
		t.Fatal("缺少设备名应报错")
	}
	// 设备不存在
	_, err := s.UpdateSettings(domain.AvdSettingsUpdate{Name: "Missing", RAMMB: 2048})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeAvdNotFound {
		t.Fatalf("设备不存在应返回 AVD_NOT_FOUND，实际 %v", err)
	}
	// 设备存在但没有 config.ini
	s2, home, _ := newTestStore(t)
	writeAvd(t, home, "Broken", "", "")
	if _, err := s2.UpdateSettings(domain.AvdSettingsUpdate{Name: "Broken", RAMMB: 2048}); !errors.As(err, &appErr) ||
		appErr.Code != domain.CodePathNotFound {
		t.Fatalf("缺 config.ini 应返回 PATH_NOT_FOUND，实际 %v", err)
	}
	// Detail 对不存在的设备同样报错
	if _, err := s.Detail("Missing"); !errors.As(err, &appErr) || appErr.Code != domain.CodeAvdNotFound {
		t.Fatalf("Detail 设备不存在应返回 AVD_NOT_FOUND，实际 %v", err)
	}
}

// TestUpdateSettingsRootUnsupported 覆盖 Google Play 镜像：可以关闭 root，但不能开启。
func TestUpdateSettingsRootUnsupported(t *testing.T) {
	play := `AvdId=Play1
PlayStore.enabled=yes
abi.type=x86_64
hw.ramSize=2048
image.sysdir.1=system-images/android-34/google_apis_playstore/x86_64/
tag.display=Google Play
tag.id=google_apis_playstore
target=android-34
`
	s, _ := settingsStore(t, "Play1", play)

	detail, err := s.Detail("Play1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.RootSupported || detail.RootReason == "" {
		t.Fatalf("Play 镜像应标记为不支持 root：%+v", detail)
	}

	yes := true
	_, err = s.UpdateSettings(domain.AvdSettingsUpdate{Name: "Play1", Root: &yes, RAMMB: 4096})
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Code != domain.CodeInvalidArgument {
		t.Fatalf("Play 镜像开启 root 应被拒绝，实际 %v", err)
	}
	if appErr.Hint == "" {
		t.Fatal("拒绝开启 root 时应给出可操作提示")
	}
	if s.RootEnabled("Play1") {
		t.Fatal("被拒绝后不应写入 root 标记")
	}
	// 被拒绝时不应留下"改了一半"的配置：同一个请求里的内存改动也不该写入
	if got := mustReadConfig(t, s, "Play1")["hw.ramSize"]; got != "2048" {
		t.Fatalf("root 校验失败时不应写 config.ini，hw.ramSize = %q", got)
	}

	// 关闭 root 永远允许（例如换了镜像后遗留的标记）
	no := false
	if _, err := s.UpdateSettings(domain.AvdSettingsUpdate{Name: "Play1", Root: &no}); err != nil {
		t.Fatalf("关闭 root 不应失败: %v", err)
	}
}

// TestRootSupportByTag 覆盖 config.ini 缺少 PlayStore.enabled 时按 tag / 镜像目录判断。
func TestRootSupportByTag(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]string
		want   bool
	}{
		{"playstore tag", map[string]string{"tag.id": "google_apis_playstore", "image.sysdir.1": "system-images/android-34/google_apis_playstore/x86_64/"}, false},
		{"playstore sysdir", map[string]string{"tag.id": "google_apis", "image.sysdir.1": "system-images/android-34/google_apis_playstore/x86_64/"}, false},
		{"play enabled", map[string]string{"PlayStore.enabled": "true", "tag.id": "google_apis"}, false},
		{"google apis", map[string]string{"tag.id": "google_apis", "image.sysdir.1": "system-images/android-34/google_apis/x86_64/"}, true},
		{"aosp", map[string]string{"tag.ids": "default,x86", "image.sysdir.1": "system-images/android-34/default/x86_64/"}, true},
		{"缺少镜像信息", map[string]string{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := rootSupport(c.config)
			if got != c.want {
				t.Fatalf("rootSupport = %v（%s），期望 %v", got, reason, c.want)
			}
			if !got && reason == "" {
				t.Fatal("不支持 root 时必须给出原因")
			}
		})
	}
}

// TestApplyRootPermissions 覆盖 root 生效流程：adb root → remount（失败重试）。
func TestApplyRootPermissions(t *testing.T) {
	fake := &fakeADB{remountErr: 2}
	l := NewLauncher(platform.Tools{}, nil, nil, fake, nil, logging.Nop())
	l.remountRetry = time.Millisecond
	inst := newTestInstance("emulator-5554")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.applyRootPermissions(ctx, inst)

	rootCalls, remountCalls := fake.rootStats()
	if rootCalls != 1 {
		t.Fatalf("adb root 应只调用一次，实际 %d 次", rootCalls)
	}
	if remountCalls != 3 {
		t.Fatalf("remount 应在第 3 次成功，实际 %d 次", remountCalls)
	}

	// user 构建：adb root 输出失败原因时不重试 remount
	user := &fakeADB{rootOut: "adbd cannot run as root in production builds"}
	l2 := NewLauncher(platform.Tools{}, nil, nil, user, nil, logging.Nop())
	l2.remountRetry = time.Millisecond
	l2.applyRootPermissions(ctx, newTestInstance("emulator-5556"))
	if _, remountCalls := user.rootStats(); remountCalls != 0 {
		t.Fatalf("user 构建不应继续 remount，实际调用 %d 次", remountCalls)
	}

	// 没有 adb 客户端时不应 panic
	l3 := NewLauncher(platform.Tools{}, nil, nil, nil, nil, logging.Nop())
	l3.applyRootPermissions(ctx, newTestInstance("emulator-5558"))
}

// newTestInstance 构造一个不产生真实进程的实例（只用于 root 流程测试）。
func newTestInstance(serial string) *instance {
	return &instance{
		exit: make(chan struct{}),
		info: domain.EmulatorInstance{ID: "emu-" + serial, AvdName: "Dev1", Serial: serial},
	}
}

// mustReadConfig 读取 config.ini，失败直接终止测试。
func mustReadConfig(t *testing.T, s *Store, name string) map[string]string {
	t.Helper()
	config, err := s.ReadConfig(name)
	if err != nil {
		t.Fatalf("ReadConfig 失败: %v", err)
	}
	return config
}
