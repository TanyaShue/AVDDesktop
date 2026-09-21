// Package sdk 只负责三件事：
//
//  1. 把官方命令行工具放进软件自己的 SDK 目录（首次运行时下载并解压 cmdline-tools）
//  2. 调用官方 sdkmanager 安装 / 查询组件（system-images、platform-tools、emulator …）
//  3. 解析必要的命令输出（包列表、系统镜像路径）
//
// SDK 仓库索引、包依赖、下载调度、许可校验等一律交给官方工具，本项目不重复实现。
package sdk

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// cmdlineToolsBaseURL 是官方命令行工具归档的来源（不做镜像/测速）。
const cmdlineToolsBaseURL = "https://dl.google.com/android/repository/"

// cmdlineToolsBuild 是官方命令行工具归档的构建号（cmdline-tools;21.0）。
const cmdlineToolsBuild = "15641748"

// sha1 常量取自官方 repository2-3.xml（同一个构建号的归档内容固定）。
var cmdlineToolsSHA1 = map[string]string{
	"windows": "2bea1388b8a248040a340a08ca0638138633f687",
	"darwin":  "b62a5d8cf63ded173b47be867be4ee058ceda6df",
	"linux":   "63523a02a975a81102238566f2a16c057d52301e",
}

// 命令超时策略：按命令类型区分，任何命令都不允许无限等待。
const (
	versionTimeout   = 60 * time.Second // --version
	listTimeout      = 3 * time.Minute  // --list（需要访问官方仓库）
	licenseTimeout   = 3 * time.Minute  // --licenses
	installTimeout   = 45 * time.Minute // 下载安装组件（system image 约 1-2 GB）
	licenseFeed      = 80               // 许可确认时预置的 "y" 行数
	bootstrapTimeout = 30 * time.Minute // 自举整体上限（下载 + 解压）
)

// LineFunc 是命令输出的逐行回调（stream 为 stdout / stderr）。
type LineFunc func(stream, line string)

// ProgressFunc 是字节进度回调。
type ProgressFunc func(done, total int64)

// Package 是 sdkmanager 列表中的一行。
type Package struct {
	Path        string `json:"path"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
}

// CmdlineToolsArchive 返回当前平台对应的官方归档文件名与 SHA-1。
func CmdlineToolsArchive() (name, sha1sum string, err error) {
	// 官方归档里的平台标识是 win / mac / linux，与 runtime.GOOS 不完全一致。
	token := map[string]string{
		"windows": "win",
		"darwin":  "mac",
		"linux":   "linux",
	}[runtime.GOOS]
	if token == "" {
		return "", "", domain.Err(domain.CodeInvalidArgument,
			"当前平台暂不支持自动初始化命令行工具: "+runtime.GOOS)
	}
	name = "commandlinetools-" + token + "-" + cmdlineToolsBuild + "_latest.zip"
	sha1sum = cmdlineToolsSHA1[runtime.GOOS]
	if sha1sum == "" {
		return "", "", domain.Err(domain.CodeInvalidArgument,
			"当前平台缺少归档校验值: "+runtime.GOOS)
	}
	return name, sha1sum, nil
}

// Bootstrap 下载并解压官方命令行工具到 <sdk>/cmdline-tools/latest。
//
// 解压先落到 .staging 目录，成功后整体替换 latest（失败时回滚旧目录），避免留下半成品。
func Bootstrap(ctx context.Context, tools platform.Tools, cacheDir string, onProgress ProgressFunc) error {
	// 下载与解压共用同一个整体超时，避免网络挂死导致无限等待。
	ctx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer cancel()

	archive, sha1sum, err := CmdlineToolsArchive()
	if err != nil {
		return err
	}
	if err := platform.EnsureDir(tools.SdkRoot); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建 SDK 目录: "+tools.SdkRoot, err)
	}
	if err := platform.EnsureDir(cacheDir); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建缓存目录: "+cacheDir, err)
	}

	zipPath := filepath.Join(cacheDir, archive)
	if err := Download(ctx, cmdlineToolsBaseURL+archive, zipPath, sha1sum, onProgress); err != nil {
		return err
	}

	staging := filepath.Join(filepath.Dir(tools.CmdlineTools), ".staging")
	_ = os.RemoveAll(staging)
	if err := ExtractZip(ctx, zipPath, staging); err != nil {
		return err
	}
	// 归档内层是单层 cmdline-tools/ 目录，ExtractZip 已按官方约定剥离。
	if !platform.FileExists(filepath.Join(staging, "bin", scriptName("sdkmanager"))) {
		_ = os.RemoveAll(staging)
		return domain.ErrDetail(domain.CodeArchiveFailed,
			"命令行工具归档内容不符合预期", "缺少 bin/sdkmanager")
	}

	// 可回滚替换：旧目录先挪到 .old，启用新目录失败时再挪回来，避免连旧安装一起丢掉。
	old := tools.CmdlineTools + ".old"
	_ = os.RemoveAll(old)
	hadOld := false
	if platform.DirExists(tools.CmdlineTools) {
		if err := os.Rename(tools.CmdlineTools, old); err != nil {
			_ = os.RemoveAll(staging)
			return domain.Wrap(domain.CodePermissionDenied, "无法替换已有的命令行工具目录", err)
		}
		hadOld = true
	}
	if err := os.Rename(staging, tools.CmdlineTools); err != nil {
		if hadOld {
			_ = os.Rename(old, tools.CmdlineTools)
		}
		_ = os.RemoveAll(staging)
		return domain.Wrap(domain.CodePermissionDenied, "无法启用命令行工具", err)
	}
	_ = os.RemoveAll(old)
	_ = os.RemoveAll(staging)

	// 官方 zip 的条目不一定带 Unix 模式位，非 Windows 平台需显式补上可执行权限。
	if runtime.GOOS != "windows" {
		for _, name := range []string{"sdkmanager", "avdmanager"} {
			_ = os.Chmod(filepath.Join(tools.CmdlineTools, "bin", name), 0o755)
		}
	}
	_ = os.Remove(zipPath)
	return nil
}

// AcceptLicenses 通过官方 sdkmanager 接受 SDK 许可（预置 y 回答，避免交互阻塞）。
func AcceptLicenses(ctx context.Context, tools platform.Tools, env []string, onLine LineFunc) error {
	_, err := runSdkmanager(ctx, tools, env, []string{"--licenses"}, licenseTimeout, onLine, yesLines(licenseFeed))
	return err
}

// InstallPackages 通过官方 sdkmanager 安装组件。
//
// sdkmanager 失败时仍可能有正常输出，因此必须检查退出码，否则会把安装失败当成功。
func InstallPackages(ctx context.Context, tools platform.Tools, env []string, packages []string, onLine LineFunc) error {
	args := append([]string{"--sdk_root=" + tools.SdkRoot}, packages...)
	res, err := runSdkmanager(ctx, tools, env, args, installTimeout, onLine, yesLines(licenseFeed))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return domain.ErrDetail(domain.CodeProcessFailed,
			"sdkmanager 安装失败", res.Combined())
	}
	return nil
}

// InstallCommand 返回可复制执行的等价命令行（供界面展示）。
func InstallCommand(tools platform.Tools, packages []string) string {
	return tools.Sdkmanager + " --sdk_root=" + tools.SdkRoot + " " + strings.Join(packages, " ")
}

// ListPackages 调用官方 sdkmanager 列出包；installedOnly 为 true 时不访问网络。
func ListPackages(ctx context.Context, tools platform.Tools, env []string, installedOnly bool, onLine LineFunc) ([]Package, error) {
	args := []string{"--list_installed"}
	timeout := listTimeout
	if !installedOnly {
		args = []string{"--list"}
	}
	res, err := runSdkmanager(ctx, tools, env, args, timeout, onLine, nil)
	if err != nil {
		return nil, err
	}
	pkgs := ParsePackages(res.Stdout + "\n" + res.Stderr)
	if len(pkgs) == 0 {
		return nil, domain.ErrDetail(domain.CodeProcessFailed,
			"sdkmanager 未返回任何包信息", firstNonEmptyLine(res.Combined()))
	}
	for i := range pkgs {
		if installedOnly {
			pkgs[i].Installed = true
		}
	}
	return pkgs, nil
}

// ListImages 返回系统镜像包（installedOnly 为 true 时只查本地）。
func ListImages(ctx context.Context, tools platform.Tools, env []string, installedOnly bool, onLine LineFunc) ([]domain.SystemImage, error) {
	pkgs, err := ListPackages(ctx, tools, env, installedOnly, onLine)
	if err != nil {
		return nil, err
	}
	return domainsFromPackages(pkgs), nil
}

// ParsePackages 解析 `sdkmanager --list` / `--list_installed` 的表格输出。
//
// 只依赖「含 | 的行，第一列是包路径」这一约定：
//
//	Installed packages:
//	  Path                | Version | Description          | Location
//	  system-images;android-34;google_apis;x86_64 | 14 | Google APIs … | system-images\…
func ParsePackages(out string) []Package {
	var (
		// 只有出现在 "Installed packages:" 表头之后的表格才算本地已安装。
		installed = false
		seen      = map[string]int{}
		list      []Package
	)
	for _, raw := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case strings.EqualFold(trimmed, "Installed packages:"):
			installed = true
			continue
		case strings.EqualFold(trimmed, "Available Packages:"):
			installed = false
			continue
		}
		if idx := strings.Index(line, "|"); idx <= 0 {
			continue
		}
		fields := strings.Split(line, "|")
		path := strings.TrimSpace(fields[0])
		if path == "" || path == "Path" || strings.HasPrefix(path, "---") {
			continue
		}
		item := Package{Path: path, Installed: installed}
		if len(fields) > 1 {
			item.Version = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			item.Description = strings.TrimSpace(fields[2])
		}
		if prev, ok := seen[path]; ok {
			// 同一个包同时出现在两个区块时去重：以「已安装」状态为准，缺失的版本号用另一处补全。
			if item.Installed && !list[prev].Installed {
				list[prev].Installed = true
			}
			if list[prev].Version == "" {
				list[prev].Version = item.Version
			}
			continue
		}
		seen[path] = len(list)
		list = append(list, item)
	}
	return list
}

// domainsFromPackages 把包列表转换为系统镜像列表。
func domainsFromPackages(pkgs []Package) []domain.SystemImage {
	var out []domain.SystemImage
	for _, p := range pkgs {
		if !strings.HasPrefix(p.Path, "system-images;") {
			continue
		}
		api, tag, abi, err := SplitImage(p.Path)
		if err != nil {
			continue
		}
		out = append(out, domain.SystemImage{
			Path:        p.Path,
			API:         api,
			Tag:         tag,
			ABI:         abi,
			Version:     p.Version,
			Description: p.Description,
			Installed:   p.Installed,
		})
	}
	return out
}

// SplitImage 拆解系统镜像包路径：
// system-images;android-34;google_apis;x86_64 → ("34", "google_apis", "x86_64")。
func SplitImage(pkgPath string) (api, tag, abi string, err error) {
	parts := strings.Split(pkgPath, ";")
	if len(parts) < 4 || parts[0] != "system-images" {
		return "", "", "", domain.ErrDetail(domain.CodeInvalidArgument,
			"系统镜像路径格式不正确", "期望 system-images;<api>;<tag>;<abi>，实际: "+pkgPath)
	}
	return strings.TrimPrefix(parts[1], "android-"), parts[2], parts[3], nil
}

// ImageDir 返回系统镜像相对 SDK 根目录的目录名（保留 android- 前缀）。
func ImageDir(pkgPath string) (string, error) {
	parts := strings.Split(pkgPath, ";")
	if len(parts) < 4 || parts[0] != "system-images" {
		return "", domain.ErrDetail(domain.CodeInvalidArgument,
			"系统镜像路径格式不正确", "期望 system-images;<api>;<tag>;<abi>，实际: "+pkgPath)
	}
	return filepath.Join(parts[0], parts[1], parts[2], parts[3]), nil
}

// TagLabel 返回系统镜像 tag 的展示名（仅用于界面显示）。
func TagLabel(tag string) string {
	switch tag {
	case "google_apis":
		return "Google APIs"
	case "google_apis_playstore":
		return "Google Play"
	case "default":
		return "AOSP"
	case "aosp_atd":
		return "AOSP ATD"
	case "google_atd":
		return "Google APIs ATD"
	case "android-desktop":
		return "Desktop"
	case "google_apis_ps16k":
		return "Google APIs 16K"
	default:
		return tag
	}
}

// ToolVersion 运行外部工具并返回版本文本（失败返回空串，不阻塞环境检查）。
//
// 注意：`java -version` 把版本写到 stderr，因此这里用合并输出。
func ToolVersion(ctx context.Context, exe string, args []string, env []string, timeout time.Duration) string {
	if exe == "" {
		return ""
	}
	res, err := proc.Run(ctx, exe, args, proc.Options{Env: env, Timeout: timeout})
	if err != nil && res.ExitCode != 0 {
		return ""
	}
	return firstNonEmptyLine(res.Combined())
}

// runSdkmanager 调用软件自带的 sdkmanager。
func runSdkmanager(ctx context.Context, tools platform.Tools, env []string, args []string, timeout time.Duration, onLine LineFunc, stdin []string) (proc.Result, error) {
	if !tools.HasSdkmanager() {
		return proc.Result{}, domain.ErrDetail(domain.CodeToolMissing,
			"未找到软件自带的 sdkmanager", tools.Sdkmanager)
	}
	return proc.Run(ctx, tools.Sdkmanager, args, proc.Options{
		Env:        env,
		Timeout:    timeout,
		StdinLines: stdin,
		OnLine:     onLine,
	})
}

func yesLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "y"
	}
	return out
}

func firstNonEmptyLine(s string) string {
	for _, line := range platform.SplitLines(s) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func scriptName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".bat"
	}
	return name
}
