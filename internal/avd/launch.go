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
	"os"
	"os/exec"
	"runtime"
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
	stopGraceTimeout                  = 30 * time.Second
	killWaitTimeout                   = 15 * time.Second
	runningProbeInterval              = 2 * time.Second
	deviceLostGrace                   = 3 * time.Second
	ringLimit                         = 200
)

// 自定义 UI 的 gRPC 控制端口范围：只占一个端口（不像 console 那样成对占用 adb），
// 且与 console/adb 端口池分开，避免与模拟器控制台互相抢占。
const (
	defaultFirstGrpcPort, defaultLastGrpcPort = 8554, 8680
)

// adbClient 是启动状态机所需的 adb 能力，便于测试替换。
type adbClient interface {
	WaitForDevice(ctx context.Context, serial string, timeout time.Duration) error
	WaitForBoot(ctx context.Context, serial string, timeout time.Duration) error
	Devices(ctx context.Context) ([]domain.AdbDevice, error)
	EmuKill(ctx context.Context, serial string) error
}

// Launcher 管理模拟器实例：端口分配、启动、状态机与停止。
type Launcher struct {
	tools platform.Tools
	env   []string
	store *Store
	adb   adbClient
	log   logging.Interface
	sink  func(event string, payload any)
	// 端口范围：first/last 为 console（0 表示默认 5554-5680），
	// grpcFirst/grpcLast 为自定义 UI 的 gRPC 控制端口（0 表示默认 8554-8680）。
	first, last         int
	grpcFirst, grpcLast int

	runningProbe  time.Duration
	deviceLostFor time.Duration

	mu        sync.Mutex
	instances map[string]*instance
	ports     map[int]bool // 已占用的 console 端口
	grpcPorts map[int]bool // 已占用的 gRPC 控制端口
	seq       int64

	// procStart 拉起模拟器进程；默认 cmd.Start，测试可替换为不产生真实进程的实现。
	procStart func(*exec.Cmd) error
}

type instance struct {
	info domain.EmulatorInstance
	cmd  *exec.Cmd
	exit chan struct{} // cmd.Wait 返回后关闭；code 只允许在其后读取
	code int

	// grpcPort 是该实例占用的 gRPC 控制端口（0 表示未启用自定义 UI），进程退出时归还。
	grpcPort int

	// stopRequested 表示用户已请求停止（受 Launcher.mu 保护）：之后的非 0 退出记为 stopped 而非 error。
	stopRequested bool
	// deviceLost 表示运行中已观测到设备从 adb 消失：通常来自手动关闭模拟器窗口。
	deviceLost bool

	ringMu sync.Mutex
	ring   []string // 最近若干行原始输出，用于分析退出原因
}

// NewLauncher 创建启动器。log 为 nil 时使用空日志器。
func NewLauncher(tools platform.Tools, env []string, store *Store, adbClient adbClient, sink func(string, any), log logging.Interface) *Launcher {
	return &Launcher{tools: tools, env: env, store: store, adb: adbClient, sink: sink, log: logging.Or(log),
		instances: map[string]*instance{}, ports: map[int]bool{}, grpcPorts: map[int]bool{},
		procStart:    func(cmd *exec.Cmd) error { return cmd.Start() },
		runningProbe: runningProbeInterval, deviceLostFor: deviceLostGrace}
}

// BuildArgs 组装 emulator 命令行参数（纯函数，便于测试与"查看等效命令"）。
//
// 顺序固定：-avd <name> -port <port> [-no-snapshot-load] [-no-window] [-grpc <grpcPort>]。
// 自定义 UI 必须无窗口（否则会出现"有 Qt 窗口 + gRPC 通道"的无意义组合）；grpcPort<=0 时不追加 -grpc。
func BuildArgs(avdName string, opts domain.LaunchOptions, port, grpcPort int) []string {
	args := []string{"-avd", avdName, "-port", strconv.Itoa(port)}
	if opts.ColdBoot {
		args = append(args, "-no-snapshot-load")
	}
	if opts.NoWindow || opts.CustomUI {
		args = append(args, "-no-window")
	}
	if opts.CustomUI && grpcPort > 0 {
		args = append(args, "-grpc", strconv.Itoa(grpcPort))
	}
	return args
}

// AllocatePort 返回一对空闲的 console/adb 端口并占用；端口耗尽时给出明确错误。
func (l *Launcher) AllocatePort() (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.allocatePortLocked()
}

// allocatePortLocked 与 AllocatePort 相同，但要求调用方已持有 l.mu。
func (l *Launcher) allocatePortLocked() (int, error) {
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

// allocateGrpcPortLocked 返回一个空闲的 gRPC 控制端口并占用；要求调用方已持有 l.mu。
//
// 与 console 端口池隔离：gRPC 只占一个端口，且已分配的 console 端口一律跳过，
// 避免自定义 UI 通道与模拟器控制台撞在同一端口上。端口耗尽时给出明确错误。
func (l *Launcher) allocateGrpcPortLocked() (int, error) {
	first, last := l.grpcFirst, l.grpcLast
	if first <= 0 {
		first, last = defaultFirstGrpcPort, defaultLastGrpcPort
	}
	for port := first; port <= last; port++ {
		if l.grpcPorts[port] || l.ports[port] || !isPortFree(port) {
			continue
		}
		l.grpcPorts[port] = true
		return port, nil
	}
	return 0, domain.Err(domain.CodePortExhausted,
		fmt.Sprintf("没有可用的模拟器 gRPC 端口（%d-%d 已用尽）", first, last)).
		WithHint("请先关闭运行中的模拟器实例，或结束占用这些端口的程序")
}

// releaseGrpcPort 归还 gRPC 控制端口。
func (l *Launcher) releaseGrpcPort(port int) {
	l.mu.Lock()
	delete(l.grpcPorts, port)
	l.mu.Unlock()
}

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
//
// "该 AVD 是否已在运行"的检查与实例登记必须在同一临界区内完成：先占位再启动进程，
// 否则进程创建（可达数百毫秒）期间第二个并发 Start 会通过检查，对同一个 AVD
// 拉起两个 emulator 实例并发读写同一份磁盘镜像。
func (l *Launcher) Start(ctx context.Context, avdName string, opts domain.LaunchOptions) (*domain.EmulatorInstance, error) {
	if !platform.FileExists(l.tools.Emulator) {
		return nil, domain.Err(domain.CodeToolMissing, "未安装模拟器（emulator）").
			WithAction("install", "安装模拟器", "emulator")
	}
	if l.store != nil && !l.store.Exists(avdName) {
		return nil, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+avdName)
	}
	l.mu.Lock()
	for _, live := range l.instances {
		if live.info.AvdName == avdName && !processExited(live.exit) {
			l.mu.Unlock()
			return nil, domain.ErrDetail(domain.CodeFileInUse, "该设备已经有一个实例在运行", live.info.Serial).
				WithHint("同一个 AVD 不能同时启动两次；请先停止正在运行的实例")
		}
	}
	port, err := l.allocatePortLocked()
	if err != nil {
		l.mu.Unlock()
		return nil, err
	}
	// 自定义 UI 需要一个独立的 gRPC 控制端口；分配失败时要把已占用的 console 端口一并归还。
	grpcPort := 0
	if opts.CustomUI {
		if grpcPort, err = l.allocateGrpcPortLocked(); err != nil {
			delete(l.ports, port)
			l.mu.Unlock()
			return nil, err
		}
	}
	l.seq++
	inst := &instance{exit: make(chan struct{}), grpcPort: grpcPort, info: domain.EmulatorInstance{
		ID: fmt.Sprintf("emu-%d-%d", port, l.seq), AvdName: avdName, Serial: adb.SerialForPort(port),
		Port: port, State: domain.AvdStarting, StartedAt: platform.NowMs(),
		CustomUI: opts.CustomUI, GrpcPort: grpcPort,
	}}
	l.instances[inst.info.ID] = inst
	l.mu.Unlock()

	args := BuildArgs(avdName, opts, port, grpcPort)
	cmd := exec.Command(l.tools.Emulator, args...)
	cmd.Dir, cmd.Env = l.tools.EmulatorDir, l.env
	proc.Prepare(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := l.procStart(cmd); err != nil {
		l.mu.Lock()
		delete(l.instances, inst.info.ID)
		delete(l.ports, port)
		delete(l.grpcPorts, grpcPort)
		l.mu.Unlock()
		_ = pw.Close()
		_ = pr.Close()
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法启动模拟器", err).
			WithHint("请确认模拟器未被杀毒软件拦截、路径没有特殊字符，且当前账户有执行权限")
	}

	l.mu.Lock()
	inst.cmd = cmd
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
		inst.info.PID = pid
	}
	inst.info.Args = args
	// 快照必须在锁内复制：monitor 随后会并发写 inst.info.State。
	snapshot := inst.info
	l.mu.Unlock()

	var captureDone sync.WaitGroup
	captureDone.Add(1)
	go func() {
		defer captureDone.Done()
		l.capture(inst, pr)
	}()
	go func() {
		err := cmd.Wait()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			inst.code = exitErr.ExitCode()
		} else if err != nil {
			inst.code = -1
		}
		_ = pw.Close()
		captureDone.Wait() // 先让 capture 读完管道，保证关闭 exit 时内存环已完整
		close(inst.exit)
	}()
	go l.monitor(ctx, inst)
	l.log.Info("emulator", "已启动 %s：pid=%d serial=%s port=%d\n  参数：%s %s", avdName,
		pid, inst.info.Serial, port, l.tools.Emulator, strings.Join(args, " "))
	return &snapshot, nil
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
		if l.adb != nil {
			l.watchRunningDevice(waitCtx, inst)
		}
	case !processExited(inst.exit) && ctx.Err() == nil: // 进程仍在运行：等待失败或超时（应用退出导致的取消不算失败）
		l.setState(inst, domain.AvdError, err.Error())
	}
	<-inst.exit
	l.releasePort(inst.info.Port)
	if inst.grpcPort > 0 {
		l.releaseGrpcPort(inst.grpcPort)
	}
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
	l.mu.Lock()
	requested := inst.stopRequested
	deviceLost := inst.deviceLost
	l.mu.Unlock()
	if requested {
		l.clearLastError(inst)
		l.setState(inst, domain.AvdStopped, "")
		l.log.Info("emulator", "%s（%s）已按用户请求停止", inst.info.AvdName, inst.info.Serial)
		return
	}
	if deviceLost {
		const message = "模拟器窗口已关闭，或设备已从 adb 断开"
		l.setState(inst, domain.AvdStopped, message)
		l.log.Info("emulator", "%s（%s）已停止：%s", inst.info.AvdName, inst.info.Serial, message)
		return
	}
	if message := normalShutdownMessage(logText); message != "" {
		l.setState(inst, domain.AvdStopped, message)
		l.log.Info("emulator", "%s（%s）已停止：%s", inst.info.AvdName, inst.info.Serial, message)
		return
	}
	l.setState(inst, domain.AvdError, reason)
	l.log.Error("emulator", "%s（%s）异常退出：%s", inst.info.AvdName, inst.info.Serial, reason)
}

// watchRunningDevice 在实例运行期间持续探测 adb 连接。
//
// Windows 上手动关闭模拟器窗口后，emulator.exe 可能还会等待约 20 秒让 QEMU 收尾，
// 父进程因此保持存活。这里一旦观测到设备持续从 adb 消失，就先切到 stopping，
// 避免界面继续显示 running；进程真正退出后再收敛为 stopped。
func (l *Launcher) watchRunningDevice(ctx context.Context, inst *instance) {
	interval := l.runningProbe
	if interval <= 0 {
		interval = runningProbeInterval
	}
	grace := l.deviceLostFor
	if grace <= 0 {
		grace = deviceLostGrace
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var missingSince time.Time
	stopping := false
	for {
		select {
		case <-inst.exit:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		l.mu.Lock()
		requested := inst.stopRequested
		l.mu.Unlock()
		if requested {
			return // 应用内停止流程会自行维护状态，避免短暂重连覆盖 stopping。
		}

		devices, err := l.adb.Devices(ctx)
		if err != nil {
			continue // adb 暂时不可用不等于设备已退出，避免误报。
		}
		present := false
		for _, d := range devices {
			if d.Serial == inst.info.Serial && d.State == "device" {
				present = true
				break
			}
		}
		if present {
			missingSince = time.Time{}
			l.setDeviceLost(inst, false)
			if stopping {
				stopping = false
				l.setState(inst, domain.AvdRunning, "")
				l.log.Info("emulator", "%s 已重新连接 adb，恢复为运行中", inst.info.Serial)
			}
			continue
		}

		if missingSince.IsZero() {
			missingSince = time.Now()
			continue
		}
		if !stopping && time.Since(missingSince) >= grace {
			stopping = true
			l.setDeviceLost(inst, true)
			l.setState(inst, domain.AvdStopping, "")
			l.log.Info("emulator", "%s 已从 adb 断开，等待模拟器进程退出收尾", inst.info.Serial)
		}
	}
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
	var procRef *os.Process
	if inst != nil {
		inst.stopRequested = true // 用户主动停止：之后的非 0 退出不记为 error
		if inst.cmd != nil {
			procRef = inst.cmd.Process
		}
	}
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
	if procRef != nil {
		// 终止进程树。模拟器可能已部分退出，taskkill 的报错不作为失败依据，
		// 以实例是否真正结束（进程退出 + 输出管道关闭）为准。
		if err := proc.KillTree(ctx, procRef.Pid); err != nil {
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
	inst, ok := l.ByAvd(avdName)
	if !ok {
		return nil
	}
	return l.Stop(ctx, inst.ID, force)
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
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, inst := range l.instances {
		// instances 会保留已结束的历史记录；必须按进程是否退出筛选，
		// 不能只看状态，否则同一 AVD 的旧记录可能遮蔽新实例。
		if inst.info.AvdName == avdName && !processExited(inst.exit) {
			return inst.info, true
		}
	}
	return domain.EmulatorInstance{}, false
}

func (l *Launcher) clearLastError(inst *instance) {
	l.mu.Lock()
	inst.info.LastError = ""
	l.mu.Unlock()
}

func (l *Launcher) setDeviceLost(inst *instance, lost bool) {
	l.mu.Lock()
	inst.deviceLost = lost
	l.mu.Unlock()
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

// normalShutdownMessage 识别模拟器自身的正常关闭流程。
//
// 手动关闭窗口、退出应用等场景可能返回非 0 退出码，日志却已经出现快照保存完成或优雅等待，
// 这类退出不应再被归类为硬件加速等启动失败。
func normalShutdownMessage(logText string) string {
	lower := strings.ToLower(logText)
	switch {
	case strings.Contains(lower, "shutdown gracefully before kill"):
		return "模拟器窗口已关闭"
	case strings.Contains(lower, "saving with gfxstream") && strings.Contains(lower, "saving snapshot"):
		return "模拟器已完成快照保存并关闭"
	default:
		return ""
	}
}

// explainExit 按日志关键词把退出码翻译为可操作的中文说明（纯函数，便于测试）。
func explainExit(logText string, code int) string {
	lower := strings.ToLower(logText)
	switch {
	case strings.Contains(lower, "requires hardware acceleration") ||
		strings.Contains(lower, "hardware acceleration is not available") ||
		strings.Contains(lower, "whpx is not installed") ||
		strings.Contains(lower, "haxm is not installed") ||
		strings.Contains(lower, "aehd is not installed"):
		// 加速不可用的排查方式各平台完全不同，必须按宿主平台给建议。
		return fmt.Sprintf("模拟器因硬件加速不可用而退出（退出码 %d）：%s", code, platform.AccelAdvice(runtime.GOOS))
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
