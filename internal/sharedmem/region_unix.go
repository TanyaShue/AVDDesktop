//go:build unix

package sharedmem

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type platformMapping struct {
	data []byte
}

func mapFile(file *os.File, size int) (*platformMapping, error) {
	data, err := syscall.Mmap(
		int(file.Fd()),
		0,
		size,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		return nil, fmt.Errorf("mmap: %w", err)
	}
	return &platformMapping{data: data}, nil
}

func (m *platformMapping) bytes() []byte {
	if m == nil {
		return nil
	}
	return m.data
}

func (m *platformMapping) close() error {
	if m == nil || m.data == nil {
		return nil
	}
	err := syscall.Munmap(m.data)
	m.data = nil
	if err != nil {
		return fmt.Errorf("munmap: %w", err)
	}
	return nil
}

func handleForPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path is not absolute: %q", path)
	}
	// AOSP's POSIX SharedMemory strips the file:// prefix and opens the
	// remaining path verbatim, so do not percent-encode this platform.
	return "file://" + path, nil
}
