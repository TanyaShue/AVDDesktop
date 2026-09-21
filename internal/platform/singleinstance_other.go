//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// AcquireSingleInstance 通过锁文件 + PID 实现单实例（类 Unix）。
func AcquireSingleInstance(name string) (release func(), alreadyRunning bool, err error) {
	lockPath := filepath.Join(AppDataDir(name), "app.lock")
	if err := EnsureDir(filepath.Dir(lockPath)); err != nil {
		return func() {}, false, err
	}

	if data, readErr := os.ReadFile(lockPath); readErr == nil {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
			if processAlive(pid) {
				return func() {}, true, nil
			}
		}
		// 陈旧锁：进程已不存在，继续接管
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return func() {}, false, err
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		_ = f.Close()
		return func() {}, false, err
	}
	_ = f.Close()

	return func() { _ = os.Remove(lockPath) }, false, nil
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// 信号 0 只做存在性检查
	return proc.Signal(syscall.Signal(0)) == nil
}

// FocusExistingInstance 在类 Unix 平台不做操作。
func FocusExistingInstance(title string) {}

// NotifyAlreadyRunning 在类 Unix 平台输出到标准错误。
func NotifyAlreadyRunning(title, message string) {
	_, _ = os.Stderr.WriteString(title + ": " + message + "\n")
}
