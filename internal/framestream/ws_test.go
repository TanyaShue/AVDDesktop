package framestream

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialTestWS 通过真实 httptest 服务和 gorilla 默认拨号器建立订阅。
func dialTestWS(t *testing.T, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	u.Scheme = "ws"
	query := u.Query()
	query.Set("token", token)
	u.RawQuery = query.Encode()

	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func TestHubServeWSReceivesRawFrame(t *testing.T) {
	var released atomic.Int64
	h := NewHub(Options{Token: "secret", WriteTimeout: time.Second})
	server := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer server.Close()
	defer h.Close()

	conn := dialTestWS(t, server, "secret")
	defer conn.Close()

	firstPix := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	firstTime := time.UnixMicro(1234567)
	h.Publish(Frame{
		Pix:       firstPix,
		Width:     2,
		Height:    2,
		Format:    FormatRGBA8888,
		Seq:       42,
		Timestamp: firstTime,
		BottomUp:  true,
		Release: func() {
			released.Add(1)
		},
	})

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	messageType, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read first frame: %v", err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("message type=%d, want binary", messageType)
	}
	if len(message) != HeaderSize+len(firstPix) {
		t.Fatalf("message length=%d, want %d", len(message), HeaderSize+len(firstPix))
	}
	header, err := ParseHeader(message)
	if err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if err := header.ValidatePayload(len(message) - HeaderSize); err != nil {
		t.Fatalf("validate payload: %v", err)
	}
	if header.Width != 2 || header.Height != 2 || header.Format != FormatRGBA8888 || header.Seq != 42 {
		t.Fatalf("header=%+v", header)
	}
	if header.Flags&FlagBottomUp == 0 {
		t.Fatal("bottom-up flag missing")
	}
	if header.TimestampUnixUs != firstTime.UnixMicro() {
		t.Fatalf("timestamp=%d, want %d", header.TimestampUnixUs, firstTime.UnixMicro())
	}
	if !bytes.Equal(message[HeaderSize:], firstPix) {
		t.Fatal("payload mismatch")
	}
	if got := h.Stats().Subscribers; got != 1 {
		t.Fatalf("subscribers=%d, want 1", got)
	}

	secondPix := []byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	h.Publish(Frame{
		Pix:       secondPix,
		Width:     2,
		Height:    2,
		Format:    FormatRGBA8888,
		Seq:       43,
		Timestamp: firstTime.Add(time.Millisecond),
		Release: func() {
			released.Add(1)
		},
	})
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, secondMessage, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read second frame: %v", err)
	}
	if !bytes.Equal(secondMessage[HeaderSize:], secondPix) {
		t.Fatal("second payload mismatch")
	}

	h.Close()
	waitFor(t, 2*time.Second, func() bool { return released.Load() == 2 })
	stats := h.Stats()
	if stats.Subscribers != 0 {
		t.Fatalf("subscribers after Close=%d, want 0", stats.Subscribers)
	}
	if stats.PublishTotal != 2 {
		t.Fatalf("publish total=%d, want 2", stats.PublishTotal)
	}
}

func TestHubServeWSRejectsBadToken(t *testing.T) {
	h := NewHub(Options{Token: "secret"})
	server := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer server.Close()
	defer h.Close()

	for _, tc := range []struct {
		name string
		url  string
		want int
	}{
		{name: "missing", url: server.URL, want: http.StatusUnauthorized},
		{name: "wrong", url: server.URL + "?token=wrong", want: http.StatusUnauthorized},
		{name: "not upgrade", url: server.URL + "?token=secret", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(tc.url)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status=%d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestHubPublishDoesNotBlockOnSlowSubscriber(t *testing.T) {
	var released atomic.Int64
	h := NewHub(Options{Token: "secret", WriteTimeout: time.Second})
	server := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer server.Close()
	defer h.Close()

	conn := dialTestWS(t, server, "secret")
	defer conn.Close()
	waitFor(t, 2*time.Second, func() bool { return h.Stats().Subscribers == 1 })

	const (
		width      = 512
		height     = 512
		payloadLen = width * height * 4
		maxFrames  = 64
	)
	published := 0
	for i := 0; i < maxFrames; i++ {
		pix := make([]byte, payloadLen)
		pix[0] = byte(i)
		h.Publish(Frame{
			Pix:    pix,
			Width:  width,
			Height: height,
			Format: FormatRGBA8888,
			Seq:    uint32(i),
			Release: func() {
				released.Add(1)
			},
		})
		published++
		if h.Stats().DropForClient > 0 {
			break
		}
	}

	stats := h.Stats()
	if stats.DropForClient == 0 {
		t.Fatalf("drop count=0 after %d large frames; slow subscriber was not dropped", published)
	}
	if stats.PublishTotal != uint64(published) {
		t.Fatalf("publish total=%d, want %d", stats.PublishTotal, published)
	}

	h.Close()
	waitFor(t, 3*time.Second, func() bool { return released.Load() == int64(published) })
}

func TestHubCloseStopsSubscriberAndReleasesFrame(t *testing.T) {
	h := NewHub(Options{Token: "secret", WriteTimeout: time.Second})
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeWS(w, r)
		close(handlerDone)
	}))
	defer server.Close()

	beforeGoroutines := runtime.NumGoroutine()
	conn := dialTestWS(t, server, "secret")
	waitFor(t, 2*time.Second, func() bool { return h.Stats().Subscribers == 1 })

	var released atomic.Int64
	h.Publish(Frame{
		Pix:    []byte{1, 2, 3, 4},
		Width:  1,
		Height: 1,
		Format: FormatRGBA8888,
		Release: func() {
			released.Add(1)
		},
	})

	h.Close()
	if got := h.Stats().Subscribers; got != 0 {
		t.Fatalf("subscribers after Close=%d, want 0", got)
	}

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeWS handler did not exit after Close")
	}
	waitFor(t, 2*time.Second, func() bool { return released.Load() == 1 })

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	_ = conn.Close()
	server.Close()

	waitFor(t, 2*time.Second, func() bool {
		return runtime.NumGoroutine() <= beforeGoroutines+2
	})
}
