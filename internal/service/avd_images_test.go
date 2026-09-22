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

	got := devicesUsingImage(store, "system-images;android-36;google_apis;x86_64")
	want := map[string]bool{"Pixel36": true, "NoSysdir": true}
	if len(got) != len(want) {
		t.Fatalf("使用该镜像的设备 = %#v，期望 %d 个", got, len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("不应命中设备 %q（结果 %#v）", name, got)
		}
	}

	if other := devicesUsingImage(store, "system-images;android-35;google_apis_playstore;x86_64"); len(other) != 0 {
		t.Fatalf("未安装的其它镜像不应命中任何设备: %#v", other)
	}
	if bad := devicesUsingImage(store, "platform-tools"); len(bad) != 0 {
		t.Fatalf("非系统镜像路径不应命中任何设备: %#v", bad)
	}
}
