package displayhost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// statsMarker 是辅助进程上报机器可读统计行的私有前缀。
	statsMarker = "__AVDDESKTOP_STATS__"

	// statsModeEncode 是正常画面链路：复制、编码并发布 JPEG。
	statsModeEncode = "encode"
	// statsModeRecvOnly 是基准模式：只复制并统计输入帧，不编码、不发布。
	statsModeRecvOnly = "recv-only"

	// statsDurationSamples 是耗时 p50/p95 使用的滑动窗口大小。
	statsDurationSamples = 256
)

// PipelineStats 是设备窗口服务端画面链路的一次统计快照。
//
// JSON 字段名由设备窗口前端直接消费，修改时必须同步前端。
type PipelineStats struct {
	UptimeMs         int64   `json:"uptimeMs"`
	Mode             string  `json:"mode"` // "encode" | "recv-only"
	StreamWidth      int     `json:"streamWidth"`
	StreamHeight     int     `json:"streamHeight"`
	RecvFPS          float64 `json:"recvFps"`
	PublishFPS       float64 `json:"publishFps"`
	RecvTotal        uint64  `json:"recvTotal"`
	PublishTotal     uint64  `json:"publishTotal"`
	Seq              uint32  `json:"seq"`
	SeqGapFrames     uint64  `json:"seqGapFrames"`
	CopyMsP50        float64 `json:"copyMsP50"`
	CopyMsP95        float64 `json:"copyMsP95"`
	EncodeMsP50      float64 `json:"encodeMsP50"`
	EncodeMsP95      float64 `json:"encodeMsP95"`
	Subscribers      int     `json:"subscribers"`
	DropBeforeEncode uint64  `json:"dropBeforeEncode"`
	DropForClient    uint64  `json:"dropForClient"`
	LastSeqAtUnixMs  int64   `json:"lastSeqAtUnixMs"`
}

// durationRing 是固定大小的原子耗时环形缓冲。
//
// 每帧只做一次原子写入；Stats 快照读取最近样本后临时排序，所以逐帧路径没有锁和按帧分配。
type durationRing struct {
	next   atomic.Uint64
	values [statsDurationSamples]atomic.Int64
}

// record 写入一个耗时样本；窗口写满后覆盖最旧样本。
func (r *durationRing) record(d time.Duration) {
	if r == nil {
		return
	}
	index := r.next.Add(1) - 1
	r.values[index%uint64(len(r.values))].Store(int64(d))
}

// percentile 返回最近样本的百分位耗时，单位为毫秒。
//
// 使用 nearest-rank：p50/p95 都返回一个实际采样值，避免插值掩盖尾延迟。
func (r *durationRing) percentile(percent float64) float64 {
	if r == nil {
		return 0
	}
	count := r.next.Load()
	if count == 0 {
		return 0
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	n := int(count)
	if n > statsDurationSamples {
		n = statsDurationSamples
	}
	samples := make([]int64, n)
	first := count - uint64(n)
	for i := range samples {
		index := (first + uint64(i)) % uint64(len(r.values))
		samples[i] = r.values[index].Load()
	}
	slices.Sort(samples)

	rank := int(math.Ceil(percent/100*float64(n))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= n {
		rank = n - 1
	}
	return float64(samples[rank]) / float64(time.Millisecond)
}

// pipelineStats 汇总设备窗口服务端画面链路统计。
//
// 接收、编码和发布由不同协程访问；除耗时环形缓冲外全部使用原子计数。
type pipelineStats struct {
	startedAt    time.Time
	mode         string
	streamWidth  int
	streamHeight int

	recvTotal         atomic.Uint64
	publishTotal      atomic.Uint64
	dropBeforeEncode  atomic.Uint64
	seq               atomic.Uint32
	seqInitialized    atomic.Bool
	seqGapFrames      atomic.Uint64
	recvFPSCurrent    atomic.Uint64 // math.Float64bits
	publishFPSCurrent atomic.Uint64 // math.Float64bits
	lastSeqAtUnixMs   atomic.Int64

	copyDurations   durationRing
	encodeDurations durationRing

	// subscribers / dropForClient 由 display.Session 的统计接口提供，
	// 由 RunHelper 在会话创建后接线；未接线时返回 0。
	subscribers   func() int
	dropForClient func() uint64
}

// newPipelineStats 创建一条链路的统计聚合器。
func newPipelineStats(mode string, streamWidth, streamHeight int) *pipelineStats {
	if strings.TrimSpace(mode) != statsModeRecvOnly {
		mode = statsModeEncode
	}
	return &pipelineStats{
		startedAt:    time.Now(),
		mode:         mode,
		streamWidth:  streamWidth,
		streamHeight: streamHeight,
	}
}

// pipelineMode 把 HelperConfig.Benchmark 归一化为统计模式。
func pipelineMode(benchmark string) string {
	if strings.TrimSpace(benchmark) == statsModeRecvOnly {
		return statsModeRecvOnly
	}
	return statsModeEncode
}

// recordFrame 记录一次成功的共享内存复制以及帧序号推进。
func (s *pipelineStats) recordFrame(seq uint32, copyDuration time.Duration) {
	if s == nil {
		return
	}
	s.recvTotal.Add(1)
	s.copyDurations.record(copyDuration)
	s.lastSeqAtUnixMs.Store(time.Now().UnixMilli())

	for {
		initialized := s.seqInitialized.Load()
		if !initialized {
			if s.seqInitialized.CompareAndSwap(false, true) {
				s.seq.Store(seq)
				return
			}
			continue
		}

		prev := s.seq.Load()
		// 重复或回退的序号不重复计 gap，保持最新序号不变。
		if seq <= prev {
			return
		}
		if s.seq.CompareAndSwap(prev, seq) {
			s.seqGapFrames.Add(uint64(seq - prev - 1))
			return
		}
	}
}

// recordEncode 记录一次 JPEG 编码耗时。
func (s *pipelineStats) recordEncode(d time.Duration) {
	if s == nil {
		return
	}
	s.encodeDurations.record(d)
}

// recordPublish 记录一次成功发布到 MJPEG 会话的帧。
func (s *pipelineStats) recordPublish() {
	if s == nil {
		return
	}
	s.publishTotal.Add(1)
}

// recordDropBeforeEncode 记录一次因没有订阅者而在 JPEG 编码前丢弃的帧。
func (s *pipelineStats) recordDropBeforeEncode() {
	if s == nil {
		return
	}
	s.dropBeforeEncode.Add(1)
}

// run 每秒计算最近一秒的接收/发布 FPS；report 为真时同时向 out 写统计行。
func (s *pipelineStats) run(ctx context.Context, report bool, out io.Writer) {
	if s == nil || ctx == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	lastTick := time.Now()
	var lastRecv, lastPublish uint64
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			elapsed := now.Sub(lastTick).Seconds()
			lastTick = now
			recv := s.recvTotal.Load()
			publish := s.publishTotal.Load()
			if elapsed > 0 {
				s.recvFPSCurrent.Store(math.Float64bits(float64(recv-lastRecv) / elapsed))
				s.publishFPSCurrent.Store(math.Float64bits(float64(publish-lastPublish) / elapsed))
			}
			lastRecv, lastPublish = recv, publish
			if report && out != nil {
				s.write(out)
			}
		}
	}
}

// write 输出一行供父进程或前端采集的 JSON 统计。
func (s *pipelineStats) write(out io.Writer) {
	if s == nil || out == nil {
		return
	}
	raw, err := json.Marshal(s.snapshot())
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(out, "%s %s\n", statsMarker, raw)
}

// snapshot 生成可安全并发读取的一次性统计快照。
func (s *pipelineStats) snapshot() PipelineStats {
	if s == nil {
		return PipelineStats{}
	}

	var uptimeMs int64
	if !s.startedAt.IsZero() {
		uptimeMs = time.Since(s.startedAt).Milliseconds()
		if uptimeMs < 0 {
			uptimeMs = 0
		}
	}

	subscribers := 0
	if s.subscribers != nil {
		subscribers = s.subscribers()
	}
	dropForClient := uint64(0)
	if s.dropForClient != nil {
		dropForClient = s.dropForClient()
	}

	return PipelineStats{
		UptimeMs:         uptimeMs,
		Mode:             s.mode,
		StreamWidth:      s.streamWidth,
		StreamHeight:     s.streamHeight,
		RecvFPS:          math.Float64frombits(s.recvFPSCurrent.Load()),
		PublishFPS:       math.Float64frombits(s.publishFPSCurrent.Load()),
		RecvTotal:        s.recvTotal.Load(),
		PublishTotal:     s.publishTotal.Load(),
		Seq:              s.seq.Load(),
		SeqGapFrames:     s.seqGapFrames.Load(),
		CopyMsP50:        s.copyDurations.percentile(50),
		CopyMsP95:        s.copyDurations.percentile(95),
		EncodeMsP50:      s.encodeDurations.percentile(50),
		EncodeMsP95:      s.encodeDurations.percentile(95),
		Subscribers:      subscribers,
		DropBeforeEncode: s.dropBeforeEncode.Load(),
		DropForClient:    dropForClient,
		LastSeqAtUnixMs:  s.lastSeqAtUnixMs.Load(),
	}
}

// Stats 返回设备窗口当前的服务端画面链路统计，可随时并发调用。
func (d *DeviceWindow) Stats() PipelineStats {
	if d == nil {
		return PipelineStats{}
	}
	if d.stats == nil {
		return PipelineStats{
			Mode:         pipelineMode(d.cfg.Benchmark),
			StreamWidth:  d.streamW,
			StreamHeight: d.streamH,
		}
	}
	return d.stats.snapshot()
}
