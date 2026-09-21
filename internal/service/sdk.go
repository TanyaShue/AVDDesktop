package service

import (
	"context"
	"runtime"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk/install"
	"AVDDesktop/internal/sdk/licenses"
	"AVDDesktop/internal/sdk/localrepo"
	"AVDDesktop/internal/sdk/query"
	"AVDDesktop/internal/sdk/repo"
)

// SdkService 提供 SDK 包管理：索引浏览、安装计划、安装、许可、引导安装。
type SdkService struct{ rt *Runtime }

// NewSdkService 创建 SdkService。
func NewSdkService(rt *Runtime) *SdkService { return &SdkService{rt: rt} }

// ListRemoteRequest 是远程包列表请求。
type ListRemoteRequest struct {
	Kinds     []string `json:"kinds"`
	Channel   string   `json:"channel"`
	SourceID  string   `json:"sourceId"`
	Refresh   bool     `json:"refresh"`
	SysImgTag string   `json:"sysImgTag"`
}

// ListInstalled 返回本地已安装包。
func (s *SdkService) ListInstalled() []domain.SdkPackage {
	scanner := query.NewScanner(s.rt.Components().Paths.SdkRoot)
	return domain.NonNil(scanner.Installed())
}

// ListRemote 返回远程可安装包（合并已安装状态）。
func (s *SdkService) ListRemote(req ListRemoteRequest) ([]domain.SdkPackage, error) {
	comp := s.rt.Components()
	source := s.sourceOr(req.SourceID)

	var indexes []*repo.Index
	if req.SysImgTag != "" {
		idx, err := comp.Installer.ResolveSysImgIndex(s.rt.Context(), source, req.SysImgTag, req.Refresh)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	} else {
		idx, err := comp.Installer.ResolveIndex(s.rt.Context(), source, req.Refresh)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}

	installed := map[string]string{}
	for _, p := range query.NewScanner(comp.Paths.SdkRoot).Installed() {
		installed[p.Path] = p.InstalledRevision
	}

	var out []domain.SdkPackage
	for _, idx := range indexes {
		for _, pkg := range idx.Packages {
			kind := pkg.Kind()
			if len(req.Kinds) > 0 && !contains(req.Kinds, kind) {
				continue
			}
			arch, err := pkg.PickArchive(runtime.GOOS, runtime.GOARCH)
			if err != nil {
				continue // 该包没有本平台归档，直接隐藏
			}
			cur, isInstalled := installed[pkg.Path]
			out = append(out, domain.SdkPackage{
				Path:              pkg.Path,
				DisplayName:       nonEmpty(pkg.DisplayName, pkg.Path),
				Kind:              kind,
				Revision:          pkg.Revision.String(),
				Channel:           nonEmpty(pkg.ChannelRef.Ref, "stable"),
				SizeBytes:         arch.Size,
				ChecksumSHA1:      arch.SHA1,
				URL:               idx.PackageURL(arch),
				Installed:         isInstalled,
				InstalledRevision: cur,
				HasUpdate:         isInstalled && !platform.VersionAtLeast(cur, pkg.Revision.String()),
				LicenseID:         pkg.LicenseID(),
				Dependencies:      dependencyPaths(pkg),
			})
		}
	}
	return domain.NonNil(out), nil
}

// ListSystemImagesRequest 是系统镜像列表请求。
type ListSystemImagesRequest struct {
	Tags          []string `json:"tags"`
	SourceID      string   `json:"sourceId"`
	Refresh       bool     `json:"refresh"`
	OnlyInstalled bool     `json:"onlyInstalled"`
}

// SystemImageTags 返回可用的系统镜像 tag 及其实测可用性。
type SysImgTagAvailability struct {
	repo.SysImgTag
	Available bool   `json:"available"`
	Count     int    `json:"count"`
	Error     string `json:"error,omitempty"`
}

// SystemImageTags 探测各 tag 在本源上是否可用。
func (s *SdkService) SystemImageTags(req ListRemoteRequest) []SysImgTagAvailability {
	comp := s.rt.Components()
	source := s.sourceOr(req.SourceID)
	out := make([]SysImgTagAvailability, 0, len(repo.KnownSysImgTags))
	for _, tag := range repo.KnownSysImgTags {
		item := SysImgTagAvailability{SysImgTag: tag}
		idx, err := comp.Installer.ResolveSysImgIndex(s.rt.Context(), source, tag.ID, req.Refresh)
		if err != nil {
			item.Error = err.Error()
		} else {
			item.Available = true
			item.Count = len(idx.Packages)
		}
		out = append(out, item)
	}
	return domain.NonNil(out)
}

// ListSystemImages 返回系统镜像包（远程 + 本地状态）。
func (s *SdkService) ListSystemImages(req ListSystemImagesRequest) ([]domain.SystemImage, error) {
	comp := s.rt.Components()
	local := map[string]domain.SystemImage{}
	for _, img := range query.NewScanner(comp.Paths.SdkRoot).SystemImages() {
		local[img.Path] = img
	}
	if req.OnlyInstalled {
		out := make([]domain.SystemImage, 0, len(local))
		for _, img := range local {
			out = append(out, img)
		}
		return out, nil
	}

	source := s.sourceOr(req.SourceID)
	tags := req.Tags
	if len(tags) == 0 {
		tags = []string{"google_apis", "google_apis_playstore", "android-desktop"}
	}

	var out []domain.SystemImage
	var lastErr error
	for _, tag := range tags {
		idx, err := comp.Installer.ResolveSysImgIndex(s.rt.Context(), source, tag, req.Refresh)
		if err != nil {
			lastErr = err
			continue
		}
		for _, pkg := range idx.Packages {
			arch, aErr := pkg.PickArchive(runtime.GOOS, runtime.GOARCH)
			if aErr != nil {
				continue
			}
			img := domain.SystemImage{
				Path:        pkg.Path,
				APILevel:    pkg.APILevel,
				TagID:       nonEmpty(pkg.TagID, tag),
				TagDisplay:  nonEmpty(pkg.TagDisplay, repo.TagDisplay(tag)),
				ABI:         pkg.ABI,
				Vendor:      pkg.VendorDisplay,
				IsPlaystore: strings.Contains(pkg.TagID, "playstore"),
				Revision:    pkg.Revision.String(),
				SizeBytes:   arch.Size,
			}
			if len(pkg.Dependencies) > 0 {
				dep := strings.SplitN(pkg.Dependencies[0].Path, "#", 2)
				if len(dep) == 2 {
					img.RequiresEmulator = dep[1]
				}
			}
			if cur, ok := local[pkg.Path]; ok {
				img.Installed = true
				img.HasUpdate = !platform.VersionAtLeast(cur.Revision, pkg.Revision.String())
			}
			out = append(out, img)
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return domain.NonNil(out), nil
}

// ListLicenses 返回指定包的许可（含是否已接受）。
func (s *SdkService) ListLicenses(packages []string) ([]domain.License, error) {
	comp := s.rt.Components()
	source := s.sourceOr("")
	idx, err := comp.Installer.ResolveIndex(s.rt.Context(), source, false)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []domain.License
	for _, p := range packages {
		pkg, ok := idx.Find(p)
		if !ok {
			continue
		}
		ref := pkg.LicenseID()
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		text := ""
		if l, ok := idx.Licenses[ref]; ok {
			text = l.Text
		}
		out = append(out, domain.License{
			ID:       ref,
			Text:     text,
			Accepted: licenses.IsAccepted(comp.Paths.Licenses, ref),
		})
	}
	return domain.NonNil(out), nil
}

// AcceptLicenses 接受指定许可并记录到设置。
func (s *SdkService) AcceptLicenses(ids []string) error {
	comp := s.rt.Components()
	for _, id := range ids {
		if !licenses.IsKnown(id) {
			return domain.Err(domain.CodeInvalidArgument, "未知许可: "+id)
		}
		if err := licenses.Accept(comp.Paths.Licenses, id); err != nil {
			return err
		}
	}
	settings := s.rt.settings.Get()
	merged := append([]string(nil), settings.AcceptedLicenseIDs...)
	for _, id := range ids {
		if !contains(merged, id) {
			merged = append(merged, id)
		}
	}
	_, err := s.rt.settings.Update(map[string]any{"acceptedLicenseIds": merged})
	return err
}

// PlanInstall 返回安装计划。
func (s *SdkService) PlanInstall(req InstallRequest) (*domain.InstallPlan, error) {
	comp := s.rt.Components()
	return comp.Installer.Plan(s.rt.Context(), s.sourceOr(req.SourceID), req.Packages)
}

// InstallRequest 是安装请求。
type InstallRequest struct {
	Packages                []string `json:"packages"`
	SourceID                string   `json:"sourceId"`
	AllowFallbackToOfficial bool     `json:"allowFallbackToOfficial"`
	AutoAcceptLicenses      bool     `json:"autoAcceptLicenses"`
}

// Install 执行安装，返回 jobID。
func (s *SdkService) Install(req InstallRequest) (string, error) {
	if len(req.Packages) == 0 {
		return "", domain.Err(domain.CodeInvalidArgument, "没有选择任何包")
	}
	comp := s.rt.Components()
	settings := s.rt.settings.Get()
	source := s.sourceOr(req.SourceID)

	// 同一 SDK 根目录同时只允许一个写任务
	unlock, ok := s.rt.locks.TryLock("sdk:" + comp.Paths.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有安装任务正在进行").
			WithHint("请等待当前安装完成，或在任务面板中取消它")
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:       domain.JobInstall,
		Title:      installTitle(req.Packages),
		Subtitle:   "源：" + source.Name,
		ItemsTotal: len(req.Packages),
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		opts := install.Options{
			SourceID:                source.ID,
			AllowFallbackToOfficial: req.AllowFallbackToOfficial || settings.AutoFallbackToOfficial,
			AutoAcceptLicenses:      req.AutoAcceptLicenses || settings.AutoAcceptLicenses,
			Concurrency:             settings.MaxConnectionsPerFile,
			Timeout:                 time.Duration(settings.TimeoutSeconds) * time.Second,
			ProxyURL:                proxyURL(settings.ProxyMode, settings.ProxyURL),
			SpeedLimitKBps:          settings.SpeedLimitKBps,
			DownloadDir:             s.rt.downloadDir(),
		}
		result, err := comp.Installer.Install(ctx, source, req.Packages, opts, j)
		if err != nil {
			return err
		}
		s.rt.detector.Invalidate()
		s.rt.Emit("sdk:changed", map[string]any{
			"installed": result.Installed,
			"skipped":   result.Skipped,
			"failed":    result.Failed,
		})
		return nil
	})
	return j.ID(), nil
}

// BootstrapRequest 是"零基础一键安装"请求。
type BootstrapRequest struct {
	SourceID          string `json:"sourceId"`
	WithPlatformTools bool   `json:"withPlatformTools"`
	WithEmulator      bool   `json:"withEmulator"`
	AcceptLicenses    bool   `json:"acceptLicenses"`
}

// BootstrapCmdlineTools 在没有命令行工具时安装它（以及可选的 adb/emulator）。
func (s *SdkService) BootstrapCmdlineTools(req BootstrapRequest) (string, error) {
	comp := s.rt.Components()
	settings := s.rt.settings.Get()
	source := s.sourceOr(req.SourceID)

	unlock, ok := s.rt.locks.TryLock("sdk:" + comp.Paths.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有安装任务正在进行")
	}
	if err := platform.EnsureDir(comp.Paths.SdkRoot); err != nil {
		unlock()
		return "", domain.Wrap(domain.CodePermissionDenied, "无法创建 SDK 目录", err)
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:     domain.JobBootstrap,
		Title:    "安装 Android 命令行工具",
		Subtitle: "源：" + source.Name,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		opts := install.Options{
			SourceID:                source.ID,
			AllowFallbackToOfficial: settings.AutoFallbackToOfficial,
			AutoAcceptLicenses:      req.AcceptLicenses || settings.AutoAcceptLicenses,
			Concurrency:             settings.MaxConnectionsPerFile,
			ProxyURL:                proxyURL(settings.ProxyMode, settings.ProxyURL),
			SpeedLimitKBps:          settings.SpeedLimitKBps,
			DownloadDir:             s.rt.downloadDir(),
		}
		res, err := comp.Installer.Bootstrap(ctx, source, req.WithPlatformTools, req.WithEmulator, opts, j)
		if err != nil {
			return err
		}
		s.rt.detector.Invalidate()
		s.rt.Emit("sdk:changed", map[string]any{"installed": res.Installed, "bootstrap": true})
		return nil
	})
	return j.ID(), nil
}

// UninstallRequest 是卸载请求。
type UninstallRequest struct {
	Packages []string `json:"packages"`
	DryRun   bool     `json:"dryRun"`
}

// Uninstall 卸载包（删除目录；system-images 会同时清理）。
func (s *SdkService) Uninstall(req UninstallRequest) (string, error) {
	if len(req.Packages) == 0 {
		return "", domain.Err(domain.CodeInvalidArgument, "没有选择任何包")
	}
	comp := s.rt.Components()
	unlock, ok := s.rt.locks.TryLock("sdk:" + comp.Paths.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有安装/卸载任务正在进行")
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:       domain.JobInstall,
		Title:      "卸载 SDK 组件",
		ItemsTotal: len(req.Packages),
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		for i, pkgPath := range req.Packages {
			if err := ctx.Err(); err != nil {
				return domain.Err(domain.CodeJobCanceled, "操作已取消")
			}
			j.SetPhase("正在卸载 " + pkgPath)
			j.SetItems(i, len(req.Packages))
			dir := path_Join(comp.Paths.SdkRoot, install.PackageDir(pkgPath))
			if req.DryRun {
				j.Logf("info", "uninstall", "（预演）将删除 %s", dir)
				continue
			}
			if err := removeAll(dir); err != nil {
				return domain.Wrap(domain.CodePermissionDenied, "无法删除 "+pkgPath, err).
					WithHint("请先停止使用该组件的模拟器实例")
			}
			j.Logf("info", "uninstall", "已删除 %s", pkgPath)
		}
		s.rt.detector.Invalidate()
		s.rt.Emit("sdk:changed", map[string]any{"uninstalled": req.Packages})
		return nil
	})
	return j.ID(), nil
}

// VerifyPackage 校验已安装包的完整性（目录 + source.properties 存在）。
func (s *SdkService) VerifyPackage(pkgPath string) (*domain.VerifyResult, error) {
	comp := s.rt.Components()
	dir := path_Join(comp.Paths.SdkRoot, install.PackageDir(pkgPath))
	out := &domain.VerifyResult{Path: pkgPath}
	if !platform.DirExists(dir) {
		out.Details = append(out.Details, "目录不存在: "+dir)
		return out, nil
	}
	out.OK = true
	if !platform.FileExists(path_Join(dir, "source.properties")) {
		out.OK = false
		out.Details = append(out.Details, "缺少 source.properties（安装可能不完整）")
	}
	// package.xml 是 sdkmanager / avdmanager / Android Studio 判定“包已安装”的依据。
	// 缺它时官方工具看不到该包（创建设备会报 "emulator" package must be installed!）。
	if !localrepo.HasMeta(dir) {
		out.Details = append(out.Details, "缺少 "+localrepo.FileName+"（官方工具会认为该包未安装；应用会在创建设备时自动补写）")
	}
	scanner := query.NewScanner(comp.Paths.SdkRoot)
	if _, ok := scanner.Find(pkgPath); !ok {
		out.OK = false
		out.Details = append(out.Details, "SDK 扫描未识别到该包")
	}
	if out.OK {
		out.Details = append(out.Details, "目录与版本信息均正常")
	}
	return out, nil
}

// IsSourceUsable 判断当前源是否可用（用于安装前提示）。
func (s *SdkService) IsSourceUsable() map[string]any {
	source := s.sourceOr("")
	res := map[string]any{"sourceId": source.ID, "name": source.Name, "baseURL": source.BaseURL}
	for _, cached := range s.rt.engine.Cached() {
		if cached.SourceID == source.ID {
			res["grade"] = cached.Grade
			res["ttfbMs"] = cached.TTFBMs
			res["throughputMBps"] = cached.ThroughputMBps
			res["measuredAt"] = cached.At
		}
	}
	return res
}

func (s *SdkService) sourceOr(id string) domain.MirrorSource {
	if strings.TrimSpace(id) == "" {
		return s.rt.ActiveSource()
	}
	sources := mirrorSources(s.rt)
	if src, ok := findSource(sources, id); ok {
		return src
	}
	return s.rt.ActiveSource()
}

func dependencyPaths(pkg repo.Package) []string {
	out := make([]string, 0, len(pkg.Dependencies))
	for _, d := range pkg.Dependencies {
		out = append(out, d.Path)
	}
	return out
}

func installTitle(packages []string) string {
	if len(packages) == 1 {
		return "安装 " + packages[0]
	}
	return "安装 " + itoa(len(packages)) + " 个组件"
}

func proxyURL(mode domain.ProxyMode, custom string) string {
	switch mode {
	case domain.ProxyCustom:
		return custom
	case domain.ProxySystem:
		return platform.SystemProxy()
	default:
		return ""
	}
}
