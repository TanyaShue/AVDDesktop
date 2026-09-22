package sdk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AVDDesktop/internal/platform"
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

const androidCLISample = `WARNING: The SDK Manager CLI tool (sdkmanager) is deprecated. Android CLI will be used instead.

Installed packages:
  cmdline-tools/latest         unknown   ->        23.0.0  Android SDK Command-line Tools (latest)
  emulator                     37.1.11                     Android Emulator
  platform-tools               37.0.1                      Android SDK Platform-Tools
  system-images/android-36.1/google_apis_playstore/x86_64 4.0.0 Google Play Intel x86_64 Atom System Image
Available packages:
  system-images/android-35/google_apis/x86_64             9.0.0 Google APIs Intel x86_64 Atom System Image
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

func TestParsePackagesAndroidCLIFormat(t *testing.T) {
	pkgs := ParsePackages(androidCLISample)
	byPath := map[string]Package{}
	for _, pkg := range pkgs {
		byPath[pkg.Path] = pkg
	}
	if len(pkgs) != 5 {
		t.Fatalf("期望解析出 5 个包，实际 %d: %+v", len(pkgs), pkgs)
	}
	cmdline := byPath["cmdline-tools;latest"]
	if !cmdline.Installed || cmdline.Version != "23.0.0" {
		t.Fatalf("cmdline-tools 解析错误: %+v", cmdline)
	}
	installedImage := byPath["system-images;android-36.1;google_apis_playstore;x86_64"]
	if !installedImage.Installed || installedImage.Version != "4.0.0" {
		t.Fatalf("已安装系统镜像解析错误: %+v", installedImage)
	}
	availableImage := byPath["system-images;android-35;google_apis;x86_64"]
	if availableImage.Installed || availableImage.Version != "9.0.0" {
		t.Fatalf("可安装系统镜像解析错误: %+v", availableImage)
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

func TestCLIPackageArgsNormalizeSemicolonPaths(t *testing.T) {
	got := cliPackageArgs([]string{
		"system-images;android-36.1;google_apis;arm64-v8a",
		" platform-tools ",
		"",
	})
	want := []string{
		"system-images/android-36.1/google_apis/arm64-v8a",
		"platform-tools",
	}
	if len(got) != len(want) {
		t.Fatalf("clI 包参数数量 = %d，期望 %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("clI 包参数[%d] = %q，期望 %q", i, got[i], want[i])
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

func TestUninstallCommandUsesSlashPaths(t *testing.T) {
	tools := platform.NewTools(filepath.Join("C:", "sdk"))
	got := UninstallCommand(tools, []string{"system-images;android-36;google_apis;x86_64"})
	if !strings.Contains(got, "--uninstall") {
		t.Fatalf("命令行缺少 --uninstall: %q", got)
	}
	if !strings.Contains(got, "system-images/android-36/google_apis/x86_64") {
		t.Fatalf("命令行未使用斜杠形式的包路径: %q", got)
	}
	if strings.Contains(got, ";") {
		t.Fatalf("命令行仍包含分号（Windows 批处理会拆参数）: %q", got)
	}
}

func TestNormalizePackagePaths(t *testing.T) {
	got := normalizePackagePaths([]string{
		" system-images;android-36;google_apis;x86_64 ",
		"",
		"system-images;android-36;google_apis;x86_64",
		"platform-tools",
	})
	want := []string{"system-images;android-36;google_apis;x86_64", "platform-tools"}
	if len(got) != len(want) {
		t.Fatalf("去重后数量 = %d，期望 %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("包路径[%d] = %q，期望 %q", i, got[i], want[i])
		}
	}
}

func TestRemainingPackageDirs(t *testing.T) {
	sdkRoot := t.TempDir()
	tools := platform.NewTools(sdkRoot)
	pkgPath := "system-images;android-36;google_apis;x86_64"
	rel, err := ImageDir(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(sdkRoot, rel)
	if remaining := remainingPackageDirs(tools, []string{pkgPath}); len(remaining) != 0 {
		t.Fatalf("镜像尚未安装时不应报告残留目录: %#v", remaining)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	remaining := remainingPackageDirs(tools, []string{pkgPath, "not-a-package;path"})
	if len(remaining) != 1 || remaining[0] != dir {
		t.Fatalf("残留目录检测错误: %#v（期望 %q）", remaining, dir)
	}
}
