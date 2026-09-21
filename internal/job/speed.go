package job

import (
	"math"
	"time"
)

// 速度估算参数。
const (
	// speedTau 是指数平滑的时间常数：越大越平稳、越迟钝。
	speedTau = 700 * time.Millisecond
	// speedStale 是停滞判定阈值：字节数超过这么久没有增长，就认为下载已经停止。
	speedStale = 2 * time.Second
)

// speedEstimator 估算下载速度。
//
// 两条约定都是踩过坑的：
//
//  1. 每个采样点都必须返回当前估计值。旧实现只在「距上次计算满 0.5s」时算一次速度、
//     其余回调写 0，界面上速度一闪一闪（显示半秒、消失半秒）。
//  2. 平滑系数按采样间隔换算（alpha = 1 - e^(-Δt/τ)），因此采样节奏变化（200ms 的
//     下载回调 / 300ms 的进度轮询）不会让数值忽高忽低。
//
// 字节数停止增长超过 speedStale 时速度归零并重新起算，避免下载结束进入解压阶段后
// 界面上还挂着一个旧速度。
type speedEstimator struct {
	started    bool
	seeded     bool
	lastAt     time.Time // 上次采样时间
	lastBytes  int64     // 上次采样的字节数
	lastDataAt time.Time // 字节数最后一次增长的时间
	speed      float64
}

// sample 记录一次采样并返回当前速度估计（字节/秒，0 表示暂时无法估算）。
func (s *speedEstimator) sample(done int64, now time.Time) int64 {
	if !s.started {
		s.started = true
		s.lastAt, s.lastBytes, s.lastDataAt = now, done, now
		return 0
	}

	elapsed := now.Sub(s.lastAt)
	if elapsed <= 0 {
		// 同一时刻重复采样或时钟回拨：沿用上次估计
		return int64(s.speed)
	}

	if done > s.lastBytes {
		s.lastDataAt = now
	}

	if now.Sub(s.lastDataAt) >= speedStale {
		// 已经没有新数据：归零并重新起算，下一次采样直接采用瞬时值
		s.lastAt, s.lastBytes = now, done
		s.speed, s.seeded = 0, false
		return 0
	}

	instant := float64(done-s.lastBytes) / elapsed.Seconds()
	if instant < 0 {
		instant = 0
	}
	if !s.seeded {
		// 首次可计算：直接采用瞬时值，避免从 0 缓慢爬升（那会让界面长时间偏低）
		s.speed, s.seeded = instant, true
	} else {
		alpha := 1 - math.Exp(-elapsed.Seconds()/speedTau.Seconds())
		s.speed += alpha * (instant - s.speed)
	}
	s.lastAt, s.lastBytes = now, done
	return int64(s.speed)
}
