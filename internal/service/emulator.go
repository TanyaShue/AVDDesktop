package service

import (
	"context"
	"sort"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// EmulatorService 管理模拟器实例：启动、停止、列表与应用退出时的统一清理。
type EmulatorService struct{ rt *Runtime }

// NewEmulatorService 创建 EmulatorService。
func NewEmulatorService(rt *Runtime) *EmulatorService { return &EmulatorService{rt: rt} }

// StartRequest 是启动请求（只保留界面需要的两个开关）。
type StartRequest struct {
	AvdName  string `json:"avdName"`
	ColdBoot bool   `json:"coldBoot"`
	NoWindow bool   `json:"noWindow"`
}

// Start 启动模拟器实例。
func (s *EmulatorService) Start(req StartRequest) (*domain.EmulatorInstance, error) {
	comp := s.rt.Components()
	if req.AvdName == "" {
		return nil, domain.Err(domain.CodeInvalidArgument, "未指定设备名称")
	}
	if !platform.FileExists(comp.Tools.Emulator) {
		return nil, domain.Err(domain.CodeToolMissing, "尚未安装模拟器（emulator）").
			WithAction("install", "安装模拟器", "emulator")
	}
	inst, err := comp.Launcher.Start(s.rt.Context(), req.AvdName, domain.LaunchOptions{
		ColdBoot: req.ColdBoot,
		NoWindow: req.NoWindow,
	})
	if err != nil {
		return nil, err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "started", "name": req.AvdName})
	return inst, nil
}

// Stop 停止实例。
func (s *EmulatorService) Stop(instanceID string, force bool) error {
	comp := s.rt.Components()
	inst, ok := comp.Launcher.Get(instanceID)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	if err := comp.Launcher.Stop(s.rt.Context(), instanceID, force); err != nil {
		return err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "stopped", "name": inst.AvdName})
	return nil
}

// StopByAvd 按 AVD 名称停止实例。
func (s *EmulatorService) StopByAvd(avdName string, force bool) error {
	comp := s.rt.Components()
	if err := comp.Launcher.StopByAvd(s.rt.Context(), avdName, force); err != nil {
		return err
	}
	s.rt.Emit("avd:changed", map[string]any{"action": "stopped", "name": avdName})
	return nil
}

// ListRunning 返回所有实例（含已停止的历史记录）。
func (s *EmulatorService) ListRunning() []domain.EmulatorInstance {
	items := s.rt.Components().Launcher.List()
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt > items[j].StartedAt })
	return domain.NonNil(items)
}

// Shutdown 停止所有实例（应用退出）。
func (s *EmulatorService) Shutdown(force bool) {
	s.rt.Components().Launcher.StopAll(context.Background(), force)
}
