// Package launch 管理模拟器实例进程：端口分配、启动参数、状态机、日志与停止。
//
// 设计要点（见 ARCHITECTURE.md §6.5）：
//   - 端口从 5554 开始步长 2 分配，同时探测 console 与 adb 端口是否空闲
//   - 状态机：starting → booting → running → stopping → stopped/failed
//   - 停止优先走 `adb emu kill`（优雅退出，能保存快照），失败再终止进程树
package launch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/adb"
	"AVDDesktop/internal/avd/store"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// 端口范围（Android 模拟器约定：console 偶数端口，adb 为 +1）。
const (
	firstPort = 5554
	lastPort  = 5680
)

// Launcher 管理所有模拟器实例。
type Launcher struct {
	Paths platform.InstallPaths
	Env   []string
	Store *store.Store
	Adb   *adb.Client

	sink func(event string, payload any)

	mu        sync.Mutex
	instances map[string]*instance
	seq       int64
}

type instance struct {
	info    domain.EmulatorInstance
	cmd     *exec.Cmd
	done    chan struct{}
	logs    []domain.LogLine
	logsMu  sync.Mutex
	stopped bool
}

// EventState 是实例状态变化事件名。
const EventState = "emulator:state"

// New 创建启动器。
func New(paths platform.InstallPaths, env []string, st *store.Store, adbClient *adb.Client, sink func(string, any)) *Launcher {
	return &Launcher{
		Paths:     paths,
		Env:       env,
		Store:     st,
		Adb:       adbClient,
		sink:      sink,
		instances: map[string]*instance{},
	}
}

// BuildArgs 组装 emulator 启动参数（纯函数，便于测试与"查看等效命令"）。
func BuildArgs(avdName string, opts domain.LaunchOptions) []string {
	args := []string{"-avd", avdName}
	port := opts.Port
	if port > 0 {
		args = append(args, "-port", strconv.Itoa(port))
	}
	if opts.ColdBoot {
		args = append(args, "-no-snapshot-load")
	}
	if opts.WipeData {
		args = append(args, "-wipe-data")
	}
	if opts.NoWindow {
		args = append(args, "-no-window")
	}
	if opts.NoAudio {
		args = append(args, "-no-audio")
	}
	if opts.NoBootAnim {
		args = append(args, "-no-boot-anim")
	}
	if opts.WritableSystem {
		args = append(args, "-writable-system")
	}
	if opts.GPUMode != "" && opts.GPUMode != "auto" {
		args = append(args, "-gpu", opts.GPUMode)
	}
	if opts.SnapshotName != "" && !opts.ColdBoot {
		args = append(args, "-snapshot", opts.SnapshotName)
	}
	if opts.MemoryMB > 0 {
		args = append(args, "-memory", strconv.Itoa(opts.MemoryMB))
	}
	if opts.Cores > 0 {
		args = append(args, "-cores", strconv.Itoa(opts.Cores))
	}
	if opts.NetSpeed != "" && opts.NetSpeed != "full" {
		args = append(args, "-netspeed", opts.NetSpeed)
	}
	if opts.NetDelay != "" && opts.NetDelay != "none" {
		args = append(args, "-netdelay", opts.NetDelay)
	}
	if len(opts.DNSServers) > 0 {
		args = append(args, "-dns-server", strings.Join(opts.DNSServers, ","))
	}
	if opts.HTTPProxy != "" {
		args = append(args, "-http-proxy", opts.HTTPProxy)
	}
	if opts.Timezone != "" {
		args = append(args, "-timezone", opts.Timezone)
	}
	if opts.Locale != "" {
		args = append(args, "-change-locale", opts.Locale)
	}
	if opts.Scale != "" {
		args = append(args, "-scale", opts.Scale)
	}
	args = append(args, opts.ExtraArgs...)
	return args
}

// AllocatePort 返回一对空闲的 console/adb 端口。
func (l *Launcher) AllocatePort() (int, error) {
	l.mu.Lock()
	used := map[int]bool{}
	for _, inst := range l.instances {
		used[inst.info.Port] = true
	}
	l.mu.Unlock()

	for port := firstPort; port <= lastPort; port += 2 {
		if used[port] {
			continue
		}
		if isPortFree(port) && isPortFree(port+1) {
			return port, nil
		}
	}
	return 0, domain.Err(domain.CodePortExhausted, "没有可用的模拟器端口（5554-5680 已用尽）").
		WithHint("请先关闭一些运行中的模拟器实例")
}

func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// Start 启动一个 AVD 实例（立即返回，启动过程由后台协程推进状态）。
func (l *Launcher) Start(ctx context.Context, avdName string, opts domain.LaunchOptions) (*domain.EmulatorInstance, error) {
	if !platform.FileExists(l.Paths.EmulatorExe) {
		return nil, domain.Err(domain.CodeToolMissing, "未安装模拟器（emulator）").
			WithAction("install", "安装模拟器", "emulator")
	}
	if l.Store != nil && !l.Store.Exists(avdName) {
		return nil, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+avdName)
	}

	l.mu.Lock()
	for _, inst := range l.instances {
		if inst.info.AvdName == avdName && !inst.stopped {
			l.mu.Unlock()
			return nil, domain.ErrDetail(domain.CodeFileInUse,
				"该设备已经有一个实例在运行", inst.info.Serial).
				WithHint("同一个 AVD 不能同时启动两次；如需多开请先克隆设备")
		}
	}
	l.mu.Unlock()

	port, err := l.AllocatePort()
	if err != nil {
		return nil, err
	}
	if opts.Port > 0 {
		port = opts.Port
	}
	opts.Port = port

	args := BuildArgs(avdName, opts)
	cmd := exec.Command(l.Paths.EmulatorExe, args...)
	cmd.Dir = l.Paths.Emulator
	cmd.Env = l.Env
	proc.Prepare(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法捕获模拟器输出", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法捕获模拟器错误输出", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法启动模拟器", err).
			WithHint("请确认模拟器未被杀毒软件拦截，且路径没有特殊字符")
	}

	l.seq++
	serial := adb.SerialForPort(port)
	inst := &instance{
		info: domain.EmulatorInstance{
			ID:        fmt.Sprintf("emu-%d-%d", port, l.seq),
			AvdName:   avdName,
			Serial:    serial,
			Port:      port,
			ADBPort:   port + 1,
			PID:       cmd.Process.Pid,
			State:     domain.AvdStarting,
			StartedAt: platform.NowMs(),
			Args:      args,
			LogsPath:  filepath.Join(l.Paths.SdkRoot, "emulator", "avddesktop-logs"),
		},
		cmd:  cmd,
		done: make(chan struct{}),
	}

	l.mu.Lock()
	l.instances[inst.info.ID] = inst
	l.mu.Unlock()

	go l.capture(inst, stdout)
	go l.capture(inst, stderr)
	go l.monitor(ctx, inst, cmd)

	if l.Store != nil {
		l.Store.TouchLastUsed(avdName)
	}
	l.emit(inst.Snapshot())
	info := inst.Snapshot()
	return &info, nil
}

// capture 持续读取模拟器输出。
func (l *Launcher) capture(inst *instance, r interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := domain.LogLine{At: platform.NowMs(), Level: guessLevel(line), Source: "emulator", Message: line}
		inst.logsMu.Lock()
		inst.logs = append(inst.logs, entry)
		if len(inst.logs) > 2000 {
			inst.logs = inst.logs[len(inst.logs)-2000:]
		}
		inst.logsMu.Unlock()
		if l.sink != nil {
			l.sink("emulator:log", map[string]any{"instanceId": inst.info.ID, "line": entry})
		}
	}
}

// monitor 推进状态机：starting → booting → running → stopped/failed。
func (l *Launcher) monitor(ctx context.Context, inst *instance, cmd *exec.Cmd) {
	defer close(inst.done)

	if l.Adb != nil {
		l.setState(inst, domain.AvdBooting, "")
		if err := l.Adb.WaitForDevice(ctx, inst.info.Serial, 5*time.Minute); err == nil {
			if err := l.Adb.WaitForBoot(ctx, inst.info.Serial, 10*time.Minute); err == nil {
				inst.info.BootCompletedAt = platform.NowMs()
				l.setState(inst, domain.AvdRunning, "")
			}
		}
	} else {
		l.setState(inst, domain.AvdRunning, "")
	}

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	inst.info.ExitCode = &exitCode
	inst.stopped = true

	if exitCode == 0 {
		l.setState(inst, domain.AvdStopped, "")
		return
	}
	l.setState(inst, domain.AvdError, l.explainExit(inst, exitCode))
}

// explainExit 把退出码翻译为可操作的中文提示。
func (l *Launcher) explainExit(inst *instance, code int) string {
	logs := inst.LogsTail(80)
	joined := strings.ToLower(strings.Join(logLines(logs), "\n"))
	switch {
	case strings.Contains(joined, "accel") || strings.Contains(joined, "hypervisor") || strings.Contains(joined, "whpx"):
		return "模拟器因硬件加速不可用而退出，请检查「虚拟机监控程序平台」是否已开启"
	case strings.Contains(joined, "permission") || strings.Contains(joined, "access is denied"):
		return "权限不足或文件被占用（退出码 " + strconv.Itoa(code) + "）"
	case strings.Contains(joined, "port") && strings.Contains(joined, "in use"):
		return "端口被占用（退出码 " + strconv.Itoa(code) + "）"
	case strings.Contains(joined, "out of memory") || strings.Contains(joined, "cannot allocate"):
		return "宿主内存不足，请降低 AVD 的 RAM 配置"
	default:
		return "模拟器异常退出（退出码 " + strconv.Itoa(code) + "），请查看实例日志"
	}
}

// Stop 停止实例：先尝试 adb emu kill，超时后终止进程树。
func (l *Launcher) Stop(ctx context.Context, instanceID string, force bool) error {
	inst, ok := l.Get(instanceID)
	if !ok {
		return domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	}
	_ = inst
	l.mu.Lock()
	internal := l.instances[instanceID]
	l.mu.Unlock()
	if internal == nil || internal.stopped {
		return nil
	}

	l.setState(internal, domain.AvdStopping, "")
	if !force && l.Adb != nil {
		_ = l.Adb.EmuKill(ctx, internal.info.Serial)
		select {
		case <-internal.done:
			return nil
		case <-time.After(20 * time.Second):
		}
	}
	if internal.cmd != nil && internal.cmd.Process != nil {
		if err := proc.KillTree(ctx, internal.cmd.Process.Pid); err != nil {
			return domain.Wrap(domain.CodeProcessFailed, "无法终止模拟器进程", err)
		}
	}
	select {
	case <-internal.done:
	case <-time.After(15 * time.Second):
	}
	return nil
}

// StopByAvd 按 AVD 名称停止实例。
func (l *Launcher) StopByAvd(ctx context.Context, avdName string, force bool) error {
	for _, inst := range l.List() {
		if inst.AvdName == avdName && inst.State != domain.AvdStopped && inst.State != domain.AvdError {
			return l.Stop(ctx, inst.ID, force)
		}
	}
	return nil
}

// StopAll 停止所有实例（应用退出时调用）。
func (l *Launcher) StopAll(ctx context.Context, force bool) {
	for _, inst := range l.List() {
		if inst.State != domain.AvdStopped && inst.State != domain.AvdError {
			_ = l.Stop(ctx, inst.ID, force)
		}
	}
}

// List 返回所有实例快照。
func (l *Launcher) List() []domain.EmulatorInstance {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]domain.EmulatorInstance, 0, len(l.instances))
	for _, inst := range l.instances {
		out = append(out, inst.Snapshot())
	}
	return out
}

// Get 返回单个实例快照。
func (l *Launcher) Get(instanceID string) (domain.EmulatorInstance, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	inst, ok := l.instances[instanceID]
	if !ok {
		return domain.EmulatorInstance{}, false
	}
	return inst.Snapshot(), true
}

// ByAvd 返回某个 AVD 当前运行的实例。
func (l *Launcher) ByAvd(avdName string) (domain.EmulatorInstance, bool) {
	for _, inst := range l.List() {
		if inst.AvdName == avdName && inst.State != domain.AvdStopped && inst.State != domain.AvdError {
			return inst, true
		}
	}
	return domain.EmulatorInstance{}, false
}

// Logs 返回实例日志。
func (l *Launcher) Logs(instanceID string, tail int) []domain.LogLine {
	l.mu.Lock()
	inst := l.instances[instanceID]
	l.mu.Unlock()
	if inst == nil {
		return nil
	}
	return inst.LogsTail(tail)
}

// Prune 清理已结束的实例记录。
func (l *Launcher) Prune(keep int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.instances) <= keep {
		return
	}
	type item struct {
		id string
		at int64
	}
	var finished []item
	for id, inst := range l.instances {
		if inst.stopped {
			finished = append(finished, item{id: id, at: inst.info.StartedAt})
		}
	}
	if len(finished) <= keep {
		return
	}
	for i := 1; i < len(finished); i++ {
		for j := i; j > 0 && finished[j].at > finished[j-1].at; j-- {
			finished[j], finished[j-1] = finished[j-1], finished[j]
		}
	}
	for _, it := range finished[keep:] {
		delete(l.instances, it.id)
	}
}

func (l *Launcher) setState(inst *instance, state domain.AvdState, errMsg string) {
	l.mu.Lock()
	inst.info.State = state
	if errMsg != "" {
		inst.info.LastError = errMsg
	}
	info := inst.info
	l.mu.Unlock()
	l.emit(info)
}

func (l *Launcher) emit(info domain.EmulatorInstance) {
	if l.sink != nil {
		l.sink(EventState, info)
	}
}

func (i *instance) Snapshot() domain.EmulatorInstance {
	i.logsMu.Lock()
	defer i.logsMu.Unlock()
	info := i.info
	info.Args = append([]string(nil), i.info.Args...)
	return info
}

// LogsTail 返回最近 n 条日志。
func (i *instance) LogsTail(n int) []domain.LogLine {
	i.logsMu.Lock()
	defer i.logsMu.Unlock()
	if n <= 0 || n > len(i.logs) {
		n = len(i.logs)
	}
	out := make([]domain.LogLine, n)
	copy(out, i.logs[len(i.logs)-n:])
	return out
}

func logLines(lines []domain.LogLine) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Message)
	}
	return out
}

func guessLevel(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "failed"):
		return "error"
	case strings.Contains(lower, "warn"):
		return "warn"
	default:
		return "info"
	}
}
