//go:build !windows

package presenter

import (
	"context"
	"fmt"
	"runtime"
)

// Supported reports whether this build has a native presenter backend.
func Supported() bool { return false }

// Run returns ErrUnsupported on non-Windows platforms.
func Run(ctx context.Context, cfg Config) error {
	_ = ctx
	_ = cfg
	return fmt.Errorf("%w: %s/%s", ErrUnsupported, runtime.GOOS, runtime.GOARCH)
}
