//go:build windows

package proc

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	createNoWindow        = 0x08000000
	createNewProcessGroup = 0x00000200
)

// applySysProcAttr 隐藏控制台窗口并让子进程独立成组（便于整树终止）。
func applySysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | createNewProcessGroup,
	}
}

// KillTree 终止进程及其子进程（模拟器会派生 qemu 等子进程）。
func KillTree(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	// 优先使用 taskkill /T /F 保证子进程一起结束
	killCtx, cancel := contextWithShortTimeout(ctx)
	defer cancel()
	cmd := exec.CommandContext(killCtx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	applySysProcAttr(cmd)
	return cmd.Run()
}
