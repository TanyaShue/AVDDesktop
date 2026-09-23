package service

import (
	"context"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/avd"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/sdk"
)

// AvdService 提供设备（AVD）的查询、创建与删除。
type AvdService struct{ rt *Runtime }

// NewAvdService 创建 AvdService。
func NewAvdService(rt *Runtime) *AvdService { return &AvdService{rt: rt} }

// List 返回所有设备（合并运行状态）。
func (s *AvdService) List() ([]domain.AvdSummary, error) {
	comp := s.rt.Components()
	items, err := comp.Store.List()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if inst, ok := comp.Launcher.ByAvd(items[i].Name); ok {
			items[i].State = inst.State
			items[i].InstanceID = inst.ID
			items[i].Serial = inst.Serial
			items[i].Port = inst.Port
		} else if items[i].State == "" {
			items[i].State = domain.AvdStopped
		}
	}
	return domain.NonNil(items), nil
}

// ValidateName 校验设备名称并给出建议。
func (s *AvdService) ValidateName(name string) (*domain.NameValidation, error) {
	res := s.rt.Components().Store.ValidateName(name)
	return &res, nil
}

// ListProfiles 返回官方 avdmanager 提供的设备档案。
func (s *AvdService) ListProfiles(refresh bool) ([]domain.DeviceProfile, error) {
	comp := s.rt.Components()
	profiles, err := avd.ListProfiles(s.rt.Context(), comp.Tools, comp.Env)
	if err != nil {
		return nil, err
	}
	return domain.NonNil(profiles), nil
}

// ListImages 返回可用于创建 AVD 的系统镜像。
//
// 数据全部来自官方 sdkmanager：installedOnly 为 true 时只读本地列表（不联网）。
func (s *AvdService) ListImages(installedOnly bool) ([]domain.SystemImage, error) {
	comp := s.rt.Components()
	_, env := mirrorEnv(s.rt)
	images, err := sdk.ListImages(s.rt.Context(), comp.Tools, env, installedOnly, func(stream, line string) {
		s.rt.Log().Debug("sdkmanager", "%s", line)
	})
	if err != nil {
		return nil, err
	}
	sdk.SortImagesForHost(images)
	return domain.NonNil(images), nil
}

// Create 创建设备，返回 jobID。
//
// 流程：校验名称 → 镜像不存在时通过 sdkmanager 安装 → avdmanager 创建。
// 与其它写 SDK/AVD 目录的任务共用同一把互斥锁（同一时刻只允许一个写任务）。
func (s *AvdService) Create(spec domain.AvdSpec) (string, error) {
	comp := s.rt.Components()
	source, sdkEnv := mirrorEnv(s.rt)
	spec.Name = strings.TrimSpace(spec.Name)
	if v := comp.Store.ValidateName(spec.Name); !v.Valid {
		return "", domain.Err(domain.CodeAvdNameInvalid, v.Reason)
	}
	if _, _, _, err := sdk.SplitImage(spec.SystemImagePath); err != nil {
		return "", err
	}

	unlock, ok := s.rt.locks.TryLock("sdk:" + comp.Tools.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有 SDK 安装或设备写任务正在进行").
			WithHint("请等待当前任务完成，或在底部任务区域取消它")
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:     domain.JobAvdCreate,
		Title:    "创建设备 " + spec.Name,
		Subtitle: spec.SystemImagePath,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()

		j.SetPhase("检查系统镜像")
		j.Logf("info", "mirror", "系统镜像使用镜像源：%s（%s）", source.Name, source.BaseURL)
		// 缺镜像时 EnsureImage 会走 sdkmanager 安装，因此同样要接进度。
		stopProgress := watchInstallProgress(ctx, j, comp.Tools.SdkRoot, []string{spec.SystemImagePath}, source.BaseURL)
		already, err := avd.EnsureImage(ctx, comp.Tools, sdkEnv, spec.SystemImagePath, jobLine(j, "sdkmanager"))
		stopProgress()
		if err != nil {
			return err
		}
		if !already {
			j.Logf("info", "sdkmanager", "系统镜像已安装：%s", spec.SystemImagePath)
		}

		j.SetPhase("avdmanager 创建中")
		summary, err := comp.Store.Create(ctx, comp.Tools, comp.Env, spec, jobLine(j, "avdmanager"))
		if err != nil {
			return err
		}
		j.Logf("info", "create", "设备 %s 创建完成：%s", summary.Name, summary.Path)
		s.rt.Emit("avd:changed", map[string]any{"action": "created", "name": spec.Name})
		return nil
	})
	return j.ID(), nil
}

// Delete 删除设备（仍在运行的实例会先停止），返回 jobID。
func (s *AvdService) Delete(name string) (string, error) {
	comp := s.rt.Components()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", domain.Err(domain.CodeInvalidArgument, "未指定设备名称")
	}
	if !comp.Store.Exists(name) {
		return "", domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	unlock, ok := s.rt.locks.TryLock("sdk:" + comp.Tools.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有 SDK 安装或设备写任务正在进行").
			WithHint("请等待当前任务完成，或在底部任务区域取消它")
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:  domain.JobAvdDelete,
		Title: "删除设备 " + name,
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		// 停实例放在任务体内：取锁失败时不留副作用（不会出现"删除没发生、模拟器却被停掉"），
		// 停止过程也可被取消，并在底部任务区域显示进度。
		if inst, running := comp.Launcher.ByAvd(name); running {
			j.SetPhase("停止运行中的实例")
			if err := comp.Launcher.Stop(ctx, inst.ID, false); err != nil {
				return err
			}
		}
		j.SetPhase("正在删除文件")
		if err := comp.Store.Delete(name); err != nil {
			return err
		}
		j.Logf("info", "delete", "设备 %s 已删除", name)
		s.rt.Emit("avd:changed", map[string]any{"action": "deleted", "name": name})
		return nil
	})
	return j.ID(), nil
}

// InstallImage 通过官方 sdkmanager 安装系统镜像，返回 jobID。
func (s *AvdService) InstallImage(pkgPath string) (string, error) {
	if _, _, _, err := sdk.SplitImage(pkgPath); err != nil {
		return "", err
	}
	comp := s.rt.Components()
	source, sdkEnv := mirrorEnv(s.rt)
	unlock, ok := s.rt.locks.TryLock("sdk:" + comp.Tools.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有 SDK 安装任务正在进行")
	}
	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:     domain.JobInstall,
		Title:    "安装系统镜像 " + pkgPath,
		Subtitle: sdk.InstallCommand(comp.Tools, []string{pkgPath}),
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()
		j.Logf("info", "mirror", "系统镜像使用镜像源：%s（%s）", source.Name, source.BaseURL)
		stopProgress := watchInstallProgress(ctx, j, comp.Tools.SdkRoot, []string{pkgPath}, source.BaseURL)
		defer stopProgress()
		if err := sdk.InstallPackages(ctx, comp.Tools, sdkEnv, []string{pkgPath}, jobLine(j, "sdkmanager")); err != nil {
			return err
		}
		j.Logf("info", "sdkmanager", "系统镜像安装完成：%s", pkgPath)
		s.rt.Emit("env:changed", nil)
		return nil
	})
	return j.ID(), nil
}

// DeleteImage 删除本机已安装的系统镜像，返回 jobID。
//
// 只删除软件自带 SDK 目录里的镜像，不联网；仍在被 AVD 引用的镜像会被拒绝，
// 因为设备配置里记录的是镜像目录，删掉后设备会直接损坏。
func (s *AvdService) DeleteImage(pkgPath string) (string, error) {
	pkgPath = strings.TrimSpace(pkgPath)
	if _, _, _, err := sdk.SplitImage(pkgPath); err != nil {
		return "", err
	}
	comp := s.rt.Components()
	if err := sdk.VerifyPackage(comp.Tools, pkgPath); err != nil {
		return "", domain.ErrDetail(domain.CodePathNotFound,
			"本机没有安装该系统镜像", pkgPath+"："+err.Error()).
			WithHint("列表里只有已安装的镜像才能删除，请先重新加载列表")
	}
	users, err := devicesUsingImage(comp.Store, pkgPath)
	if err != nil {
		// 读不到设备列表时拒绝删除：宁可删不掉，也不能误删仍被引用的镜像。
		return "", domain.Wrap(domain.CodePathNotFound,
			"无法读取设备列表，为避免误删正在使用的镜像已取消操作", err)
	}
	if len(users) > 0 {
		return "", domain.ErrDetail(domain.CodeFileInUse,
			"仍有设备在使用该系统镜像", strings.Join(users, "、")).
			WithHint("请先删除这些设备，或把它们改用其它系统镜像")
	}

	unlock, ok := s.rt.locks.TryLock("sdk:" + comp.Tools.SdkRoot)
	if !ok {
		return "", domain.Err(domain.CodeJobBusy, "已有 SDK 安装或删除任务正在进行").
			WithHint("请等待当前任务完成，或在底部任务区域取消它")
	}

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:     domain.JobImageDelete,
		Title:    "删除系统镜像 " + pkgPath,
		Subtitle: sdk.UninstallCommand(comp.Tools, []string{pkgPath}),
	}, func(ctx context.Context, j *job.Job) error {
		defer unlock()

		j.SetPhase("正在删除本地镜像")
		if err := sdk.UninstallPackages(ctx, comp.Tools, comp.Env, []string{pkgPath}, jobLine(j, "sdkmanager")); err != nil {
			return err
		}
		j.Logf("info", "sdkmanager", "系统镜像已删除：%s", pkgPath)
		s.rt.Emit("env:changed", nil)
		return nil
	})
	return j.ID(), nil
}

// devicesUsingImage 返回仍在引用该系统镜像的设备名。
//
// 判据优先用 config.ini 的 image.sysdir.1（模拟器启动时实际读取的目录），
// 配置缺失时退化为 API / tag / ABI 三元组匹配。
//
// 读取设备列表失败时返回错误而不是空列表：调用方据此中止删除，
// 避免把"读不到"当成"没有设备引用"而误删仍在使用的镜像。
func devicesUsingImage(store *avd.Store, pkgPath string) ([]string, error) {
	api, tag, abi, err := sdk.SplitImage(pkgPath)
	if err != nil {
		return nil, err
	}
	rel, err := sdk.ImageDir(pkgPath)
	if err != nil {
		return nil, err
	}
	want := strings.Trim(strings.ToLower(filepath.ToSlash(rel)), "/")

	summaries, err := store.List()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, sum := range summaries {
		if config, cfgErr := store.ReadConfig(sum.Name); cfgErr == nil {
			sysdir := strings.Trim(strings.ToLower(filepath.ToSlash(config["image.sysdir.1"])), "/")
			if sysdir != "" {
				if sysdir == want {
					names = append(names, sum.Name)
				}
				continue
			}
		}
		if sum.API == api && sum.Tag == tag && sum.ABI == abi {
			names = append(names, sum.Name)
		}
	}
	return names, nil
}
