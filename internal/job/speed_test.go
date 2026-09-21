package job

import (
	"testing"
	"time"
)

// TestSpeedReportedOnEverySample 回归测试：每个采样点都必须给出速度估计值。
//
// 旧实现只在「距上次计算满 0.5s」时算一次速度、其余回调写 0，
// 界面上速度就会一闪一闪（显示半秒、每秒才刷新一次）。
func TestSpeedReportedOnEverySample(t *testing.T) {
	const total = int64(100 << 20) // 100 MB
	j := &Job{}
	base := time.Now()

	// 每 200ms 推进 2MB ≈ 10 MB/s
	var speeds []int64
	for i := 1; i <= 10; i++ {
		done := int64(i) * 2 << 20
		j.setBytesAt(done, total, base.Add(time.Duration(i)*200*time.Millisecond))
		speeds = append(speeds, j.Info().SpeedBps)
	}

	// 第一个采样点没有对比基准，允许为 0
	for i, s := range speeds[1:] {
		if s <= 0 {
			t.Fatalf("第 %d 个采样点速度为 %d（界面会一闪一闪）", i+2, s)
		}
		if s < 8<<20 || s > 12<<20 {
			t.Errorf("第 %d 个采样点速度 = %d，期望接近 10 MB/s（平滑不应失真）", i+2, s)
		}
	}
}

// TestSpeedMatchesRealRate 验证平滑不会长期偏离真实速率。
func TestSpeedMatchesRealRate(t *testing.T) {
	const total = int64(1 << 30)
	j := &Job{}
	base := time.Now()
	// 每 300ms 推进 3MB ≈ 10 MB/s
	var last int64
	for i := 1; i <= 20; i++ {
		j.setBytesAt(int64(i)*3<<20, total, base.Add(time.Duration(i)*300*time.Millisecond))
		last = j.Info().SpeedBps
	}
	if last < 8<<20 || last > 12<<20 {
		t.Errorf("稳定后的速度 = %d，期望接近 10 MB/s", last)
	}
}

// TestSpeedDecaysWhenStalled 验证下载停滞（例如已进入解压阶段）时速度会归零，
// 不会一直显示旧数字。
func TestSpeedDecaysWhenStalled(t *testing.T) {
	const total = int64(1 << 30)
	j := &Job{}
	base := time.Now()
	j.setBytesAt(1<<20, total, base)
	j.setBytesAt(3<<20, total, base.Add(200*time.Millisecond))
	if j.Info().SpeedBps <= 0 {
		t.Fatal("应先估算出速度")
	}
	// 字节数不再增长，持续按采样节奏回调
	for i := 1; i <= 12; i++ {
		j.setBytesAt(3<<20, total, base.Add(200*time.Millisecond+time.Duration(i)*300*time.Millisecond))
	}
	if got := j.Info().SpeedBps; got != 0 {
		t.Errorf("停滞 3.6s 后速度 = %d，期望 0", got)
	}
}

// TestSpeedETAUsesSmoothedRate 验证 ETA 用的是平滑后的速度而不是瞬时值。
func TestSpeedETAUsesSmoothedRate(t *testing.T) {
	const total = int64(100 << 20)
	j := &Job{}
	base := time.Now()
	for i := 1; i <= 10; i++ {
		j.setBytesAt(int64(i)*2<<20, total, base.Add(time.Duration(i)*200*time.Millisecond))
	}
	info := j.Info()
	if info.SpeedBps <= 0 {
		t.Fatal("速度应大于 0")
	}
	want := int((total - info.BytesDone) / info.SpeedBps)
	if diff := info.ETASeconds - want; diff < -1 || diff > 1 {
		t.Errorf("ETA = %d 秒，期望约 %d 秒（应按当前速度推算）", info.ETASeconds, want)
	}
}
