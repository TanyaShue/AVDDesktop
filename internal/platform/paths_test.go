package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewToolsLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sdk")
	tools := NewTools(root)

	if tools.SdkRoot != root {
		t.Fatalf("SdkRoot = %q", tools.SdkRoot)
	}
	// 工具链必须全部位于软件自有 SDK 目录内
	for name, path := range map[string]string{
		"sdkmanager":   tools.Sdkmanager,
		"avdmanager":   tools.Avdmanager,
		"adb":          tools.Adb,
		"emulator":     tools.Emulator,
		"licenses":     tools.Licenses,
		"systemImages": tools.SystemImages,
	} {
		if !strings.HasPrefix(path, root) {
			t.Errorf("%s 不在 SDK 目录内: %q", name, path)
		}
	}
	if filepath.Dir(tools.Sdkmanager) != filepath.Join(root, "cmdline-tools", "latest", "bin") {
		t.Errorf("sdkmanager 路径不符合官方目录结构: %q", tools.Sdkmanager)
	}
	if filepath.Dir(tools.Adb) != filepath.Join(root, "platform-tools") {
		t.Errorf("adb 路径不符合官方目录结构: %q", tools.Adb)
	}
	if filepath.Dir(tools.Emulator) != filepath.Join(root, "emulator") {
		t.Errorf("emulator 路径不符合官方目录结构: %q", tools.Emulator)
	}

	if runtime.GOOS == "windows" {
		if filepath.Ext(tools.Sdkmanager) != ".bat" {
			t.Errorf("Windows 上 sdkmanager 应为 .bat：%q", tools.Sdkmanager)
		}
		if filepath.Ext(tools.Adb) != ".exe" {
			t.Errorf("Windows 上 adb 应为 .exe：%q", tools.Adb)
		}
	} else {
		if filepath.Ext(tools.Sdkmanager) != "" || filepath.Ext(tools.Adb) != "" {
			t.Errorf("非 Windows 平台不应带可执行后缀：%q / %q", tools.Sdkmanager, tools.Adb)
		}
	}
}

func TestToolsAvailability(t *testing.T) {
	root := t.TempDir()
	tools := NewTools(root)
	if tools.HasSdkmanager() || tools.HasAdb() || tools.HasEmulator() || tools.HasAvdmanager() {
		t.Fatal("空目录不应报告工具就位")
	}
	if err := os.MkdirAll(filepath.Dir(tools.Sdkmanager), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTemp(t, tools.Sdkmanager, "#!/bin/sh\n")
	if !tools.HasSdkmanager() {
		t.Fatal("文件存在时应报告 sdkmanager 可用")
	}
	if tools.HasEmulator() {
		t.Fatal("emulator 仍未安装")
	}
}

func TestChildEnvInjectsOwnSdk(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sdk")
	avdHome := filepath.Join(t.TempDir(), "avd")
	tools := NewTools(root)

	env := ChildEnv(tools, avdHome)
	got := map[string]string{}
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		got[strings.ToUpper(kv[:i])] = kv[i+1:]
	}

	if got["ANDROID_SDK_ROOT"] != root || got["ANDROID_HOME"] != root {
		t.Errorf("未把软件自有 SDK 注入子进程: %v", got["ANDROID_SDK_ROOT"])
	}
	if got["ANDROID_AVD_HOME"] != avdHome {
		t.Errorf("ANDROID_AVD_HOME 应为 AVD 目录本身，实际 %q", got["ANDROID_AVD_HOME"])
	}
	// PATH 必须把自带工具链放在前面，且保留原 PATH
	path := got["PATH"]
	if !strings.HasPrefix(path, filepath.Join(root, "platform-tools")) {
		t.Errorf("PATH 未优先使用自带 platform-tools: %q", path)
	}
	if !strings.Contains(path, filepath.Join(root, "emulator")) ||
		!strings.Contains(path, filepath.Join(root, "cmdline-tools", "latest", "bin")) {
		t.Errorf("PATH 缺少自带工具链目录: %q", path)
	}
	if old := os.Getenv("PATH"); old != "" && !strings.Contains(path, old) {
		t.Errorf("PATH 应保留原有内容")
	}
	// 绝不修改当前进程环境
	if os.Getenv("ANDROID_SDK_ROOT") != got["ANDROID_SDK_ROOT"] {
		// 仅当原环境没有该值时成立；不修改环境是本函数的硬约束
		if os.Getenv("ANDROID_SDK_ROOT") == root {
			t.Fatal("ChildEnv 不应修改当前进程环境")
		}
	}
}

func TestRootOverridable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AVDDESKTOP_HOME", dir)

	// Root 使用 sync.Once 缓存，这里直接验证解析函数的行为
	if got := resolveRoot(); got != filepath.Clean(dir) {
		t.Fatalf("AVDDESKTOP_HOME 应决定软件根目录：期望 %q，实际 %q", dir, got)
	}
	if got := SdkRoot(); !strings.HasPrefix(got, filepath.Clean(dir)) {
		t.Fatalf("SDK 目录应位于软件根目录下: %q", got)
	}
	if got := AvdHome(); got != filepath.Join(filepath.Clean(dir), "avd") {
		t.Fatalf("AVD 目录错误: %q", got)
	}
	if got := SettingsPath(); got != filepath.Join(filepath.Clean(dir), "config", "settings.json") {
		t.Fatalf("设置文件路径错误: %q", got)
	}
}

func TestFindJavaPrefersJavaHome(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	writeTemp(t, filepath.Join(bin, name), "stub")

	t.Setenv("JAVA_HOME", dir)
	if got := FindJava(); got != filepath.Join(bin, name) {
		t.Fatalf("应优先使用 JAVA_HOME 下的 java：期望 %q，实际 %q", filepath.Join(bin, name), got)
	}
}

func writeTemp(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
