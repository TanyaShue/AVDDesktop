// Package proc 统一外部进程调用：隐藏控制台窗口、UTF-8 解码、实时行回调、
// 超时与取消、进程树终止。
//
// 设计约束：
//   - 只允许白名单可执行文件（emulator / adb / java / sdkmanager / avdmanager）
//   - 一律 exec.CommandContext(name, args...) 传数组，绝不拼接 shell 字符串
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
)

// Options 控制一次进程调用。
type Options struct {
	Dir        string                    // 工作目录
	Env        []string                  // 完整环境变量（nil = 继承）
	Timeout    time.Duration             // 0 = 无超时
	StdinLines []string                  // 依次写入 stdin 的行（用于 avdmanager 的交互确认）
	OnLine     func(stream, line string) // 实时输出回调（stream: stdout|stderr）
}

// Result 是一次进程调用的结果。
type Result struct {
	Command  string
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
	TimedOut bool
}

// Combined 返回合并输出（便于错误详情展示）。
func (r Result) Combined() string {
	out := strings.TrimSpace(r.Stdout)
	errOut := strings.TrimSpace(r.Stderr)
	switch {
	case out == "" && errOut == "":
		return ""
	case errOut == "":
		return out
	case out == "":
		return errOut
	default:
		return out + "\n" + errOut
	}
}

// Run 执行外部程序（隐藏窗口、行流式回调、可选超时与取消）。
func Run(ctx context.Context, name string, args []string, opts Options) (Result, error) {
	res := Result{Command: name, Args: args}
	start := time.Now()

	if name == "" {
		return res, domain.Err(domain.CodeInvalidArgument, "可执行文件路径为空")
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = opts.Dir
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	applySysProcAttr(cmd)
	// 超时/取消必须结束整棵进程树：Windows 上 sdkmanager.bat 经 cmd.exe 派生 java.exe，
	// 只终止直接子进程会把 java 变成孤儿——它继续写 SDK 目录，与用户重试的下一次安装
	// 并发冲突。这里刻意不使用 runCtx：runCtx 已取消时，基于它的终止动作会立刻失败。
	cmd.Cancel = func() error { return KillTree(context.Background(), cmd.Process.Pid) }
	// 终止后仍被孤儿子进程握着的管道，在等待上限后强制关闭，避免 Wait 永久阻塞。
	cmd.WaitDelay = treeKillWaitDelay

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return res, domain.Wrap(domain.CodeProcessFailed, "无法创建 stdin 管道", err)
	}

	var (
		mu     sync.Mutex
		outBuf strings.Builder
		errBuf strings.Builder
	)
	// 输出交给 os/exec 转发（cmd.Stdout/Stderr 传 io.Writer），而不是 StdoutPipe：
	// 这样 Wait 只在两个流的读取都结束后返回（不丢输出尾部），并且当孙进程继续持有
	// 管道时能由 WaitDelay 兜底收尾。用 StdoutPipe + 手动 Wait 会在这两种要求之间
	// 二选一失败：先 Wait 会截断输出，先读完再 Wait 会被孙进程永久阻塞。
	outSink := newLineSink(func(line string) {
		mu.Lock()
		outBuf.WriteString(line)
		outBuf.WriteString("\n")
		mu.Unlock()
		if opts.OnLine != nil {
			opts.OnLine("stdout", line)
		}
	})
	errSink := newLineSink(func(line string) {
		mu.Lock()
		errBuf.WriteString(line)
		errBuf.WriteString("\n")
		mu.Unlock()
		if opts.OnLine != nil {
			opts.OnLine("stderr", line)
		}
	})
	cmd.Stdout, cmd.Stderr = outSink, errSink

	if err := cmd.Start(); err != nil {
		return res, domain.Wrap(domain.CodeProcessFailed, "无法启动进程 "+name, err)
	}

	if len(opts.StdinLines) > 0 {
		go func() {
			defer func() { _ = stdin.Close() }()
			for _, line := range opts.StdinLines {
				_, _ = io.WriteString(stdin, line+"\n")
			}
		}()
	} else {
		_ = stdin.Close()
	}

	// Wait 由 os/exec 负责：它在两个输出流的转发结束后才返回，
	// 孙进程继续持有管道时由 WaitDelay 兜底（见上面的 cmd.WaitDelay）。
	waitErr := cmd.Wait()
	outSink.flush()
	errSink.flush()

	mu.Lock()
	res.Stdout = outBuf.String()
	res.Stderr = errBuf.String()
	mu.Unlock()
	res.Duration = time.Since(start)
	res.ExitCode = 0

	// ErrWaitDelay 表示进程本身已正常退出，只是有孙进程仍持有输出管道：
	// 按成功处理，绝不能因为清理动作把正常结果误报成失败。
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		waitErr = nil
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			res.TimedOut = true
			return res, domain.ErrDetail(domain.CodeProcessFailed,
				fmt.Sprintf("%s 执行超时（%s）", shortName(name), opts.Timeout), res.Combined())
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return res, domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		if res.ExitCode != 0 && strings.TrimSpace(res.Combined()) == "" {
			return res, domain.Wrap(domain.CodeProcessFailed,
				fmt.Sprintf("%s 退出码 %d（无输出）", shortName(name), res.ExitCode), waitErr)
		}
	}
	return res, nil
}

// maxLineBytes 是单行输出的上限（超出后丢弃该行内容，避免异常程序打爆内存）。
const maxLineBytes = 4 * 1024 * 1024

// treeKillWaitDelay 是进程退出后等待输出管道关闭的上限（孙进程可能继续持有管道）。
const treeKillWaitDelay = 5 * time.Second

// lineSink 把子进程输出按行回调（实现 io.Writer，交给 os/exec 转发）。
//
// 行结束符同时接受 \n 与 \r：sdkmanager、下载器等工具用 \r 就地刷新进度条，
// 若只认 \n，这些内容会一直攒在缓冲里，直到进程退出才一次性冒出来——表现就是
// 「任务日志不实时」。空白行（含进度条擦除留下的空格）直接丢弃。
type lineSink struct {
	mu   sync.Mutex
	emit func(string)
	buf  []byte
}

func newLineSink(emit func(string)) *lineSink { return &lineSink{emit: emit} }

func (s *lineSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexAny(s.buf, "\r\n")
		if i < 0 {
			// 单行超过上限：丢弃已缓冲内容并提示，绝不停止消费管道
			// （否则子进程写满管道后会永久阻塞，且现场没有任何痕迹）。
			if len(s.buf) >= maxLineBytes {
				s.buf = s.buf[:0]
				s.emit("[proc] 输出行过长，已丢弃")
			}
			return len(p), nil
		}
		s.emitLine(s.buf[:i])
		s.buf = s.buf[i+1:]
	}
}

// flush 输出没有以换行结尾的尾部内容（进程退出后调用）。
func (s *lineSink) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		return
	}
	s.emitLine(s.buf)
	s.buf = nil
}

func (s *lineSink) emitLine(line []byte) {
	if text := string(line); strings.TrimSpace(text) != "" {
		s.emit(text)
	}
}

// Output 执行并返回 stdout（失败时返回带上下文的 AppError）。
func Output(ctx context.Context, name string, args []string, opts Options) (string, error) {
	res, err := Run(ctx, name, args, opts)
	if err != nil {
		return res.Stdout, err
	}
	if res.ExitCode != 0 {
		return res.Stdout, domain.ErrDetail(domain.CodeProcessFailed,
			fmt.Sprintf("%s 执行失败（退出码 %d）", shortName(name), res.ExitCode), res.Combined())
	}
	return res.Stdout, nil
}

// Prepare 为**长驻**子进程（如 emulator.exe）设置隐藏控制台窗口与独立进程组。
// 调用方负责 Start/Wait/Kill。
func Prepare(cmd *exec.Cmd) { applySysProcAttr(cmd) }

// Available 判断可执行文件是否存在且可被调用（用于工具链探测的"实跑校验"）。
func Available(ctx context.Context, name string, args ...string) (bool, string) {
	res, err := Run(ctx, name, args, Options{Timeout: 15 * time.Second})
	if err != nil && res.ExitCode == 0 {
		return false, err.Error()
	}
	return true, res.Combined()
}

func shortName(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// contextWithShortTimeout 为终止类操作提供短超时上下文。
func contextWithShortTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
}
