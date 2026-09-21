// Package job 是统一的长任务管理器。
//
// 设计（见 ARCHITECTURE.md ADR-03 / §7）：
//   - 所有耗时操作（测速、下载、安装、创建 AVD、启动模拟器）都登记为 Job
//   - 绑定方法立即返回 jobID，进度与日志通过事件流推送
//   - 进度事件节流到 10Hz，日志批量发送，避免 Wails 序列化抖动
//   - 同类型资源互斥由调用方通过 KeyedMutex 保证（SDK 写锁 / AVD 写锁）
package job

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
)

// Sink 是事件出口（由 service 层桥接到 Wails runtime.EventsEmit）。
type Sink func(event string, payload any)

// LogHook 把任务日志同时转写到应用日志（便于用户离开界面后仍能回溯）。
//
// 参数：jobID、jobKind、level、source、message。
type LogHook func(jobID, kind, level, source, message string)

// 事件名（与 docs/API-CONTRACT.md §6 一致）。
const (
	EventCreated  = "job:created"
	EventProgress = "job:progress"
	EventLog      = "job:log"
	EventDone     = "job:done"
	EventFailed   = "job:failed"
)

const (
	progressInterval = 100 * time.Millisecond
	logFlushInterval = 200 * time.Millisecond
	maxLogLines      = 500
	maxBufferedLogs  = 50
)

// Spec 描述一个任务。
type Spec struct {
	Kind       domain.JobKind
	Title      string
	Subtitle   string
	Group      string
	ItemsTotal int
	BytesTotal int64
}

// Runner 是任务主体；返回 nil 表示成功。
//
// Runner 内必须持续检查 ctx.Done()（下载/解压/进程调用都要带 ctx）。
type Runner func(ctx context.Context, j *Job) error

// Job 是一个运行中的任务句柄（线程安全）。
type Job struct {
	id   string
	spec Spec

	mu       sync.Mutex
	info     domain.JobInfo
	logs     []domain.LogLine
	pending  []domain.LogLine
	dirty    bool
	ended    bool
	cancel   context.CancelFunc
	onThread func(domain.LogLine) // 可选：转发给应用日志
}

// ID 返回任务 ID。
func (j *Job) ID() string { return j.id }

// Spec 返回任务元信息。
func (j *Job) Spec() Spec { return j.spec }

// SetPhase 更新当前阶段文案（例如"下载 emulator"、"解压"）。
func (j *Job) SetPhase(phase string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.info.Phase == phase {
		return
	}
	j.info.Phase = phase
	j.dirty = true
}

// SetSubtitle 更新副标题。
func (j *Job) SetSubtitle(s string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.info.Subtitle = s
	j.dirty = true
}

// SetPercent 直接设置百分比（0-100，内部裁剪）。
func (j *Job) SetPercent(p float64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.info.Percent = clamp(p, 0, 100)
	j.dirty = true
}

// Progress 按已完成/总量更新百分比（total <= 0 时忽略）。
func (j *Job) Progress(done, total int64) {
	if total <= 0 {
		return
	}
	j.SetPercent(float64(done) / float64(total) * 100)
}

// SetBytes 更新字节进度与速度。
func (j *Job) SetBytes(done, total int64, speedBps int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.info.BytesDone = done
	j.info.BytesTotal = total
	j.info.SpeedBps = speedBps
	if total > 0 {
		j.info.Percent = clamp(float64(done)/float64(total)*100, 0, 100)
	}
	if speedBps > 0 && total > done {
		j.info.ETASeconds = int((total - done) / speedBps)
	}
	j.dirty = true
}

// SetItems 更新子任务计数（例如第 3/5 个包）。
func (j *Job) SetItems(done, total int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.info.ItemsDone = done
	j.info.ItemsTotal = total
	j.dirty = true
}

// Log 追加一条日志（会批量推送给前端，并保留最近 maxLogLines 条）。
func (j *Job) Log(level, source, message string) {
	line := domain.LogLine{At: time.Now().UnixMilli(), Level: level, Source: source, Message: message}
	j.mu.Lock()
	j.logs = append(j.logs, line)
	if len(j.logs) > maxLogLines {
		j.logs = j.logs[len(j.logs)-maxLogLines:]
	}
	j.pending = append(j.pending, line)
	j.dirty = true
	thread := j.onThread
	j.mu.Unlock()
	if thread != nil {
		thread(line)
	}
}

// Logf 是 Log 的格式化封装。
func (j *Job) Logf(level, source, format string, args ...any) {
	j.Log(level, source, sprintf(format, args...))
}

// Info 返回当前快照。
func (j *Job) Info() domain.JobInfo {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.info
}

// Logs 返回最近日志副本。
func (j *Job) Logs() []domain.LogLine {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]domain.LogLine, len(j.logs))
	copy(out, j.logs)
	return out
}

// Context 已移除：Runner 收到的 ctx 就是任务上下文，请直接使用它。

// Manager 管理所有任务。
type Manager struct {
	sink Sink
	log  logging.Interface

	hookMu  sync.Mutex
	logHook LogHook

	mu   sync.Mutex
	jobs map[string]*Job
	seq  int64

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewManager 创建任务管理器并启动节流推送协程。
func NewManager(sink Sink, log logging.Interface) *Manager {
	m := &Manager{sink: sink, log: logging.Or(log), jobs: map[string]*Job{}, stopCh: make(chan struct{})}
	go m.ticker()
	return m
}

// SetLogHook 设置任务日志转写钩子。
func (m *Manager) SetLogHook(hook LogHook) {
	m.hookMu.Lock()
	m.logHook = hook
	m.hookMu.Unlock()
}

// Stop 停止后台协程（应用退出时调用）。
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
}

// Start 注册并异步执行一个任务，立即返回句柄。
//
// runner 在独立 goroutine 中运行；调用方通过 jobID 订阅事件。
func (m *Manager) Start(parent context.Context, spec Spec, runner Runner) *Job {
	ctx, cancel := context.WithCancel(parent)

	m.mu.Lock()
	m.seq++
	id := genID(m.seq)
	j := &Job{
		id:     id,
		spec:   spec,
		cancel: cancel,
		info: domain.JobInfo{
			ID:         id,
			Kind:       spec.Kind,
			Title:      spec.Title,
			Subtitle:   spec.Subtitle,
			Group:      spec.Group,
			Status:     domain.JobQueued,
			ItemsTotal: spec.ItemsTotal,
			BytesTotal: spec.BytesTotal,
			StartedAt:  time.Now().UnixMilli(),
		},
		dirty: true,
	}
	// 任务日志同时转写到应用日志，便于用户关闭界面后回溯。
	j.onThread = func(line domain.LogLine) {
		m.dispatchHook(id, string(spec.Kind), line.Level, line.Source, line.Message)
	}
	m.jobs[id] = j
	m.mu.Unlock()

	m.emit(EventCreated, j.Info())

	go func() {
		j.mu.Lock()
		j.info.Status = domain.JobRunning
		j.dirty = true
		j.mu.Unlock()
		m.emit(EventProgress, j.Info())
		m.log.Info("job", "开始任务 %s（%s）：%s", id, spec.Kind, spec.Title)

		start := time.Now()
		var runErr error
		// 任务运行在独立 goroutine 中，必须兵底捕获 panic，否则会直接拖垮整个应用。
		if recovered := withRecover(func() { runErr = runner(ctx, j) }, func(r any, stack []byte) {
			runErr = domain.ErrDetail(domain.CodeUnknown,
				"任务发生内部错误（panic），已中止",
				fmt.Sprintf("%v\n%s", r, truncate(stack)))
			m.log.Error("job", "任务 %s panic: %v\n%s", id, r, truncate(stack))
		}); recovered {
			// 具体错误已由上面的回调构造
		}

		j.mu.Lock()
		now := time.Now().UnixMilli()
		j.info.EndedAt = now
		j.ended = true
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || ctx.Err() == context.Canceled {
				j.info.Status = domain.JobCanceled
			} else {
				j.info.Status = domain.JobFailed
				j.info.Error = toAppError(runErr)
			}
		} else {
			j.info.Status = domain.JobSucceeded
			j.info.Percent = 100
		}
		info := j.info
		j.mu.Unlock()

		m.flushLogs(j, true)
		switch info.Status {
		case domain.JobFailed:
			m.log.Error("job", "任务 %s 失败（%s）：%v", id, spec.Kind, info.Error)
			m.emit(EventFailed, info)
		case domain.JobCanceled:
			m.log.Warn("job", "任务 %s 已取消（%s）", id, spec.Kind)
			m.emit(EventDone, info)
		default:
			m.log.Info("job", "任务 %s 完成（%s，耗时 %s）", id, spec.Kind, time.Since(start).Round(time.Millisecond))
			m.emit(EventDone, info)
		}
	}()
	return j
}

// Get 查询任务。
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// List 返回所有任务的快照（按开始时间倒序）。
func (m *Manager) List() []domain.JobInfo {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()

	out := make([]domain.JobInfo, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.Info())
	}
	sort.Slice(out, func(i, k int) bool { return out[i].StartedAt > out[k].StartedAt })
	return out
}

// Cancel 取消一个任务。
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "任务不存在: "+id)
	}
	j.mu.Lock()
	ended := j.ended
	j.mu.Unlock()
	if ended {
		return nil
	}
	j.cancel()
	return nil
}

// CancelAll 取消所有未结束任务（应用退出时调用）。
func (m *Manager) CancelAll() {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	for _, j := range jobs {
		j.mu.Lock()
		ended := j.ended
		j.mu.Unlock()
		if !ended {
			j.cancel()
		}
	}
}

// Prune 清理已结束任务，保留最近 keep 条。
func (m *Manager) Prune(keep int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.jobs) <= keep {
		return
	}
	type item struct {
		id string
		at int64
	}
	finished := make([]item, 0, len(m.jobs))
	for id, j := range m.jobs {
		j.mu.Lock()
		ended := j.ended
		at := j.info.EndedAt
		j.mu.Unlock()
		if ended {
			finished = append(finished, item{id: id, at: at})
		}
	}
	if len(finished) <= keep {
		return
	}
	sort.Slice(finished, func(i, k int) bool { return finished[i].at > finished[k].at })
	for _, it := range finished[keep:] {
		delete(m.jobs, it.id)
	}
}

// ticker 周期性推送进度与批量日志。
func (m *Manager) ticker() {
	t := time.NewTicker(progressInterval)
	defer t.Stop()
	logTick := time.NewTicker(logFlushInterval)
	defer logTick.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-t.C:
			m.flushProgress()
		case <-logTick.C:
			m.flushAllLogs()
		}
	}
}

func (m *Manager) flushProgress() {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	for _, j := range jobs {
		j.mu.Lock()
		if !j.dirty {
			j.mu.Unlock()
			continue
		}
		j.dirty = false
		info := j.info
		j.mu.Unlock()
		if info.Status == domain.JobRunning || info.Status == domain.JobQueued {
			m.emit(EventProgress, info)
		}
	}
}

func (m *Manager) flushAllLogs() {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	for _, j := range jobs {
		m.flushLogs(j, false)
	}
}

func (m *Manager) flushLogs(j *Job, force bool) {
	j.mu.Lock()
	if len(j.pending) == 0 || (!force && len(j.pending) < maxBufferedLogs) {
		j.mu.Unlock()
		return
	}
	lines := j.pending
	j.pending = nil
	j.mu.Unlock()
	m.emit(EventLog, map[string]any{"jobId": j.id, "lines": lines})
}

func (m *Manager) emit(event string, payload any) {
	if m.sink != nil {
		m.sink(event, payload)
	}
}

// dispatchHook 把任务日志转写到应用日志。
func (m *Manager) dispatchHook(jobID, kind, level, source, message string) {
	m.hookMu.Lock()
	hook := m.logHook
	m.hookMu.Unlock()
	if hook != nil {
		hook(jobID, kind, level, source, message)
	}
}

// withRecover 执行 fn 并捕获 panic；onPanic 在发生 panic 时被调用。
func withRecover(fn func(), onPanic func(r any, stack []byte)) (recovered bool) {
	defer func() {
		if r := recover(); r != nil {
			recovered = true
			onPanic(r, debug.Stack())
		}
	}()
	fn()
	return false
}

func truncate(b []byte) string {
	const max = 2048
	if len(b) > max {
		return string(b[:max]) + "\n…（堆栈已截断）"
	}
	return string(b)
}

func toAppError(err error) *domain.AppError {
	var ae *domain.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return domain.Wrap(domain.CodeUnknown, "任务执行失败", err)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// KeyedMutex 提供"按资源键互斥"，用于保证同一个 SDK 根目录/AVD 目录不会被并发写。
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// genID 生成任务 ID（时间前缀 + 序号，便于日志排序阅读）。
func genID(seq int64) string {
	return time.Now().Format("150405") + "-" + itoa64(seq)
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// NewKeyedMutex 创建按资源键互斥的锁集合。
func NewKeyedMutex() *KeyedMutex {
	return &KeyedMutex{locks: map[string]*sync.Mutex{}}
}

// TryLock 尝试获取键对应的锁；已被占用时返回 false（不阻塞）。
func (k *KeyedMutex) TryLock(key string) (unlock func(), ok bool) {
	k.mu.Lock()
	l, exists := k.locks[key]
	if !exists {
		l = &sync.Mutex{}
		k.locks[key] = l
	}
	k.mu.Unlock()

	if !l.TryLock() {
		return nil, false
	}
	return l.Unlock, true
}

// Lock 阻塞获取锁。
func (k *KeyedMutex) Lock(key string) (unlock func()) {
	k.mu.Lock()
	l, exists := k.locks[key]
	if !exists {
		l = &sync.Mutex{}
		k.locks[key] = l
	}
	k.mu.Unlock()
	l.Lock()
	return l.Unlock
}
