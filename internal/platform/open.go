package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// 打开目录 / 打开终端（宿主机交互）。
//
// 注意：**不要**用 wailsruntime.BrowserOpenURL 拼 `file://` URL 来实现"打开目录"。
// Wails v2.16 的 utils.ValidateAndSanitizeURL 会拒绝 `file` 方案，并且把路径里的
// 空格、反斜杠、括号等当作 shell 元字符一并拒绝；而 BrowserOpenURL 没有返回值，
// 失败只会写进 Wails 自己的日志。表现到界面上就是"点了按钮没反应"。
// 正确做法是直接调用系统 API / 文件管理器（本文件）。

// OpenPath 在系统文件管理器中打开路径。
//
//   - 目录：直接打开该目录；
//   - 文件：打开所在目录并选中该文件（对齐"打开位置"的语义）；
//   - 路径不存在：退到最近的已存在父目录（不创建目录，也不报错），
//     这样组件缺失时点"打开位置"仍能落到 SDK 根目录而不是弹一个无用的错误。
func OpenPath(path string) error {
	target, selectFile, err := resolveOpenTarget(path)
	if err != nil {
		return err
	}
	return openPathOS(target, selectFile)
}

// OpenTerminal 在指定目录打开系统终端（传入文件路径时使用其所在目录）。
func OpenTerminal(dir string) error {
	target, selectFile, err := resolveOpenTarget(dir)
	if err != nil {
		return err
	}
	if selectFile {
		target = filepath.Dir(target)
	}
	return openTerminalOS(target)
}

// resolveOpenTarget 把用户给的路径归一化成绝对路径，并判断是否需要"选中"。
func resolveOpenTarget(path string) (target string, selectFile bool, err error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", false, errors.New("路径为空")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		abs = trimmed
	}
	abs = filepath.Clean(abs)

	if DirExists(abs) {
		return abs, false, nil
	}
	if FileExists(abs) {
		return abs, true, nil
	}

	// 路径还不存在（例如设备目录被手删、组件未安装）：向上找到最近的已存在目录。
	parent := filepath.Dir(abs)
	for {
		if DirExists(parent) {
			return parent, false, nil
		}
		up := filepath.Dir(parent)
		if up == parent { // 已经到根，仍然不存在
			return "", false, fmt.Errorf("路径不存在: %s", path)
		}
		parent = up
	}
}

// startDetached 启动外部程序并立即返回（文件管理器/终端会长期驻留）。
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动 %s: %w", filepath.Base(name), err)
	}
	// 后台回收子进程，避免留下僵尸进程（调用方不等它退出）。
	go func() { _ = cmd.Wait() }()
	return nil
}
