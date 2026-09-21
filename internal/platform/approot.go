// Package platform 封装宿主环境差异：软件自有目录布局、工具链路径、通用文件操作与单实例锁。
//
// 目录布局（跨平台一致，不再依赖系统 Android SDK）：
//
//	<Root>/sdk      软件自己的 ANDROID_SDK_ROOT
//	<Root>/avd      软件自己的 ANDROID_AVD_HOME
//	<Root>/config   设置
//	<Root>/logs     应用日志
//	<Root>/cache    下载临时文件
package platform

import (
	"os"
	"path/filepath"
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
//  2. 可执行文件所在目录（可写时）
//  3. 用户数据目录（安装到只读位置时的回退）
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
		if IsWritable(dir) {
			return dir
		}
	}
	return userDataDir(AppDirName)
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
	return filepath.Join(".", name)
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

// EnsureLayout 创建软件自有目录布局（幂等）。
func EnsureLayout() error {
	for _, dir := range []string{SdkRoot(), AvdHome(), filepath.Dir(SettingsPath()), LogDir(), CacheDir()} {
		if err := EnsureDir(dir); err != nil {
			return err
		}
	}
	return nil
}
