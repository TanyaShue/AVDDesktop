package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
)

// procTestHelper 是子进程入口：以当前测试二进制自身作为被测外部程序，
// 这样跨平台（不依赖 sh / cmd）且行为完全可控。
func procTestHelper(t *testing.T) {
	mode := os.Getenv("PROC_TEST_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "progress":
		// 典型下载器行为：用 \r 就地刷新同一行，长时间不换行。
		_, _ = os.Stdout.WriteString("第一行\r")
		time.Sleep(1500 * time.Millisecond)
		_, _ = os.Stdout.WriteString("第二行\n")
	case "crlf":
		_, _ = os.Stdout.WriteString("a\r\n\r\nb\n")
	case "tree-parent":
		// 模拟 sdkmanager.bat 经 cmd.exe 派生 java.exe：父进程拉起孙进程后自己保持存活，
		// 孙进程继承 stdout 管道并持续写心跳文件（用于判断它是否真的被终止）。
		child := exec.Command(os.Args[0], "-test.run=TestRunKillsProcessTreeOnCancel")
		child.Env = append(os.Environ(), "PROC_TEST_HELPER=tree-child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		time.Sleep(120 * time.Second)
	case "tree-child":
		beat := os.Getenv("PROC_TEST_TREE_BEAT")
		if pidFile := os.Getenv("PROC_TEST_TREE_PIDFILE"); pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
		}
		for {
			f, err := os.OpenFile(beat, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				os.Exit(1)
			}
			_, _ = f.WriteString("x")
			_ = f.Close()
			time.Sleep(100 * time.Millisecond)
		}
	case "orphan-pipe":
		// 模拟「直接子进程退出、孙进程仍持有输出管道」：工具（如 .bat 包装器或
		// emulator 的辅助进程）派生后台进程后立即返回，后台进程继承 stdout。
		child := exec.Command(os.Args[0], "-test.run=TestRunReturnsWhenGrandchildHoldsPipe")
		child.Env = append(os.Environ(), "PROC_TEST_HELPER=hold-pipe")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		os.Exit(0) // 父进程立刻退出，不等孙进程
	case "hold-pipe":
		// 记录 PID 供测试收尾清理（否则这个进程会持有测试二进制直到超时）。
		if pidFile := os.Getenv("PROC_TEST_TREE_PIDFILE"); pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
		}
		time.Sleep(120 * time.Second)
	}
	os.Exit(0)
}

// TestRunStreamsCarriageReturnOutput 验证以 \r 结尾的输出会被实时回调。
//
// 若按 \n 切分，\r 内容会一直攒在缓冲里直到进程退出才一次性出现，
// 表现就是「任务日志不实时更新」。
func TestRunStreamsCarriageReturnOutput(t *testing.T) {
	helperMode(t, "progress")

	var (
		mu    sync.Mutex
		lines []string
	)
	firstLine := make(chan struct{})
	done := make(chan Result, 1)

	go func() {
		res, _ := Run(context.Background(), os.Args[0],
			[]string{"-test.run=TestRunStreamsCarriageReturnOutput"}, Options{
				Env:     append(os.Environ(), "PROC_TEST_HELPER=progress"),
				Timeout: 30 * time.Second,
				OnLine: func(stream, line string) {
					mu.Lock()
					lines = append(lines, line)
					n := len(lines)
					mu.Unlock()
					if n == 1 {
						close(firstLine)
					}
				},
			})
		done <- res
	}()

	select {
	case <-firstLine:
		// 进程尚未退出就收到了第一行：实时性达标。
	case <-time.After(1200 * time.Millisecond):
		t.Fatal("1.2s 内没有收到以 \\r 结尾的输出行：输出被缓冲到进程结束（按 \\n 切分的症状）")
	}

	res := <-done
	mu.Lock()
	got := append([]string(nil), lines...)
	mu.Unlock()

	want := []string{"第一行", "第二行"}
	if len(got) != len(want) {
		t.Fatalf("OnLine 收到 %d 行 %q，期望 %d 行 %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行 = %q，期望 %q", i+1, got[i], want[i])
		}
	}
	if res.Stdout != "第一行\n第二行\n" {
		t.Errorf("Result.Stdout = %q，期望 %q（应把 \\r 规范化为换行）", res.Stdout, "第一行\n第二行\n")
	}
}

// TestRunTreatsCRLFAsOneSeparator 验证 \r\n 只切一次，且空行被丢弃。
func TestRunTreatsCRLFAsOneSeparator(t *testing.T) {
	helperMode(t, "crlf")

	var (
		mu    sync.Mutex
		lines []string
	)
	res, err := Run(context.Background(), os.Args[0],
		[]string{"-test.run=TestRunTreatsCRLFAsOneSeparator"}, Options{
			Env:     append(os.Environ(), "PROC_TEST_HELPER=crlf"),
			Timeout: 30 * time.Second,
			OnLine: func(stream, line string) {
				mu.Lock()
				lines = append(lines, line)
				mu.Unlock()
			},
		})
	if err != nil {
		t.Fatalf("Run 返回错误：%v", err)
	}
	mu.Lock()
	got := append([]string(nil), lines...)
	mu.Unlock()

	want := []string{"a", "b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("OnLine = %q，期望 %q（\\r\\n 只算一个分隔符，空行丢弃）", got, want)
	}
	if res.Stdout != "a\nb\n" {
		t.Errorf("Result.Stdout = %q，期望 %q", res.Stdout, "a\nb\n")
	}
}

// TestRunKillsProcessTreeOnCancel 验证取消/超时会结束整棵进程树。
//
// 只终止直接子进程会留下孤儿孙进程（Windows 上 sdkmanager.bat → cmd.exe → java.exe），
// 它继续持有 stdout 管道并写 SDK 目录，与用户重试的下一次安装并发冲突。
func TestRunKillsProcessTreeOnCancel(t *testing.T) {
	helperMode(t, "tree-parent")

	dir := t.TempDir()
	beat := filepath.Join(dir, "heartbeat.txt")
	pidFile := filepath.Join(dir, "grandchild.pid")
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil {
				_ = KillTree(context.Background(), pid) // 失败时清理，避免留下测试孤儿
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, os.Args[0], []string{"-test.run=TestRunKillsProcessTreeOnCancel"}, Options{
			Env: append(os.Environ(),
				"PROC_TEST_HELPER=tree-parent",
				"PROC_TEST_TREE_BEAT="+beat,
				"PROC_TEST_TREE_PIDFILE="+pidFile,
			),
			Timeout: 2 * time.Minute,
		})
		done <- err
	}()

	waitForFile(t, beat, 30*time.Second) // 孙进程已开始写心跳
	cancel()

	select {
	case err := <-done:
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.CodeJobCanceled {
			t.Fatalf("Run 返回错误 = %v，期望 %s", err, domain.CodeJobCanceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run 在取消后 30 秒仍未返回（进程树终止可能挂死）")
	}

	size := fileSize(t, beat)
	time.Sleep(700 * time.Millisecond)
	if got := fileSize(t, beat); got != size {
		t.Fatalf("取消后孙进程仍在写心跳（%d → %d 字节）：进程树未被终止", size, got)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待文件 %s 出现超时", path)
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

// TestRunReturnsWhenGrandchildHoldsPipe 回归测试：直接子进程退出后，
// 若孙进程仍持有输出管道，Run 必须在有限时间内返回。
//
// 只"先排空管道再 Wait"会在这里永久阻塞：管道由孙进程持有，父进程已经退出，
// 终止进程树的兜底动作找不到目标，读取端永远等不到 EOF。
func TestRunReturnsWhenGrandchildHoldsPipe(t *testing.T) {
	helperMode(t, "orphan-pipe")

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	t.Cleanup(func() {
		// 孙进程稍后才写入 PID：轮询等待，确保一定被清理。
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(pidFile)
			if err != nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil {
				_ = KillTree(context.Background(), pid) // 清理持有管道的孙进程
			}
			return
		}
	})

	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), os.Args[0],
			[]string{"-test.run=TestRunReturnsWhenGrandchildHoldsPipe"}, Options{
				Env: append(os.Environ(),
					"PROC_TEST_HELPER=orphan-pipe",
					"PROC_TEST_TREE_PIDFILE="+pidFile,
				),
				Timeout: 3 * time.Second,
			})
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("孙进程持有输出管道时 Run 未在 20 秒内返回（读取端永久阻塞）")
	}
}

// helperMode 让本次测试进程在进入被测逻辑前挂起为“父进程模式”。
func helperMode(t *testing.T, mode string) {
	t.Helper()
	if os.Getenv("PROC_TEST_HELPER") != "" {
		procTestHelper(t)
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("PROC_TEST_HELPER") != "" {
		procTestHelper(nil)
	}
	os.Exit(m.Run())
}
