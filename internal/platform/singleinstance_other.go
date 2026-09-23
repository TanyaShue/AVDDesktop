//go:build !windows

package platform

import (
	"os"
	"path/filepath"
)

// AcquireSingleInstance 通过锁文件实现单实例（类 Unix）。
//
// 具体加锁方式由平台实现决定（见 lockfile_flock.go / lockfile_pid.go）：
// 优先使用内核 flock——进程崩溃时由内核自动释放，既不依赖 PID 存活探测，
// 也不受 PID 复用影响。
func AcquireSingleInstance(name string) (release func(), alreadyRunning bool, err error) {
	lockPath := filepath.Join(Root(), "config", "app.lock")
	if err := EnsureDir(filepath.Dir(lockPath)); err != nil {
		return func() {}, false, err
	}
	release, held, err := acquireLockFile(lockPath)
	if err != nil {
		return func() {}, false, err
	}
	if !held {
		return func() {}, true, nil
	}
	return release, false, nil
}

// FocusExistingInstance 在类 Unix 平台不做操作。
func FocusExistingInstance(title string) {}

// NotifyAlreadyRunning 在类 Unix 平台输出到标准错误。
func NotifyAlreadyRunning(title, message string) {
	_, _ = os.Stderr.WriteString(title + ": " + message + "\n")
}
