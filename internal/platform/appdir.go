package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// AppDataDir 返回应用数据目录（配置、缓存、日志）。
//
// Windows: %LOCALAPPDATA%\AVDDesktop
// macOS:   ~/Library/Application Support/AVDDesktop
// Linux:   ~/.local/share/AVDDesktop（尊重 XDG_DATA_HOME）
func AppDataDir(appName string) string {
	switch runtime.GOOS {
	case "windows":
		if v := envOr("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, appName)
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", appName)
		}
	default:
		if v := envOr("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, appName)
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", appName)
		}
	}
	return filepath.Join(".", appName)
}

// SubDir 返回应用数据目录下的子目录（并确保存在）。
func SubDir(appName, sub string) string {
	dir := filepath.Join(AppDataDir(appName), sub)
	_ = EnsureDir(dir)
	return dir
}
