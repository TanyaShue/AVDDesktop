//go:build windows

package presenter

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type smokeFrameSource struct {
	sent bool
}

func (s *smokeFrameSource) Next(ctx context.Context) (Frame, error) {
	if !s.sent {
		s.sent = true
		return Frame{
			Pix:    []byte{0, 255, 0, 255},
			Width:  1,
			Height: 1,
			Seq:    1,
		}, nil
	}
	<-ctx.Done()
	return Frame{}, ctx.Err()
}

func TestWindowsPresenterSmoke(t *testing.T) {
	if os.Getenv("AVDDESKTOP_PRESENTER_SMOKE") != "1" {
		t.Skip("set AVDDESKTOP_PRESENTER_SMOKE=1 to create real native windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	closed := make(chan struct{})
	err := Run(ctx, Config{
		Title:        "presenter smoke",
		InstanceID:   "smoke",
		DeviceWidth:  1,
		DeviceHeight: 1,
		Source:       &smokeFrameSource{},
		SendTouch:    func(int32, int32, bool) error { return nil },
		SendKey:      func(string) error { return nil },
		OnClose:      func() { close(closed) },
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context deadline exceeded", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("OnClose was not called")
	}
}

func TestWindowsPresenterToolbarClose(t *testing.T) {
	if os.Getenv("AVDDESKTOP_PRESENTER_SMOKE") != "1" {
		t.Skip("set AVDDESKTOP_PRESENTER_SMOKE=1 to create real native windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := make(chan error, 1)
	closed := make(chan struct{})
	go func() {
		result <- Run(ctx, Config{
			Title:        "presenter toolbar smoke",
			InstanceID:   "toolbar-smoke",
			DeviceWidth:  1,
			DeviceHeight: 1,
			Source:       &smokeFrameSource{},
			SendTouch:    func(int32, int32, bool) error { return nil },
			SendKey:      func(string) error { return nil },
			OnClose:      func() { close(closed) },
		})
	}()

	var toolbar windows.Handle
	deadline := time.Now().Add(3 * time.Second)
	for toolbar == 0 && time.Now().Before(deadline) {
		registryMu.RLock()
		for hwnd := range toolbarWindows {
			toolbar = hwnd
			break
		}
		registryMu.RUnlock()
		if toolbar == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if toolbar == 0 {
		t.Fatal("toolbar window was not created")
	}
	if err := postMessage(toolbar, wmClose, 0, 0); err != nil {
		t.Fatalf("PostMessage(toolbar, WM_CLOSE) failed: %v", err)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run error = %v, want nil after toolbar close", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("toolbar close did not stop presenter")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("OnClose was not called")
	}
}
