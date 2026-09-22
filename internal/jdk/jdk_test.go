package jdk

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestArtifactForKnownPlatforms(t *testing.T) {
	targets := []struct {
		goos   string
		goarch string
		suffix string
	}{
		{"windows", "amd64", ".zip"},
		{"windows", "arm64", ".zip"},
		{"darwin", "amd64", ".tar.gz"},
		{"darwin", "arm64", ".tar.gz"},
		{"linux", "amd64", ".tar.gz"},
		{"linux", "arm64", ".tar.gz"},
	}
	for _, tc := range targets {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			got, err := artifactFor(tc.goos, tc.goarch)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(got.Name, tc.suffix) {
				t.Fatalf("归档名 %q 应以 %q 结尾", got.Name, tc.suffix)
			}
			if !strings.HasSuffix(got.URL, got.Name) {
				t.Fatalf("下载地址 %q 未指向 %q", got.URL, got.Name)
			}
			if len(got.SHA256) != 64 {
				t.Fatalf("SHA-256 长度 = %d，期望 64", len(got.SHA256))
			}
		})
	}
}

func TestArtifactForUnsupportedPlatform(t *testing.T) {
	if _, err := artifactFor("plan9", "amd64"); err == nil {
		t.Fatal("不支持的平台必须返回错误")
	}
}

func TestInstallArchiveReplacesExistingJdk(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "jdk")
	home := jdkHomeForTest(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(root, "old.txt")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "jdk.zip")
	rel, err := filepath.Rel(root, home)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.ToSlash(filepath.Join("jdk-test", rel, "bin", testJavaExe()))
	writeTestZip(t, archivePath, map[string]string{entry: "java"})

	if err := installArchive(context.Background(), root, home, archivePath); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "bin", testJavaExe())); err != nil {
		t.Fatalf("java 未安装到预期位置: %v", err)
	}
	if _, err := os.Stat(oldFile); err == nil {
		t.Fatal("旧 JDK 内容仍存在，说明没有完成替换")
	}
	for _, leftover := range []string{root + ".staging", root + ".old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Fatalf("安装后残留目录: %s", leftover)
		}
	}
}

func TestInstallArchiveRejectsMissingJava(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "jdk")
	home := jdkHomeForTest(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "bad.zip")
	writeTestZip(t, archivePath, map[string]string{"jdk-test/readme.txt": "no java"})
	if err := installArchive(context.Background(), root, home, archivePath); err == nil {
		t.Fatal("缺少 java 的归档必须报错")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("安装失败不应破坏已有 JDK 目录")
	}
}

func jdkHomeForTest(root string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(root, "Contents", "Home")
	}
	return root
}

func testJavaExe() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
