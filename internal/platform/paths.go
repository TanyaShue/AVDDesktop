package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func runtimeGOOS() string { return runtime.GOOS }

// envOr 读取环境变量（忽略前后空白）。
func envOr(key string) string { return strings.TrimSpace(os.Getenv(key)) }

// Tools 汇总软件自有 SDK 下的工具链路径（不保证存在）。
//
// 可执行文件名称按平台区分：Windows 为 sdkmanager.bat / avdmanager.bat / adb.exe / emulator.exe，
// macOS 与 Linux 不带后缀。
type Tools struct {
	SdkRoot       string
	CmdlineTools  string // <sdk>/cmdline-tools/latest
	Sdkmanager    string
	Avdmanager    string
	PlatformTools string
	Adb           string
	EmulatorDir   string
	Emulator      string
	Licenses      string
	SystemImages  string
}

// NewTools 基于 SDK 根目录推导工具链路径（不保证存在）。
func NewTools(sdkRoot string) Tools {
	cmdline := filepath.Join(sdkRoot, "cmdline-tools", "latest")
	platformTools := filepath.Join(sdkRoot, "platform-tools")
	emulatorDir := filepath.Join(sdkRoot, "emulator")
	return Tools{
		SdkRoot:       sdkRoot,
		CmdlineTools:  cmdline,
		Sdkmanager:    filepath.Join(cmdline, "bin", scriptName("sdkmanager")),
		Avdmanager:    filepath.Join(cmdline, "bin", scriptName("avdmanager")),
		PlatformTools: platformTools,
		Adb:           filepath.Join(platformTools, exeName("adb")),
		EmulatorDir:   emulatorDir,
		Emulator:      filepath.Join(emulatorDir, exeName("emulator")),
		Licenses:      filepath.Join(sdkRoot, "licenses"),
		SystemImages:  filepath.Join(sdkRoot, "system-images"),
	}
}

// HasSdkmanager 判断 sdkmanager 是否已就位。
func (t Tools) HasSdkmanager() bool { return FileExists(t.Sdkmanager) }

// HasAvdmanager 判断 avdmanager 是否已就位。
func (t Tools) HasAvdmanager() bool { return FileExists(t.Avdmanager) }

// HasEmulator 判断 emulator 是否已就位。
func (t Tools) HasEmulator() bool { return FileExists(t.Emulator) }

// HasAdb 判断 adb 是否已就位。
func (t Tools) HasAdb() bool { return FileExists(t.Adb) }

// exeName 返回平台可执行文件后缀形式。
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// scriptName 返回命令行工具的包装脚本名（Windows 上是 .bat）。
func scriptName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".bat"
	}
	return name
}

// FindJava 返回可用的 java 可执行文件（JAVA_HOME → PATH）。
//
// sdkmanager / avdmanager 依赖 JDK；软件不代管 JDK，也不扫描 Android Studio 自带运行时。
func FindJava() string {
	if v := envOr("JAVA_HOME"); v != "" {
		if c := filepath.Join(v, "bin", exeName("java")); FileExists(c) {
			return c
		}
	}
	for _, dir := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if dir = strings.TrimSpace(dir); dir == "" {
			continue
		}
		if c := filepath.Join(dir, exeName("java")); FileExists(c) {
			return c
		}
	}
	return ""
}

// ChildEnv 构造子进程环境变量：把软件自有 SDK 与 AVD 目录注入，并让工具链互相可见。
//
// 只影响我们启动的子进程，不修改系统 PATH。
func ChildEnv(t Tools, avdHome string) []string {
	env := os.Environ()
	set := func(key, value string) {
		if value == "" {
			return
		}
		prefix := key + "="
		for i, kv := range env {
			if strings.HasPrefix(strings.ToUpper(kv), strings.ToUpper(prefix)) {
				env[i] = prefix + value
				return
			}
		}
		env = append(env, prefix+value)
	}

	set("ANDROID_HOME", t.SdkRoot)
	set("ANDROID_SDK_ROOT", t.SdkRoot)
	// ANDROID_AVD_HOME 指向「包含 <name>.avd 与 <name>.ini 的目录」本身，而不是它的父目录。
	set("ANDROID_AVD_HOME", avdHome)

	paths := []string{t.PlatformTools, t.EmulatorDir, filepath.Join(t.CmdlineTools, "bin")}
	if old := os.Getenv("PATH"); old != "" {
		paths = append(paths, old)
	}
	set("PATH", strings.Join(paths, string(os.PathListSeparator)))
	return env
}
