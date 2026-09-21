// Package proc 统一外部进程调用：隐藏控制台窗口、UTF-8 解码、实时行回调、
// 超时与取消、进程树终止。
//
// 设计约束（见 ARCHITECTURE.md §12-安全）：
//   - 只允许白名单可执行文件（emulator / adb / java / sdkmanager / avdmanager）
//   - 一律 exec.CommandContext(name, args...) 传数组，绝不拼接 shell 字符串
package proc

import (
	"bufio"
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
	LogTag     string                    // 日志前缀（可选）
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

// Cmdline 返回等价命令行（用于 UI 上的"查看等效命令"）。
func (r Result) Cmdline() string {
	if len(r.Args) == 0 {
		return r.Command
	}
	return r.Command + " " + strings.Join(r.Args, " ")
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

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return res, domain.Wrap(domain.CodeProcessFailed, "无法创建 stdout 管道", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return res, domain.Wrap(domain.CodeProcessFailed, "无法创建 stderr 管道", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return res, domain.Wrap(domain.CodeProcessFailed, "无法创建 stdin 管道", err)
	}

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

	var (
		mu       sync.Mutex
		outBuf   strings.Builder
		errBuf   strings.Builder
		readWait sync.WaitGroup
	)
	readWait.Add(2)
	go func() {
		defer readWait.Done()
		scan := bufio.NewScanner(stdout)
		scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scan.Scan() {
			line := scan.Text()
			mu.Lock()
			outBuf.WriteString(line)
			outBuf.WriteString("\n")
			mu.Unlock()
			if opts.OnLine != nil {
				opts.OnLine("stdout", line)
			}
		}
	}()
	go func() {
		defer readWait.Done()
		scan := bufio.NewScanner(stderr)
		scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scan.Scan() {
			line := scan.Text()
			mu.Lock()
			errBuf.WriteString(line)
			errBuf.WriteString("\n")
			mu.Unlock()
			if opts.OnLine != nil {
				opts.OnLine("stderr", line)
			}
		}
	}()

	waitErr := cmd.Wait()
	readWait.Wait()

	mu.Lock()
	res.Stdout = outBuf.String()
	res.Stderr = errBuf.String()
	mu.Unlock()
	res.Duration = time.Since(start)
	res.ExitCode = 0

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
