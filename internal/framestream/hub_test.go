package framestream

import (
	"sync/atomic"
	"testing"
	"time"
)

// waitFor 轮询等待异步条件成立，避免测试依赖固定 sleep。
func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestHubStatsAndReleaseSemantics(t *testing.T) {
	var released atomic.Int64
	h := NewHub(Options{})

	for i := 0; i < 3; i++ {
		h.Publish(Frame{
			Pix:       []byte{byte(i), 0, 0, 0},
			Width:     1,
			Height:    1,
			Format:    FormatRGBA8888,
			Seq:       uint32(i),
			Timestamp: time.Now(),
			Release: func() {
				released.Add(1)
			},
		})
	}

	if got := released.Load(); got != 2 {
		t.Fatalf("released after latest overwrite=%d, want 2", got)
	}
	stats := h.Stats()
	if stats.Subscribers != 0 {
		t.Fatalf("subscribers=%d, want 0", stats.Subscribers)
	}
	if stats.PublishTotal != 3 {
		t.Fatalf("publish total=%d, want 3", stats.PublishTotal)
	}
	if stats.BytesPerSec != 12 {
		t.Fatalf("bytes/sec=%v, want 12", stats.BytesPerSec)
	}
	if stats.LastPublishUnixMs <= 0 {
		t.Fatalf("last publish=%d, want > 0", stats.LastPublishUnixMs)
	}

	h.Close()
	if got := released.Load(); got != 3 {
		t.Fatalf("released after Close=%d, want 3", got)
	}
	if got := h.Stats().Subscribers; got != 0 {
		t.Fatalf("subscribers after Close=%d, want 0", got)
	}

	h.Publish(Frame{
		Pix:    []byte{9, 9, 9, 9},
		Width:  1,
		Height: 1,
		Format: FormatRGBA8888,
		Release: func() {
			released.Add(1)
		},
	})
	if got := released.Load(); got != 4 {
		t.Fatalf("released after closed Publish=%d, want 4", got)
	}
	if got := h.Stats().PublishTotal; got != 3 {
		t.Fatalf("publish total after closed Publish=%d, want 3", got)
	}
}
