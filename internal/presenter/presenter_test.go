package presenter

import (
	"context"
	"runtime"
	"testing"
)

type testFrameSource struct{}

func (testFrameSource) Next(context.Context) (Frame, error) { return Frame{}, nil }

func TestSupportedMatchesPlatform(t *testing.T) {
	want := runtime.GOOS == "windows"
	if got := Supported(); got != want {
		t.Fatalf("Supported() = %v on %s, want %v", got, runtime.GOOS, want)
	}
}

func TestConfigValidate(t *testing.T) {
	valid := Config{
		DeviceWidth:  1080,
		DeviceHeight: 1920,
		Source:       testFrameSource{},
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "missing source", cfg: Config{DeviceWidth: 1, DeviceHeight: 1}},
		{name: "zero width", cfg: Config{DeviceHeight: 1, Source: testFrameSource{}}},
		{name: "negative height", cfg: Config{DeviceWidth: 1, DeviceHeight: -1, Source: testFrameSource{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
