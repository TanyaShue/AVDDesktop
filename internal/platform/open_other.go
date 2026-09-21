//go:build !windows

package platform

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openPathOS 在 macOS / Linux 的文件管理器中打开目标。
func openPathOS(target string, selectFile bool) error {
	if runtime.GOOS == "darwin" {
		if selectFile {
			return startDetached("open", "-R", target) // 打开所在目录并选中
		}
		return startDetached("open", target)
	}
	if selectFile {
		target = filepath.Dir(target)
	}
	return startDetached("xdg-open", target)
}

// openTerminalOS 在 dir 目录打开系统终端。
func openTerminalOS(dir string) error {
	if runtime.GOOS == "darwin" {
		return startDetached("open", "-a", "Terminal", dir)
	}
	// Linux 各发行版终端不一，按常见顺序探测（第一个能起来的就用）。
	candidates := [][]string{
		{"x-terminal-emulator", "--working-directory=" + dir},
		{"gnome-terminal", "--working-directory=" + dir},
		{"konsole", "--workdir", dir},
		{"xfce4-terminal", "--working-directory=" + dir},
		{"alacritty", "--working-directory", dir},
		{"kitty", "--directory", dir},
	}
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err != nil {
			continue
		}
		if err := startDetached(candidate[0], candidate[1:]...); err != nil {
			continue
		}
		return nil
	}
	return errors.New("未找到可用的终端程序")
}
