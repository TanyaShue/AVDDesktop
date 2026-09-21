package detect

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
)

// TestDetectCacheAndSingleFlight 验证缓存与并发单飞：
// 启动推送与前端请求常常同时触发自检，不应重复执行完整扫描。
func TestDetectCacheAndSingleFlight(t *testing.T) {
	d := NewDetector(nil)
	var scans atomic.Int64
	d.scanHook = func() { scans.Add(1) }

	ctx := context.Background()
	opts := Options{InjectEnv: false}

	// 并发 8 次调用
	var wg sync.WaitGroup
	results := make([]*domain.EnvReport, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rep, err := d.Detect(ctx, opts)
			if err != nil {
				t.Errorf("第 %d 次自检失败: %v", idx, err)
				return
			}
			results[idx] = rep
		}(i)
	}
	wg.Wait()

	if got := scans.Load(); got != 1 {
		t.Errorf("并发调用应只执行一次扫描（单飞），实际 %d 次", got)
	}
	first := results[0]
	for i, rep := range results {
		if rep == nil {
			t.Fatalf("第 %d 个结果为空", i)
		}
		if rep.ScannedAt != first.ScannedAt {
			t.Errorf("第 %d 个结果来自不同扫描（ScannedAt 不一致）", i)
		}
	}

	// TTL 内再调用仍命中缓存
	if _, err := d.Detect(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if got := scans.Load(); got != 1 {
		t.Errorf("TTL 内应命中缓存，实际扫描 %d 次", got)
	}

	// 失效后重新扫描
	d.Invalidate()
	if _, err := d.Detect(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if got := scans.Load(); got != 2 {
		t.Errorf("失效后应重新扫描，实际 %d 次", got)
	}
}

// TestDetectCancelWhileWaiting 验证等待单飞时能响应取消。
func TestDetectCancelWhileWaiting(t *testing.T) {
	d := NewDetector(nil)
	// 手工构造“已有扫描在进行”的状态
	d.mu.Lock()
	d.inflight = make(chan struct{})
	inflight := d.inflight
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.inflight = nil
		d.mu.Unlock()
		close(inflight)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := d.Detect(ctx, Options{}); err == nil {
		t.Error("等待期间上下文超时应返回错误")
	}
}

// TestDetectScanMs 验证扫描耗时被记录（UI 用于展示自检性能）。
func TestDetectScanMs(t *testing.T) {
	d := NewDetector(nil)
	report, err := d.Detect(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.ScanMs <= 0 {
		t.Errorf("ScanMs 应大于 0，实际 %d", report.ScanMs)
	}
}
