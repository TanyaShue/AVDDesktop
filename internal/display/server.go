// Package display 把设备画面（JPEG 帧）以 MJPEG 形式暴露给应用内自建的「设备窗口」。
//
// 设计要点：
//   - 只监听 127.0.0.1：画面与触摸都在本机完成，不对外暴露端口
//   - 每个会话只保留最新一帧，慢客户端（WebView 卡顿 / 用户拖动窗口）自然丢帧，
//     Publish 永不阻塞推流方
//   - 一帧只有一个副本：多个订阅者共享同一份字节，读取期间不得写入
//
// 注意：Wails v2.16 没有多窗口 API，「设备窗口」是前端全屏浮层，因此这里提供的是
// 供 WebView 内 <img src="..."> 直接订阅的 HTTP 流，而不是独立原生窗口。
package display

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/logging"
)

// boundary 是 multipart 分隔符；每部分是一个 JPEG 帧。
const boundary = "frame"

// frameHeartbeat 是「静止画面」下的重发间隔：Chromium 只在收到下一个 part 时才提交
// 当前帧，没有心跳时静止画面永远不显示（真机 + Chromium 实测 500ms 可正常显示）。
const frameHeartbeat = 500 * time.Millisecond

// frameSeparator 是每部分与分隔符之间的固定收尾。
var frameSeparator = []byte("\r\n")

// Server 是 MJPEG 画面服务。
type Server struct {
	mu       sync.Mutex
	ln       net.Listener
	http     *http.Server
	sessions map[string]*Session
	log      logging.Interface
	closed   bool
}

// NewServer 创建并启动画面服务，绑定 127.0.0.1 的随机端口。log 为 nil 时使用空日志器。
func NewServer(log logging.Interface) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, domain.ErrDetail(domain.CodeProcessFailed, "无法启动设备画面服务", err.Error())
	}
	s := &Server{
		ln:       ln,
		sessions: make(map[string]*Session),
		log:      logging.Or(log),
	}
	s.http = &http.Server{Handler: s}
	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Warn("display", "设备画面服务意外退出：%v", err)
		}
	}()
	return s, nil
}

// Addr 返回监听地址（形如 "127.0.0.1:51234"）。
func (s *Server) Addr() string {
	if s == nil || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// URL 返回某个会话的订阅地址。
func (s *Server) URL(id string) string { return "http://" + s.Addr() + "/display/" + id }

// Session 幂等获取（不存在则创建）会话。
func (s *Server) Session(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ss, ok := s.sessions[id]; ok {
		return ss
	}
	ss := newSession()
	s.sessions[id] = ss
	return ss
}

// DropSession 结束并移除会话（之后的订阅请求返回 404）。不存在时为空操作。
func (s *Server) DropSession(id string) {
	s.mu.Lock()
	ss, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	if ok {
		ss.Close()
	}
}

// Close 关闭监听与所有连接、结束所有会话；可重复调用。
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	all := make([]*Session, 0, len(s.sessions))
	for id, ss := range s.sessions {
		all = append(all, ss)
		delete(s.sessions, id)
	}
	s.mu.Unlock()

	for _, ss := range all {
		ss.Close()
	}
	return s.http.Close()
}

// ServeHTTP 处理 `GET /display/{id}`：输出 multipart/x-mixed-replace 的 JPEG 帧流。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "设备画面流只支持 GET", http.StatusMethodNotAllowed)
		return
	}
	ss := s.lookup(id)
	if ss == nil {
		http.NotFound(w, r)
		return
	}
	ss.serve(w, r)
}

func (s *Server) lookup(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// sessionID 从路径中解析会话 id；非 /display/<id> 形状时返回 false。
func sessionID(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/display/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return rest, true
}

// Session 是一条画面流：推流方 Publish 最新帧，零个或多个 HTTP 订阅者消费。
type Session struct {
	mu     sync.Mutex
	latest []byte
	subs   map[chan struct{}]struct{}
	done   chan struct{}
	closed bool
}

func newSession() *Session {
	return &Session{
		subs: make(map[chan struct{}]struct{}),
		done: make(chan struct{}),
	}
}

// Publish 发布最新一帧 JPEG：覆盖式保留（慢客户端自动丢帧），永不阻塞。
//
// jpeg 的所有权转移给会话：发布后不得再修改该切片（订阅者会直接读取它）。
func (ss *Session) Publish(jpeg []byte) {
	if ss == nil || len(jpeg) == 0 {
		return
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return
	}
	ss.latest = jpeg
	// 通知订阅者；容量 1 的唤醒信号可以合并，慢订阅者只是少收到几次唤醒。
	for wake := range ss.subs {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// Close 结束所有订阅者（HTTP 响应随之结束），并丢弃缓存的帧；可重复调用。
func (ss *Session) Close() {
	if ss == nil {
		return
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return
	}
	ss.closed = true
	ss.latest = nil
	close(ss.done)
}

// serve 是订阅者的处理循环：先补一帧当前画面（画面静止时不会有新帧），
// 之后每次被唤醒就写最新帧。
func (ss *Session) serve(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "当前运行环境不支持流式响应", http.StatusInternalServerError)
		return
	}
	wake := ss.subscribe()
	defer ss.unsubscribe(wake)

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if frame := ss.frame(); len(frame) > 0 {
		if !writeFrame(w, frame) {
			return
		}
		flusher.Flush()
	}

	// 心跳：Chromium 只在收到「下一个 part」时才提交当前帧，因此即使没有新帧也必须
	// 周期性重发最近一帧，否则画面静止时设备窗口永远空白（实测 500ms 可正常显示）。
	// 有新帧时立刻写，帧率不受影响（只是同一帧可能被重复写出）。
	heartbeat := time.NewTicker(frameHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ss.done:
			return
		case <-r.Context().Done():
			return
		case <-wake:
		case <-heartbeat.C:
		}
		frame := ss.frame()
		if len(frame) == 0 {
			continue
		}
		if !writeFrame(w, frame) {
			return
		}
		flusher.Flush()
	}
}

func (ss *Session) frame() []byte {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.latest
}

func (ss *Session) subscribe() chan struct{} {
	wake := make(chan struct{}, 1)
	ss.mu.Lock()
	ss.subs[wake] = struct{}{}
	ss.mu.Unlock()
	return wake
}

func (ss *Session) unsubscribe(wake chan struct{}) {
	ss.mu.Lock()
	delete(ss.subs, wake)
	ss.mu.Unlock()
}

// writeFrame 写出一部分 multipart（含 Content-Length），失败表示客户端已断开。
func writeFrame(w http.ResponseWriter, jpeg []byte) bool {
	if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(jpeg)); err != nil {
		return false
	}
	if _, err := w.Write(jpeg); err != nil {
		return false
	}
	if _, err := w.Write(frameSeparator); err != nil {
		return false
	}
	return true
}
