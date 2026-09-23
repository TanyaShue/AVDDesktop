package service

import (
	"os"
	"path/filepath"
	"testing"

	"AVDDesktop/internal/avd"
)

// writeTestAvd 在临时 AVD 主目录里写入一个设备（.ini + config.ini）。
func writeTestAvd(t *testing.T, home, name, config string) {
	t.Helper()
	dir := filepath.Join(home, name+".avd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := "avd.ini.encoding=UTF-8\npath=" + dir + "\n"
	if err := os.WriteFile(filepath.Join(home, name+".ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.ini"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDevicesUsingImage(t *testing.T) {
	home := t.TempDir()
	writeTestAvd(t, home, "Pixel36", "abi.type=x86_64\ntag.id=google_apis\ntarget=android-36\n"+
		"image.sysdir.1=system-images/android-36/google_apis/x86_64/\n")
	writeTestAvd(t, home, "OtherImage", "abi.type=arm64-v8a\ntag.id=google_apis\ntarget=android-34\n"+
		"image.sysdir.1=system-images/android-34/google_apis/arm64-v8a/\n")
	// 缺少 image.sysdir.1：退化为 API / tag / ABI 三元组匹配
	writeTestAvd(t, home, "NoSysdir", "abi.type=x86_64\ntag.id=google_apis\ntarget=android-36\n")
	writeTestAvd(t, home, "Broken", "")

	store := avd.New(home, filepath.Join(t.TempDir(), "sdk"))

	got, err := devicesUsingImage(store, "system-images;android-36;google_apis;x86_64")
	if err != nil {
		t.Fatalf("devicesUsingImage 返回错误: %v", err)
	}
	want := map[string]bool{"Pixel36": true, "NoSysdir": true}
	if len(got) != len(want) {
		t.Fatalf("使用该镜像的设备 = %#v，期望 %d 个", got, len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("不应命中设备 %q（结果 %#v）", name, got)
		}
	}

	other, err := devicesUsingImage(store, "system-images;android-35;google_apis_playstore;x86_64")
	if err != nil {
		t.Fatalf("devicesUsingImage 返回错误: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("未安装的其它镜像不应命中任何设备: %#v", other)
	}
	if _, err := devicesUsingImage(store, "platform-tools"); err == nil {
		t.Fatal("非系统镜像路径应当报错，而不是当作没有设备引用")
	}
}

// TestDevicesUsingImageListError 覆盖「读不到设备列表」：必须返回错误，
// 让删除流程中止，而不是把读取失败当成「没有设备引用」继续删除。
func TestDevicesUsingImageListError(t *testing.T) {
	// 含 NUL 的路径在 Windows 与 Unix 上都必然以非 ENOENT 的错误失败，
	// 对应现实中的「AVD 目录无法读取（权限 / 损坏）」场景。
	store := avd.New(filepath.Join(t.TempDir(), "bad\x00dir"), filepath.Join(t.TempDir(), "sdk"))

	if _, err := devicesUsingImage(store, "system-images;android-36;google_apis;x86_64"); err == nil {
		t.Fatal("设备列表不可读时必须返回错误，否则会误删仍被引用的镜像")
	}
}
