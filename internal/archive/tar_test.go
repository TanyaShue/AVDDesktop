package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	linkname string
}

func writeTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	w := tar.NewWriter(gz)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     mode,
			Size:     int64(len(e.body)),
			Typeflag: typeflag,
			Linkname: e.linkname,
		}
		if typeflag == tar.TypeDir {
			hdr.Size = 0
		}
		if err := w.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typeflag == tar.TypeReg || typeflag == tar.TypeRegA {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractTarGzStripsSingleRoot(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "jdk.tar.gz")
	writeTarGz(t, archivePath, []tarEntry{
		// 真实 tar 的顶层目录条目可能不带结尾斜杠。
		{name: "jdk-21", typeflag: tar.TypeDir, mode: 0o755},
		{name: "jdk-21/bin/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "jdk-21/bin/java", body: "#!/bin/sh\n", mode: 0o755},
		{name: "jdk-21/lib/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "jdk-21/lib/data.txt", body: "data", mode: 0o644},
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractTarGz(context.Background(), archivePath, dest); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	for _, rel := range []string{filepath.Join("bin", "java"), filepath.Join("lib", "data.txt")} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("期望存在 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "jdk-21")); err == nil {
		t.Error("包装目录不应被保留")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dest, "bin", "java"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("可执行位未保留: %v", info.Mode())
		}
	}
}

func TestExtractTarGzSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上创建符号链接需要额外权限，Temurin 使用 zip 归档")
	}
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "jdk.tar.gz")
	writeTarGz(t, archivePath, []tarEntry{
		// 真实 tar 的顶层目录条目可能不带结尾斜杠。
		{name: "jdk-21", typeflag: tar.TypeDir, mode: 0o755},
		{name: "jdk-21/bin/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "jdk-21/bin/java", body: "java", mode: 0o755},
		{name: "jdk-21/bin/java2", typeflag: tar.TypeSymlink, linkname: "java", mode: 0o777},
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractTarGz(context.Background(), archivePath, dest); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	target, err := os.Readlink(filepath.Join(dest, "bin", "java2"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "java" {
		t.Fatalf("符号链接目标 = %q，期望 java", target)
	}
}

func TestExtractTarGzRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "evil.tar.gz")
	writeTarGz(t, archivePath, []tarEntry{
		{name: "../evil.txt", body: "pwned"},
	})

	err := ExtractTarGz(context.Background(), archivePath, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("越界条目必须被拒绝")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "evil.txt")); statErr == nil {
		t.Fatal("越界条目逃出了目标目录")
	}
}

func TestExtractTarGzContextCanceled(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "jdk.tar.gz")
	writeTarGz(t, archivePath, []tarEntry{
		{name: "jdk-21/file.txt", body: strings.Repeat("x", 1024)},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ExtractTarGz(ctx, archivePath, filepath.Join(dir, "out")); err == nil {
		t.Fatal("已取消的上下文应返回错误")
	}
}
