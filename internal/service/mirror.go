package service

import (
	"context"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/jdk"
	"AVDDesktop/internal/mirror"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk"
)

// MirrorService 提供 Android SDK / JDK 镜像源选择与实时检测。
type MirrorService struct {
	rt         *Runtime
	checker    *mirror.Checker
	jdkChecker *jdk.Checker
}

// NewMirrorService 创建 MirrorService。
func NewMirrorService(rt *Runtime) *MirrorService {
	return &MirrorService{rt: rt, checker: mirror.NewChecker(), jdkChecker: jdk.NewChecker()}
}

// ListSources 返回所有内置镜像，并标记当前默认源。
func (s *MirrorService) ListSources() []domain.MirrorSource {
	active := activeMirrorSource(s.rt)
	sources := mirror.MarkActive(mirror.Builtin(), active.ID)
	mirror.SortForDisplay(sources)
	return domain.NonNil(sources)
}

// Active 返回当前默认镜像。
func (s *MirrorService) Active() domain.MirrorSource {
	return activeMirrorSource(s.rt)
}

// SetActiveSource 保存默认镜像。仅接受内置镜像 ID，防止持久化无效地址。
func (s *MirrorService) SetActiveSource(id string) (*domain.MirrorSource, error) {
	source, ok := mirror.Find(id)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "镜像源不存在: "+strings.TrimSpace(id))
	}
	if _, err := s.rt.settings.Update(map[string]any{"mirrorSourceId": source.ID}); err != nil {
		return nil, err
	}
	source.Active = true
	return &source, nil
}

// CheckAll 并发检测所有镜像的连通性、延迟、采样速度和资源完整性。
func (s *MirrorService) CheckAll() ([]domain.MirrorCheck, error) {
	ctx, cancel := context.WithTimeout(s.rt.Context(), 30*time.Second)
	defer cancel()
	results := s.checker.CheckAll(ctx, mirror.Builtin(), s.checkOptions())
	for _, result := range results {
		message := "%s：%s，延迟 %dms，速度 %.2fMB/s，资源 %d/%d"
		args := []any{result.SourceName, compatibilityText(result.Compatible), result.LatencyMs,
			float64(result.ThroughputBps) / (1 << 20), availableResourceCount(result.Resources), len(result.Resources)}
		if result.Recommended {
			s.rt.Log().Info("mirror", message, args...)
		} else {
			s.rt.Log().Debug("mirror", message, args...)
		}
	}
	return domain.NonNil(results), nil
}

// ListJDKSources 返回所有内置 JDK 镜像，并标记当前默认源。
func (s *MirrorService) ListJDKSources() []domain.MirrorSource {
	active := activeJDKSource(s.rt)
	return domain.NonNil(jdk.MarkSourcesActive(jdk.Sources(), active.ID))
}

// ActiveJDK 返回当前默认 JDK 镜像。
func (s *MirrorService) ActiveJDK() domain.MirrorSource {
	return activeJDKSource(s.rt)
}

// SetActiveJDKSource 保存默认 JDK 镜像。仅接受内置镜像 ID。
func (s *MirrorService) SetActiveJDKSource(id string) (*domain.MirrorSource, error) {
	source, ok := jdk.FindSource(id)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "JDK 镜像源不存在: "+strings.TrimSpace(id))
	}
	if _, err := s.rt.settings.Update(map[string]any{"jdkMirrorSourceId": source.ID}); err != nil {
		return nil, err
	}
	source.Active = true
	return &source, nil
}

// CheckJDKAll 并发检测所有 JDK 镜像的连通性、延迟、采样速度和归档可用性。
func (s *MirrorService) CheckJDKAll() ([]domain.MirrorCheck, error) {
	ctx, cancel := context.WithTimeout(s.rt.Context(), 30*time.Second)
	defer cancel()
	results := s.jdkChecker.CheckAll(ctx, jdk.Sources(), s.jdkCheckOptions())
	for _, result := range results {
		message := "JDK %s：%s，延迟 %dms，速度 %.2fMB/s，归档 %d/%d"
		args := []any{result.SourceName, compatibilityText(result.Compatible), result.LatencyMs,
			float64(result.ThroughputBps) / (1 << 20), availableResourceCount(result.Resources), len(result.Resources)}
		if result.Recommended {
			s.rt.Log().Info("jdk-mirror", message, args...)
		} else {
			s.rt.Log().Debug("jdk-mirror", message, args...)
		}
	}
	return domain.NonNil(results), nil
}

// CheckJDK 检测单个 JDK 镜像。
func (s *MirrorService) CheckJDK(id string) (*domain.MirrorCheck, error) {
	source, ok := jdk.FindSource(id)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "JDK 镜像源不存在: "+strings.TrimSpace(id))
	}
	ctx, cancel := context.WithTimeout(s.rt.Context(), 18*time.Second)
	defer cancel()
	result := s.jdkChecker.Check(ctx, source, s.jdkCheckOptions())
	return &result, nil
}

// Check 检测单个镜像。
func (s *MirrorService) Check(id string) (*domain.MirrorCheck, error) {
	source, ok := mirror.Find(id)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "镜像源不存在: "+strings.TrimSpace(id))
	}
	ctx, cancel := context.WithTimeout(s.rt.Context(), 18*time.Second)
	defer cancel()
	result := s.checker.Check(ctx, source, s.checkOptions())
	return &result, nil
}

func (s *MirrorService) checkOptions() mirror.CheckOptions {
	tools := s.rt.Components().Tools
	return mirror.CheckOptions{
		NeedCmdlineTools:  !tools.HasSdkmanager(),
		NeedPlatformTools: !tools.HasAdb(),
		NeedEmulator:      !tools.HasEmulator(),
		CheckSystemImages: true,
	}
}

func (s *MirrorService) jdkCheckOptions() jdk.CheckOptions {
	return jdk.CheckOptions{NeedJDK: platform.FindJava() == ""}
}

func activeMirrorSource(rt *Runtime) domain.MirrorSource {
	return mirror.Resolve(rt.settings.Get().MirrorSourceID)
}

func activeJDKSource(rt *Runtime) domain.MirrorSource {
	return jdk.ResolveSource(rt.settings.Get().JDKMirrorSourceID)
}

// mirrorEnv 返回当前生效的 Android SDK 镜像，以及把仓库根地址指向该镜像的子进程环境。
// 镜像源只能由 MirrorService 保存，这里不再接受调用方传入的源 ID（避免准备/安装路径偷偷改设置）。
func mirrorEnv(rt *Runtime) (domain.MirrorSource, []string) {
	source := activeMirrorSource(rt)
	return source, sdk.WithBaseURL(rt.Components().Env, source.BaseURL)
}

func compatibilityText(ok bool) string {
	if ok {
		return "资源完整"
	}
	return "资源不完整"
}

func availableResourceCount(resources []domain.MirrorResource) int {
	count := 0
	for _, resource := range resources {
		if resource.Available {
			count++
		}
	}
	return count
}
