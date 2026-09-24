package display

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer 启动一个绑定 127.0.0.1 随机端口的画面服务。
func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer(nil)
	if err != nil {
		t.Fatalf("启动画面服务失败: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// streamReader 按 multipart 规则读出一帧。
type streamReader struct {
	t  *testing.T
	br *bufio.Reader
}

func (s *streamReader) expectLine(want string) {
	s.t.Helper()
	line, err := s.br.ReadString('\n')
	if err != nil {
		s.t.Fatalf("读取响应行失败: %v", err)
	}
	if got := strings.TrimRight(line, "\r\n"); got != want {
		s.t.Fatalf("响应行 = %q，want %q", got, want)
	}
}

func (s *streamReader) readFrame() []byte {
	s.t.Helper()
	s.expectLine("--frame")
	s.expectLine("Content-Type: image/jpeg")

	line, err := s.br.ReadString('\n')
	if err != nil {
		s.t.Fatalf("读取 Content-Length 失败: %v", err)
	}
	var size int
	if _, err := fmt.Sscanf(strings.TrimRight(line, "\r\n"), "Content-Length: %d", &size); err != nil {
		s.t.Fatalf("Content-Length 头不合法: %q", line)
	}
	s.expectLine("")
	body := make([]byte, size)
	if _, err := io.ReadFull(s.br, body); err != nil {
		s.t.Fatalf("读取帧内容失败: %v", err)
	}
	s.expectLine("")
	return body
}

func TestServer_StreamsPublishedFramesAndEndsOnClose(t *testing.T) {
	srv := newTestServer(t)
	ss := srv.Session("inst-1")

	frame1 := []byte{0xFF, 0xD8, 0x01, 0x02, 0xFF, 0xD9}
	frame2 := []byte{0xFF, 0xD8, 0x03, 0x04, 0x05, 0xFF, 0xD9}
	ss.Publish(frame1)

	resp, err := http.Get(srv.URL("inst-1"))
	if err != nil {
		t.Fatalf("订阅画面失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "multipart/x-mixed-replace; boundary=frame" {
		t.Fatalf("Content-Type = %q", ct)
	}

	reader := &streamReader{t: t, br: bufio.NewReader(resp.Body)}
	// 心跳会重复写出同一帧，因此只断言「两帧都出现过」而不关心出现次数。
	seen := map[string]bool{}
	deadline := time.Now().Add(3 * time.Second)
	for !seen[string(frame1)] || !seen[string(frame2)] {
		if time.Now().After(deadline) {
			t.Fatalf("超时前未收到两帧，已收到: %v", seen)
		}
		got := reader.readFrame()
		if !bytes.Equal(got, frame1) && !bytes.Equal(got, frame2) {
			t.Fatalf("收到未知帧内容: %v", got)
		}
		seen[string(got)] = true
		if !seen[string(frame2)] {
			ss.Publish(frame2)
		}
	}

	// 会话关闭后响应必须结束（handler 返回）。
	ss.Close()
	if _, err := io.ReadAll(reader.br); err != nil {
		t.Fatalf("会话关闭后画面流未正常结束: %v", err)
	}
}

// Chromium 只在收到「下一个 part」时才提交当前帧：会话只发布一帧后不再有新帧，
// 也必须靠心跳把同一帧重复写出，否则设备窗口永远空白。
func TestServer_HeartbeatRepeatsLatestFrameWhenStatic(t *testing.T) {
	srv := newTestServer(t)
	ss := srv.Session("static")

	frame := []byte{0xFF, 0xD8, 0x21, 0x22, 0xFF, 0xD9}
	ss.Publish(frame)

	client := &http.Client{Timeout: 2 * time.Second}
	started := time.Now()
	resp, err := client.Get(srv.URL("static"))
	if err != nil {
		t.Fatalf("订阅画面失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	reader := &streamReader{t: t, br: bufio.NewReader(resp.Body)}
	for i := 0; i < 2; i++ {
		if got := reader.readFrame(); !bytes.Equal(got, frame) {
			t.Fatalf("第 %d 个 part = %v，want %v", i+1, got, frame)
		}
	}
	if elapsed := time.Since(started); elapsed > 1200*time.Millisecond {
		t.Fatalf("静止画面下 2 个 part 耗时 %s，心跳未生效", elapsed)
	}
}

func TestServer_UnknownSessionIsNotFound(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL("nope"))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("未知会话状态码 = %d，want 404", resp.StatusCode)
	}

	// 会话被 DropSession 之后同样返回 404。
	srv.Session("gone")
	srv.DropSession("gone")
	resp2, err := http.Get(srv.URL("gone"))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("已移除会话状态码 = %d，want 404", resp2.StatusCode)
	}
}

func TestServer_PublishNeverBlocksOnSlowSubscriber(t *testing.T) {
	srv := newTestServer(t)
	ss := srv.Session("slow")

	resp, err := http.Get(srv.URL("slow"))
	if err != nil {
		t.Fatalf("订阅画面失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// 故意不读响应体：模拟卡住的 WebView。

	frame := bytes.Repeat([]byte{0xAB}, 1<<20)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 64; i++ {
			ss.Publish(frame)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("慢订阅者阻塞了 Publish")
	}
}

func TestServer_HandlerReturnsWhenClientDisconnects(t *testing.T) {
	srv := newTestServer(t)
	srv.Session("dc")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/display/dc", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	ss := srv.Session("dc")
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(rec, req)
	}()

	// 等 handler 已建立订阅（响应头已写出），再断开客户端。
	deadline := time.Now().Add(2 * time.Second)
	for {
		ss.mu.Lock()
		subscribed := len(ss.subs) > 0
		ss.mu.Unlock()
		if subscribed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handler 未建立订阅")
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("客户端断开后 handler 未返回")
	}
}

func TestServer_AddsAndURLsAreLoopback(t *testing.T) {
	srv := newTestServer(t)
	if !strings.HasPrefix(srv.Addr(), "127.0.0.1:") {
		t.Fatalf("监听地址 = %q，应当只监听 127.0.0.1", srv.Addr())
	}
	if want := "http://" + srv.Addr() + "/display/abc"; srv.URL("abc") != want {
		t.Fatalf("URL = %q，want %q", srv.URL("abc"), want)
	}
}

func TestServer_CloseReleasesListener(t *testing.T) {
	srv, err := NewServer(nil)
	if err != nil {
		t.Fatalf("启动画面服务失败: %v", err)
	}
	addr := srv.Addr()
	ss := srv.Session("x")

	if err := srv.Close(); err != nil {
		t.Fatalf("关闭画面服务失败: %v", err)
	}
	// 重复关闭必须安全。
	if err := srv.Close(); err != nil {
		t.Fatalf("重复关闭失败: %v", err)
	}
	// 关闭后端口不再可连（用短超时避免依赖系统行为）。
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get("http://" + addr + "/display/x"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("服务关闭后仍可访问")
	}
	// 关闭时所有会话一起结束。
	select {
	case <-ss.done:
	case <-time.After(time.Second):
		t.Fatal("会话未随服务关闭而结束")
	}
}
