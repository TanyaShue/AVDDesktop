// Package sdk 只负责三件事：
//
//  1. 把官方命令行工具放进软件自己的 SDK 目录（首次运行时下载并解压 cmdline-tools）
//  2. 调用官方 sdkmanager 安装 / 查询组件（system-images、platform-tools、emulator …）
//  3. 解析必要的命令输出（包列表、系统镜像路径）
//
// 组件安装、包依赖、下载调度与许可校验仍交给官方工具；这里只解析仓库索引，
// 用于选择镜像上的 cmdline-tools 归档和校验当前镜像是否包含所需资源。
package sdk

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"AVDDesktop/internal/archive"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// officialRepoBaseURL 是官方 Android SDK 仓库根地址；镜像不可用时可回退到这里。
const officialRepoBaseURL = "https://dl.google.com/android/repository/"

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

// Bootstrap 下载并解压命令行工具到 <sdk>/cmdline-tools/latest。
//
// baseURL 可以是官方仓库或任意完整镜像站；先读取 repository2-3.xml，再按索引中的
// 当前平台归档 URL 与 SHA-1 下载，因此镜像站更新命令行工具后无需同步修改客户端常量。
// 解压先落到 .staging 目录，成功后整体替换 latest（失败时回滚旧目录），避免留下半成品。
func Bootstrap(ctx context.Context, tools platform.Tools, cacheDir, baseURL string, onProgress ProgressFunc) error {
	// 下载与解压共用同一个整体超时，避免网络挂死导致无限等待。
	ctx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer cancel()

	base, err := NormalizeRepositoryBase(baseURL)
	if err != nil {
		return err
	}
	archiveURL, archiveName, sha1sum, err := cmdlineToolsArchiveFromRepository(ctx, base)
	if err != nil {
		return err
	}
	if err := platform.EnsureDir(tools.SdkRoot); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建 SDK 目录: "+tools.SdkRoot, err)
	}
	if err := platform.EnsureDir(cacheDir); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建缓存目录: "+cacheDir, err)
	}

	zipPath := filepath.Join(cacheDir, archiveName)
	if err := archive.Download(ctx, archiveURL, zipPath, sha1sum, onProgress); err != nil {
		return err
	}

	staging := filepath.Join(filepath.Dir(tools.CmdlineTools), ".staging")
	_ = os.RemoveAll(staging)
	if err := archive.ExtractZip(ctx, zipPath, staging); err != nil {
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
	_ = os.RemoveAll(staging)

	// 官方 zip 的条目不一定带 Unix 模式位，非 Windows 平台需显式补上可执行权限。
	if runtime.GOOS != "windows" {
		for _, name := range []string{"sdkmanager", "avdmanager"} {
			_ = os.Chmod(filepath.Join(tools.CmdlineTools, "bin", name), 0o755)
		}
	}
	if err := VerifyPackage(tools, "cmdline-tools;latest"); err != nil {
		_ = os.RemoveAll(tools.CmdlineTools)
		if hadOld {
			_ = os.Rename(old, tools.CmdlineTools)
		}
		return domain.ErrDetail(domain.CodeArchiveFailed,
			"命令行工具安装后校验失败", err.Error())
	}
	_ = os.RemoveAll(old)
	_ = os.Remove(zipPath)
	return nil
}

// cmdlineToolsArchiveFromRepository 从镜像索引解析当前平台命令行工具归档。
// 官方源索引暂时不可用时才使用内置的固定归档作为最后兜底。
func cmdlineToolsArchiveFromRepository(ctx context.Context, base string) (archiveURL, name, sha1sum string, err error) {
	client := &http.Client{Timeout: 90 * time.Second}
	idx, fetchErr := FetchRepository(ctx, base, client)
	if fetchErr == nil {
		item, matchErr := idx.ArchiveFor("cmdline-tools;latest", runtime.GOOS, runtime.GOARCH)
		if matchErr != nil {
			return "", "", "", matchErr
		}
		if strings.TrimSpace(item.SHA1) == "" {
			return "", "", "", domain.Err(domain.CodeProcessFailed, "镜像索引中的命令行工具缺少 SHA-1 校验值")
		}
		name = filepath.Base(item.URL)
		if name == "" || name == "." || name == "/" {
			return "", "", "", domain.Err(domain.CodeProcessFailed, "镜像索引中的命令行工具地址无效")
		}
		return item.URL, name, item.SHA1, nil
	}
	if base != officialRepoBaseURL {
		return "", "", "", fetchErr
	}
	name, sha1sum, err = CmdlineToolsArchive()
	if err != nil {
		return "", "", "", err
	}
	return base + name, name, sha1sum, nil
}

// WithBaseURL 返回一份设置了 SDK_TEST_BASE_URL 的子进程环境。
//
// sdkmanager 会用它覆盖 Android SDK 仓库根地址；其它环境变量保持不变。
func WithBaseURL(env []string, baseURL string) []string {
	base, err := NormalizeRepositoryBase(baseURL)
	if err != nil {
		return append([]string(nil), env...)
	}
	out := append([]string(nil), env...)
	const key = "SDK_TEST_BASE_URL"
	for i, kv := range out {
		if len(kv) >= len(key)+1 && strings.EqualFold(kv[:len(key)+1], key+"=") {
			out[i] = key + "=" + base
			return out
		}
	}
	return append(out, key+"="+base)
}

// AcceptLicenses 通过官方 sdkmanager 接受 SDK 许可（预置 y 回答，避免交互阻塞）。
func AcceptLicenses(ctx context.Context, tools platform.Tools, env []string, onLine LineFunc) error {
	res, err := runSdkmanager(ctx, tools, env, []string{"--licenses"}, licenseTimeout, onLine, yesLines(licenseFeed))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		// 新版 sdkmanager 可能在许可已经写入后因遥测上传失败返回非零退出。
		if entries, readErr := os.ReadDir(tools.Licenses); readErr == nil && len(entries) > 0 {
			if onLine != nil {
				onLine("stderr", "sdkmanager 许可命令返回非零退出码，但许可文件已经存在，按成功处理")
			}
			return nil
		}
		return domain.ErrDetail(domain.CodeProcessFailed,
			"sdkmanager 接受许可失败", res.Combined())
	}
	return nil
}

// InstallPackages 通过官方 sdkmanager 安装组件，并自动替换不完整的旧残留。
func InstallPackages(ctx context.Context, tools platform.Tools, env []string, packages []string, onLine LineFunc) error {
	return installPackages(ctx, tools, env, packages, false, onLine)
}

// RepairPackages 强制替换并重新安装指定组件，用于设置页的一键修复。
func RepairPackages(ctx context.Context, tools platform.Tools, env []string, packages []string, onLine LineFunc) error {
	return installPackages(ctx, tools, env, packages, true, onLine)
}

func installPackages(ctx context.Context, tools platform.Tools, env []string, packages []string, force bool, onLine LineFunc) error {
	staged, err := stagePackages(tools, packages, force)
	if err != nil {
		return err
	}
	for _, item := range staged {
		if onLine != nil {
			onLine("stdout", "检测到不完整或需要修复的组件，已暂存旧目录: "+item.Original)
		}
	}

	// 必须加 --verbose：默认情况下 sdkmanager 安装过程一言不发（实测连 TTY 下也没有
	// 进度条），任务日志会长时间空着；加了之后至少能看到 Preparing/Installing/complete 阶段。
	// 新版 Windows sdkmanager.bat 会把包路径分号当成参数分隔符；统一用 Android CLI
	// 支持的斜杠形式传递，避免 system-images;android-X;tag;abi 被拆成多个包名。
	args := append([]string{"--verbose", "--sdk_root=" + tools.SdkRoot}, cliPackageArgs(packages)...)
	res, err := runSdkmanager(ctx, tools, env, args, installTimeout, onLine, yesLines(licenseFeed))
	if err != nil {
		restoreStaged(staged)
		return err
	}

	// 新版 sdkmanager 可能在安装完成后因遥测上传失败返回非零退出码。
	// 最终以组件是否完整落盘为准；退出码非零但校验通过时记录说明并按成功处理。
	verifyErr := VerifyPackages(tools, packages)
	if verifyErr != nil {
		restoreStaged(staged)
		if res.ExitCode != 0 {
			return domain.ErrDetail(domain.CodeProcessFailed,
				"sdkmanager 安装失败且组件不完整", res.Combined()+"\n"+verifyErr.Error())
		}
		return domain.ErrDetail(domain.CodeProcessFailed,
			"sdkmanager 返回成功但组件完整性校验失败", verifyErr.Error())
	}
	discardStaged(staged)
	if res.ExitCode != 0 && onLine != nil {
		onLine("stderr", "sdkmanager 返回退出码 "+itoa(res.ExitCode)+
			"，但目标组件已完整落盘并通过校验；这通常是遥测或版本提示导致的非零退出，按成功处理")
	} else if len(staged) > 0 && onLine != nil {
		onLine("stdout", "旧版本残留已清理，组件修复完成")
	}
	return nil
}

type stagedPackage struct {
	Path     string
	Original string
	Backup   string
}

func stagePackages(tools platform.Tools, packages []string, force bool) ([]stagedPackage, error) {
	seen := map[string]bool{}
	var staged []stagedPackage
	for _, pkgPath := range packages {
		pkgPath = strings.TrimSpace(pkgPath)
		if pkgPath == "" || seen[pkgPath] {
			continue
		}
		seen[pkgPath] = true
		dir, err := PackageDirectory(tools, pkgPath)
		if err != nil {
			restoreStaged(staged)
			return nil, err
		}
		backup := dir + ".repair-old"
		if !platform.DirExists(dir) {
			// 上次修复可能被取消或异常退出：保留旧目录作为回滚点，
			// 本次成功后删除，失败时恢复。
			if platform.DirExists(backup) {
				staged = append(staged, stagedPackage{Path: pkgPath, Original: dir, Backup: backup})
			}
			continue
		}
		if !force && VerifyPackage(tools, pkgPath) == nil {
			continue
		}
		_ = os.RemoveAll(backup)
		if err := os.Rename(dir, backup); err != nil {
			restoreStaged(staged)
			return nil, domain.Wrap(domain.CodePermissionDenied,
				"无法暂存旧组件（请先关闭正在运行的模拟器）", err)
		}
		staged = append(staged, stagedPackage{Path: pkgPath, Original: dir, Backup: backup})
	}
	return staged, nil
}

func restoreStaged(staged []stagedPackage) {
	for i := len(staged) - 1; i >= 0; i-- {
		item := staged[i]
		_ = os.RemoveAll(item.Original)
		_ = os.Rename(item.Backup, item.Original)
	}
}

func discardStaged(staged []stagedPackage) {
	for _, item := range staged {
		_ = os.RemoveAll(item.Backup)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// InstallCommand 返回可复制执行的等价命令行（供界面展示）。
func InstallCommand(tools platform.Tools, packages []string) string {
	return tools.Sdkmanager + " --sdk_root=" + tools.SdkRoot + " " + strings.Join(cliPackageArgs(packages), " ")
}

// cliPackageArgs 把 sdkmanager 的 SDK 路径转换为 Android CLI 的斜杠形式。
//
// 新版 Windows sdkmanager.bat 用 cmd.exe 批处理转发参数，分号会被当成 token
// 分隔符，导致 system-images;android-36.1;google_apis;arm64-v8a 变成四个包名。
func cliPackageArgs(packages []string) []string {
	out := make([]string, 0, len(packages))
	for _, pkgPath := range packages {
		pkgPath = strings.TrimSpace(pkgPath)
		if pkgPath == "" {
			continue
		}
		out = append(out, strings.ReplaceAll(pkgPath, ";", "/"))
	}
	return out
}

// ListPackages 调用官方 sdkmanager 列出包；installedOnly 为 true 时只扫描本地目录。
func ListPackages(ctx context.Context, tools platform.Tools, env []string, installedOnly bool, onLine LineFunc) ([]Package, error) {
	if installedOnly {
		return ScanInstalledPackages(tools), nil
	}
	res, err := runSdkmanager(ctx, tools, env, []string{"--list"}, listTimeout, onLine, nil)
	if err != nil {
		return nil, err
	}
	pkgs := ParsePackages(res.Stdout + "\n" + res.Stderr)
	if len(pkgs) == 0 {
		return nil, domain.ErrDetail(domain.CodeProcessFailed,
			"sdkmanager 未返回任何包信息", firstNonEmptyLine(res.Combined()))
	}
	return pkgs, nil
}

// ListImages 返回系统镜像包（installedOnly 为 true 时只扫描本地目录）。
func ListImages(ctx context.Context, tools platform.Tools, env []string, installedOnly bool, onLine LineFunc) ([]domain.SystemImage, error) {
	pkgs, err := ListPackages(ctx, tools, env, installedOnly, onLine)
	if err != nil {
		return nil, err
	}
	return domainsFromPackages(pkgs), nil
}

// ParsePackages 同时兼容旧版 sdkmanager 的 `|` 表格和新版 Android CLI 的空格表格。
func ParsePackages(out string) []Package {
	var (
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
		case strings.EqualFold(trimmed, "Available packages:"):
			installed = false
			continue
		}
		item, ok := parsePackageLine(line, installed)
		if !ok {
			continue
		}
		item.Path = canonicalPackagePath(item.Path)
		if prev, ok := seen[item.Path]; ok {
			// 同一个包同时出现在两个区块时去重：以「已安装」状态为准，并补全缺失字段。
			if item.Installed && !list[prev].Installed {
				list[prev].Installed = true
			}
			if list[prev].Version == "" {
				list[prev].Version = item.Version
			}
			if list[prev].Description == "" {
				list[prev].Description = item.Description
			}
			continue
		}
		seen[item.Path] = len(list)
		list = append(list, item)
	}
	return list
}

func parsePackageLine(line string, installed bool) (Package, bool) {
	if idx := strings.Index(line, "|"); idx > 0 {
		fields := strings.Split(line, "|")
		path := strings.TrimSpace(fields[0])
		if path == "" || path == "Path" || strings.HasPrefix(path, "---") {
			return Package{}, false
		}
		item := Package{Path: path, Installed: installed}
		if len(fields) > 1 {
			item.Version = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			item.Description = strings.TrimSpace(fields[2])
		}
		return item, true
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(line, "  ") {
		return Package{}, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 2 || !looksLikePackagePath(fields[0]) {
		return Package{}, false
	}
	item := Package{Path: fields[0], Installed: installed}
	rest := strings.TrimSpace(trimmed[len(fields[0]):])
	if arrow := strings.Index(rest, "->"); arrow >= 0 {
		rest = strings.TrimSpace(rest[arrow+2:])
	}
	restFields := strings.Fields(rest)
	if len(restFields) == 0 {
		return Package{}, false
	}
	item.Version = restFields[0]
	if len(restFields) > 1 {
		item.Description = strings.Join(restFields[1:], " ")
	}
	return item, true
}

func looksLikePackagePath(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t") || strings.HasPrefix(value, "---") || value == "Path" {
		return false
	}
	if strings.Contains(value, "/") || strings.Contains(value, ";") {
		return true
	}
	switch value {
	case "platform-tools", "emulator", "tools":
		return true
	default:
		return false
	}
}

func canonicalPackagePath(path string) string {
	path = strings.TrimSpace(path)
	if strings.Contains(path, ";") || !strings.Contains(path, "/") {
		return path
	}
	return strings.Join(strings.Split(path, "/"), ";")
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
			Path:           p.Path,
			API:            api,
			Tag:            tag,
			ABI:            abi,
			Version:        p.Version,
			Description:    p.Description,
			AndroidVersion: AndroidVersion(api),
			RootSupported:  rootSupported(tag),
			Installed:      p.Installed,
		})
	}
	return out
}

// AndroidVersion 把 API 等级转换为界面可读的 Android 版本名。
//
// API 带有小数扩展（例如 android-36.1）时仍按主版本展示，避免把构建版本号误当成 Android 版本。
func AndroidVersion(api string) string {
	major := atoiDigits(api)
	switch major {
	case 19:
		return "Android 4.4"
	case 20:
		return "Android 4.4W"
	case 21:
		return "Android 5.0"
	case 22:
		return "Android 5.1"
	case 23:
		return "Android 6.0"
	case 24:
		return "Android 7.0"
	case 25:
		return "Android 7.1"
	case 26:
		return "Android 8.0"
	case 27:
		return "Android 8.1"
	case 28:
		return "Android 9"
	case 29:
		return "Android 10"
	case 30:
		return "Android 11"
	case 31:
		return "Android 12"
	case 32:
		return "Android 12L"
	case 33:
		return "Android 13"
	case 34:
		return "Android 14"
	case 35:
		return "Android 15"
	case 36:
		return "Android 16"
	case 37:
		return "Android 17"
	default:
		if api == "" {
			return "Android 版本未知"
		}
		return "Android API " + api
	}
}

// rootSupported 根据官方镜像 tag 判断是否支持 root。
//
// Google Play 系统镜像使用锁定 bootloader 的发布构建，`adb root` 不可用；
// Google APIs / AOSP / ATD / Desktop 等开发镜像默认可 root。
func rootSupported(tag string) bool {
	return !strings.Contains(strings.ToLower(tag), "playstore")
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

// ToolVersion 运行外部工具并返回首个非空输出行（失败返回空串，不阻塞环境检查）。
//
// 注意：`java -version` 把版本写到 stderr，因此这里用合并输出。
func ToolVersion(ctx context.Context, exe string, args []string, env []string, timeout time.Duration) string {
	return firstNonEmptyLine(ToolOutput(ctx, exe, args, env, timeout))
}

// ToolOutput 运行外部工具并返回完整的合并输出（失败返回空串）。
//
// 部分工具会在版本行之前输出 INFO/警告，调用方需要从全文里定位版本信息。
func ToolOutput(ctx context.Context, exe string, args []string, env []string, timeout time.Duration) string {
	if exe == "" {
		return ""
	}
	res, err := proc.Run(ctx, exe, args, proc.Options{Env: env, Timeout: timeout})
	if err != nil && res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(res.Combined())
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
