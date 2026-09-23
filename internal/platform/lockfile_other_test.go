//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestAcquireLockFileExcludesSecondHolder 验证锁文件互斥：
// 第二个持有者必须拿到 held=false（而不是两个实例同时写同一份 SDK/AVD 目录），
// 释放后可以再次获取。
func TestAcquireLockFileExcludesSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "app.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	release, held, err := acquireLockFile(path)
	if err != nil || !held {
		t.Fatalf("首次获取失败：held=%v err=%v", held, err)
	}
	defer release()

	secondRelease, secondHeld, err := acquireLockFile(path)
	if err != nil {
		t.Fatalf("第二次获取返回错误：%v", err)
	}
	if secondHeld {
		secondRelease()
		t.Fatal("锁被占用时第二个持有者不应拿到锁")
	}

	// 锁文件里记录当前 PID，便于人工排查
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("锁文件不可读：%v", err)
	}
	if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr != nil || pid != os.Getpid() {
		t.Fatalf("锁文件内容 = %q，期望当前进程 PID %d", string(data), os.Getpid())
	}

	release()
	thirdRelease, thirdHeld, err := acquireLockFile(path)
	if err != nil || !thirdHeld {
		t.Fatalf("释放后应能重新获取：held=%v err=%v", thirdHeld, err)
	}
	thirdRelease()
}
