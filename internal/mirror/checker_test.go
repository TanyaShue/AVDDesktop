package mirror

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
)

func TestCheckerFindsCompleteMirror(t *testing.T) {
	srv := newMirrorTestServer(t, false, 0)
	defer srv.Close()

	checker := NewChecker()
	result := checker.Check(context.Background(), domain.MirrorSource{
		ID:      "test",
		Name:    "测试镜像",
		BaseURL: srv.URL + "/repository/",
	}, CheckOptions{
		NeedCmdlineTools:  true,
		NeedPlatformTools: true,
		NeedEmulator:      true,
		CheckSystemImages: true,
		SampleBytes:       32,
		Timeout:           3 * time.Second,
	})

	if !result.Reachable || !result.Compatible || !result.Recommended {
		t.Fatalf("完整镜像应可用: %+v", result)
	}
	if len(result.Resources) != 5 {
		t.Fatalf("资源数量 = %d，期望 5: %+v", len(result.Resources), result.Resources)
	}
	for _, resource := range result.Resources {
		if !resource.Available {
			t.Fatalf("资源 %s 应可用: %+v", resource.ID, resource)
		}
	}
}

func TestCheckerMarksMissingRequiredResourceIncompatible(t *testing.T) {
	srv := newMirrorTestServer(t, true, 0)
	defer srv.Close()

	result := NewChecker().Check(context.Background(), domain.MirrorSource{
		ID:      "test",
		Name:    "测试镜像",
		BaseURL: srv.URL + "/repository/",
	}, CheckOptions{
		NeedPlatformTools: true,
		CheckSystemImages: true,
		SampleBytes:       32,
		Timeout:           3 * time.Second,
	})

	if result.Compatible {
		t.Fatalf("缺少 platform-tools 时不应兼容: %+v", result)
	}
	if result.Error == "" {
		t.Fatal("缺少必需资源时应给出错误说明")
	}
}

func TestCheckAllRecommendsLowerLatencyCompleteMirror(t *testing.T) {
	fast := newMirrorTestServer(t, false, 0)
	defer fast.Close()
	slow := newMirrorTestServer(t, false, 60*time.Millisecond)
	defer slow.Close()

	results := NewChecker().CheckAll(context.Background(), []domain.MirrorSource{
		{ID: "slow", Name: "慢镜像", BaseURL: slow.URL + "/repository/"},
		{ID: "fast", Name: "快镜像", BaseURL: fast.URL + "/repository/"},
	}, CheckOptions{
		NeedCmdlineTools:  true,
		NeedPlatformTools: true,
		NeedEmulator:      true,
		CheckSystemImages: true,
		SampleBytes:       32,
		Timeout:           3 * time.Second,
	})

	if len(results) != 2 || !results[0].Recommended || results[0].SourceID != "fast" {
		t.Fatalf("应推荐延迟最低的完整镜像: %+v", results)
	}
}

func TestCheckerReportsUnreachableSource(t *testing.T) {
	result := NewChecker().Check(context.Background(), domain.MirrorSource{
		ID:      "offline",
		Name:    "离线镜像",
		BaseURL: "http://127.0.0.1:1/repository/",
	}, CheckOptions{Timeout: time.Second})
	if result.Reachable || result.Compatible {
		t.Fatalf("连接失败的镜像不应标记为可用: %+v", result)
	}
}

func TestCheckerFallsBackWhenRangeUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/repository2-3.xml") {
			_, _ = fmt.Fprint(w, repositoryTestXML)
			return
		}
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if r.URL.Path == "/repository/sys-img/google_apis/sys-img2-3.xml" {
			_, _ = fmt.Fprint(w, `<sdk-sys-img><remotePackage path="x"/></sdk-sys-img>`)
			return
		}
		writeTestDownload(w)
	}))
	defer srv.Close()

	result := NewChecker().Check(context.Background(), domain.MirrorSource{
		ID:      "no-range",
		Name:    "无 Range 镜像",
		BaseURL: srv.URL + "/repository/",
	}, CheckOptions{
		NeedCmdlineTools:  true,
		NeedPlatformTools: true,
		NeedEmulator:      true,
		CheckSystemImages: true,
		SampleBytes:       16,
		Timeout:           3 * time.Second,
	})
	if !result.Compatible {
		t.Fatalf("不支持 Range 时仍应通过普通 GET 校验资源: %+v", result)
	}
}

func newMirrorTestServer(t *testing.T, missingPlatformTools bool, indexDelay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/repository2-3.xml") {
			if indexDelay > 0 {
				time.Sleep(indexDelay)
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, repositoryTestXML)
			return
		}
		switch r.URL.Path {
		case "/repository/commandlinetools-test.zip", "/repository/emulator-test.zip":
			writeTestDownload(w)
		case "/repository/platform-tools-test.zip":
			if missingPlatformTools {
				http.NotFound(w, r)
				return
			}
			writeTestDownload(w)
		case "/repository/sys-img/google_apis/sys-img2-3.xml":
			_, _ = fmt.Fprint(w, `<sdk-sys-img><remotePackage path="system-images;android-35;google_apis;x86_64"/></sdk-sys-img>`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeTestDownload(w http.ResponseWriter) {
	w.Header().Set("Content-Length", "1024")
	_, _ = w.Write([]byte(strings.Repeat("x", 1024)))
}

const repositoryTestXML = `<?xml version="1.0" encoding="UTF-8"?>
<sdk-repository>
  <remotePackage path="cmdline-tools;latest">
    <display-name>Command-line Tools</display-name>
    <archives><archive><complete><size>1024</size><checksum type="sha1">aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</checksum><url>commandlinetools-test.zip</url></complete></archive></archives>
  </remotePackage>
  <remotePackage path="platform-tools">
    <display-name>Platform Tools</display-name>
    <archives><archive><complete><size>1024</size><checksum type="sha1">bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb</checksum><url>platform-tools-test.zip</url></complete></archive></archives>
  </remotePackage>
  <remotePackage path="emulator">
    <display-name>Emulator</display-name>
    <archives><archive><complete><size>1024</size><checksum type="sha1">cccccccccccccccccccccccccccccccccccccccc</checksum><url>emulator-test.zip</url></complete></archive></archives>
  </remotePackage>
</sdk-repository>`
