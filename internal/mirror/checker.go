package mirror

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
	"AVDDesktop/internal/sdk"
)

// CheckOptions 控制镜像站检测的必选资源与吞吐采样大小。
type CheckOptions struct {
	NeedCmdlineTools  bool
	NeedPlatformTools bool
	NeedEmulator      bool
	CheckSystemImages bool
	SampleBytes       int64
	Timeout           time.Duration
}

func (o CheckOptions) withDefaults() CheckOptions {
	if o.SampleBytes <= 0 {
		o.SampleBytes = 512 << 10
	}
	if o.SampleBytes > 4<<20 {
		o.SampleBytes = 4 << 20
	}
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	return o
}

// Checker 执行镜像站检测；可并发复用。
type Checker struct {
	Client *http.Client
}

// NewChecker 创建检测器。
func NewChecker() *Checker {
	return &Checker{Client: &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 12 * time.Second,
		},
	}}
}

// Check 检测单个镜像站。
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

	base, err := NormalizeBaseURL(source.BaseURL)
	if err != nil {
		check.Error = err.Error()
		check.Resources = fallbackResources(opts, "镜像地址非法")
		check.ElapsedMs = time.Since(started).Milliseconds()
		return check
	}
	check.BaseURL = base

	indexResult := c.fetchIndex(ctx, base, opts)
	check.Reachable = indexResult.reachable
	check.LatencyMs = indexResult.ttfbMs
	if indexResult.err != nil && check.Error == "" {
		check.Error = indexResult.err.Error()
	}

	check.Resources = append(check.Resources, indexResult.resource)
	if indexResult.index == nil {
		check.Resources = append(check.Resources, fallbackResources(opts, "仓库索引不可用，无法继续校验")...)
		check.Compatible = false
		check.ElapsedMs = time.Since(started).Milliseconds()
		return check
	}

	check.Resources = append(check.Resources,
		c.checkPackage(ctx, indexResult.index, "cmdline-tools;latest", "Android 命令行工具", opts.NeedCmdlineTools),
		c.checkPackage(ctx, indexResult.index, "platform-tools", "platform-tools / adb", opts.NeedPlatformTools),
		c.checkPackage(ctx, indexResult.index, "emulator", "Android Emulator", opts.NeedEmulator),
	)
	if opts.CheckSystemImages {
		check.Resources = append(check.Resources, c.checkSystemImageIndex(ctx, base))
	}

	check.Compatible = true
	for _, resource := range check.Resources {
		if resource.Required && !resource.Available {
			check.Compatible = false
			if check.Error == "" {
				check.Error = resource.Name + "不可用：" + resource.Error
			}
		}
	}

	check.ThroughputBps = c.sampleThroughput(ctx, check.Resources, opts.SampleBytes)
	check.Recommended = check.Compatible
	check.ElapsedMs = time.Since(started).Milliseconds()
	return check
}

// CheckAll 并发检测所有镜像，并把资源完整、延迟最低的源标记为推荐。
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
		li, lj := usableLatency(results[i]), usableLatency(results[j])
		if li != lj {
			return li < lj
		}
		if results[i].ThroughputBps != results[j].ThroughputBps {
			return results[i].ThroughputBps > results[j].ThroughputBps
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

type indexFetchResult struct {
	index     *sdk.RepositoryIndex
	resource  domain.MirrorResource
	reachable bool
	ttfbMs    int64
	err       error
}

func (c *Checker) fetchIndex(ctx context.Context, base string, opts CheckOptions) indexFetchResult {
	target := base + sdk.RepositoryIndexPath
	status, ttfb, data, err := c.get(ctx, target, 16<<20, "")
	result := indexFetchResult{
		reachable: status > 0,
		ttfbMs:    ttfb,
		err:       err,
		resource: domain.MirrorResource{
			ID:         "repository",
			Name:       "SDK 仓库索引",
			Path:       sdk.RepositoryIndexPath,
			URL:        target,
			Required:   true,
			StatusCode: status,
			LatencyMs:  ttfb,
		},
	}
	if err != nil {
		result.resource.Error = err.Error()
		return result
	}
	idx, parseErr := sdk.ParseRepository(data, base)
	if parseErr != nil {
		result.err = parseErr
		result.resource.Error = parseErr.Error()
		return result
	}
	result.index = idx
	result.resource.Available = true
	result.resource.SizeBytes = int64(len(data))
	result.resource.Error = ""
	return result
}

func (c *Checker) checkPackage(ctx context.Context, idx *sdk.RepositoryIndex, pkgPath, name string, required bool) domain.MirrorResource {
	resource := domain.MirrorResource{ID: pkgPath, Name: name, Path: pkgPath, Required: required}
	archive, err := idx.ArchiveFor(pkgPath, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		resource.Error = err.Error()
		return resource
	}
	resource.URL = archive.URL
	resource.SizeBytes = archive.Size
	status, ttfb, _, probeErr := c.get(ctx, archive.URL, 1, "bytes=0-0")
	if probeErr != nil {
		// 部分反向代理不支持 Range，但资源本身可正常下载；此时退回普通 GET。
		status, ttfb, _, probeErr = c.get(ctx, archive.URL, 1, "")
	}
	resource.StatusCode = status
	resource.LatencyMs = ttfb
	if probeErr != nil {
		resource.Error = probeErr.Error()
		return resource
	}
	resource.Available = true
	return resource
}

func (c *Checker) checkSystemImageIndex(ctx context.Context, base string) domain.MirrorResource {
	rel := "sys-img/google_apis/sys-img2-3.xml"
	target := sdk.ResolveRepositoryURL(base, rel)
	resource := domain.MirrorResource{
		ID:       "system-images:google_apis",
		Name:     "Google APIs 系统镜像索引",
		Path:     rel,
		URL:      target,
		Required: false,
	}
	status, ttfb, data, err := c.get(ctx, target, 2<<20, "")
	resource.StatusCode = status
	resource.LatencyMs = ttfb
	if err != nil {
		resource.Error = err.Error()
		return resource
	}
	text := string(data)
	if !strings.Contains(text, "<remotePackage") {
		resource.Error = "返回内容不是有效的系统镜像索引"
		return resource
	}
	resource.Available = true
	resource.SizeBytes = int64(len(data))
	return resource
}

func (c *Checker) get(ctx context.Context, target string, limit int64, rangeHeader string) (status int, ttfbMs int64, data []byte, err error) {
	client := c.httpClient()
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if reqErr != nil {
		return 0, 0, nil, reqErr
	}
	req.Header.Set("User-Agent", "AVDDesktop/0.2 mirror-check")
	req.Header.Set("Accept", "*/*")
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	started := time.Now()
	resp, doErr := client.Do(req)
	if doErr != nil {
		return 0, 0, nil, doErr
	}
	defer func() { _ = resp.Body.Close() }()
	status = resp.StatusCode

	var body []byte
	buf := make([]byte, 64<<10)
	for int64(len(body)) < limit {
		readSize := len(buf)
		if remaining := limit - int64(len(body)); remaining < int64(readSize) {
			readSize = int(remaining)
		}
		n, readErr := resp.Body.Read(buf[:readSize])
		if n > 0 {
			if ttfbMs == 0 {
				ttfbMs = time.Since(started).Milliseconds()
				if ttfbMs <= 0 {
					ttfbMs = 1
				}
			}
			body = append(body, buf[:n]...)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return status, ttfbMs, body, readErr
		}
	}
	if status < 200 || status >= 300 {
		return status, ttfbMs, body, fmt.Errorf("HTTP %d", status)
	}
	return status, ttfbMs, body, nil
}

func (c *Checker) sampleThroughput(ctx context.Context, resources []domain.MirrorResource, limit int64) int64 {
	preference := []string{"platform-tools", "cmdline-tools;latest", "emulator", "repository"}
	var target string
	for _, id := range preference {
		for _, resource := range resources {
			if resource.ID == id && resource.Available && resource.URL != "" {
				target = resource.URL
				break
			}
		}
		if target != "" {
			break
		}
	}
	if target == "" {
		return 0
	}
	sampleCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(sampleCtx, http.MethodGet, target, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "AVDDesktop/0.2 mirror-check")
	req.Header.Set("Range", "bytes=0-"+strconv.FormatInt(limit-1, 10))
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		req, err = http.NewRequestWithContext(sampleCtx, http.MethodGet, target, nil)
		if err != nil {
			return 0
		}
		req.Header.Set("User-Agent", "AVDDesktop/0.2 mirror-check")
		resp, err = c.httpClient().Do(req)
		if err != nil {
			return 0
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return 0
		}
	}
	started := time.Now()
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

func (c *Checker) httpClient() *http.Client {
	if c != nil && c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

func fallbackResources(opts CheckOptions, reason string) []domain.MirrorResource {
	items := []domain.MirrorResource{
		{ID: "cmdline-tools;latest", Name: "Android 命令行工具", Path: "cmdline-tools;latest", Required: opts.NeedCmdlineTools, Error: reason},
		{ID: "platform-tools", Name: "platform-tools / adb", Path: "platform-tools", Required: opts.NeedPlatformTools, Error: reason},
		{ID: "emulator", Name: "Android Emulator", Path: "emulator", Required: opts.NeedEmulator, Error: reason},
	}
	if opts.CheckSystemImages {
		items = append(items, domain.MirrorResource{
			ID: "system-images:google_apis", Name: "Google APIs 系统镜像索引",
			Path: "sys-img/google_apis/sys-img2-3.xml", Error: reason,
		})
	}
	return items
}

func usableLatency(check domain.MirrorCheck) int64 {
	if check.LatencyMs > 0 {
		return check.LatencyMs
	}
	return 1<<62 - 1
}
