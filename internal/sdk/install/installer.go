// Package install 是包安装编排：解析依赖 → 下载 → 校验 → 解压 → 落盘 → 写许可。
//
// 设计（见 ARCHITECTURE.md ADR-01/§6.3）：不依赖 sdkmanager，直接用仓库索引 +
// 自研下载器，原因是可以整站换源（镜像）、不依赖 JDK、能做断点续传与精确进度。
package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"AVDDesktop/internal/archive"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/download"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk/licenses"
	"AVDDesktop/internal/sdk/query"
	"AVDDesktop/internal/sdk/repo"
)

// Options 控制一次安装。
type Options struct {
	SourceID                string
	AllowFallbackToOfficial bool
	AutoAcceptLicenses      bool
	Concurrency             int
	Timeout                 time.Duration
	ProxyURL                string
	SpeedLimitKBps          int
	DownloadDir             string
	SkipDependencies        bool
}

// Installer 执行安装。
type Installer struct {
	Paths        platform.InstallPaths
	Fetcher      *repo.Fetcher
	OfficialBase string // 回退用的官方源（默认 dl.google.com）
}

// New 创建安装器。
func New(paths platform.InstallPaths, cacheDir string) *Installer {
	return &Installer{
		Paths:        paths,
		Fetcher:      repo.NewFetcher(cacheDir, 90*time.Second),
		OfficialBase: "https://dl.google.com/android/repository/",
	}
}

// PackageDir 把包路径映射为 SDK 下的相对目录。
//
// 规则：把 ';' 换成 '/'（实测映射见 ARCHITECTURE.md §6.3）。
func PackageDir(pkgPath string) string {
	return filepath.Join(strings.Split(pkgPath, ";")...)
}

// ResolveIndex 获取仓库索引（带缓存）。
func (in *Installer) ResolveIndex(ctx context.Context, source domain.MirrorSource, force bool) (*repo.Index, error) {
	base, err := normalizeBase(source.BaseURL)
	if err != nil {
		return nil, err
	}
	return in.Fetcher.GetIndex(ctx, cacheKey(source.ID), base+repo.IndexRepository, force)
}

// ResolveSysImgIndex 获取指定 tag 的系统镜像索引。
func (in *Installer) ResolveSysImgIndex(ctx context.Context, source domain.MirrorSource, tag string, force bool) (*repo.Index, error) {
	base, err := normalizeBase(source.BaseURL)
	if err != nil {
		return nil, err
	}
	rel := fmt.Sprintf(repo.IndexSysImgTmpl, tag)
	return in.Fetcher.GetIndex(ctx, cacheKey(source.ID)+"-sysimg-"+tag, base+rel, force)
}

// Plan 生成安装计划（展示依赖、总大小、需接受的许可）。
func (in *Installer) Plan(ctx context.Context, source domain.MirrorSource, packages []string) (*domain.InstallPlan, error) {
	idx, err := in.ResolveIndex(ctx, source, false)
	if err != nil {
		return nil, err
	}
	scanner := query.NewScanner(in.Paths.SdkRoot)
	installed := map[string]string{}
	for _, p := range scanner.Installed() {
		installed[p.Path] = p.InstalledRevision
	}

	order, err := resolveDependencies(idx, packages)
	if err != nil {
		return nil, err
	}

	plan := &domain.InstallPlan{}
	licenseSet := map[string]bool{}
	for _, pkgPath := range order {
		pkg, ok := idx.Find(pkgPath)
		if !ok {
			return nil, domain.ErrDetail(domain.CodeInvalidArgument,
				"镜像索引中没有该包", pkgPath)
		}
		arch, err := pkg.PickArchive(runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return nil, err
		}
		step := domain.PlanStep{
			Path:      pkgPath,
			Action:    "install",
			SizeBytes: arch.Size,
			SourceURL: idx.PackageURL(arch),
		}
		if cur, ok := installed[pkgPath]; ok {
			if platform.VersionAtLeast(cur, pkg.Revision.String()) {
				step.Action = "skip"
				step.Reason = "已安装版本 " + cur
			} else {
				step.Action = "update"
				step.Reason = "当前 " + cur + " → " + pkg.Revision.String()
			}
		}
		if step.Action != "skip" {
			plan.TotalBytes += arch.Size
		}
		plan.Steps = append(plan.Steps, step)
		if ref := pkg.LicenseID(); ref != "" {
			licenseSet[ref] = true
		}
	}

	for id := range licenseSet {
		text := ""
		if idx != nil {
			if l, ok := idx.Licenses[id]; ok {
				text = l.Text
			}
		}
		plan.Licenses = append(plan.Licenses, domain.License{
			ID:       id,
			Text:     text,
			Accepted: licenses.IsAccepted(in.Paths.Licenses, id),
		})
	}
	if len(plan.Steps) == 0 {
		plan.Warnings = append(plan.Warnings, "没有需要安装的包")
	}
	return plan, nil
}

// InstallResult 是安装结果。
type InstallResult struct {
	Installed []string
	Skipped   []string
	Failed    []string
}

// Install 执行安装计划。
//
// job 可为 nil（同步调用，无进度事件）。ctx 取消会中断下载与解压。
func (in *Installer) Install(ctx context.Context, source domain.MirrorSource, packages []string, opts Options, j *job.Job) (*InstallResult, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Minute
	}
	if opts.DownloadDir == "" {
		opts.DownloadDir = filepath.Join(os.TempDir(), "avddesktop-downloads")
	}
	_ = os.MkdirAll(opts.DownloadDir, 0o755)

	idx, err := in.ResolveIndex(ctx, source, false)
	if err != nil {
		return nil, err
	}
	order, err := resolveDependencies(idx, packages)
	if err != nil {
		return nil, err
	}

	result := &InstallResult{}
	scanner := query.NewScanner(in.Paths.SdkRoot)

	if j != nil {
		j.SetItems(0, len(order))
		j.Logf("info", "install", "共 %d 个包待处理（源：%s）", len(order), source.Name)
	}

	for i, pkgPath := range order {
		if err := ctx.Err(); err != nil {
			return result, domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		pkg, ok := idx.Find(pkgPath)
		if !ok {
			result.Failed = append(result.Failed, pkgPath)
			continue
		}
		if j != nil {
			j.SetItems(i, len(order))
			j.SetPhase("正在安装 " + displayName(pkg))
		}
		if cur, ok := scanner.Find(pkgPath); ok {
			if platform.VersionAtLeast(cur.InstalledRevision, pkg.Revision.String()) {
				result.Skipped = append(result.Skipped, pkgPath)
				if j != nil {
					j.Logf("info", "install", "跳过 %s（已安装 %s）", pkgPath, cur.InstalledRevision)
				}
				continue
			}
		}

		if err := in.installOne(ctx, source, idx, pkg, opts, j); err != nil {
			result.Failed = append(result.Failed, pkgPath)
			if j != nil {
				j.Logf("error", "install", "安装 %s 失败：%v", pkgPath, err)
			}
			// 官方回退：镜像文件缺失/校验失败时尝试官方源
			if opts.AllowFallbackToOfficial && isMirrorProblem(err) {
				if j != nil {
					j.Logf("warn", "install", "切换到 Google 官方源重试 %s", pkgPath)
				}
				official := domain.MirrorSource{ID: "google-official", Name: "Google 官方",
					BaseURL: in.OfficialBase, Kind: domain.MirrorOfficial}
				offIdx, oErr := in.ResolveIndex(ctx, official, false)
				if oErr == nil {
					if offPkg, ok := offIdx.Find(pkgPath); ok {
						if err := in.installOne(ctx, official, offIdx, offPkg, opts, j); err == nil {
							result.Installed = append(result.Installed, pkgPath)
							continue
						}
					}
				}
			}
			return result, err
		}
		result.Installed = append(result.Installed, pkgPath)
	}

	if j != nil {
		j.SetItems(len(order), len(order))
	}
	return result, nil
}

// installOne 安装单个包（下载 → 校验 → 解压 → 原子替换）。
func (in *Installer) installOne(ctx context.Context, source domain.MirrorSource, idx *repo.Index, pkg repo.Package, opts Options, j *job.Job) error {
	arch, err := pkg.PickArchive(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	url := idx.PackageURL(arch)

	// 许可检查（无 JDK 场景必须由我们写授权文件）
	if ref := pkg.LicenseID(); ref != "" && !licenses.IsAccepted(in.Paths.Licenses, ref) {
		if !opts.AutoAcceptLicenses {
			return domain.ErrDetail(domain.CodeLicenseNotAccepted,
				"需要先接受许可协议："+licenses.DisplayName(ref), ref).
				WithAction("acceptLicense", "查看并接受许可", ref)
		}
		if err := licenses.Accept(in.Paths.Licenses, ref); err != nil {
			return err
		}
	}

	fileName := filepath.Base(arch.URL)
	localPath := filepath.Join(opts.DownloadDir, fileName)

	if j != nil {
		j.Logf("info", "download", "下载 %s（%s）", fileName, platform.HumanSize(arch.Size))
	}
	_, err = download.Fetch(ctx, url, download.Options{
		Dest:        localPath,
		Concurrency: opts.Concurrency,
		Timeout:     0, // 由 ctx 控制
		ProxyURL:    opts.ProxyURL,
		Resume:      true,
		SpeedLimit:  opts.SpeedLimitKBps,
		OnProgress: func(p download.Progress) {
			if j == nil {
				return
			}
			j.SetBytes(p.Done, p.Total, p.SpeedBps)
		},
	})
	if err != nil {
		return err
	}
	if err := archive.VerifySHA1(localPath, arch.SHA1); err != nil {
		_ = os.Remove(localPath)
		return err
	}

	targetDir := filepath.Join(in.Paths.SdkRoot, PackageDir(pkg.Path))
	tmpDir := targetDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建目标目录", err)
	}
	if j != nil {
		j.SetPhase("解压 " + displayName(pkg))
	}
	if err := archive.ExtractZip(localPath, tmpDir, func(done, total int64, current string) error {
		if err := ctx.Err(); err != nil {
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		if j != nil && total > 0 {
			j.SetPercent(float64(done) / float64(total) * 100)
		}
		return nil
	}); err != nil {
		_ = os.RemoveAll(tmpDir)
		return err
	}

	// cmdline-tools 特例：压缩包内层是 cmdline-tools/，需要提升一层
	if err := flattenSingleDir(tmpDir, "cmdline-tools"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return err
	}

	if err := platform.EnsureDir(filepath.Dir(targetDir)); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法创建包目录", err)
	}
	if err := archive.RenameAtomic(tmpDir, targetDir); err != nil {
		return err
	}
	if j != nil {
		j.Logf("info", "install", "%s 安装完成（%s）", pkg.Path, pkg.Revision.String())
	}
	return nil
}

// BootstrapResult 是引导安装结果。
type BootstrapResult struct {
	CmdlineToolsDir string
	Installed       []string
}

// Bootstrap 在没有 cmdline-tools 时安装它（以及可选的 platform-tools / emulator）。
//
// 这是"零基础用户"的第一步：只要网络可用，不依赖任何已安装组件。
func (in *Installer) Bootstrap(ctx context.Context, source domain.MirrorSource, withPlatformTools, withEmulator bool, opts Options, j *job.Job) (*BootstrapResult, error) {
	packages := []string{"cmdline-tools;latest"}
	if withPlatformTools {
		packages = append(packages, "platform-tools")
	}
	if withEmulator {
		packages = append(packages, "emulator")
	}
	res, err := in.Install(ctx, source, packages, opts, j)
	out := &BootstrapResult{CmdlineToolsDir: in.Paths.CmdlineTools}
	if res != nil {
		out.Installed = res.Installed
	}
	if err != nil {
		return out, err
	}
	if !platform.FileExists(in.Paths.Sdkmanager) {
		// 压缩包内层结构可能不同，给出明确诊断而不是静默失败
		return out, domain.ErrDetail(domain.CodeArchiveFailed,
			"命令行工具安装后未找到 sdkmanager",
			"期望路径: "+in.Paths.Sdkmanager).
			WithHint("请检查防病毒软件是否拦截了解压过程")
	}
	return out, nil
}

// ---------------------------------------------------------------- 依赖解析

// resolveDependencies 做依赖拓扑排序（被依赖的包先安装）。
func resolveDependencies(idx *repo.Index, packages []string) ([]string, error) {
	var order []string
	state := map[string]int{} // 0=未访问 1=访问中 2=完成
	var visit func(pkgPath string, depth int) error
	visit = func(pkgPath string, depth int) error {
		if depth > 8 {
			return nil // 防御性截断
		}
		switch state[pkgPath] {
		case 1, 2:
			return nil
		}
		state[pkgPath] = 1
		pkg, ok := idx.Find(pkgPath)
		if !ok {
			return domain.ErrDetail(domain.CodeInvalidArgument,
				"镜像索引中找不到该包（可能是该镜像未同步或路径拼写错误）", pkgPath)
		}
		for _, dep := range pkg.Dependencies {
			depPath := strings.TrimSpace(strings.SplitN(dep.Path, "#", 2)[0])
			if depPath == "" {
				continue
			}
			if _, ok := idx.Find(depPath); !ok {
				continue // 依赖不在索引里（例如平台包），跳过而不是报错
			}
			if err := visit(depPath, depth+1); err != nil {
				return err
			}
		}
		state[pkgPath] = 2
		order = append(order, pkgPath)
		return nil
	}
	for _, p := range packages {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if err := visit(p, 0); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// flattenSingleDir 若 tmpDir 下只有一个目录且名为 name，则把内容上移一层。
func flattenSingleDir(tmpDir, name string) error {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil
	}
	if len(entries) != 1 || !entries[0].IsDir() || !strings.EqualFold(entries[0].Name(), name) {
		return nil
	}
	inner := filepath.Join(tmpDir, entries[0].Name())
	sub, err := os.ReadDir(inner)
	if err != nil {
		return err
	}
	for _, e := range sub {
		from := filepath.Join(inner, e.Name())
		to := filepath.Join(tmpDir, e.Name())
		if err := os.Rename(from, to); err != nil {
			return domain.Wrap(domain.CodeArchiveFailed, "无法整理解压目录", err)
		}
	}
	return os.Remove(inner)
}

func displayName(pkg repo.Package) string {
	if pkg.DisplayName != "" {
		return pkg.DisplayName
	}
	return pkg.Path
}

func normalizeBase(raw string) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		return "", domain.Err(domain.CodeInvalidArgument, "镜像地址为空")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base, nil
}

func cacheKey(sourceID string) string {
	var b strings.Builder
	for _, r := range sourceID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return "index-" + b.String()
}

// isMirrorProblem 判断错误是否适合回退到官方源重试。
func isMirrorProblem(err error) bool {
	ae, ok := err.(*domain.AppError)
	if !ok {
		return false
	}
	switch ae.Code {
	case domain.CodeMirrorUnreachable, domain.CodeDownloadFailed,
		domain.CodeChecksumMismatch, domain.CodeMirrorIndexOnly:
		return true
	default:
		return false
	}
}
