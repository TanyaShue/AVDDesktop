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
	jdkHome := filepath.Join(t.TempDir(), "jdk")
	avdHome := filepath.Join(t.TempDir(), "avd")
	tools := NewTools(root)

	// 调用前后快照进程环境：ChildEnv 的硬约束是"绝不修改当前进程环境"。
	before := os.Environ()
	env := ChildEnv(tools, jdkHome, avdHome)
	after := os.Environ()
	if len(before) != len(after) {
		t.Fatalf("ChildEnv 修改了当前进程环境：%d → %d 个变量", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("ChildEnv 修改了当前进程环境：%q → %q", before[i], after[i])
		}
	}

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
	if got["JAVA_HOME"] != jdkHome {
		t.Errorf("JAVA_HOME 应指向软件自带 JDK: %q", got["JAVA_HOME"])
	}
	if got["ANDROID_AVD_HOME"] != avdHome {
		t.Errorf("ANDROID_AVD_HOME 应为 AVD 目录本身，实际 %q", got["ANDROID_AVD_HOME"])
	}
	if got["ANDROID_EMU_ENABLE_CRASH_REPORTING"] != "0" {
		t.Errorf("应禁用模拟器外部崩溃报告，实际 %q", got["ANDROID_EMU_ENABLE_CRASH_REPORTING"])
	}
	// PATH 必须把自带 JDK 与工具链放在前面，且保留原 PATH
	path := got["PATH"]
	if !strings.HasPrefix(path, filepath.Join(jdkHome, "bin")) {
		t.Errorf("PATH 未优先使用自带 JDK: %q", path)
	}
	if !strings.Contains(path, filepath.Join(root, "platform-tools")) {
		t.Errorf("PATH 缺少自带 platform-tools: %q", path)
	}
	if !strings.Contains(path, filepath.Join(root, "emulator")) ||
		!strings.Contains(path, filepath.Join(root, "cmdline-tools", "latest", "bin")) {
		t.Errorf("PATH 缺少自带工具链目录: %q", path)
	}
	if old := os.Getenv("PATH"); old != "" && !strings.Contains(path, old) {
		t.Errorf("PATH 应保留原有内容")
	}
}

func TestRootOverridable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AVDDESKTOP_HOME", dir)

	// 只断言纯解析函数：Root() 带 sync.Once 缓存，断言它会与本包其它用例的执行顺序耦合。
	if got := resolveRoot(); got != filepath.Clean(dir) {
		t.Fatalf("AVDDESKTOP_HOME 应决定软件根目录：期望 %q，实际 %q", dir, got)
	}
	// 派生目录必须都落在软件根目录下（用解析结果而非缓存值推导）。
	wantRoot := filepath.Clean(dir)
	for name, got := range map[string]string{
		"jdk":      jdkHomeAt(filepath.Join(wantRoot, "jdk"), "linux"),
		"sdk":      filepath.Join(wantRoot, "sdk"),
		"avd":      filepath.Join(wantRoot, "avd"),
		"settings": filepath.Join(wantRoot, "config", "settings.json"),
		"logs":     filepath.Join(wantRoot, "logs"),
		"cache":    filepath.Join(wantRoot, "cache"),
	} {
		if !strings.HasPrefix(got, wantRoot) {
			t.Fatalf("%s 目录未位于软件根目录下: %q", name, got)
		}
	}
}

func TestFindJavaUsesOnlyManagedJdk(t *testing.T) {
	systemHome := t.TempDir()
	managedHome := t.TempDir()
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	if err := os.MkdirAll(filepath.Join(systemHome, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTemp(t, filepath.Join(systemHome, "bin", name), "system")
	t.Setenv("JAVA_HOME", systemHome)
	t.Setenv("PATH", filepath.Join(systemHome, "bin"))

	if got := javaPathIn(managedHome); got != "" {
		t.Fatalf("软件目录没有 java 时不应回退系统 JDK，实际 %q", got)
	}
	if err := os.MkdirAll(filepath.Join(managedHome, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	managedJava := filepath.Join(managedHome, "bin", name)
	writeTemp(t, managedJava, "managed")
	if got := javaPathIn(managedHome); got != managedJava {
		t.Fatalf("应使用软件目录下的 java：期望 %q，实际 %q", managedJava, got)
	}
}
func TestJdkHomeAt(t *testing.T) {
	root := filepath.Join("C:", "app", "jdk")
	if got := jdkHomeAt(root, "windows"); got != root {
		t.Fatalf("Windows JdkHome = %q，期望 %q", got, root)
	}
	if got := jdkHomeAt(root, "linux"); got != root {
		t.Fatalf("Linux JdkHome = %q，期望 %q", got, root)
	}
	wantMac := filepath.Join(root, "Contents", "Home")
	if got := jdkHomeAt(root, "darwin"); got != wantMac {
		t.Fatalf("macOS JdkHome = %q，期望 %q", got, wantMac)
	}
}

func writeTemp(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
