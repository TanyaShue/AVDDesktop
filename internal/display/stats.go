package display

import "time"

const (
	// statsBucketDuration 与 statsBucketCount 共同组成约 1 秒的滑动窗口。
	statsBucketDuration = 100 * time.Millisecond
	statsBucketCount    = 10
)

// SessionStats 是画面会话的瞬时统计快照。
type SessionStats struct {
	Subscribers       int
	PublishTotal      uint64
	PublishFPS        float64
	BytesPerSec       float64
	DropForClient     uint64
	LastPublishUnixMs int64
}

// statsBucket 记录一个时间桶内的成功发布量与字节数。
type statsBucket struct {
	publishes uint64
	bytes     uint64
}

// sessionStats 记录会话累计值与最近 1 秒的发布速率。
//
// 该结构由 Session.mu 统一保护；固定大小的环形桶避免在每帧发布路径上分配内存。
type sessionStats struct {
	publishTotal      uint64
	dropForClient     uint64
	lastPublishUnixMs int64

	windowStart time.Time
	current     int
	buckets     [statsBucketCount]statsBucket
}

// recordPublish 记录一次成功发布。
func (s *sessionStats) recordPublish(now time.Time, size int) {
	s.advance(now)
	bucket := &s.buckets[s.current]
	bucket.publishes++
	bucket.bytes += uint64(size)
	s.publishTotal++
	s.lastPublishUnixMs = now.UnixMilli()
}

// recordDropForClient 记录一次因订阅者未及时消费而被合并的唤醒。
func (s *sessionStats) recordDropForClient() {
	s.dropForClient++
}

// snapshot 返回当前统计值，并顺带淘汰窗口外的旧桶。
func (s *sessionStats) snapshot(now time.Time, subscribers int) SessionStats {
	s.advance(now)
	var publishes, bytes uint64
	for i := range s.buckets {
		publishes += s.buckets[i].publishes
		bytes += s.buckets[i].bytes
	}
	return SessionStats{
		Subscribers:       subscribers,
		PublishTotal:      s.publishTotal,
		PublishFPS:        float64(publishes),
		BytesPerSec:       float64(bytes),
		DropForClient:     s.dropForClient,
		LastPublishUnixMs: s.lastPublishUnixMs,
	}
}

// advance 把环形窗口推进到 now；长时间无流量时直接清空旧桶。
func (s *sessionStats) advance(now time.Time) {
	if s.windowStart.IsZero() || now.Before(s.windowStart) {
		s.resetWindow(now)
		return
	}
	steps := int(now.Sub(s.windowStart) / statsBucketDuration)
	if steps <= 0 {
		return
	}
	if steps >= statsBucketCount {
		s.resetWindow(now)
		return
	}
	for i := 0; i < steps; i++ {
		s.current = (s.current + 1) % statsBucketCount
		s.buckets[s.current] = statsBucket{}
	}
	s.windowStart = s.windowStart.Add(time.Duration(steps) * statsBucketDuration)
}

// resetWindow 清空统计窗口并从 now 重新开始。
func (s *sessionStats) resetWindow(now time.Time) {
	s.windowStart = now
	s.current = 0
	s.buckets = [statsBucketCount]statsBucket{}
}
