//go:build !windows

package platform

import (
	"os"
	"strconv"
	"syscall"
)

// acquireLockFile 用 flock 抢占锁文件：由内核维护，进程退出（含崩溃）时自动释放。
//
// 文件不删除：删除会让"另一个进程持有已删除 inode 的锁、第三个进程又新建同名文件"
// 同时成立，反而破坏互斥。写入的 PID 只用于人工排查。
func acquireLockFile(path string) (release func(), held bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr != nil {
		_ = f.Close()
		if lockErr == syscall.EWOULDBLOCK || lockErr == syscall.EAGAIN {
			return nil, false, nil // 已有实例持有锁
		}
		return nil, false, lockErr
	}
	// 持有锁：记录 PID 便于排查（锁的归属不依赖该内容）。
	if truncErr := f.Truncate(0); truncErr == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	return func() { _ = f.Close() }, true, nil
}
