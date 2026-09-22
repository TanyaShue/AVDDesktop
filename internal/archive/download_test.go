package archive

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestDownloadVerifiesSHA1(t *testing.T) {
	const payload = "command-line-tools-payload"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "tools.zip")

	// 校验值不匹配 → 报错，且不留下目标文件与 .part 临时文件
	if err := Download(context.Background(), srv.URL, dest, sha1Hex("别的文件"), nil); err == nil {
		t.Fatal("SHA-1 不匹配时必须返回错误")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("校验失败后不应留下目标文件")
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error("校验失败后不应留下 .part 临时文件")
	}

	// 校验值匹配 → 写入正确内容
	if err := Download(context.Background(), srv.URL, dest, sha1Hex(payload), nil); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != payload {
		t.Errorf("内容不一致: %q", string(body))
	}
}

func TestDownloadVerifiesSHA256(t *testing.T) {
	const payload = "temurin-jdk-payload"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "jdk.zip")
	if err := Download(context.Background(), srv.URL, dest, sha256Hex(payload), nil); err != nil {
		t.Fatalf("SHA-256 下载失败: %v", err)
	}
	if err := Download(context.Background(), srv.URL, dest+".bad", strings.Repeat("0", 64), nil); err == nil {
		t.Fatal("SHA-256 不匹配时必须返回错误")
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.zip")
	err := Download(context.Background(), srv.URL, dest, "", nil)
	if err == nil {
		t.Fatal("HTTP 404 必须返回错误")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("失败时不应留下目标文件")
	}
}

func TestDownloadContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1024)))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := filepath.Join(t.TempDir(), "y.zip")
	err := Download(ctx, srv.URL, dest, "", nil)
	if err == nil {
		t.Fatal("已取消的上下文必须返回错误")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("取消后不应留下目标文件")
	}
}

func TestDownloadRejectsUnknownChecksumLength(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "z.zip")
	if err := Download(context.Background(), "https://example.invalid/z.zip", dest, "abc", nil); err == nil {
		t.Fatal("未知长度的校验值必须返回错误")
	}
}
