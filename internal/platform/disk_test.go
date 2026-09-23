package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiskSpaceReadsVolume 验证卷容量探测：总容量与剩余空间都来自真实卷，
// 且不存在的路径会向上回退到最近的已存在祖先（软件根目录可能尚未创建）。
func TestDiskSpaceReadsVolume(t *testing.T) {
	dir := t.TempDir()

	got := DiskSpace(dir)
	if got.Path != dir {
		t.Fatalf("Path = %q，期望 %q", got.Path, dir)
	}
	if got.TotalGB <= 0 {
		t.Fatalf("总容量 = %d GB，应大于 0", got.TotalGB)
	}
	if got.FreeGB <= 0 || got.FreeGB > got.TotalGB {
		t.Fatalf("剩余空间 = %d GB（总容量 %d GB），应落在 (0, 总量] 区间", got.FreeGB, got.TotalGB)
	}

	// 不存在的深层路径：回退到已存在的祖先目录，仍然能读到容量。
	missing := filepath.Join(dir, "not-created-yet", "sdk")
	if got := DiskSpace(missing); got.TotalGB <= 0 {
		t.Fatalf("不存在的路径应回退到已存在的祖先目录，实际 %+v", got)
	}
}

// TestDiskSpaceEmptyPath 空路径不应触发任何探测，返回零值。
func TestDiskSpaceEmptyPath(t *testing.T) {
	if got := DiskSpace(""); got.TotalGB != 0 || got.FreeGB != 0 || got.Sufficient {
		t.Fatalf("空路径应返回零值，实际 %+v", got)
	}
}

// TestDiskSpaceSufficientThreshold 验证 12 GB 的判定阈值（README 要求的分区预留量）。
func TestDiskSpaceSufficientThreshold(t *testing.T) {
	dir := t.TempDir()
	got := DiskSpace(dir)
	if want := got.FreeGB >= 12; got.Sufficient != want {
		t.Fatalf("Sufficient = %v，剩余 %d GB 时期望 %v", got.Sufficient, got.FreeGB, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("测试目录异常：%v", err)
	}
}
