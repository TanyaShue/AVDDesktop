//go:build windows

package sharedmem

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type platformMapping struct {
	handle  windows.Handle
	address uintptr
	data    []byte
}

func mapFile(file *os.File, size int) (*platformMapping, error) {
	maxSize := uint64(size)
	handle, err := windows.CreateFileMapping(
		windows.Handle(file.Fd()),
		nil,
		windows.PAGE_READWRITE,
		uint32(maxSize>>32),
		uint32(maxSize),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateFileMapping: %w", err)
	}

	address, err := windows.MapViewOfFile(
		handle,
		windows.FILE_MAP_READ|windows.FILE_MAP_WRITE,
		0,
		0,
		uintptr(size),
	)
	if err != nil {
		closeErr := windows.CloseHandle(handle)
		return nil, errors.Join(fmt.Errorf("MapViewOfFile: %w", err), closeErr)
	}

	return &platformMapping{
		handle:  handle,
		address: address,
		data:    unsafe.Slice((*byte)(unsafe.Pointer(address)), size),
	}, nil
}

func (m *platformMapping) bytes() []byte {
	if m == nil {
		return nil
	}
	return m.data
}

func (m *platformMapping) close() error {
	if m == nil {
		return nil
	}

	var errs []error
	if m.address != 0 {
		if err := windows.UnmapViewOfFile(m.address); err != nil {
			errs = append(errs, fmt.Errorf("UnmapViewOfFile: %w", err))
		}
		m.address = 0
		m.data = nil
	}
	if m.handle != 0 {
		if err := windows.CloseHandle(m.handle); err != nil {
			errs = append(errs, fmt.Errorf("CloseHandle: %w", err))
		}
		m.handle = 0
	}
	return errors.Join(errs...)
}

func handleForPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path is not absolute: %q", path)
	}

	slashPath := filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(slashPath, "//") {
		hostAndPath := strings.TrimPrefix(slashPath, "//")
		host, sharePath, _ := strings.Cut(hostAndPath, "/")
		if host == "" {
			return "", fmt.Errorf("invalid UNC path: %q", path)
		}
		if sharePath != "" {
			sharePath = "/" + sharePath
		}
		return (&url.URL{Scheme: "file", Host: host, Path: sharePath}).String(), nil
	}

	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String(), nil
}
