package sdk

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeZip 用 archive/zip 现场生成测试归档。
func writeZip(t *testing.T, path string, entries map[string]string) {
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

func TestExtractZipStripsSingleRoot(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "a.zip")
	writeZip(t, archivePath, map[string]string{
		"cmdline-tools/bin/sdkmanager": "#!/bin/sh\n",
		"cmdline-tools/source.txt":     "v21.0",
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractZip(context.Background(), archivePath, dest); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	// 唯一的顶层目录（cmdline-tools/）必须被剥离
	for _, rel := range []string{filepath.Join("bin", "sdkmanager"), "source.txt"} {
		got := filepath.Join(dest, rel)
		if _, err := os.Stat(got); err != nil {
			t.Errorf("期望存在 %s: %v", got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "cmdline-tools")); err == nil {
		t.Error("包装目录不应被保留")
	}
	body, err := os.ReadFile(filepath.Join(dest, "source.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "v21.0" {
		t.Errorf("文件内容不一致: %q", string(body))
	}
}

func TestExtractZipKeepsMultipleRoots(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "b.zip")
	writeZip(t, archivePath, map[string]string{
		"one/a.txt": "a",
		"two/b.txt": "b",
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractZip(context.Background(), archivePath, dest); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	// 多个顶层目录 → 不剥离
	for _, rel := range []string{filepath.Join("one", "a.txt"), filepath.Join("two", "b.txt")} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("期望存在 %s: %v", rel, err)
		}
	}
}

func TestExtractZipRejectsEscapes(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"../evil.txt", "/abs.txt"} {
		archivePath := filepath.Join(dir, "evil.zip")
		writeZip(t, archivePath, map[string]string{
			name:     "pwned",
			"ok.txt": "fine",
		})

		dest := filepath.Join(dir, "out")
		_ = os.RemoveAll(dest)
		err := ExtractZip(context.Background(), archivePath, dest)
		if err == nil {
			t.Fatalf("越界条目 %q 必须被拒绝", name)
		}
		// 越界文件不能出现在目标目录之外
		if _, statErr := os.Stat(filepath.Join(dir, "evil.txt")); statErr == nil {
			t.Fatalf("越界条目 %q 逃出了目标目录", name)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "abs.txt")); statErr == nil {
			t.Fatalf("绝对路径条目 %q 逃出了目标目录", name)
		}
	}
}

func TestExtractZipContextCanceled(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "c.zip")
	writeZip(t, archivePath, map[string]string{"root/file.txt": "x"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ExtractZip(ctx, archivePath, filepath.Join(dir, "out")); err == nil {
		t.Fatal("已取消的上下文应返回错误")
	}
}
