//go:build !windows

package proc

import (
	"context"
	"os/exec"
	"syscall"
)

// applySysProcAttr 让子进程独立成进程组，便于整组终止。
func applySysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillTree 终止进程组。
//
// 与 Windows 实现同理：终止动作不继承调用方已取消的 ctx，否则兜底的 kill 命令
// 根本不会执行（先尝试的 SIGKILL 失败时尤为明显）。
func KillTree(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
		return nil
	}
	killCtx, cancel := contextWithShortTimeout(context.Background())
	defer cancel()
	return exec.CommandContext(killCtx, "kill", "-9", itoa(pid)).Run()
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
