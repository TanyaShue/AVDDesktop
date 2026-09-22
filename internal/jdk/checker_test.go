package jdk

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
)

func TestArtifactForBuiltinSources(t *testing.T) {
	for _, source := range Sources() {
		source := source
		t.Run(source.ID, func(t *testing.T) {
			spec, err := artifactForSource(source, "windows", "amd64")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(spec.URL, spec.Name) {
				t.Fatalf("下载地址 %q 未指向 %q", spec.URL, spec.Name)
			}
			if len(spec.SHA256) != 64 {
				t.Fatalf("SHA-256 长度 = %d，期望 64", len(spec.SHA256))
			}
			if source.ID != OfficialSourceID && !strings.Contains(spec.URL, "/21/jdk/x64/windows/") {
				t.Fatalf("国内镜像地址未包含版本与平台路径: %s", spec.URL)
			}
		})
	}
}

func TestCheckerCheckAllRecommendsFasterMirror(t *testing.T) {
	fast := newArtifactServer(t, 0)
	defer fast.Close()
	slow := newArtifactServer(t, 40*time.Millisecond)
	defer slow.Close()

	results := NewChecker().CheckAll(context.Background(), []domain.MirrorSource{
		{ID: "slow", Name: "慢镜像", BaseURL: slow.URL + "/"},
		{ID: "fast", Name: "快镜像", BaseURL: fast.URL + "/"},
	}, CheckOptions{SampleBytes: 64 << 10, Timeout: 3 * time.Second})

	if len(results) != 2 || !results[0].Recommended || results[0].SourceID != "fast" {
		t.Fatalf("应推荐延迟最低的可用镜像: %+v", results)
	}
	for _, result := range results {
		if !result.Compatible || !result.Reachable || result.ThroughputBps <= 0 {
			t.Fatalf("镜像检测结果不完整: %+v", result)
		}
		if len(result.Resources) != 1 || !result.Resources[0].Available {
			t.Fatalf("JDK 归档应标记为可用: %+v", result.Resources)
		}
	}
}

func TestCheckerMarksMissingArtifactIncompatible(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	result := NewChecker().Check(context.Background(), domain.MirrorSource{
		ID: "missing", Name: "缺失镜像", BaseURL: srv.URL + "/",
	}, CheckOptions{NeedJDK: true, Timeout: time.Second})

	if result.Compatible {
		t.Fatalf("404 不应被标记为可用: %+v", result)
	}
	if len(result.Resources) != 1 || result.Resources[0].Available || result.Resources[0].Error == "" {
		t.Fatalf("缺少归档时应有明确错误: %+v", result.Resources)
	}
}

func TestCheckerFallsBackWhenRangeUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			http.Error(w, "range not supported", http.StatusForbidden)
			return
		}
		writeArtifactBody(w, 0)
	}))
	defer srv.Close()

	result := NewChecker().Check(context.Background(), domain.MirrorSource{
		ID: "no-range", Name: "无 Range 镜像", BaseURL: srv.URL + "/",
	}, CheckOptions{SampleBytes: 16 << 10, Timeout: 2 * time.Second})
	if !result.Compatible {
		t.Fatalf("不支持 Range 时仍应通过普通 GET 判定可用: %+v", result)
	}
}

func newArtifactServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-262143/262144")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(bytes.Repeat([]byte("x"), 256<<10))
			return
		}
		writeArtifactBody(w, 256<<10)
	}))
}

func writeArtifactBody(w http.ResponseWriter, size int) {
	if size <= 0 {
		size = 64 << 10
	}
	w.Header().Set("Content-Length", strconv.Itoa(size))
	_, _ = fmt.Fprintf(w, "%s", strings.Repeat("x", size))
}
