package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCurrentDownloadReportsActiveArchive 验证能发现 sdkmanager 正在下载的归档及其已写入字节数。
func TestCurrentDownloadReportsActiveArchive(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".temp", "PackageOperation01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "arm64-v8a-36_r07.zip"), make([]byte, 1234), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := currentDownload(root)
	if !ok {
		t.Fatal("未发现正在下载的归档")
	}
	if got.Archive != "arm64-v8a-36_r07.zip" {
		t.Errorf("Archive = %q，期望 %q", got.Archive, "arm64-v8a-36_r07.zip")
	}
	if got.Done != 1234 {
		t.Errorf("Done = %d，期望 1234", got.Done)
	}
}

// TestCurrentDownloadWithoutTemp 验证没有下载时返回 ok=false。
func TestCurrentDownloadWithoutTemp(t *testing.T) {
	if _, ok := currentDownload(t.TempDir()); ok {
		t.Error("没有下载任务时不应报告进度")
	}
}

// TestArchiveURLsPrefersSystemImageTag 验证系统镜像归档优先到 sys-img/<tag>/ 下找。
func TestArchiveURLsPrefersSystemImageTag(t *testing.T) {
	urls := archiveURLs("https://example.com/repo/",
		[]string{"system-images;android-36;google_apis;arm64-v8a"}, "x.zip")
	if len(urls) == 0 {
		t.Fatal("没有生成候选地址")
	}
	want := "https://example.com/repo/sys-img/google_apis/x.zip"
	if urls[0] != want {
		t.Errorf("首个候选地址 = %q，期望 %q", urls[0], want)
	}
	if urls[len(urls)-1] != "https://example.com/repo/x.zip" {
		t.Errorf("兜底候选地址 = %q，期望仓库根目录", urls[len(urls)-1])
	}
}

// TestRemoteArchiveSizeUsesHEAD 验证通过 HEAD 拿到归档总大小。
func TestRemoteArchiveSizeUsesHEAD(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		if r.Method != http.MethodHead {
			t.Errorf("请求方法 = %s，期望 HEAD", r.Method)
		}
		w.Header().Set("Content-Length", "2248704000")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := progressWatcher{baseURL: srv.URL + "/", client: srv.Client()}
	size := w.remoteSize(context.Background(),
		[]string{"system-images;android-37.2;google_apis_ps16k;arm64-v8a"}, "arm64-v8a-ps16k-37.2_r05.zip")
	if size != 2248704000 {
		t.Errorf("size = %d，期望 2248704000", size)
	}
	if !strings.HasSuffix(asked, "/sys-img/google_apis_ps16k/arm64-v8a-ps16k-37.2_r05.zip") {
		t.Errorf("请求路径 = %q，未命中 sys-img 路径", asked)
	}
}

// TestWatchInstallProgressStreamsBytes 验证轮询会把增长的字节数回调出去（含总量）。
func TestWatchInstallProgressStreamsBytes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".temp", "PackageOperation01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "img.zip")
	if err := os.WriteFile(file, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan DownloadProgress, 128)
	w := progressWatcher{
		sdkRoot:  root,
		packages: []string{"emulator"},
		baseURL:  srv.URL + "/",
		client:   srv.Client(),
		interval: 20 * time.Millisecond,
	}
	go w.run(ctx, func(p DownloadProgress) { got <- p })

	select {
	case p := <-got:
		if p.Done != 100 || p.Total != 1000 {
			t.Fatalf("首个进度 = %+v，期望 Done=100 Total=1000", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内没有收到任何进度回调")
	}

	if err := os.WriteFile(file, make([]byte, 400), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case p := <-got:
			if p.Done == 400 {
				return
			}
		case <-deadline:
			t.Fatal("2s 内没有收到增长后的进度回调")
		}
	}
}
