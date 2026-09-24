package presenter

import "testing"

func TestFitRect(t *testing.T) {
	tests := []struct {
		name                       string
		clientW, clientH           int32
		deviceW, deviceH           int32
		wantX, wantY, wantW, wantH int32
		wantOK                     bool
	}{
		{
			name:    "square fills square client",
			clientW: 100, clientH: 100,
			deviceW: 100, deviceH: 100,
			wantX: 0, wantY: 0, wantW: 100, wantH: 100, wantOK: true,
		},
		{
			name:    "portrait device letterboxes horizontally",
			clientW: 1000, clientH: 1000,
			deviceW: 100, deviceH: 200,
			wantX: 250, wantY: 0, wantW: 500, wantH: 1000, wantOK: true,
		},
		{
			name:    "landscape device is height constrained",
			clientW: 800, clientH: 600,
			deviceW: 1920, deviceH: 1080,
			wantX: 0, wantY: 75, wantW: 800, wantH: 450, wantOK: true,
		},
		{
			name:    "portrait device is width constrained",
			clientW: 600, clientH: 800,
			deviceW: 1080, deviceH: 1920,
			wantX: 75, wantY: 0, wantW: 450, wantH: 800, wantOK: true,
		},
		{
			name:    "rounds odd letterbox offset",
			clientW: 500, clientH: 500,
			deviceW: 3, deviceH: 2,
			wantX: 0, wantY: 83, wantW: 500, wantH: 333, wantOK: true,
		},
		{
			name:    "invalid dimensions",
			clientW: 0, clientH: 100,
			deviceW: 10, deviceH: 10,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := fitRect(tt.clientW, tt.clientH, tt.deviceW, tt.deviceH)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.Left != tt.wantX || got.Top != tt.wantY || got.width() != tt.wantW || got.height() != tt.wantH {
				t.Fatalf("fitRect = {%d,%d,%d,%d}, want {%d,%d,%d,%d}", got.Left, got.Top, got.width(), got.height(), tt.wantX, tt.wantY, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestClientToDeviceLetterbox(t *testing.T) {
	tests := []struct {
		name         string
		clientX      int32
		clientY      int32
		wantX, wantY int32
		wantInside   bool
	}{
		{name: "top left of content", clientX: 250, clientY: 0, wantX: 0, wantY: 0, wantInside: true},
		{name: "bottom right of content", clientX: 749, clientY: 999, wantX: 99, wantY: 199, wantInside: true},
		{name: "middle of content", clientX: 500, clientY: 500, wantX: 50, wantY: 100, wantInside: true},
		{name: "left letterbox clamps x", clientX: 249, clientY: 500, wantX: 0, wantY: 100, wantInside: false},
		{name: "right edge clamps x", clientX: 750, clientY: 500, wantX: 99, wantY: 100, wantInside: false},
		{name: "top letterbox clamps y", clientX: 500, clientY: -1, wantX: 50, wantY: 0, wantInside: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, inside := clientToDevice(tt.clientX, tt.clientY, 1000, 1000, 100, 200)
			if x != tt.wantX || y != tt.wantY || inside != tt.wantInside {
				t.Fatalf("clientToDevice = (%d,%d,%v), want (%d,%d,%v)", x, y, inside, tt.wantX, tt.wantY, tt.wantInside)
			}
		})
	}
}

func TestClientToDeviceMapsEdges(t *testing.T) {
	x, y, inside := clientToDevice(199, 99, 200, 100, 20, 10)
	if !inside || x != 19 || y != 9 {
		t.Fatalf("edge mapping = (%d,%d,%v), want (19,9,true)", x, y, inside)
	}

	x, y, inside = clientToDevice(200, 100, 200, 100, 20, 10)
	if inside || x != 19 || y != 9 {
		t.Fatalf("outside edge mapping = (%d,%d,%v), want (19,9,false)", x, y, inside)
	}
}
