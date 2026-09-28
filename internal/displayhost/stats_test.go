package displayhost

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"AVDDesktop/internal/emulatorgrpc"
)

func TestPipelineStatsSnapshot(t *testing.T) {
	stats := newPipelineStats(statsModeRecvOnly, 540, 960)
	stats.recordFrame(10, 2*time.Millisecond)
	stats.recordFrame(13, 4*time.Millisecond)
	stats.recordFrame(13, 6*time.Millisecond)
	stats.recordEncode(8 * time.Millisecond)
	stats.recordEncode(12 * time.Millisecond)
	stats.recordPublish()

	got := stats.snapshot()
	if got.Mode != statsModeRecvOnly || got.StreamWidth != 540 || got.StreamHeight != 960 {
		t.Fatalf("pipeline metadata = %+v", got)
	}
	if got.RecvTotal != 3 || got.PublishTotal != 1 {
		t.Fatalf("frame totals = recv %d publish %d", got.RecvTotal, got.PublishTotal)
	}
	if got.Seq != 13 || got.SeqGapFrames != 2 {
		t.Fatalf("sequence stats = seq %d gap %d", got.Seq, got.SeqGapFrames)
	}
	if got.CopyMsP50 != 4 || got.CopyMsP95 != 6 {
		t.Fatalf("copy percentiles = p50 %.3f p95 %.3f", got.CopyMsP50, got.CopyMsP95)
	}
	if got.EncodeMsP50 != 8 || got.EncodeMsP95 != 12 {
		t.Fatalf("encode percentiles = p50 %.3f p95 %.3f", got.EncodeMsP50, got.EncodeMsP95)
	}
	if got.LastSeqAtUnixMs <= 0 {
		t.Fatalf("last seq timestamp = %d", got.LastSeqAtUnixMs)
	}
}

func TestDurationRingKeepsRecentSamples(t *testing.T) {
	var ring durationRing
	for i := 1; i <= statsDurationSamples+44; i++ {
		ring.record(time.Duration(i) * time.Millisecond)
	}
	if got := ring.percentile(50); got != 172 {
		t.Fatalf("ring p50 = %.3f, want 172", got)
	}
	if got := ring.percentile(95); got != 288 {
		t.Fatalf("ring p95 = %.3f, want 288", got)
	}
}

func TestPipelineStatsJSONFieldNames(t *testing.T) {
	raw, err := json.Marshal(PipelineStats{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"uptimeMs", "mode", "streamWidth", "streamHeight",
		"recvFps", "publishFps", "recvTotal", "publishTotal",
		"seq", "seqGapFrames", "copyMsP50", "copyMsP95",
		"encodeMsP50", "encodeMsP95", "subscribers",
		"dropBeforeEncode", "dropForClient", "lastSeqAtUnixMs",
	} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("JSON 缺少字段 %q", name)
		}
	}
}

func TestHelperArgsRoundTripStatsFlags(t *testing.T) {
	want := HelperConfig{
		InstanceID:  "emu-5554-1",
		GRPCAddress: "127.0.0.1:8554",
		Stats:       true,
		Benchmark:   statsModeRecvOnly,
	}
	got, err := ParseHelperConfig(HelperArgs(want))
	if err != nil {
		t.Fatalf("ParseHelperConfig(HelperArgs()) failed: %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}

	_, err = ParseHelperConfig([]string{
		"--display-host", "--instance-id", "i1", "--grpc", "127.0.0.1:8554", "--benchmark", "unknown",
	})
	if err == nil {
		t.Fatal("unsupported benchmark mode must fail")
	}
}

func TestFramePumpRecvOnlySkipsEncodeAndPublish(t *testing.T) {
	region, err := newRegion("test-recv-only", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = region.Close() }()
	copy(region.Bytes(), []byte{1, 2, 3, 4})

	meta := make(chan emulatorgrpc.FrameMeta, 1)
	meta <- emulatorgrpc.FrameMeta{Width: 1, Height: 1, Seq: 7}
	close(meta)

	stats := newPipelineStats(statsModeRecvOnly, 1, 1)
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
		sink: func([]byte) {
			sinkCalled = true
		},
		recvOnly: true,
		stats:    stats,
	}
	pump.run(context.Background())

	got := stats.snapshot()
	if sinkCalled {
		t.Fatal("recv-only mode must not publish")
	}
	if got.RecvTotal != 1 || got.PublishTotal != 0 {
		t.Fatalf("recv-only totals = recv %d publish %d", got.RecvTotal, got.PublishTotal)
	}
	if got.EncodeMsP50 != 0 || got.EncodeMsP95 != 0 {
		t.Fatalf("recv-only recorded encode time: p50 %.3f p95 %.3f", got.EncodeMsP50, got.EncodeMsP95)
	}
}
