package query

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"AVDDesktop/internal/sdk/localrepo"
)

// writeFile 在测试目录里落一个文件（自动建父目录）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeSdk 搭一个最小 SDK 目录：emulator 只有 source.properties（模拟本项目安装的结果），
// platform-tools 有 package.xml（官方工具可见）。
func fakeSdk(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "emulator", "source.properties"), "Pkg.Revision=37.1.11\nPkg.Desc=Android Emulator\n")
	writeFile(t, filepath.Join(root, "platform-tools", "source.properties"), "Pkg.Revision=37.0.1\n")
	writeFile(t, filepath.Join(root, "platform-tools", localrepo.FileName), "<ns2:repository xmlns:ns2=\"x\"/>\n")
	writeFile(t, filepath.Join(root, "system-images", "android-34", "android-desktop", "x86_64", "source.properties"),
		"Pkg.Revision=1\nPkg.Desc=Desktop Image\n")
	// 没有 source.properties 的目录不算已安装
	writeFile(t, filepath.Join(root, "build-tools", "36.1.0", "d8.bat"), "@echo off\n")
	return root
}

func TestScannerInstalledAndMetadataMissing(t *testing.T) {
	root := fakeSdk(t)
	s := NewScanner(root)

	var paths []string
	for _, p := range s.Installed() {
		paths = append(paths, p.Path)
	}
	want := []string{"emulator", "platform-tools", "system-images;android-34;android-desktop;x86_64"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("Installed() = %v, want %v", paths, want)
	}

	missing := s.MetadataMissing()
	wantMissing := []string{"emulator", "system-images;android-34;android-desktop;x86_64"}
	if !reflect.DeepEqual(missing, wantMissing) {
		t.Fatalf("MetadataMissing() = %v, want %v", missing, wantMissing)
	}

	if got := NewScanner(filepath.Join(root, "does-not-exist")).Installed(); got != nil {
		t.Errorf("不存在的 SDK 目录应返回空列表，实际 %v", got)
	}
}

func TestPackageDir(t *testing.T) {
	cases := map[string]string{
		"emulator":             "emulator",
		"platform-tools":       "platform-tools",
		"platforms;android-36": "platforms/android-36",
		"system-images;android-34;google_apis;x86_64": "system-images/android-34/google_apis/x86_64",
	}
	for in, want := range cases {
		if got := PackageDir(in); got != filepath.FromSlash(want) {
			t.Errorf("PackageDir(%q) = %q, want %q", in, got, filepath.FromSlash(want))
		}
	}
}
