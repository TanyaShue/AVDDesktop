// Package download 是统一的下载器。
//
// 设计依据（见 docs/RESEARCH-NOTES.md §1.1）：镜像对 HTTP Range 的支持差异很大
// （Google 支持 206；某些镜像会挂起或直接忽略 Range），因此必须支持三种模式：
//
//	range-parallel   支持 Range 且启用多连接 → 分片并发写同一个文件
//	range-sequential 支持 Range → 单连接 + 断点续传
//	plain            不支持 Range → 从头顺序下载（不支持续传）
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
)

// Progress 是下载进度快照。
type Progress struct {
	Done     int64
	Total    int64
	SpeedBps int64
}

// Options 控制一次下载。
type Options struct {
	Dest        string        // 目标文件路径
	Concurrency int           // 分片并发数（<=1 表示单连接）
	Timeout     time.Duration // 单请求超时（0 = 不限制，由 ctx 控制）
	ProxyURL    string
	OnProgress  func(Progress)
	Resume      bool
	FilePerm    os.FileMode
	SpeedLimit  int // KB/s，0 = 不限速
	UserAgent   string
	// Logger 可选：下载模式选择、断点续传与重试会写日志。
	Logger logging.Interface
}

// Result 是下载结果。
type Result struct {
	Path           string
	Bytes          int64
	RangeSupported bool
	Resumed        bool
	Elapsed        time.Duration
	FinalURL       string
}

const (
	chunkSize    = 4 << 20 // 4 MiB
	minParallel  = 8 << 20 // 小于该大小不做分片
	probeTimeout = 15 * time.Second
	maxRetries   = 3
)

// Fetch 下载 url 到 opts.Dest（自动选择模式、断点续传、进度回调）。
func Fetch(ctx context.Context, rawURL string, opts Options) (Result, error) {
	log := logging.Or(opts.Logger)
	res := Result{Path: opts.Dest}
	start := time.Now()
	defer func() { res.Elapsed = time.Since(start) }()

	if rawURL == "" || opts.Dest == "" {
		return res, domain.Err(domain.CodeInvalidArgument, "下载地址或目标路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(opts.Dest), 0o755); err != nil {
		return res, domain.Wrap(domain.CodePermissionDenied, "无法创建下载目录", err)
	}

	client, err := newClient(opts)
	if err != nil {
		return res, err
	}

	probe, err := probeURL(ctx, client, rawURL, opts.UserAgent)
	if err != nil {
		return res, err
	}
	res.RangeSupported = probe.rangeSupported
	res.FinalURL = probe.finalURL
	log.Debug("download", "探测 %s：size=%d range支持=%v 最终地址=%s",
		filepath.Base(opts.Dest), probe.size, probe.rangeSupported, probe.finalURL)

	// 处理续传
	var existing int64
	if opts.Resume {
		if st, statErr := os.Stat(opts.Dest); statErr == nil {
			existing = st.Size()
		}
	}
	if existing > 0 && (!probe.rangeSupported || (probe.size > 0 && existing >= probe.size)) {
		if probe.size > 0 && existing >= probe.size {
			// 已完成，直接返回
			res.Bytes = existing
			log.Debug("download", "%s 已存在且大小相符，跳过下载", filepath.Base(opts.Dest))
			return res, nil
		}
		log.Warn("download", "%s 存在 %d 字节残留但不支持续传，从头重新下载", filepath.Base(opts.Dest), existing)
		_ = os.Remove(opts.Dest) // 无法续传则重下
		existing = 0
	}
	res.Resumed = existing > 0
	if res.Resumed {
		log.Info("download", "%s 断点续传：从 %d / %d 字节继续",
			filepath.Base(opts.Dest), existing, probe.size)
	}

	perm := opts.FilePerm
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(opts.Dest, os.O_CREATE|os.O_WRONLY, perm)
	if err != nil {
		return res, domain.Wrap(domain.CodePermissionDenied, "无法创建目标文件", err)
	}
	defer func() { _ = f.Close() }()

	if probe.size > 0 {
		if err := f.Truncate(probe.size); err != nil {
			return res, domain.Wrap(domain.CodeDownloadFailed, "无法预分配文件", err)
		}
	}

	var counter atomicProgress
	counter.setTotal(probe.size)
	counter.setDone(existing)
	stopReporter := counter.report(ctx, opts.OnProgress)
	defer stopReporter()

	limiter := newLimiter(opts.SpeedLimit)

	if probe.rangeSupported && opts.Concurrency > 1 && probe.size >= minParallel {
		log.Debug("download", "%s 使用分片并发（并发=%d 大小=%d）",
			filepath.Base(opts.Dest), opts.Concurrency, probe.size)
		if err := parallelFetch(ctx, client, rawURL, f, probe.size, existing, opts, &counter, limiter); err != nil {
			return res, err
		}
	} else {
		log.Debug("download", "%s 使用单连接（range支持=%v 大小=%d）",
			filepath.Base(opts.Dest), probe.rangeSupported, probe.size)
		if err := sequentialFetch(ctx, client, rawURL, f, existing, opts.UserAgent, &counter, limiter, log); err != nil {
			return res, err
		}
	}

	if err := f.Sync(); err != nil {
		return res, domain.Wrap(domain.CodeDownloadFailed, "写入磁盘失败", err)
	}
	st, err := f.Stat()
	if err == nil {
		res.Bytes = st.Size()
	}
	if probe.size > 0 && res.Bytes != probe.size {
		return res, domain.ErrDetail(domain.CodeDownloadFailed, "下载未完成（大小不符）",
			fmt.Sprintf("期望 %d 字节，实际 %d 字节", probe.size, res.Bytes))
	}
	return res, nil
}

type probeResult struct {
	size           int64
	rangeSupported bool
	finalURL       string
}

// probeURL 探测文件大小与 Range 支持情况。
//
// 使用 Range: bytes=0-0，期望得到 206 + Content-Range；若无 Range 支持则退化为普通请求
// 并只读第一个字节（随后立即关闭连接）。
func probeURL(ctx context.Context, client *http.Client, rawURL, ua string) (probeResult, error) {
	res := probeResult{finalURL: rawURL}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return res, domain.Wrap(domain.CodeInvalidArgument, "下载地址非法", err)
	}
	req.Header.Set("Range", "bytes=0-0")
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := client.Do(req)
	if err != nil {
		return res, networkError(rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	res.finalURL = resp.Request.URL.String()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		res.rangeSupported = true
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndex(cr, "/"); i >= 0 {
				if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil {
					res.size = n
				}
			}
		}
		_, _ = io.CopyN(io.Discard, resp.Body, 1)
	case http.StatusOK:
		res.rangeSupported = false
		res.size = resp.ContentLength // 可能为 -1
		if res.size < 0 {
			res.size = 0
		}
		_, _ = io.CopyN(io.Discard, resp.Body, 1)
	case http.StatusForbidden, http.StatusUnauthorized:
		return res, domain.ErrDetail(domain.CodeDownloadFailed,
			fmt.Sprintf("下载源拒绝访问（HTTP %d）", resp.StatusCode), rawURL).
			WithHint("该镜像可能不提供文件下载，请切换到其它镜像源")
	case http.StatusNotFound:
		return res, domain.ErrDetail(domain.CodeDownloadFailed,
			"下载源上不存在该文件（HTTP 404）", rawURL).
			WithHint("该镜像可能只镜像了索引文件（index-only），请切换到其它镜像源或回退官方源")
	default:
		return res, domain.ErrDetail(domain.CodeDownloadFailed,
			fmt.Sprintf("下载源返回异常状态码 HTTP %d", resp.StatusCode), rawURL)
	}
	return res, nil
}

func sequentialFetch(ctx context.Context, client *http.Client, rawURL string, f *os.File, existing int64, ua string, counter *atomicProgress, limiter *limiter, log logging.Interface) error {
	var attempt int
	for {
		attempt++
		err := sequentialOnce(ctx, client, rawURL, f, existing, ua, counter, limiter)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		if attempt >= maxRetries {
			return err
		}
		log.Warn("download", "下载中断（第 %d 次重试）：%v", attempt, err)
		select {
		case <-ctx.Done():
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		case <-time.After(time.Duration(attempt) * time.Second):
		}
		// 续传：从当前已写字节继续
		if st, statErr := f.Stat(); statErr == nil {
			existing = st.Size()
		}
		counter.setDone(existing)
	}
}

func sequentialOnce(ctx context.Context, client *http.Client, rawURL string, f *os.File, existing int64, ua string, counter *atomicProgress, limiter *limiter) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return domain.Wrap(domain.CodeInvalidArgument, "下载地址非法", err)
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if existing > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existing))
	}
	resp, err := client.Do(req)
	if err != nil {
		return networkError(rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		if existing > 0 {
			// 服务端忽略 Range：从头覆盖写
			if err := f.Truncate(0); err != nil {
				return domain.Wrap(domain.CodeDownloadFailed, "无法重置文件", err)
			}
			existing = 0
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return domain.Wrap(domain.CodeDownloadFailed, "无法定位写入位置", err)
		}
	case http.StatusPartialContent:
		if _, err := f.Seek(existing, io.SeekStart); err != nil {
			return domain.Wrap(domain.CodeDownloadFailed, "无法定位写入位置", err)
		}
	default:
		return domain.ErrDetail(domain.CodeDownloadFailed,
			fmt.Sprintf("下载失败（HTTP %d）", resp.StatusCode), rawURL)
	}

	if counter.total.Load() == 0 && resp.ContentLength > 0 {
		counter.setTotal(existing + resp.ContentLength)
	}
	return copyWithProgress(ctx, f, resp.Body, counter, limiter)
}

func parallelFetch(ctx context.Context, client *http.Client, rawURL string, f *os.File, size, existing int64, opts Options, counter *atomicProgress, limiter *limiter) error {
	concurrency := opts.Concurrency
	if concurrency < 2 {
		concurrency = 2
	}
	if concurrency > 16 {
		concurrency = 16
	}

	start := existing // 已下载的部分不再重复下载
	type chunk struct{ from, to int64 }
	var chunks []chunk
	for off := start; off < size; off += chunkSize {
		end := off + chunkSize - 1
		if end >= size {
			end = size - 1
		}
		chunks = append(chunks, chunk{from: off, to: end})
	}
	if len(chunks) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, len(chunks))
	var wg sync.WaitGroup

	for _, c := range chunks {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			err := fetchChunk(ctx, client, rawURL, f, c.from, c.to, opts.UserAgent, counter, limiter, logging.Or(opts.Logger))
			if err != nil {
				errCh <- err
				cancel()
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func fetchChunk(ctx context.Context, client *http.Client, rawURL string, f *os.File, from, to int64, ua string, counter *atomicProgress, limiter *limiter, log logging.Interface) error {
	var attempt int
	for {
		attempt++
		err := fetchChunkOnce(ctx, client, rawURL, f, from, to, ua, counter, limiter)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		if attempt >= maxRetries {
			return err
		}
		log.Warn("download", "分片 %d-%d 下载失败（第 %d 次重试）：%v", from, to, attempt, err)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
}

func fetchChunkOnce(ctx context.Context, client *http.Client, rawURL string, f *os.File, from, to int64, ua string, counter *atomicProgress, limiter *limiter) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return domain.Wrap(domain.CodeInvalidArgument, "下载地址非法", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, to))
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := client.Do(req)
	if err != nil {
		return networkError(rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return domain.ErrDetail(domain.CodeDownloadFailed,
			fmt.Sprintf("分片下载失败（HTTP %d）", resp.StatusCode), rawURL)
	}

	w := &offsetWriter{f: f, off: from, counter: counter, limiter: limiter}
	_, err = io.CopyBuffer(w, resp.Body, make([]byte, 256<<10))
	if err != nil {
		return domain.Wrap(domain.CodeDownloadFailed, "分片写入失败", err)
	}
	if w.off-1 < to {
		return domain.ErrDetail(domain.CodeDownloadFailed, "分片下载不完整",
			fmt.Sprintf("期望写到 %d，实际到 %d", to, w.off-1))
	}
	return nil
}

// offsetWriter 把流写入文件的指定偏移，并累计全局进度。
type offsetWriter struct {
	f       *os.File
	off     int64
	counter *atomicProgress
	limiter *limiter
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	if w.counter != nil {
		w.counter.add(int64(n))
	}
	if w.limiter != nil {
		w.limiter.wait(n)
	}
	return n, err
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, counter *atomicProgress, limiter *limiter) error {
	buf := make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return domain.Wrap(domain.CodeDownloadFailed, "写入磁盘失败", werr)
			}
			counter.add(int64(n))
			if limiter != nil {
				limiter.wait(n)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return domain.Wrap(domain.CodeDownloadFailed, "网络连接中断", err)
		}
	}
}

// ---------------------------------------------------------------- 进度与限速

type atomicProgress struct {
	done  atomic.Int64
	total atomic.Int64
}

func (a *atomicProgress) add(n int64)      { a.done.Add(n) }
func (a *atomicProgress) setDone(n int64)  { a.done.Store(n) }
func (a *atomicProgress) setTotal(n int64) { a.total.Store(n) }

// report 启动周期性进度上报，返回停止函数。
func (a *atomicProgress) report(ctx context.Context, fn func(Progress)) func() {
	if fn == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		var lastDone int64
		lastAt := time.Now()
		for {
			select {
			case <-done:
				fn(Progress{Done: a.done.Load(), Total: a.total.Load()})
				return
			case <-ctx.Done():
				return
			case <-t.C:
				cur := a.done.Load()
				now := time.Now()
				elapsed := now.Sub(lastAt).Seconds()
				var speed int64
				if elapsed > 0 {
					speed = int64(float64(cur-lastDone) / elapsed)
				}
				lastDone, lastAt = cur, now
				fn(Progress{Done: cur, Total: a.total.Load(), SpeedBps: speed})
			}
		}
	}()
	return func() { close(done) }
}

// limiter 是一个简单的令牌桶限速器（KB/s）。
type limiter struct {
	mu      sync.Mutex
	perSec  int
	window  time.Time
	written int
}

func newLimiter(kbps int) *limiter {
	if kbps <= 0 {
		return nil
	}
	return &limiter{perSec: kbps * 1024, window: time.Now()}
}

func (l *limiter) wait(n int) {
	l.mu.Lock()
	l.written += n
	elapsed := time.Since(l.window)
	if elapsed >= time.Second {
		l.window = time.Now()
		l.written = 0
		l.mu.Unlock()
		return
	}
	if l.written <= l.perSec {
		l.mu.Unlock()
		return
	}
	excess := l.written - l.perSec
	sleep := time.Duration(float64(excess)/float64(l.perSec)*1000) * time.Millisecond
	l.mu.Unlock()
	time.Sleep(sleep)
}

// ---------------------------------------------------------------- HTTP 客户端

func newClient(opts Options) (*http.Client, error) {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	if strings.TrimSpace(opts.ProxyURL) != "" {
		u, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, domain.Wrap(domain.CodeInvalidArgument, "代理地址非法", err)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: tr, Timeout: opts.Timeout}, nil
}

func networkError(rawURL string, err error) error {
	var netErr net.Error
	msg := "网络请求失败"
	if errors.As(err, &netErr) && netErr.Timeout() {
		msg = "网络请求超时"
	} else if errors.Is(err, context.Canceled) {
		return domain.Err(domain.CodeJobCanceled, "操作已取消")
	}
	return domain.ErrDetail(domain.CodeMirrorUnreachable, msg, fmt.Sprintf("%s\n%v", rawURL, err)).
		WithHint("请在测速弹窗中更换镜像源，或检查代理设置")
}
