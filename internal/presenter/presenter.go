// Package presenter provides a small native window presenter for device
// frames. The Windows implementation is deliberately cgo-free and uses only
// Win32 user32/gdi32 entry points through syscall.
package presenter

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnsupported is returned by Run on platforms without a native presenter
// implementation.
var ErrUnsupported = errors.New("presenter: native presenter is only supported on Windows")

// Frame is one complete device framebuffer.
//
// Pix must contain row-major top-down RGBA8888 pixels. The presenter copies
// the pixels before handing them to GDI, so the source may reuse Pix after
// Next returns.
type Frame struct {
	Pix    []byte
	Width  int
	Height int
	Seq    uint32
}

// FrameSource supplies device frames until ctx is canceled or the source is
// closed. Next must return promptly when ctx is canceled.
type FrameSource interface {
	Next(ctx context.Context) (Frame, error)
}

// Config describes one presenter window.
type Config struct {
	Title string

	// InstanceID is optional. When set it is used as a stable, readable part
	// of the native window class name.
	InstanceID string

	DeviceWidth  int
	DeviceHeight int

	Source FrameSource

	// SendTouch receives absolute device coordinates in [0, DeviceWidth) and
	// [0, DeviceHeight). release is false for down/move and true for up.
	SendTouch func(x, y int32, release bool) error

	// SendKey receives one of "Back", "Home" and "AppSwitch" from the toolbar.
	SendKey func(key string) error

	// OnClose is called after the presenter has stopped and all native
	// resources have been released. It is not called when configuration or
	// window initialization fails.
	OnClose func()
}

func (c Config) validate() error {
	if c.DeviceWidth <= 0 || c.DeviceHeight <= 0 {
		return fmt.Errorf("presenter: invalid device size %dx%d", c.DeviceWidth, c.DeviceHeight)
	}
	if int64(c.DeviceWidth) > int64(^uint32(0)>>1) || int64(c.DeviceHeight) > int64(^uint32(0)>>1) {
		return fmt.Errorf("presenter: device size %dx%d exceeds int32", c.DeviceWidth, c.DeviceHeight)
	}
	if c.Source == nil {
		return errors.New("presenter: frame source is nil")
	}
	return nil
}

func (c Config) windowTitle() string {
	if c.Title == "" {
		return "AVD Desktop"
	}
	return c.Title
}
