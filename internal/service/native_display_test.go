package service

import (
	"testing"

	"AVDDesktop/internal/logging"
)

func TestWindowSupportedIsAlwaysAvailable(t *testing.T) {
	s := &DisplayService{}
	if !s.WindowSupported() {
		t.Fatal("WindowSupported() = false, want true（设备窗口由 WebView 承载）")
	}
}

func TestCloseTakesNativeSessionBeforeWebSession(t *testing.T) {
	native := &nativeSession{
		info: DisplaySession{InstanceID: "i1", AvdName: "Dev1", Mode: "webview"},
		done: make(chan struct{}),
	}
	close(native.done)
	s := &DisplayService{
		rt:         &Runtime{log: logging.Discard()},
		items:      map[string]*displayItem{},
		natives:    map[string]*nativeSession{"i1": native},
		closeEpoch: map[string]uint64{},
	}
	if err := s.Close("i1"); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if s.natives["i1"] != nil {
		t.Fatal("native session was not removed")
	}
	if got := s.closeEpoch["i1"]; got != 0 {
		t.Fatalf("native close incremented web closeEpoch to %d", got)
	}
}

func TestActiveIncludesNativeSessions(t *testing.T) {
	s := &DisplayService{
		items:   map[string]*displayItem{"web": {}},
		natives: map[string]*nativeSession{"native": {}},
	}
	got := s.Active()
	if len(got) != 2 || got[0] != "native" || got[1] != "web" {
		t.Fatalf("Active() = %v, want [native web]", got)
	}
}

func TestReadyLinePayload(t *testing.T) {
	if payload, ok := readyLinePayload(nativeReadyMarker); !ok || payload != nil {
		t.Fatalf("裸 marker 行应被识别且不带负载：payload=%q ok=%v", payload, ok)
	}
	payload, ok := readyLinePayload(nativeReadyMarker + ` {"url":"http://127.0.0.1:1/display/i1"}`)
	if !ok || string(payload) != `{"url":"http://127.0.0.1:1/display/i1"}` {
		t.Fatalf("带负载的 marker 行解析错误：payload=%q ok=%v", payload, ok)
	}
	if _, ok := readyLinePayload("[device] 设备窗口已就绪"); ok {
		t.Fatal("普通日志行不应被当成握手行")
	}
}

func TestApplyReadyInfoFillsSession(t *testing.T) {
	native := &nativeSession{info: DisplaySession{InstanceID: "i1", Mode: "webview"}}
	native.applyReadyInfo([]byte(`{"url":"http://127.0.0.1:5000/display/i1","deviceWidth":720,` +
		`"deviceHeight":1280,"streamWidth":540,"streamHeight":960}`))
	want := DisplaySession{
		InstanceID:   "i1",
		Mode:         "webview",
		URL:          "http://127.0.0.1:5000/display/i1",
		DeviceWidth:  720,
		DeviceHeight: 1280,
		StreamWidth:  540,
		StreamHeight: 960,
	}
	if native.info != want {
		t.Fatalf("info = %+v, want %+v", native.info, want)
	}

	// 负载损坏时不应清空已有会话信息。
	native.applyReadyInfo([]byte("not json"))
	if native.info.URL != want.URL {
		t.Fatalf("非法负载不应改动会话信息，得到 %+v", native.info)
	}
}
