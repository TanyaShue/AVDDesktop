// Package service 是 Wails 绑定层：只做「参数校验 → 调用领域服务 → 注册 Job → 返回 jobID」。
//
// 所有 Service 共享一个 Runtime（依赖容器 + 事件出口），见 runtime.go。
package service

import (
	"context"
	"path/filepath"
	"sync"

	"AVDDesktop/internal/adb"
	"AVDDesktop/internal/avd/launch"
	"AVDDesktop/internal/avd/store"
	"AVDDesktop/internal/config"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/mirror"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk/detect"
	"AVDDesktop/internal/sdk/install"
)

// Runtime 是服务层共享的依赖容器。
type Runtime struct {
	AppName string
	Version string

	settings *config.Manager
	jobs     *job.Manager
	locks    *job.KeyedMutex
	detector *detect.Detector
	engine   *mirror.Engine

	emitFn func(event string, payload any)

	ctxMu sync.RWMutex
	ctx   context.Context

	compMu   sync.Mutex
	compRoot string
	compAvd  string
	comp     *components
}

// components 是按当前 SDK 根目录解析出的工具链句柄集合。
type components struct {
	Paths     platform.InstallPaths
	Store     *store.Store
	Installer *install.Installer
	Launcher  *launch.Launcher
	Adb       *adb.Client
	Env       []string
}

// NewRuntime 创建运行时。
func NewRuntime(appName, version string, settings *config.Manager, cacheDir string) *Runtime {
	r := &Runtime{
		AppName:  appName,
		Version:  version,
		settings: settings,
		locks:    job.NewKeyedMutex(),
		detector: detect.NewDetector(),
		engine:   mirror.NewEngine(cacheDir),
	}
	r.jobs = job.NewManager(r.Emit)
	return r
}

// SetContext 由 Wails 的 startup 回调注入。
func (r *Runtime) SetContext(ctx context.Context) {
	r.ctxMu.Lock()
	r.ctx = ctx
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

// SetEmitter 设置事件出口（由 main.go 注入 wails runtime.EventsEmit）。
func (r *Runtime) SetEmitter(fn func(event string, payload any)) { r.emitFn = fn }

// Emit 发送事件到前端。
func (r *Runtime) Emit(event string, payload any) {
	if r.emitFn != nil {
		r.emitFn(event, payload)
	}
}

// Jobs 返回任务管理器。
func (r *Runtime) Jobs() *job.Manager { return r.jobs }

// Settings 返回设置管理器。
func (r *Runtime) Settings() *config.Manager { return r.settings }

// Detector 返回环境探测器。
func (r *Runtime) Detector() *detect.Detector { return r.detector }

// Engine 返回镜像测速引擎。
func (r *Runtime) Engine() *mirror.Engine { return r.engine }

// Shutdown 停止所有任务与实例（应用退出时调用）。
func (r *Runtime) Shutdown() {
	r.jobs.CancelAll()
	if comp := r.comp; comp != nil && comp.Launcher != nil {
		comp.Launcher.StopAll(context.Background(), true)
	}
	r.jobs.Stop()
}

// Components 按当前设置解析工具链句柄（SDK 根变化时自动重建）。
func (r *Runtime) Components() *components {
	settings := r.settings.Get()
	sdkRes := platform.ResolveSdkRoot(settings.SdkRoot)
	avdRes := platform.ResolveAvdHome(settings.AvdHome)

	r.compMu.Lock()
	defer r.compMu.Unlock()
	if r.comp != nil && r.compRoot == sdkRes.Path && r.compAvd == avdRes.Path {
		return r.comp
	}

	paths := platform.NewInstallPaths(sdkRes.Path)
	env := platform.ChildEnv(sdkRes.Path, avdRes.Path, settings.InjectEnvForChild)
	if !settings.InjectEnvForChild {
		env = nil
	}

	st := store.New(avdRes.Path)
	st.SetSdkRoot(sdkRes.Path)

	comp := &components{
		Paths: paths,
		Store: st,
	}
	comp.Adb = adb.New(paths.Adb, env)
	comp.Installer = install.New(paths, platform.SubDir(r.AppName, "cache"))
	comp.Launcher = launch.New(paths, env, st, comp.Adb, r.Emit)
	comp.Env = env

	r.compRoot = sdkRes.Path
	r.compAvd = avdRes.Path
	r.comp = comp
	return comp
}

// InvalidateComponents 在 SDK 根或 AVD 目录变化后强制重建。
func (r *Runtime) InvalidateComponents() {
	r.compMu.Lock()
	r.comp = nil
	r.compMu.Unlock()
	r.detector.Invalidate()
}

// ResolvedPaths 返回当前解析后的关键路径（供 UI 展示与诊断）。
type ResolvedPaths struct {
	SdkRoot       string `json:"sdkRoot"`
	SdkRootSource string `json:"sdkRootSource"`
	AvdHome       string `json:"avdHome"`
	AvdHomeSource string `json:"avdHomeSource"`
	JdkPath       string `json:"jdkPath"`
	CacheDir      string `json:"cacheDir"`
	DownloadDir   string `json:"downloadDir"`
	LogDir        string `json:"logDir"`
}

// Resolved 返回当前路径解析结果。
func (r *Runtime) Resolved() ResolvedPaths {
	settings := r.settings.Get()
	sdkRes := platform.ResolveSdkRoot(settings.SdkRoot)
	avdRes := platform.ResolveAvdHome(settings.AvdHome)
	return ResolvedPaths{
		SdkRoot:       sdkRes.Path,
		SdkRootSource: sdkRes.Source,
		AvdHome:       avdRes.Path,
		AvdHomeSource: avdRes.Source,
		JdkPath:       platform.FindJava(settings.JdkPath),
		CacheDir:      platform.SubDir(r.AppName, "cache"),
		DownloadDir:   r.downloadDir(),
		LogDir:        platform.SubDir(r.AppName, "logs"),
	}
}

func (r *Runtime) downloadDir() string {
	if v := r.settings.Get().DownloadDir; v != "" {
		return v
	}
	return filepath.Join(platform.AppDataDir(r.AppName), "downloads")
}

// detectOptions 构造探测参数（供各 Service 复用）。
func (r *Runtime) detectOptions() detect.Options {
	settings := r.settings.Get()
	return detect.Options{
		SdkRootOverride: settings.SdkRoot,
		JdkPathOverride: settings.JdkPath,
		AvdHomeOverride: settings.AvdHome,
		InjectEnv:       settings.InjectEnvForChild,
	}
}

// ActiveSource 返回当前活动的镜像源。
func (r *Runtime) ActiveSource() domain.MirrorSource {
	settings := r.settings.Get()
	sources := mirror.Merge(settings.CustomSources)
	if settings.ActiveSourceID != "" {
		if src, ok := mirror.Find(sources, settings.ActiveSourceID); ok {
			return src
		}
	}
	return sources[0]
}

// ---------------------------------------------------------------- JobService

// JobService 暴露任务列表（任务抽屉使用）。
type JobService struct{ rt *Runtime }

// NewJobService 创建 JobService。
func NewJobService(rt *Runtime) *JobService { return &JobService{rt: rt} }

// List 返回所有任务快照。
func (s *JobService) List() []domain.JobInfo { return s.rt.jobs.List() }

// Get 返回单个任务。
func (s *JobService) Get(id string) (*domain.JobInfo, error) {
	j, ok := s.rt.jobs.Get(id)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "任务不存在: "+id)
	}
	info := j.Info()
	return &info, nil
}

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
	return logs, nil
}

// Cancel 取消任务。
func (s *JobService) Cancel(id string) error { return s.rt.jobs.Cancel(id) }

// CancelAll 取消全部任务。
func (s *JobService) CancelAll() { s.rt.jobs.CancelAll() }

// PruneAsk 清理已结束任务（保留最近 keep 条）。
func (s *JobService) PruneAsk(keep int) { s.rt.jobs.Prune(keep) }
