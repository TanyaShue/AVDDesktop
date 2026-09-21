package sdk

import (
	"path/filepath"
	"testing"
)

// 样本取自本机 `sdkmanager --list_installed` / `--list` 的真实输出（仅保留结构）。
const installedSample = `Warning: This version only understands SDK XML versions up to 3 but an SDK XML file of version 4 was encountered.
Loading package information...
[=========                              ] 25% Loading local repository...
Installed packages:
  Path                                                    | Version           | Description                                    | Location
  -------                                                 | -------           | -------                                        | -------
  emulator                                                | 37.1.11           | Android Emulator                               | emulator
  platform-tools                                          | 37.0.1            | Android SDK Platform-Tools 37.0.1              | platform-tools
  system-images;android-34;android-desktop;x86_64         | 1                 | Desktop Intel x86_64 Atom System Image         | system-images\android-34\android-desktop\x86_64
  system-images;android-36.1;google_apis_playstore;x86_64 | 4                 | Google Play Intel x86_64 Atom System Image     | system-images\android-36.1\google_apis_playstore\x86_64

`

const bothSectionsSample = `Installed packages:
  Path                | Version | Description       | Location
  -------             | ------- | -------           | -------
  platform-tools      | 37.0.1  | Platform-Tools    | platform-tools

Available Packages:
  Path                                                     | Version | Description                                 | Location
  -------                                                  | ------- | -------                                     | -------
  platform-tools                                           | 37.0.1  | Android SDK Platform-Tools                  | platform-tools
  system-images;android-34;google_apis;x86_64              | 14      | Google APIs Intel x86_64 Atom System Image  |
  system-images;android-35;google_apis;arm64-v8a           | 6       | Google APIs ARM 64 v8a System Image         |
`

func TestParsePackagesInstalled(t *testing.T) {
	pkgs := ParsePackages(installedSample)
	if len(pkgs) != 4 {
		t.Fatalf("期望解析出 4 个包，实际 %d 个: %+v", len(pkgs), pkgs)
	}
	for _, p := range pkgs {
		if !p.Installed {
			t.Errorf("包 %s 应被标记为已安装", p.Path)
		}
	}
	if pkgs[0].Path != "emulator" || pkgs[0].Version != "37.1.11" {
		t.Errorf("首行解析错误: %+v", pkgs[0])
	}
	// 进度行与警告行不应被当作包
	for _, p := range pkgs {
		if p.Path == "---" || p.Path == "Path" || p.Path == "" {
			t.Errorf("表头/分隔行被误解析为包: %+v", p)
		}
	}
}

func TestParsePackagesPrefersInstalled(t *testing.T) {
	pkgs := ParsePackages(bothSectionsSample)
	byPath := map[string]Package{}
	for _, p := range pkgs {
		byPath[p.Path] = p
	}
	if len(pkgs) != 3 {
		t.Fatalf("同一个包出现在两个区块时应去重，实际 %d 个: %+v", len(pkgs), pkgs)
	}
	if pt, ok := byPath["platform-tools"]; !ok || !pt.Installed {
		t.Errorf("platform-tools 应保留「已安装」状态: %+v", pt)
	}
	if img, ok := byPath["system-images;android-34;google_apis;x86_64"]; !ok || img.Installed {
		t.Errorf("未安装的镜像不应被标记为已安装: %+v", img)
	}
}

func TestParsePackagesAvailableOnly(t *testing.T) {
	out := bothSectionsSample[indexOf(bothSectionsSample, "Available Packages:"):]
	pkgs := ParsePackages(out)
	if len(pkgs) != 3 {
		t.Fatalf("期望 3 个可安装包，实际 %d", len(pkgs))
	}
	for _, p := range pkgs {
		if p.Installed {
			t.Errorf("可安装区块不应标记为已安装: %+v", p)
		}
	}
}

func TestSplitImageAndDir(t *testing.T) {
	api, tag, abi, err := SplitImage("system-images;android-36.1;google_apis_playstore;x86_64")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if api != "36.1" || tag != "google_apis_playstore" || abi != "x86_64" {
		t.Fatalf("解析结果错误: api=%q tag=%q abi=%q", api, tag, abi)
	}
	dir, err := ImageDir("system-images;android-34;android-desktop;x86_64")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	// 目录名必须保留 android- 前缀（真实目录是 system-images/android-34/...）
	want := filepath.Join("system-images", "android-34", "android-desktop", "x86_64")
	if dir != want {
		t.Fatalf("镜像目录错误: 期望 %q，实际 %q", want, dir)
	}

	for _, bad := range []string{"", "platform-tools", "system-images;android-34;x86_64"} {
		if _, _, _, err := SplitImage(bad); err == nil {
			t.Errorf("非法镜像路径应报错: %q", bad)
		}
	}
}

func TestTagLabel(t *testing.T) {
	cases := map[string]string{
		"google_apis":           "Google APIs",
		"google_apis_playstore": "Google Play",
		"default":               "AOSP",
		"unknown_tag":           "unknown_tag",
	}
	for in, want := range cases {
		if got := TagLabel(in); got != want {
			t.Errorf("TagLabel(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestCmdlineToolsArchive(t *testing.T) {
	name, sha1sum, err := CmdlineToolsArchive()
	if err != nil {
		t.Fatalf("当前平台应支持自举: %v", err)
	}
	if len(sha1sum) != 40 {
		t.Fatalf("SHA-1 长度异常: %q", sha1sum)
	}
	if !contains(name, "commandlinetools-") || !contains(name, cmdlineToolsBuild) {
		t.Fatalf("归档名不符合官方命名: %q", name)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool { return indexOf(s, sub) >= 0 }
