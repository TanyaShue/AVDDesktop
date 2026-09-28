package framestream

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// subscriber 是一个 WebSocket 订阅者。
//
// pending 与 registered 由 Hub.mu 保护；wake 是容量为 1 的合并唤醒信号，
// 每个订阅者只有一个写协程消费它。done 和 writeDone 分别表示连接已关闭和
// 写协程已退出。
type subscriber struct {
	conn *websocket.Conn

	wake      chan struct{}
	done      chan struct{}
	writeDone chan struct{}
	closeOnce sync.Once

	pending    *frameRef
	registered bool
}

// newSubscriber 创建订阅者运行时状态。
func newSubscriber(conn *websocket.Conn) *subscriber {
	return &subscriber{
		conn:      conn,
		wake:      make(chan struct{}, 1),
		done:      make(chan struct{}),
		writeDone: make(chan struct{}),
	}
}

// close 关闭订阅者连接并通知写协程退出；可重复调用。
func (s *subscriber) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.done)
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}

// ServeWS 把一次 HTTP 请求升级为原始帧 WebSocket 订阅。
//
// 本端点是本机回环地址上的内部传输，仅校验可选的 query token；不做 Origin
// 白名单，也不启用 permessage-deflate。
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "frame stream unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.opts.Token != "" && r.URL.Query().Get("token") != h.opts.Token {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// 先做一次关闭检查，避免 Hub 已关闭时仍完成 WebSocket upgrade。
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		http.Error(w, "frame stream closed", http.StatusServiceUnavailable)
		return
	}

	if _, ok := w.(http.Hijacker); !ok {
		http.Error(w, "websocket upgrade unsupported", http.StatusBadRequest)
		return
	}
	upgrader := websocket.Upgrader{
		EnableCompression: false,
		// 单帧约 1.5–2.5MB，较大的写缓冲减少小包系统调用开销。
		WriteBufferSize: 64 * 1024,
		ReadBufferSize:  1024,
		CheckOrigin: func(*http.Request) bool {
			// 回环地址 + token 是本端点的访问边界；允许 WebView 自身的 Origin。
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logf("frame stream websocket upgrade failed: %v", err)
		return
	}
	conn.EnableWriteCompression(false)

	sub := h.addSubscriber(conn)
	if sub == nil {
		return
	}

	go h.writeLoop(sub)

	defer func() {
		h.removeSubscriber(sub)
		// removeSubscriber 已关闭连接；等写协程退出，避免测试或运行时留下泄漏。
		<-sub.writeDone
	}()

	// 协议只使用服务端到客户端的二进制帧。读循环的目的仅是感知客户端断开，
	// 从而及时注销订阅者并释放 mailbox 帧。
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writeLoop 是订阅者唯一的写协程。它消费 mailbox 中的最新帧，网络写出永远不阻塞
// Hub 的发布路径。
func (h *Hub) writeLoop(sub *subscriber) {
	defer close(sub.writeDone)

	writeTimeout := h.opts.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = defaultWriteTimeout
	}

	for {
		select {
		case <-h.done:
			return
		case <-sub.done:
			return
		case <-sub.wake:
		}

		ref, live := h.takePending(sub)
		if !live {
			return
		}
		if ref == nil {
			// 合并唤醒可能对应已经消费掉的 mailbox，继续等待下一帧即可。
			continue
		}

		if err := writeWSFrame(sub.conn, ref.frame, writeTimeout); err != nil {
			h.logf("frame stream write failed: %v", err)
			h.removeSubscriber(sub)
			release(ref.frame)
			return
		}
		release(ref.frame)
	}
}

// writeWSFrame 写出一条 32 字节帧头 + 负载的二进制消息。
func writeWSFrame(conn *websocket.Conn, frame Frame, timeout time.Duration) error {
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	writer, err := conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}

	header := HeaderBytes(frame)
	if _, err = writer.Write(header[:]); err == nil {
		_, err = writer.Write(frame.Pix)
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return err
}

// logf 在配置了日志函数时输出诊断信息。
func (h *Hub) logf(format string, args ...any) {
	if h != nil && h.opts.Logf != nil {
		h.opts.Logf(format, args...)
	}
}
