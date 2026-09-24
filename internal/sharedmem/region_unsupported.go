//go:build !windows && !unix

package sharedmem

import (
	"fmt"
	"os"
	"runtime"
)

type platformMapping struct{}

func mapFile(_ *os.File, _ int) (*platformMapping, error) {
	return nil, fmt.Errorf("shared memory mapping is unsupported on %s", runtime.GOOS)
}

func (m *platformMapping) bytes() []byte {
	return nil
}

func (m *platformMapping) close() error {
	return nil
}

func handleForPath(_ string) (string, error) {
	return "", fmt.Errorf("file-backed shared memory is unsupported on %s", runtime.GOOS)
}
