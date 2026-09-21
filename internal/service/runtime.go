// Package service 是 Wails 绑定层：只做「参数校验 → 调用领域逻辑 → 注册 Job → 返回 jobID」。
//
// 所有 Service 共享一个 Runtime（依赖容器 + 事件出口）。软件自带 SDK/AVD 目录在启动时
// 固定解析一次，运行期不再变化。
package service

import (
	"context"
	"sync"

	"AVDDesktop/internal/adb"
	"AVDDesktop/internal/avd/launch"
	"AVDDesktop/internal/avd/store"
	"AVDDesktop/internal/config"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
)

// Runtime 是服务层共享的运行时容器。
type Runtime struct {
	AppName string
	Version string

	log      *logging.Logger
	settings *config.Manager
	jobs     *job.Manager
	locks    *job.KeyedMutex

	comp *components

	emitFn func(event string, payload any)

	ctxMu    sync.RWMutex
	ctx      context.Context
	ctxReady bool
}

// components 是软件自有 SDK / AVD 目录下的工具链句柄集合。
type components struct {
	Tools    platform.Tools
	Store    *store.Store
	Launcher *launch.Launcher
	Adb      *adb.Client
	Env      []string
}

// NewRuntime 创建运行时。log 为 nil 时使用空日志器。
func NewRuntime(appName, version string, settings *config.Manager, log *logging.Logger) (*Runtime, error) {
	if log == nil {
		log = logging.Discard()
	}
	tools := platform.NewTools(platform.SdkRoot())
	avdHome := platform.AvdHome()
	env := platform.ChildEnv(tools, avdHome)

	st := store.New(avdHome)
	st.SetSdkRoot(tools.SdkRoot)

	r := &Runtime{
		AppName:  appName,
		Version:  version,
		log:      log,
		settings: settings,
		locks:    job.NewKeyedMutex(),
	}
	r.comp = &components{
		Tools: tools,
		Store: st,
		Adb:   adb.New(tools.Adb, env, log),
		Env:   env,
	}
	r.comp.Launcher = launch.New(tools, env, st, r.comp.Adb, r.Emit, log)
	r.jobs = job.NewManager(r.Emit, log)
	return r, nil
}

// Log 返回应用日志器。
func (r *Runtime) Log() *logging.Logger { return r.log }

// Components 返回工具链句柄集合。
func (r *Runtime) Components() *components { return r.comp }

// SetContext 由 Wails 的 startup 回调注入。
//
// 在 startup 之前（例如 `wails generate module` 的绑定生成阶段、单元测试）
// 不允许调用任何 Wails runtime 方法，因此 Emit 会先检查 ctxReady。
func (r *Runtime) SetContext(ctx context.Context) {
	r.ctxMu.Lock()
	r.ctx = ctx
	r.ctxReady = true
	r.ctxMu.Unlock()
}

// Context 返回应用上下文（用于 Wails runtime 调用）。
func (r *Runtime) Context() context.Context {
	r.ctxMu.RLock()
	defer r.ctxMu.RUnlock()
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// ContextReady 返回 Wails 生命周期上下文是否已就绪。
func (r *Runtime) ContextReady() bool {
	r.ctxMu.RLock()
	defer r.ctxMu.RUnlock()
	return r.ctxReady
}

// SetEmitter 设置事件出口（由 main.go 注入 wails runtime.EventsEmit）。
func (r *Runtime) SetEmitter(fn func(event string, payload any)) { r.emitFn = fn }

// Emit 发送事件到前端；上下文未就绪时静默丢弃。
func (r *Runtime) Emit(event string, payload any) {
	if r.emitFn == nil || !r.ContextReady() {
		return
	}
	r.emitFn(event, payload)
}

// Jobs 返回任务管理器。
func (r *Runtime) Jobs() *job.Manager { return r.jobs }

// Settings 返回设置管理器。
func (r *Runtime) Settings() *config.Manager { return r.settings }

// Shutdown 停止任务与所有模拟器实例（应用退出时调用）。
func (r *Runtime) Shutdown() {
	r.log.Info("app", "开始关闭：取消任务并停止模拟器实例")
	r.jobs.CancelAll()
	r.comp.Launcher.StopAll(context.Background(), true)
	r.jobs.Stop()
}

// ResolvedPaths 是软件自有目录的解析结果（供设置页展示）。
type ResolvedPaths struct {
	AppRoot  string `json:"appRoot"`
	SdkRoot  string `json:"sdkRoot"`
	AvdHome  string `json:"avdHome"`
	JavaPath string `json:"javaPath,omitempty"`
	LogDir   string `json:"logDir"`
	CacheDir string `json:"cacheDir"`
}

// Resolved 返回当前解析出的关键路径。
func (r *Runtime) Resolved() ResolvedPaths {
	return ResolvedPaths{
		AppRoot:  platform.Root(),
		SdkRoot:  platform.SdkRoot(),
		AvdHome:  platform.AvdHome(),
		JavaPath: platform.FindJava(),
		LogDir:   platform.LogDir(),
		CacheDir: platform.CacheDir(),
	}
}

// JobService 暴露任务列表（底部任务/日志面板使用）。
type JobService struct{ rt *Runtime }

// NewJobService 创建 JobService。
func NewJobService(rt *Runtime) *JobService { return &JobService{rt: rt} }

// List 返回所有任务快照。
func (s *JobService) List() []domain.JobInfo { return domain.NonNil(s.rt.jobs.List()) }

// Logs 返回任务日志。
func (s *JobService) Logs(id string, tail int) ([]domain.LogLine, error) {
	j, ok := s.rt.jobs.Get(id)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "任务不存在: "+id)
	}
	logs := j.Logs()
	if tail > 0 && len(logs) > tail {
		logs = logs[len(logs)-tail:]
	}
	return domain.NonNil(logs), nil
}

// Cancel 取消任务。
func (s *JobService) Cancel(id string) error { return s.rt.jobs.Cancel(id) }

// CancelAll 取消全部任务。
func (s *JobService) CancelAll() { s.rt.jobs.CancelAll() }
