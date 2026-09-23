//go:build windows

package platform

import "testing"

// TestAcquireSingleInstanceExcludesSecondHolder 验证命名互斥体的单实例语义：
// 第二个实例必须拿到 alreadyRunning=true，释放后可以再次获取。
func TestAcquireSingleInstanceExcludesSecondHolder(t *testing.T) {
	const name = "AVDDesktopSingleInstanceTest"

	release, alreadyRunning, err := AcquireSingleInstance(name)
	if err != nil {
		t.Fatalf("首次获取失败：%v", err)
	}
	if alreadyRunning {
		t.Fatal("首次获取不应报告已有实例")
	}
	defer release()

	secondRelease, secondRunning, err := AcquireSingleInstance(name)
	if err != nil {
		t.Fatalf("第二次获取返回错误：%v", err)
	}
	if !secondRunning {
		secondRelease()
		t.Fatal("互斥体已被占用时第二个实例必须报告 alreadyRunning")
	}

	release()
	thirdRelease, thirdRunning, err := AcquireSingleInstance(name)
	if err != nil || thirdRunning {
		t.Fatalf("释放后应能重新获取：alreadyRunning=%v err=%v", thirdRunning, err)
	}
	thirdRelease()
}
