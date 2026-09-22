package jdk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
)

// CheckOptions 控制 JDK 镜像连通性检测与吞吐采样。
type CheckOptions struct {
	NeedJDK     bool
	SampleBytes int64
	Timeout     time.Duration
}

func (o CheckOptions) withDefaults() CheckOptions {
	if o.SampleBytes <= 0 {
		o.SampleBytes = 1 << 20
	}
	if o.SampleBytes > 4<<20 {
		o.SampleBytes = 4 << 20
	}
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	return o
}

// Checker 执行 JDK 镜像检测；可并发复用。
type Checker struct {
	Client *http.Client
}

// NewChecker 创建 JDK 镜像检测器。
func NewChecker() *Checker {
	return &Checker{Client: &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          16,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 12 * time.Second,
		},
	}}
}

// Check 检测单个 JDK 镜像是否能提供当前平台的 Temurin 归档。
func (c *Checker) Check(ctx context.Context, source domain.MirrorSource, opts CheckOptions) domain.MirrorCheck {
	opts = opts.withDefaults()
	started := time.Now()
	check := domain.MirrorCheck{
		SourceID:   source.ID,
		SourceName: source.Name,
		BaseURL:    source.BaseURL,
		Region:     source.Region,
		CheckedAt:  time.Now().UnixMilli(),
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	spec, err := artifactForSource(source, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		check.Error = err.Error()
		check.ElapsedMs = time.Since(started).Milliseconds()
		return check
	}
	check.BaseURL = spec.URL

	resource := domain.MirrorResource{
		ID:       "jdk",
		Name:     "Eclipse Temurin " + Version(),
		Path:     spec.Name,
		URL:      spec.URL,
		Required: opts.NeedJDK,
	}
	status, ttfb, size, probeErr := c.probe(ctx, spec.URL)
	resource.StatusCode = status
	resource.LatencyMs = ttfb
	resource.SizeBytes = size
	check.Reachable = status > 0
	check.LatencyMs = ttfb
	if probeErr != nil {
		resource.Error = probeErr.Error()
		check.Error = probeErr.Error()
	} else {
		resource.Available = true
		check.Compatible = true
		check.Recommended = true
	}
	check.Resources = []domain.MirrorResource{resource}

	if resource.Available {
		check.ThroughputBps = c.sampleThroughput(ctx, spec.URL, opts.SampleBytes)
	}
	check.ElapsedMs = time.Since(started).Milliseconds()
	return check
}

// CheckAll 并发检测所有 JDK 镜像，并把采样速度最高、延迟较低的可用源标记为推荐。
func (c *Checker) CheckAll(ctx context.Context, sources []domain.MirrorSource, opts CheckOptions) []domain.MirrorCheck {
	opts = opts.withDefaults()
	results := make([]domain.MirrorCheck, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Add(1)
		go func(index int, src domain.MirrorSource) {
			defer wg.Done()
			results[index] = c.Check(ctx, src, opts)
		}(i, source)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Compatible != results[j].Compatible {
			return results[i].Compatible
		}
		if results[i].ThroughputBps != results[j].ThroughputBps {
			return results[i].ThroughputBps > results[j].ThroughputBps
		}
		li, lj := usableLatency(results[i]), usableLatency(results[j])
		if li != lj {
			return li < lj
		}
		return results[i].SourceName < results[j].SourceName
	})
	for i := range results {
		results[i].Recommended = false
	}
	for i := range results {
		if results[i].Compatible {
			results[i].Recommended = true
			break
		}
	}
	return domain.NonNil(results)
}

func (c *Checker) probe(ctx context.Context, target string) (status int, ttfbMs int64, size int64, err error) {
	status, ttfbMs, size, err = c.probeOnce(ctx, target, "bytes=0-0")
	if err == nil {
		return status, ttfbMs, size, nil
	}
	// 部分反向代理拒绝 Range，但普通 GET 可以正常下载。
	fallbackStatus, fallbackTTFB, fallbackSize, fallbackErr := c.probeOnce(ctx, target, "")
	if fallbackErr == nil {
		return fallbackStatus, fallbackTTFB, fallbackSize, nil
	}
	return status, ttfbMs, size, err
}

func (c *Checker) probeOnce(ctx context.Context, target, rangeHeader string) (status int, ttfbMs int64, size int64, err error) {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if reqErr != nil {
		return 0, 0, 0, reqErr
	}
	req.Header.Set("User-Agent", "AVDDesktop/0.2 jdk-mirror-check")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	started := time.Now()
	resp, doErr := c.httpClient().Do(req)
	if doErr != nil {
		return 0, 0, 0, doErr
	}
	defer func() { _ = resp.Body.Close() }()
	status = resp.StatusCode

	buf := make([]byte, 1)
	n, readErr := resp.Body.Read(buf)
	if n > 0 {
		ttfbMs = time.Since(started).Milliseconds()
		if ttfbMs <= 0 {
			ttfbMs = 1
		}
	}
	if readErr != nil && readErr != io.EOF {
		return status, ttfbMs, responseSize(resp), readErr
	}
	if status < 200 || status >= 300 {
		return status, ttfbMs, responseSize(resp), fmt.Errorf("HTTP %d", status)
	}
	return status, ttfbMs, responseSize(resp), nil
}

func (c *Checker) sampleThroughput(ctx context.Context, target string, limit int64) int64 {
	sampleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	started := time.Now()
	resp, err := c.openSample(sampleCtx, target, limit)
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0
	}

	var total int64
	buf := make([]byte, 64<<10)
	for total < limit {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			total += int64(n)
		}
		if readErr != nil {
			break
		}
	}
	if total <= 0 {
		return 0
	}
	elapsed := time.Since(started)
	if elapsed < time.Millisecond {
		elapsed = time.Millisecond
	}
	return int64(float64(total) / elapsed.Seconds())
}

func (c *Checker) openSample(ctx context.Context, target string, limit int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AVDDesktop/0.2 jdk-mirror-check")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", "bytes=0-"+strconv.FormatInt(limit-1, 10))

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	_ = resp.Body.Close()

	fallback, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	fallback.Header.Set("User-Agent", "AVDDesktop/0.2 jdk-mirror-check")
	fallback.Header.Set("Accept", "*/*")
	fallback.Header.Set("Accept-Encoding", "identity")
	return c.httpClient().Do(fallback)
}

func (c *Checker) httpClient() *http.Client {
	if c != nil && c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

func responseSize(resp *http.Response) int64 {
	if resp == nil {
		return 0
	}
	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		if i := strings.LastIndexByte(contentRange, '/'); i >= 0 {
			if n, err := strconv.ParseInt(contentRange[i+1:], 10, 64); err == nil && n > 0 {
				return n
			}
		}
	}
	if resp.ContentLength > 0 {
		return resp.ContentLength
	}
	return 0
}

func usableLatency(check domain.MirrorCheck) int64 {
	if check.LatencyMs > 0 {
		return check.LatencyMs
	}
	return 1<<62 - 1
}
