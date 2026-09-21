//go:build !windows

package platform

// windowsSystemProxy 在非 Windows 平台无实现（环境变量已覆盖绝大多数场景）。
func windowsSystemProxy() string { return "" }
