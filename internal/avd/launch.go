// 模拟器实例启动器：端口分配、启动参数、状态机、停止与退出原因分析。
// 每个等待都有上限；模拟器进程提前退出会立即取消等待，绝不空等满超时。
package avd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/adb"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// EventState 是实例状态变化事件名。
const EventState = "emulator:state"

// 端口范围（console 为偶数端口，adb 为 +1）与各阶段等待上限。
const (
	defaultFirstPort, defaultLastPort = 5554, 5680
	deviceWaitTimeout                 = 3 * time.Minute
	bootWaitTimeout                   = 5 * time.Minute
	stopGraceTimeout                  = 20 * time.Second
	killWaitTimeout                   = 15 * time.Second
	ringLimit                         = 200
)

// Launcher 管理模拟器实例：端口分配、启动、状态机与停止。
type Launcher struct {
	tools       platform.Tools
	env         []string
	store       *Store
	adb         *adb.Client
	log         logging.Interface
	sink        func(event string, payload any)
	first, last int // 端口范围，0 表示默认 5554-5680

	mu        sync.Mutex
	instances map[string]*instance
	ports     map[int]bool // 已占用的 console 端口
	seq       int64
}

type instance struct {
	info domain.EmulatorInstance
	cmd  *exec.Cmd
	exit chan struct{} // cmd.Wait 返回后关闭；code 只允许在其后读取
	code int

	ringMu sync.Mutex
	ring   []string // 最近若干行原始输出，用于分析退出原因
}

// NewLauncher 创建启动器。log 为 nil 时使用空日志器。
func NewLauncher(tools platform.Tools, env []string, store *Store, adbClient *adb.Client, sink func(string, any), log logging.Interface) *Launcher {
	return &Launcher{tools: tools, env: env, store: store, adb: adbClient, sink: sink, log: logging.Or(log),
		instances: map[string]*instance{}, ports: map[int]bool{}}
}

// BuildArgs 组装 emulator 命令行参数（纯函数，便于测试与"查看等效命令"）。
func BuildArgs(avdName string, opts domain.LaunchOptions, port int) []string {
	args := []string{"-avd", avdName, "-port", strconv.Itoa(port)}
	if opts.ColdBoot {
		args = append(args, "-no-snapshot-load")
	}
	if opts.NoWindow {
		args = append(args, "-no-window")
	}
	return args
}

// AllocatePort 返回一对空闲的 console/adb 端口并占用；端口耗尽时给出明确错误。
func (l *Launcher) AllocatePort() (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	first, last := l.first, l.last
	if first <= 0 {
		first, last = defaultFirstPort, defaultLastPort
	}
	for port := first; port <= last; port += 2 {
		if !l.ports[port] && isPortFree(port) && isPortFree(port+1) {
			l.ports[port] = true
			return port, nil
		}
	}
	return 0, domain.Err(domain.CodePortExhausted,
		fmt.Sprintf("没有可用的模拟器端口（%d-%d 已用尽）", first, last)).
		WithHint("请先关闭运行中的模拟器实例，或结束占用这些端口的程序")
}

func (l *Launcher) releasePort(port int) { l.mu.Lock(); delete(l.ports, port); l.mu.Unlock() }

// isPortFree 探测本机端口是否可绑定。
func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// Start 启动一个 AVD 实例：立即返回，启动状态由后台协程推进。
func (l *Launcher) Start(ctx context.Context, avdName string, opts domain.LaunchOptions) (*domain.EmulatorInstance, error) {
	if !platform.FileExists(l.tools.Emulator) {
		return nil, domain.Err(domain.CodeToolMissing, "未安装模拟器（emulator）").
			WithAction("install", "安装模拟器", "emulator")
	}
	if l.store != nil && !l.store.Exists(avdName) {
		return nil, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+avdName)
	}
	l.mu.Lock()
	for _, inst := range l.instances {
		if inst.info.AvdName == avdName && !processExited(inst.exit) {
			l.mu.Unlock()
			return nil, domain.ErrDetail(domain.CodeFileInUse, "该设备已经有一个实例在运行", inst.info.Serial).
				WithHint("同一个 AVD 不能同时启动两次；如需多开请先克隆设备")
		}
	}
	l.mu.Unlock()
	port, err := l.AllocatePort()
	if err != nil {
		return nil, err
	}
	args := BuildArgs(avdName, opts, port)
	cmd := exec.Command(l.tools.Emulator, args...)
	cmd.Dir, cmd.Env = l.tools.EmulatorDir, l.env
	proc.Prepare(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		l.releasePort(port)
		_ = pw.Close()
		_ = pr.Close()
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法启动模拟器", err).
			WithHint("请确认模拟器未被杀毒软件拦截、路径没有特殊字符，且当前账户有执行权限")
	}

	l.seq++
	inst := &instance{cmd: cmd, exit: make(chan struct{}), info: domain.EmulatorInstance{
		ID: fmt.Sprintf("emu-%d-%d", port, l.seq), AvdName: avdName, Serial: adb.SerialForPort(port),
		Port: port, PID: cmd.Process.Pid, State: domain.AvdStarting, StartedAt: platform.NowMs(), Args: args,
	}}
	l.mu.Lock()
	l.instances[inst.info.ID] = inst
	l.mu.Unlock()
	go func() {
		err := cmd.Wait()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			inst.code = exitErr.ExitCode()
		} else if err != nil {
			inst.code = -1
		}
		_ = pw.Close()
		close(inst.exit)
	}()
	go l.capture(inst, pr)
	go l.monitor(ctx, inst)
	l.log.Info("emulator", "已启动 %s：pid=%d serial=%s port=%d\n  参数：%s %s", avdName,
		cmd.Process.Pid, inst.info.Serial, port, l.tools.Emulator, strings.Join(args, " "))
	info := inst.info
	return &info, nil
}

// monitor 推进状态机（booting → running/error），并在进程退出后收敛状态。
func (l *Launcher) monitor(ctx context.Context, inst *instance) {
	l.setState(inst, domain.AvdBooting, "")
	waitCtx, cancel := cancelWhenExited(ctx, inst.exit)
	defer cancel()
	started := time.UnixMilli(inst.info.StartedAt)
	var err error
	if l.adb != nil {
		l.log.Info("emulator", "等待 %s 上线（serial=%s，最长 %s）", inst.info.AvdName, inst.info.Serial, deviceWaitTimeout)
		if err = l.adb.WaitForDevice(waitCtx, inst.info.Serial, deviceWaitTimeout); err == nil {
			l.log.Info("emulator", "%s 已连接，等待系统启动完成（最长 %s）", inst.info.Serial, bootWaitTimeout)
			err = l.adb.WaitForBoot(waitCtx, inst.info.Serial, bootWaitTimeout)
		}
	}
	switch {
	case err == nil:
		l.setState(inst, domain.AvdRunning, "")
		l.log.Info("emulator", "%s（%s）已开机完成，耗时 %s", inst.info.AvdName, inst.info.Serial,
			time.Since(started).Round(time.Second))
	case !processExited(inst.exit) && ctx.Err() == nil: // 进程仍在运行：等待失败或超时（应用退出导致的取消不算失败）
		l.setState(inst, domain.AvdError, err.Error())
	}
	<-inst.exit
	l.releasePort(inst.info.Port)
	code := inst.code
	l.mu.Lock()
	inst.info.ExitCode, inst.info.EndedAt = &code, platform.NowMs()
	l.mu.Unlock()
	if code == 0 {
		l.setState(inst, domain.AvdStopped, "")
		l.log.Info("emulator", "%s（%s）已停止", inst.info.AvdName, inst.info.Serial)
		return
	}
	inst.ringMu.Lock()
	logText := strings.ToLower(strings.Join(inst.ring, "\n"))
	inst.ringMu.Unlock()
	reason := explainExit(logText, code)
	l.setState(inst, domain.AvdError, reason)
	l.log.Error("emulator", "%s（%s）异常退出：%s", inst.info.AvdName, inst.info.Serial, reason)
}

// capture 逐行读取模拟器输出：写应用日志（module=emulator）并保留最近输出用于退出分析。
func (l *Launcher) capture(inst *instance, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		inst.ringMu.Lock()
		inst.ring = append(inst.ring, line)
		if len(inst.ring) > ringLimit {
			inst.ring = inst.ring[len(inst.ring)-ringLimit:]
		}
		inst.ringMu.Unlock()
		l.log.Info("emulator", "[%s] %s", inst.info.AvdName, line)
	}
}

// Stop 停止实例：优先 `adb emu kill`（有界），失败再终止进程树；全过程有界。
func (l *Launcher) Stop(ctx context.Context, instanceID string, force bool) error {
	l.mu.Lock()
	inst := l.instances[instanceID]
	l.mu.Unlock()
	switch {
	case inst == nil:
		return domain.Err(domain.CodeInvalidArgument, "实例不存在: "+instanceID)
	case processExited(inst.exit):
		return nil
	}
	l.setState(inst, domain.AvdStopping, "")
	if !force && l.adb != nil {
		_ = l.adb.EmuKill(ctx, inst.info.Serial)
		if waitClosed(inst.exit, stopGraceTimeout) {
			return nil
		}
		l.log.Warn("emulator", "%s 未在 %s 内优雅退出，改为终止进程树", inst.info.Serial, stopGraceTimeout)
	}
	if inst.cmd != nil && inst.cmd.Process != nil {
		// 终止进程树。模拟器可能已部分退出，taskkill 的报错不作为失败依据，
		// 以实例是否真正结束（进程退出 + 输出管道关闭）为准。
		if err := proc.KillTree(ctx, inst.cmd.Process.Pid); err != nil {
			l.log.Warn("emulator", "终止 %s 的进程树返回错误（继续等待退出）：%v", inst.info.Serial, err)
		}
	}
	if !waitClosed(inst.exit, killWaitTimeout) {
		return domain.ErrDetail(domain.CodeProcessFailed, "模拟器进程未在预期时间内退出", inst.info.Serial).
			WithHint("请在任务管理器中结束 emulator 进程后重试")
	}
	return nil
}

// StopByAvd 按 AVD 名称停止实例；StopAll 停止所有实例（应用退出时调用）。
func (l *Launcher) StopByAvd(ctx context.Context, avdName string, force bool) error {
	for _, inst := range l.List() {
		if inst.AvdName == avdName {
			return l.Stop(ctx, inst.ID, force)
		}
	}
	return nil
}

func (l *Launcher) StopAll(ctx context.Context, force bool) {
	for _, inst := range l.List() {
		_ = l.Stop(ctx, inst.ID, force)
	}
}

// List 返回所有实例快照。
func (l *Launcher) List() []domain.EmulatorInstance {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]domain.EmulatorInstance, 0, len(l.instances))
	for _, inst := range l.instances {
		out = append(out, inst.info)
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
	return inst.info, true
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

func (l *Launcher) setState(inst *instance, state domain.AvdState, errMsg string) {
	l.mu.Lock()
	inst.info.State = state
	if errMsg != "" {
		inst.info.LastError = errMsg
	}
	info := inst.info
	l.mu.Unlock()
	if l.sink != nil {
		l.sink(EventState, info)
	}
}

// cancelWhenExited 返回一个「进程退出即取消」的上下文。
//
// 这是"进程提前退出必须立刻中断等待"的核心逻辑：没有它，状态机只能等满超时。
func cancelWhenExited(ctx context.Context, exit <-chan struct{}) (context.Context, context.CancelFunc) {
	waitCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-exit:
		case <-waitCtx.Done():
		}
		cancel()
	}()
	return waitCtx, cancel
}

// processExited 判断进程是否已经退出（不阻塞）。
func processExited(exit <-chan struct{}) bool {
	select {
	case <-exit:
		return true
	default:
		return false
	}
}

// waitClosed 等待 channel 关闭，最多等 timeout。
func waitClosed(ch <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// explainExit 按日志关键词把退出码翻译为可操作的中文说明（纯函数，便于测试）。
func explainExit(logText string, code int) string {
	lower := strings.ToLower(logText)
	switch {
	case strings.Contains(lower, "accel") || strings.Contains(lower, "hypervisor") || strings.Contains(lower, "whpx"):
		return fmt.Sprintf("模拟器因硬件加速不可用而退出（退出码 %d），请检查「虚拟机监控程序平台」是否已在 Windows 功能中开启", code)
	case strings.Contains(lower, "permission") || strings.Contains(lower, "access is denied"):
		return fmt.Sprintf("权限不足或文件被占用（退出码 %d），请检查杀毒软件拦截或改用管理员身份运行", code)
	case strings.Contains(lower, "port") && strings.Contains(lower, "in use"):
		return fmt.Sprintf("端口被占用（退出码 %d），请关闭占用 5554-5681 的程序后重试", code)
	case strings.Contains(lower, "out of memory") || strings.Contains(lower, "cannot allocate"):
		return fmt.Sprintf("宿主内存不足（退出码 %d），请降低 AVD 的 RAM 配置", code)
	default:
		return fmt.Sprintf("模拟器异常退出（退出码 %d），详情见应用日志（模块 emulator）", code)
	}
}
