package service

import (
	"context"
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
	_, env := mirrorEnv(s.rt, "")
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
	source, sdkEnv := mirrorEnv(s.rt, "")
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
	if inst, ok := comp.Launcher.ByAvd(name); ok {
		if err := comp.Launcher.Stop(s.rt.Context(), inst.ID, false); err != nil {
			return "", err
		}
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
	source, sdkEnv := mirrorEnv(s.rt, "")
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
