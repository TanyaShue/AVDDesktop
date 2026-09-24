package service

import (
	"testing"

	"AVDDesktop/internal/logging"
	"AVDDesktop/internal/presenter"
)

func TestNativeSupportedMatchesPresenter(t *testing.T) {
	s := &DisplayService{}
	if got, want := s.NativeSupported(), presenter.Supported(); got != want {
		t.Fatalf("NativeSupported() = %v, want %v", got, want)
	}
}

func TestCloseTakesNativeSessionBeforeWebSession(t *testing.T) {
	native := &nativeSession{
		info: DisplaySession{InstanceID: "i1", AvdName: "Dev1", Mode: "native"},
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
