package mirror

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/sdk/repo"
)

// TestOptions 控制一次测速。
//
// 默认值参考 UI：每个源最多采 4 MiB，整体 25 秒超时。
type TestOptions struct {
	MaxThroughputBytes int64         // 吞吐采样上限（默认 4 MiB）
	Timeout            time.Duration // 单源总超时（默认 25s）
	ProbeBytes         int64         // 索引探测读取上限（默认 64 KiB，仅用于 TTFB）
	SkipThroughput     bool          // 只测延迟与索引可用性（快速模式）
	UserAgent          string
}

// WithDefaults 填充默认值。
func (o TestOptions) WithDefaults() TestOptions {
	if o.MaxThroughputBytes <= 0 {
		o.MaxThroughputBytes = 4 << 20
	}
	if o.Timeout <= 0 {
		o.Timeout = 25 * time.Second
	}
	if o.ProbeBytes <= 0 {
		o.ProbeBytes = 64 << 10
	}
	if o.UserAgent == "" {
		o.UserAgent = "AVDDesktop/0.1 SpeedTest"
	}
	return o
}

// Engine 执行镜像测速（无状态，可并发调用）。
type Engine struct {
	log logging.Interface

	mu      sync.RWMutex
	results map[string]domain.SpeedResult
	fetcher *repo.Fetcher
}

// NewEngine 创建测速引擎。log 为 nil 时使用空日志器。
func NewEngine(cacheDir string, log logging.Interface) *Engine {
	return &Engine{
		log:     logging.Or(log),
		results: map[string]domain.SpeedResult{},
		fetcher: repo.NewFetcher(cacheDir, 90*time.Second),
	}
}

// Cached 返回缓存的测速结果。
func (e *Engine) Cached() []domain.SpeedResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]domain.SpeedResult, 0, len(e.results))
	for _, r := range e.results {
		out = append(out, r)
	}
	return out
}

// SetCached 注入缓存（启动时从磁盘加载）。
func (e *Engine) SetCached(results []domain.SpeedResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range results {
		e.results[r.SourceID] = r
	}
}

// Test 测试单个镜像源。
func (e *Engine) Test(ctx context.Context, src domain.MirrorSource, opts TestOptions) domain.SpeedResult {
	opts = opts.WithDefaults()
	res := domain.SpeedResult{SourceID: src.ID, At: time.Now().UnixMilli()}
	started := time.Now()

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	base, err := NormalizeBaseURL(src.BaseURL)
	if err != nil {
		res.Error = err.Error()
		res.Grade = "unusable"
		e.store(res)
		return res
	}
	indexURL := base + repo.IndexRepository
	e.log.Debug("speedtest", "开始测速 %s（%s）", src.Name, indexURL)

	u, err := url.Parse(indexURL)
	if err != nil {
		res.Error = "地址非法: " + err.Error()
		res.Grade = "unusable"
		e.store(res)
		return res
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// ① DNS
	dnsStart := time.Now()
	ips, dnsErr := net.DefaultResolver.LookupHost(ctx, host)
	res.DNSMs = time.Since(dnsStart).Milliseconds()
	res.ResolveIPs = ips
	if dnsErr != nil {
		res.Error = "域名解析失败: " + dnsErr.Error()
		res.Grade = "unusable"
		e.store(res)
		e.log.Warn("speedtest", "%s 域名解析失败：%v", src.Name, dnsErr)
		return res
	}

	// ② 建连（含 TLS）
	connStart := time.Now()
	conn, connErr := dialWithTLS(ctx, u.Scheme, net.JoinHostPort(host, port), host)
	res.ConnectMs = time.Since(connStart).Milliseconds()
	if connErr != nil {
		res.Error = "连接失败: " + connErr.Error()
		res.Grade = "unreachable"
		e.store(res)
		e.log.Warn("speedtest", "%s 连接失败（%s）：%v", src.Name, host, connErr)
		return res
	}
	_ = conn.Close()

	// ③ 索引探测（TTFB + HTTP 状态 + Range 支持 + 完整性）
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			MaxIdleConnsPerHost:   8,
		},
		Timeout: opts.Timeout,
	}
	status, ttfb, rangeOK, bodyBytes, httpErr := probeIndex(ctx, client, indexURL, opts)
	res.HTTPStatus = status
	res.TTFBMs = ttfb
	res.RangeSupported = rangeOK
	if httpErr != nil {
		res.Error = httpErr.Error()
		res.Grade = gradeNoIndex(status)
		e.store(res)
		return res
	}

	// ④ 索引解析 + 关键包检查（用完整索引，缓存命中时几乎无成本）
	idx, err := e.fetcher.GetIndex(ctx, cacheKeyFor(src.ID), indexURL, false)
	if err != nil {
		res.Error = "索引解析失败: " + err.Error()
		res.Grade = "index-only"
		e.store(res)
		return res
	}
	res.XMLOK = true
	_, res.HasCmdlineTools = idx.Find("cmdline-tools;latest")
	_, res.HasEmulator = idx.Find("emulator")
	res.HasSystemImages = true // 由具体 tag 的索引决定，这里不阻塞判定

	// ⑤ 吞吐采样
	if !opts.SkipThroughput {
		throughput, jitter, tErr := e.measureThroughput(ctx, client, idx, opts)
		res.ThroughputMBps = throughput
		res.JitterMs = jitter
		if tErr != nil && res.Error == "" {
			res.Error = "吞吐采样失败: " + tErr.Error()
		}
	}
	res.OK = res.XMLOK && (res.HTTPStatus == http.StatusOK || res.HTTPStatus == http.StatusPartialContent)
	res.Score = score(res)
	res.Grade = scoreGrade(res)
	_ = bodyBytes

	e.log.Info("speedtest", "%s 测速完成：等级=%s 评分=%d TTFB=%dms 下载=%.2fMB/s 抖动=%dms Range=%v 耗时=%s",
		src.Name, res.Grade, res.Score, res.TTFBMs, res.ThroughputMBps, res.JitterMs,
		res.RangeSupported, time.Since(started).Round(time.Millisecond))
	if res.Error != "" {
		e.log.Warn("speedtest", "%s 存在问题：%s", src.Name, res.Error)
	}

	e.store(res)
	return res
}

// TestAll 并发测试多个源，每完成一个就通过 onResult 回调。
func (e *Engine) TestAll(ctx context.Context, sources []domain.MirrorSource, opts TestOptions, onResult func(domain.SpeedResult)) {
	sem := make(chan struct{}, 3) // 全局最多 3 个源并发，避免互相抢带宽
	var wg sync.WaitGroup
	for _, s := range sources {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			r := e.Test(ctx, s, opts)
			if onResult != nil {
				onResult(r)
			}
		}()
	}
	wg.Wait()
}

func (e *Engine) store(r domain.SpeedResult) {
	e.mu.Lock()
	e.results[r.SourceID] = r
	e.mu.Unlock()
}

func dialWithTLS(ctx context.Context, scheme, addr, serverName string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if scheme != "https" {
		return d.DialContext(ctx, "tcp", addr)
	}
	return (&tls.Dialer{
		NetDialer: d,
		Config:    &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12},
	}).DialContext(ctx, "tcp", addr)
}

// probeIndex 发起 Range 请求测 TTFB，同时判定是否支持 Range。
func probeIndex(ctx context.Context, client *http.Client, indexURL string, opts TestOptions) (status int, ttfbMs int64, rangeOK bool, bytesRead int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return 0, 0, false, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", opts.ProbeBytes-1))
	req.Header.Set("User-Agent", opts.UserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, false, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	status = resp.StatusCode
	// 实测：部分镜像返回 206 但不带 Accept-Ranges 头，因此以 206 作为支持 Range 的依据。
	rangeOK = status == http.StatusPartialContent

	buf := make([]byte, 4096)
	n, readErr := resp.Body.Read(buf)
	ttfbMs = time.Since(start).Milliseconds()
	if n > 0 {
		bytesRead = int64(n)
	}
	if readErr != nil && readErr != io.EOF {
		return status, ttfbMs, rangeOK, bytesRead, readErr
	}
	if status != http.StatusOK && status != http.StatusPartialContent {
		return status, ttfbMs, rangeOK, bytesRead,
			fmt.Errorf("HTTP %d", status)
	}
	return status, ttfbMs, rangeOK, bytesRead, nil
}

// measureThroughput 采样下载速度（优先 Range，降级为"读满即断开"）。
func (e *Engine) measureThroughput(ctx context.Context, client *http.Client, idx *repo.Index, opts TestOptions) (mbps float64, jitterMs int64, err error) {
	// 选择一个小包作为样本（platform-tools ~8 MB 最合适）
	samplePath := "platform-tools"
	if _, ok := idx.Find(samplePath); !ok {
		for _, p := range idx.Packages {
			a, aErr := p.PickArchive(runtime.GOOS, runtime.GOARCH)
			if aErr != nil || a.Size == 0 {
				continue
			}
			if a.Size > 2<<20 && a.Size < 64<<20 {
				samplePath = p.Path
				break
			}
		}
	}
	pkg, ok := idx.Find(samplePath)
	if !ok {
		return 0, 0, fmt.Errorf("索引中没有可用于测速的样本包")
	}
	archive, aErr := pkg.PickArchive(runtime.GOOS, runtime.GOARCH)
	if aErr != nil {
		return 0, 0, aErr
	}
	sampleURL := idx.PackageURL(archive)

	var samples []float64
	ttfbs := make([]int64, 0, 3)
	var lastErr error
	for i := 0; i < 3; i++ {
		speed, ttfb, sErr := downloadSample(ctx, client, sampleURL, opts, archive.Size)
		if sErr != nil {
			lastErr = sErr
			continue
		}
		samples = append(samples, speed)
		ttfbs = append(ttfbs, ttfb)
		if len(samples) >= 2 && i >= 1 {
			break // 两次有效采样即可，节省时间
		}
	}
	if len(samples) == 0 {
		return 0, 0, lastErr
	}
	mbps = median(samples)
	if len(ttfbs) > 1 {
		jitterMs = maxInt64(ttfbs) - minInt64(ttfbs)
	}
	return mbps, jitterMs, nil
}

// downloadSample 下载 min(样本大小, MaxThroughputBytes) 字节并计算 MB/s。
func downloadSample(ctx context.Context, client *http.Client, sampleURL string, opts TestOptions, fileSize int64) (float64, int64, error) {
	limit := opts.MaxThroughputBytes
	if fileSize > 0 && fileSize < limit {
		limit = fileSize
	}
	// 单次采样最多 8 秒
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sampleURL, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
	req.Header.Set("User-Agent", opts.UserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, 0, fmt.Errorf("样本下载返回 HTTP %d", resp.StatusCode)
	}

	var (
		total    int64
		ttfb     int64
		firstGot bool
		buf      = make([]byte, 64<<10)
	)
	for total < limit {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			if !firstGot {
				ttfb = time.Since(start).Milliseconds()
				firstGot = true
			}
			total += int64(n)
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			// 超时也算部分成功：只要有数据就可以估算速度
			if total == 0 {
				return 0, ttfb, rErr
			}
			break
		}
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 || total == 0 {
		return 0, ttfb, fmt.Errorf("样本下载无数据")
	}
	return float64(total) / (1 << 20) / elapsed, ttfb, nil
}

// score 计算 0-100 综合评分（延迟 25% + 吞吐 50% + 稳定性 25%）。
func score(r domain.SpeedResult) int {
	if !r.XMLOK && r.HTTPStatus == 0 {
		return 0
	}
	latencyScore := clamp01(1 - float64(r.TTFBMs)/1500.0)
	throughputScore := clamp01(r.ThroughputMBps / 12.0)
	stabilityScore := clamp01(1 - float64(r.JitterMs)/500.0)
	total := 0.25*latencyScore + 0.5*throughputScore + 0.25*stabilityScore
	return int(total*100 + 0.5)
}

func scoreGrade(r domain.SpeedResult) string {
	if !r.XMLOK {
		return "unusable"
	}
	switch {
	case r.Score >= 70 && r.ThroughputMBps >= 2:
		return "recommended"
	case r.Score >= 35:
		return "usable"
	default:
		return "usable"
	}
}

func gradeNoIndex(status int) string {
	if status == 0 {
		return "unreachable"
	}
	return "unusable"
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j] < cp[j-1]; j-- {
			cp[j], cp[j-1] = cp[j-1], cp[j]
		}
	}
	return cp[len(cp)/2]
}

func maxInt64(v []int64) int64 {
	m := v[0]
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
}

func minInt64(v []int64) int64 {
	m := v[0]
	for _, x := range v {
		if x < m {
			m = x
		}
	}
	return m
}

func cacheKeyFor(sourceID string) string {
	return "index-" + sanitize(sourceID)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
