package proc

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
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
