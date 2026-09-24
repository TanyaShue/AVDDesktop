package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/platform"
)

// startTrackInterval 是启动任务跟随实例状态的轮询间隔。
const startTrackInterval = 500 * time.Millisecond

// EmulatorService 管理模拟器实例：启动、停止、列表与应用退出时的统一清理。
type EmulatorService struct{ rt *Runtime }

// NewEmulatorService 创建 EmulatorService。
func NewEmulatorService(rt *Runtime) *EmulatorService { return &EmulatorService{rt: rt} }

// StartRequest 是启动请求（只保留界面需要的三个开关）。
type StartRequest struct {
	AvdName  string `json:"avdName"`
	ColdBoot bool   `json:"coldBoot"`
	NoWindow bool   `json:"noWindow"`
	// CustomUI 表示使用应用自建窗口接管画面：不启动 Qt 窗口，并开启模拟器 gRPC 图像通道。
	CustomUI bool `json:"customUI"`
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
	opts := domain.LaunchOptions{ColdBoot: req.ColdBoot, NoWindow: req.NoWindow, CustomUI: req.CustomUI}
	if opts.CustomUI {
		// 自定义 UI 必须无窗口：否则会出现"有 Qt 窗口 + gRPC 通道"这种无意义组合。
		opts.NoWindow = true
	}
	inst, err := comp.Launcher.Start(s.rt.Context(), req.AvdName, opts)
	if err != nil {
		return nil, err
	}
	s.trackStart(inst)
	s.rt.Emit("avd:changed", map[string]any{"action": "started", "name": req.AvdName})
	return inst, nil
}

// trackStart 把一次启动登记为任务：跟随实例状态，把关键节点写进底部统一任务日志。
//
// 任务在实例进入 running（成功）、error（失败）或就绪前退出（已取消）时结束，
// 使「启动模拟器」与其它耗时链路一样，进度与日志只出现在底部任务区域。
func (s *EmulatorService) trackStart(inst *domain.EmulatorInstance) {
	launcher := s.rt.Components().Launcher
	s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:     domain.JobEmulatorStart,
		Title:    "启动模拟器 " + inst.AvdName,
		Subtitle: inst.Serial,
	}, func(ctx context.Context, j *job.Job) error {
		// 进程与状态细节已由 launcher 写入应用日志（module=emulator），
		// 这里只推进任务阶段，避免同一件事在统一控制台出现两行。
		ticker := time.NewTicker(startTrackInterval)
		defer ticker.Stop()
		seen := domain.AvdState("")
		for {
			cur, ok := launcher.Get(inst.ID)
			if !ok {
				return domain.Err(domain.CodeUnknown, "实例记录已丢失")
			}
			if cur.State != seen {
				seen = cur.State
				j.SetPhase(startStateText(cur.State))
				switch cur.State {
				case domain.AvdRunning:
					return nil
				case domain.AvdError:
					return domain.Err(domain.CodeProcessFailed, startFailureText(cur))
				case domain.AvdStopped:
					// 就绪前退出（通常是用户主动停止）：按已取消结束，避免误报失败
					return context.Canceled
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	})
}

// startStateText 把实例状态翻译成启动任务的阶段文案。
func startStateText(state domain.AvdState) string {
	switch state {
	case domain.AvdStarting:
		return "正在启动模拟器进程"
	case domain.AvdBooting:
		return "等待开机完成"
	case domain.AvdRunning:
		return "模拟器已就绪"
	case domain.AvdStopping:
		return "启动被中止：实例正在停止"
	case domain.AvdStopped:
		return "启动被中止：实例已停止"
	default:
		return "启动异常"
	}
}

// startFailureText 取实例的失败原因；没有细节时给出可操作提示。
func startFailureText(inst domain.EmulatorInstance) string {
	if msg := strings.TrimSpace(inst.LastError); msg != "" {
		return msg
	}
	return "模拟器在就绪前退出，详情见应用日志（模块 emulator）"
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
