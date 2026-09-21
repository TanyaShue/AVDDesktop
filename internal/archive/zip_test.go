package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// makeZip 创建测试用 zip（条目名按传入顺序写入）。
func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w := zip.NewWriter(f)
	// 保证顺序稳定：先写目录项再写文件
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
	return path
}

// TestExtractStripsSingleRoot 验证官方 SDK 归档的“包装目录”会被剥离。
//
// 这是端到端测试发现的真实问题：platform-tools_r37.0.1-win.zip 内层是
// platform-tools/adb.exe，若不剥离就会解压成 <sdk>/platform-tools/platform-tools/adb.exe。
func TestExtractStripsSingleRoot(t *testing.T) {
	zipPath := makeZip(t, map[string]string{
		"platform-tools/":                  "",
		"platform-tools/adb.exe":           "adb-binary",
		"platform-tools/source.properties": "Pkg.Revision=37.0.1\n",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest, nil); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !fileExists(filepath.Join(dest, "adb.exe")) {
		t.Fatalf("包装目录未被剥离，解压结果：%v", listAll(t, dest))
	}
	if fileExists(filepath.Join(dest, "platform-tools", "adb.exe")) {
		t.Error("仍存在多余的一层目录")
	}
}

// TestExtractKeepsMultipleRoots 验证多顶层目录的归档不会被错误剥离。
func TestExtractKeepsMultipleRoots(t *testing.T) {
	zipPath := makeZip(t, map[string]string{
		"x86_64/system.img": "system",
		"data/misc.img":     "data",
	})
	dest := t.TempDir()
	if err := ExtractZip(zipPath, dest, nil); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	for _, want := range []string{"x86_64/system.img", "data/misc.img"} {
		if !fileExists(filepath.Join(dest, filepath.FromSlash(want))) {
			t.Errorf("缺少 %s，解压结果：%v", want, listAll(t, dest))
		}
	}
}

// TestExtractKeepsTopLevelFiles 验证含顶层文件的归档保持原结构。
func TestExtractKeepsTopLevelFiles(t *testing.T) {
	zipPath := makeZip(t, map[string]string{
		"NOTICE.txt":              "notice",
		"cmdline-tools/bin/x.bat": "script",
	})
	dest := t.TempDir()
	if err := ExtractZip(zipPath, dest, nil); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !fileExists(filepath.Join(dest, "NOTICE.txt")) {
		t.Errorf("顶层文件丢失：%v", listAll(t, dest))
	}
	if !fileExists(filepath.Join(dest, "cmdline-tools", "bin", "x.bat")) {
		t.Errorf("嵌套文件路径错误：%v", listAll(t, dest))
	}
}

// TestExtractRejectsTraversal 验证目录穿越防护。
func TestExtractRejectsTraversal(t *testing.T) {
	zipPath := makeZip(t, map[string]string{
		"../evil.txt": "pwned",
	})
	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ExtractZip(zipPath, dest, nil); err == nil {
		t.Fatal("包含 .. 的条目应被拒绝")
	}
}

// TestSingleRootPrefix 验证前缀判定逻辑本身。
func TestSingleRootPrefix(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		want    string
	}{
		{"单一目录", []string{"a/", "a/b.txt", "a/c/d.txt"}, "a/"},
		{"两个目录", []string{"a/b.txt", "c/d.txt"}, ""},
		{"含顶层文件", []string{"a/b.txt", "top.txt"}, ""},
		{"顶层文件在前", []string{"top.txt", "a/b.txt"}, ""},
		{"空 zip", []string{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := make([]*zip.File, 0, len(c.entries))
			for _, e := range c.entries {
				files = append(files, &zip.File{FileHeader: zip.FileHeader{Name: e}})
			}
			if got := SingleRootPrefix(files); got != c.want {
				t.Errorf("SingleRootPrefix(%v) = %q，期望 %q", c.entries, got, c.want)
			}
		})
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func listAll(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, rel)
		return nil
	})
	return out
}
