//go:build windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// openPathOS 用 explorer.exe 打开目标。
//
// 参数一律以数组形式交给 CreateProcess，不经过 shell，路径里的空格、& 、
// 括号都不会被解释，因此不需要（也不应该）自己做转义。
func openPathOS(target string, selectFile bool) error {
	explorer := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	if !FileExists(explorer) {
		explorer = "explorer.exe" // 交给 PATH 解析
	}
	arg := target
	if selectFile {
		arg = "/select," + target // 文件：打开父目录并选中该文件
	}
	// explorer.exe 成功时也可能返回退出码 1，所以只 Start，不判退出码。
	if err := startDetached(explorer, arg); err != nil {
		return fmt.Errorf("无法打开文件管理器: %w", err)
	}
	return nil
}

// openTerminalOS 在 dir 目录打开一个新的 cmd 窗口。
//
// 走 ShellExecute（等同在资源管理器里双击 cmd.exe）并把工作目录交给系统设置：
// 命令行里不出现任何用户数据，天然免疫引号/元字符注入，新窗口也保证可见、可交互。
func openTerminalOS(dir string) error {
	if !DirExists(dir) {
		if err := EnsureDir(dir); err != nil {
			return fmt.Errorf("目录不存在: %s", dir)
		}
	}
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if !FileExists(cmdExe) {
		cmdExe = "cmd.exe"
	}
	if err := shellExecute(cmdExe, "", dir); err != nil {
		return fmt.Errorf("无法打开终端: %w", err)
	}
	return nil
}

// shellExecute 调用 Win32 ShellExecuteW（"用系统默认方式打开"）。
func shellExecute(file, params, workingDir string) error {
	var paramsPtr, dirPtr *uint16
	if params != "" {
		p, err := windows.UTF16PtrFromString(params)
		if err != nil {
			return err
		}
		paramsPtr = p
	}
	if workingDir != "" {
		d, err := windows.UTF16PtrFromString(workingDir)
		if err != nil {
			return err
		}
		dirPtr = d
	}
	return windows.ShellExecute(0, nil, windows.StringToUTF16Ptr(file), paramsPtr, dirPtr, windows.SW_SHOWNORMAL)
}
