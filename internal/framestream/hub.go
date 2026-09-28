package framestream

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// defaultWriteTimeout 是 WebSocket 单条帧消息的默认写出超时。
	defaultWriteTimeout = 250 * time.Millisecond

	// statsBucketDuration 与 statsBucketCount 共同组成约 1 秒的滑动窗口。
	statsBucketDuration = 100 * time.Millisecond
	statsBucketCount    = 10
)

// Options 控制 Hub 的认证与发送行为。
type Options struct {
	Token        string
	WriteTimeout time.Duration
	Logf         func(format string, args ...any)
}

// Stats 是原始帧 Hub 的瞬时统计快照。
type Stats struct {
	Subscribers       int
	PublishTotal      uint64
	PublishFPS        float64
	DropForClient     uint64
	BytesPerSec       float64
	LastPublishUnixMs int64
}

// frameRef 是一帧在 Hub 生命周期内的引用计数对象。
//
// refs 只在 Hub.mu 保护下修改；Release 回调必须在解锁后调用。
type frameRef struct {
	frame Frame
	refs  int
}

// Hub 维护每个设备的一条原始帧流。
//
// 设计约束：
//   - latest 持有一份引用，每个订阅者的单槽 mailbox 至多持有一份引用；
//   - Publish 只替换最新帧和 mailbox，绝不等待网络写出；
//   - 慢订阅者覆盖旧帧时累计 DropForClient。
type Hub struct {
	mu     sync.Mutex
	opts   Options
	done   chan struct{}
	closed bool

	latest *frameRef
	subs   map[*subscriber]struct{}
	stats  hubStats
}

// NewHub 创建原始帧 Hub。
func NewHub(opts Options) *Hub {
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = defaultWriteTimeout
	}
	return &Hub{
		opts: opts,
		done: make(chan struct{}),
		subs: make(map[*subscriber]struct{}),
	}
}

// Publish 取得 frame 的所有权，并立即返回。
//
// 新帧会替换 latest 和所有订阅者的单槽 mailbox。被替换或最终无人引用的帧会在
// 解锁后统一调用 Release；因此 Release 回调中再次调用 Hub 方法也不会死锁。
func (h *Hub) Publish(frame Frame) {
	if h == nil {
		release(frame)
		return
	}

	h.mu.Lock()
	now := time.Now()
	if h.closed {
		h.mu.Unlock()
		release(frame)
		return
	}

	ref := &frameRef{frame: frame, refs: 1}
	var releases []Frame

	oldLatest := h.latest
	h.latest = ref
	h.releaseRefLocked(oldLatest, &releases)

	for sub := range h.subs {
		ref.refs++

		oldPending := sub.pending
		if oldPending != nil {
			h.stats.dropForClient++
			h.releaseRefLocked(oldPending, &releases)
		}
		sub.pending = ref

		// 唤醒信号允许合并；真正是否丢帧由上面的 mailbox 覆盖判定。
		select {
		case sub.wake <- struct{}{}:
		default:
		}
	}

	h.stats.recordPublish(now, len(frame.Pix))
	h.mu.Unlock()

	for _, pending := range releases {
		release(pending)
	}
}

// Stats 返回当前统计快照，可与 Publish/订阅并发调用。
func (h *Hub) Stats() Stats {
	if h == nil {
		return Stats{}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stats.snapshot(time.Now(), len(h.subs))
}

// Close 关闭 Hub、唤醒并关闭所有订阅者，同时释放仍被 Hub 持有的帧。可重复调用。
func (h *Hub) Close() {
	if h == nil {
		return
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	if h.done != nil {
		close(h.done)
	}

	var releases []Frame
	h.releaseRefLocked(h.latest, &releases)
	h.latest = nil

	subs := make([]*subscriber, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
		h.releaseRefLocked(sub.pending, &releases)
		sub.pending = nil
		sub.registered = false
	}
	h.subs = nil
	h.mu.Unlock()

	// 先关闭连接让读循环和写循环尽快退出，再执行可能较慢的生产者回调。
	for _, sub := range subs {
		sub.close()
	}
	for _, pending := range releases {
		release(pending)
	}
}

// addSubscriber 注册一个已升级的 WebSocket 连接。
//
// 新订阅者会立即获得当前 latest 的一份引用，这样静止画面无需等待下一帧。
func (h *Hub) addSubscriber(conn *websocket.Conn) *subscriber {
	sub := newSubscriber(conn)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		sub.close()
		return nil
	}
	if h.subs == nil {
		h.subs = make(map[*subscriber]struct{})
	}
	h.subs[sub] = struct{}{}
	sub.registered = true

	if h.latest != nil {
		h.latest.refs++
		sub.pending = h.latest
		select {
		case sub.wake <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
	return sub
}

// takePending 把订阅者 mailbox 中的引用转交给写协程。
//
// live=false 表示 Hub 已关闭或订阅者已注销，写协程应直接退出。
func (h *Hub) takePending(sub *subscriber) (ref *frameRef, live bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed || sub == nil || !sub.registered {
		return nil, false
	}
	ref = sub.pending
	sub.pending = nil
	return ref, true
}

// removeSubscriber 幂等注销订阅者，并释放其 mailbox 中尚未消费的帧。
func (h *Hub) removeSubscriber(sub *subscriber) {
	if h == nil || sub == nil {
		return
	}

	var releases []Frame
	h.mu.Lock()
	if sub.registered {
		delete(h.subs, sub)
		sub.registered = false
	}
	h.releaseRefLocked(sub.pending, &releases)
	sub.pending = nil
	h.mu.Unlock()

	for _, pending := range releases {
		release(pending)
	}
	sub.close()
}

// releaseRefLocked 减少一次引用；只有引用归零才把帧交给解锁后的释放阶段。
func (h *Hub) releaseRefLocked(ref *frameRef, releases *[]Frame) {
	if ref == nil || ref.refs <= 0 {
		return
	}
	ref.refs--
	if ref.refs == 0 {
		*releases = append(*releases, ref.frame)
	}
}

// hubStats 记录 Hub 累计值及最近约 1 秒的发布速率。
type hubStats struct {
	publishTotal      uint64
	dropForClient     uint64
	lastPublishUnixMs int64

	windowStart time.Time
	current     int
	buckets     [statsBucketCount]hubStatsBucket
}

// hubStatsBucket 保存一个固定时间片内的发布量和字节数。
type hubStatsBucket struct {
	publishes uint64
	bytes     uint64
}

// recordPublish 记录一次成功发布。
func (s *hubStats) recordPublish(now time.Time, size int) {
	s.advance(now)
	bucket := &s.buckets[s.current]
	bucket.publishes++
	bucket.bytes += uint64(size)
	s.publishTotal++
	s.lastPublishUnixMs = now.UnixMilli()
}

// snapshot 返回统计值，并顺带推进滑动窗口。
func (s *hubStats) snapshot(now time.Time, subscribers int) Stats {
	s.advance(now)

	var publishes, bytes uint64
	for i := range s.buckets {
		publishes += s.buckets[i].publishes
		bytes += s.buckets[i].bytes
	}
	return Stats{
		Subscribers:       subscribers,
		PublishTotal:      s.publishTotal,
		PublishFPS:        float64(publishes),
		BytesPerSec:       float64(bytes),
		DropForClient:     s.dropForClient,
		LastPublishUnixMs: s.lastPublishUnixMs,
	}
}

// advance 把环形窗口推进到 now；长时间无流量时直接清空旧桶。
func (s *hubStats) advance(now time.Time) {
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
		s.buckets[s.current] = hubStatsBucket{}
	}
	s.windowStart = s.windowStart.Add(time.Duration(steps) * statsBucketDuration)
}

// resetWindow 清空统计窗口并从 now 重新开始。
func (s *hubStats) resetWindow(now time.Time) {
	s.windowStart = now
	s.current = 0
	s.buckets = [statsBucketCount]hubStatsBucket{}
}
