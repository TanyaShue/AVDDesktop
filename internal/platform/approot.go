// Package platform 封装宿主环境差异：软件自有目录布局、工具链路径、通用文件操作与单实例锁。
//
// 目录布局（跨平台一致，不再依赖系统 Android SDK / JDK）：
//
//	<Root>/jdk      软件自己的 JDK（JAVA_HOME）
//	<Root>/sdk      软件自己的 ANDROID_SDK_ROOT
//	<Root>/avd      软件自己的 ANDROID_AVD_HOME
//	<Root>/config   设置
//	<Root>/logs     应用日志
//	<Root>/cache    下载临时文件
package platform

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AppDirName 是回退到用户数据目录时使用的目录名。
const AppDirName = "AVDDesktop"

var (
	rootOnce sync.Once
	rootDir  string
)

// Root 返回软件自有数据根目录。
//
// 解析顺序：
//  1. 环境变量 AVDDESKTOP_HOME（便携部署 / 测试用）
//  2. 可执行文件所在目录（可写，且不是临时目录或 macOS .app 包内部时）
//  3. 用户数据目录
func Root() string {
	rootOnce.Do(func() { rootDir = resolveRoot() })
	return rootDir
}

func resolveRoot() string {
	if v := envOr("AVDDESKTOP_HOME"); v != "" {
		return filepath.Clean(v)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if usableAppDir(dir) && IsWritable(dir) {
			return dir
		}
	}
	return userDataDir(AppDirName)
}

// usableAppDir 判断可执行文件目录是否适合当作软件根目录。
//
// 排除两类情况：
//   - 临时目录（wails dev / go run 的构建产物在临时目录里）
//   - macOS 的 .app 包内部（SDK 不应该装进应用包）
func usableAppDir(dir string) bool {
	slash := filepath.ToSlash(dir)
	if strings.Contains(slash, ".app/Contents") {
		return false
	}
	tmp := filepath.ToSlash(os.TempDir())
	return tmp == "" || !strings.HasPrefix(slash+"/", strings.TrimSuffix(tmp, "/")+"/")
}

// userDataDir 返回各平台的用户数据目录。
func userDataDir(name string) string {
	switch runtimeGOOS() {
	case "windows":
		if v := envOr("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, name)
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", name)
		}
	default:
		if v := envOr("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, name)
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", name)
		}
	}
	// 标准位置都不可用（精简环境 / 服务式启动）时退回当前工作目录的绝对路径：
	// 相对路径会让数据根随启动方式漂移（双击与从终端启动得到不同目录，表现为"设置丢失"，
	// 而且各自的 config 目录不同会让单实例锁失效）。
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, name)
	}
	return filepath.Join(os.TempDir(), name)
}

// JdkRoot 返回软件自带 JDK 的根目录（macOS 归档保留 Contents 目录结构）。
func JdkRoot() string { return filepath.Join(Root(), "jdk") }

// JdkHome 返回实际 JAVA_HOME。Windows / Linux 与 JdkRoot 相同，
// macOS 的 Temurin 归档把真正的 JDK Home 放在 Contents/Home 下。
func JdkHome() string { return jdkHomeAt(JdkRoot(), runtimeGOOS()) }

func jdkHomeAt(root, goos string) string {
	if goos == "darwin" {
		return filepath.Join(root, "Contents", "Home")
	}
	return root
}

// SdkRoot 返回软件自有 SDK 根目录。
func SdkRoot() string { return filepath.Join(Root(), "sdk") }

// AvdHome 返回软件自有 AVD 主目录（存放 <name>.ini 与 <name>.avd）。
func AvdHome() string { return filepath.Join(Root(), "avd") }

// SettingsPath 返回设置文件路径。
func SettingsPath() string { return filepath.Join(Root(), "config", "settings.json") }

// LogDir 返回日志目录。
func LogDir() string { return filepath.Join(Root(), "logs") }

// CacheDir 返回下载临时目录。
func CacheDir() string { return filepath.Join(Root(), "cache") }

// ScreenshotDir 返回设备窗口截图目录。
func ScreenshotDir() string { return filepath.Join(Root(), "screenshots") }

// EnsureLayout 创建软件自有目录布局（幂等）。
func EnsureLayout() error {
	for _, dir := range []string{JdkRoot(), SdkRoot(), AvdHome(), filepath.Dir(SettingsPath()), LogDir(), CacheDir(), ScreenshotDir()} {
		if err := EnsureDir(dir); err != nil {
			return err
		}
	}
	return nil
}
