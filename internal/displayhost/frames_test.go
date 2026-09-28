package displayhost

import (
	"context"
	"testing"

	"AVDDesktop/internal/emulatorgrpc"
)

func TestFramePumpSkipsEncodeWithoutSubscribers(t *testing.T) {
	region, err := newRegion("test-no-subscribers", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = region.Close() }()
	copy(region.Bytes(), []byte{1, 2, 3, 4})

	meta := make(chan emulatorgrpc.FrameMeta, 1)
	meta <- emulatorgrpc.FrameMeta{Width: 1, Height: 1, Seq: 1}
	close(meta)

	stats := newPipelineStats(statsModeEncode, 1, 1)
	sinkCalled := false
	pump := &framePump{
		source: &frameSource{
			ctx:    context.Background(),
			region: region,
			meta:   meta,
			width:  1,
			height: 1,
			stats:  stats,
		},
		sink:           func([]byte) { sinkCalled = true },
		hasSubscribers: func() bool { return false },
		stats:          stats,
	}
	pump.run(context.Background())

	got := stats.snapshot()
	if sinkCalled {
		t.Fatal("无订阅者时不应调用发布回调")
	}
	if got.RecvTotal != 1 || got.PublishTotal != 0 {
		t.Fatalf("无订阅者时 totals = recv %d publish %d", got.RecvTotal, got.PublishTotal)
	}
	if got.DropBeforeEncode != 1 {
		t.Fatalf("dropBeforeEncode = %d, want 1", got.DropBeforeEncode)
	}
	if got.EncodeMsP50 != 0 || got.EncodeMsP95 != 0 {
		t.Fatalf("无订阅者时不应编码：p50 %.3f p95 %.3f", got.EncodeMsP50, got.EncodeMsP95)
	}
}
