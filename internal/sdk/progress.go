package sdk

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DownloadProgress 是一次组件下载的进度快照。
type DownloadProgress struct {
	Archive string // 归档文件名
	Done    int64  // 已写入字节数
	Total   int64  // 归档总大小；0 表示未知（界面应退化成不确定进度）
}

// ActiveDownload 是 sdkmanager 正在下载的归档。
type ActiveDownload struct {
	Archive string
	Path    string
	Done    int64
}

const (
	// progressPollInterval 是轮询下载临时目录的间隔。
	progressPollInterval = 300 * time.Millisecond
	// progressHTTPTimeout 是查询归档总大小的超时。
	progressHTTPTimeout = 20 * time.Second
	// tempDirName 是 sdkmanager 存放下载中间产物的目录。
	tempDirName = ".temp"
	// operationDirPrefix 是下载中间目录的前缀（PackageOperation01 / 02 …）。
	operationDirPrefix = "PackageOperation"
)

// currentDownload 返回 sdkmanager 当前正在下载的归档（没有则 ok=false）。
//
// sdkmanager 把归档下载到 <sdk>/.temp/PackageOperationNN/<归档名>，下载期间文件持续增长，
// 完成后整个目录被清空。这是它在非交互环境下唯一可见的进度信号——实测 sdkmanager
// 即使接上 TTY 也不输出任何进度条或百分比。
func currentDownload(sdkRoot string) (ActiveDownload, bool) {
	tempRoot := filepath.Join(sdkRoot, tempDirName)
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return ActiveDownload{}, false
	}

	type dirItem struct {
		path string
		mod  time.Time
	}
	var dirs []dirItem
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), operationDirPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, dirItem{path: filepath.Join(tempRoot, e.Name()), mod: info.ModTime()})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mod.After(dirs[j].mod) })

	for _, d := range dirs {
		// 只认归档文件本体：解压阶段目录里会出现大量非归档文件，
		// 拿它们的体积当进度会让进度条突然回跳。
		files, err := os.ReadDir(d.path)
		if err != nil {
			continue
		}
		var best ActiveDownload
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".zip") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.Size() > best.Done {
				best = ActiveDownload{
					Archive: f.Name(),
					Path:    filepath.Join(d.path, f.Name()),
					Done:    info.Size(),
				}
			}
		}
		if best.Archive != "" {
			return best, true
		}
	}
	return ActiveDownload{}, false
}

// progressWatcher 轮询下载临时目录并把字节进度回调出去。
type progressWatcher struct {
	sdkRoot  string
	packages []string
	baseURL  string
	client   *http.Client
	interval time.Duration
}

// WatchInstallProgress 在 ctx 存续期间轮询 sdkmanager 的下载进度并回调，直到 ctx 结束。
//
// 总量通过官方仓库的 HEAD 请求获得；拿不到总量时 Total 为 0，调用方应显示不确定进度，
// 绝不推算假百分比。
func WatchInstallProgress(ctx context.Context, sdkRoot string, packages []string, onProgress func(DownloadProgress)) {
	if onProgress == nil {
		return
	}
	w := progressWatcher{
		sdkRoot:  sdkRoot,
		packages: packages,
		baseURL:  repoBaseURL,
		client:   &http.Client{Timeout: progressHTTPTimeout},
		interval: progressPollInterval,
	}
	w.run(ctx, onProgress)
}

func (w progressWatcher) run(ctx context.Context, onProgress func(DownloadProgress)) {
	interval := w.interval
	if interval <= 0 {
		interval = progressPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sizes := map[string]int64{}
	lastArchive := ""
	var lastDone int64 = -1

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		active, ok := currentDownload(w.sdkRoot)
		if !ok {
			// 下载结束（进入解压/安装阶段）：继续按节奏上报同一个字节数，
			// 让界面把速度平滑衰减到 0，而不是一直挂着下载阶段的速度。
			if lastArchive == "" {
				continue
			}
			onProgress(DownloadProgress{Archive: lastArchive, Done: lastDone, Total: sizes[lastArchive]})
			continue
		}
		if active.Archive != lastArchive {
			lastArchive, lastDone = active.Archive, -1
		}
		if active.Done == lastDone {
			continue
		}
		lastDone = active.Done

		total, seen := sizes[active.Archive]
		if !seen {
			total = w.remoteSize(ctx, w.packages, active.Archive)
			sizes[active.Archive] = total
		}
		onProgress(DownloadProgress{Archive: active.Archive, Done: active.Done, Total: total})
	}
}

// remoteSize 返回归档在官方仓库中的大小（查不到返回 0）。
func (w progressWatcher) remoteSize(ctx context.Context, packages []string, archive string) int64 {
	client := w.client
	if client == nil {
		client = &http.Client{Timeout: progressHTTPTimeout}
	}
	base := strings.TrimSpace(w.baseURL)
	if base == "" {
		base = repoBaseURL
	}
	for _, url := range archiveURLs(base, packages, archive) {
		if size, err := headContentLength(ctx, client, url); err == nil && size > 0 {
			return size
		}
	}
	return 0
}

// archiveURLs 返回归档在官方仓库中的候选地址（按可能性排序）。
//
// 系统镜像放在 sys-img/<tag>/ 子目录下，其它组件放在仓库根目录。
func archiveURLs(baseURL string, packages []string, archive string) []string {
	base := baseURL
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	var out []string
	seen := map[string]bool{}
	add := func(url string) {
		if !seen[url] {
			seen[url] = true
			out = append(out, url)
		}
	}
	for _, pkg := range packages {
		if tag, ok := imageTag(pkg); ok {
			add(base + "sys-img/" + tag + "/" + archive)
		}
	}
	add(base + archive)
	return out
}

// imageTag 从 system-images;<api>;<tag>;<abi> 中取出 tag（如 google_apis_ps16k）。
func imageTag(pkg string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(pkg), ";")
	if len(parts) < 4 || parts[0] != "system-images" || strings.TrimSpace(parts[2]) == "" {
		return "", false
	}
	return parts[2], true
}

// headContentLength 用 HEAD 请求取归档大小。
func headContentLength(ctx context.Context, client *http.Client, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("HEAD " + url + " 返回 " + resp.Status)
	}
	return resp.ContentLength, nil
}
