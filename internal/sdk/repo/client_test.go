package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseRealIndexes 用实测抓取的索引文件验证解析器（仅在本机存在样本时运行）。
func TestParseRealIndexes(t *testing.T) {
	dir := os.Getenv("AVDDESKTOP_SAMPLE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "avddesign")
	}
	cases := []struct {
		file string
		want string
	}{
		{"repo_google.xml", "emulator"},
		{"repo_tencent.xml", "cmdline-tools;latest"},
		{"sysimg_tencent.xml", "system-images;android-36.1;google_apis;x86_64"},
	}
	for _, c := range cases {
		path := filepath.Join(dir, c.file)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("样本文件不存在，跳过: %s", path)
		}
		idx, err := ParseIndex(data, "https://mirrors.cloud.tencent.com/AndroidSDK/"+c.file)
		if err != nil {
			t.Fatalf("%s 解析失败: %v", c.file, err)
		}
		pkg, ok := idx.Find(c.want)
		if !ok {
			t.Fatalf("%s 中找不到包 %s", c.file, c.want)
		}
		a, err := pkg.PickArchive("windows", "amd64")
		if err != nil {
			t.Fatalf("%s 选择归档失败: %v", c.want, err)
		}
		url := idx.PackageURL(a)
		t.Logf("%s | 包=%s 版本=%s 归档=%s size=%d sha1=%s\n    url=%s licence=%s",
			c.file, pkg.Path, pkg.Revision, a.URL, a.Size, a.SHA1, url, pkg.LicenseID())
		if url == a.URL {
			t.Errorf("归档 URL 未解析为绝对地址: %s", url)
		}
		if a.Size == 0 || a.SHA1 == "" {
			t.Errorf("归档缺少 size/checksum: %+v", a)
		}
	}
}

// TestParseSysImgRelativeBase 验证系统镜像的 URL 基准是索引所在目录。
func TestParseSysImgRelativeBase(t *testing.T) {
	dir := os.Getenv("AVDDESKTOP_SAMPLE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "avddesign")
	}
	data, err := os.ReadFile(filepath.Join(dir, "sysimg_tencent.xml"))
	if err != nil {
		t.Skip("样本文件不存在")
	}
	source := "https://mirrors.cloud.tencent.com/AndroidSDK/sys-img/google_apis/sys-img2-3.xml"
	idx, err := ParseIndex(data, source)
	if err != nil {
		t.Fatal(err)
	}
	pkg, ok := idx.Find("system-images;android-36.1;google_apis;x86_64")
	if !ok {
		t.Fatal("未找到系统镜像包")
	}
	a, err := pkg.PickArchive("windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://mirrors.cloud.tencent.com/AndroidSDK/sys-img/google_apis/x86_64-36.1_r04.zip"
	if got := idx.PackageURL(a); got != want {
		t.Errorf("URL 基准错误\n got=%s\nwant=%s", got, want)
	}
	if pkg.TagID != "google_apis" || pkg.ABI != "x86_64" {
		t.Errorf("type-details 解析异常: tag=%q abi=%q api=%q", pkg.TagID, pkg.ABI, pkg.APILevel)
	}
}
