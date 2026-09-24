// Package sharedmem provides file-backed shared memory regions used by the
// emulator MMAP screenshot transport.
package sharedmem

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// maxRegionSize keeps every offset and size representable on 32-bit hosts.
const maxRegionSize = int64(1<<31 - 1)

type regionFileOps struct {
	createTemp func(pattern string) (*os.File, error)
	mapRegion  func(file *os.File, size int) (*platformMapping, error)
}

var defaultRegionFileOps = regionFileOps{
	createTemp: func(pattern string) (*os.File, error) {
		return os.CreateTemp("", pattern)
	},
	mapRegion: mapFile,
}

// Region is an exclusively-created, writable, file-backed memory mapping.
//
// A Region is intended to be created by the process that consumes frames and
// then shared with the emulator through Handle. Callers must not access Bytes
// after Close returns.
type Region struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	handle  string
	size    int
	mapping *platformMapping
	closed  bool
}

// New creates an exclusive temporary file, truncates it to size bytes, and
// maps it read-write. name is used only as a prefix for the temporary file.
func New(name string, size int) (*Region, error) {
	return newRegion(name, size, defaultRegionFileOps)
}

func newRegion(name string, size int, ops regionFileOps) (*Region, error) {
	pattern, err := temporaryPattern(name)
	if err != nil {
		return nil, err
	}
	if err := validateRegionSize(size); err != nil {
		return nil, err
	}
	if ops.createTemp == nil || ops.mapRegion == nil {
		return nil, errors.New("shared memory file operations are not configured")
	}

	file, err := ops.createTemp(pattern)
	if err != nil {
		return nil, fmt.Errorf("create shared memory file: %w", err)
	}
	path := file.Name()
	cleanup := func() error {
		return cleanupCreatedFile(file, path)
	}

	if !filepath.IsAbs(path) {
		cause := fmt.Errorf("shared memory path is not absolute: %q", path)
		return nil, joinCleanupError(cause, cleanup())
	}
	if err := file.Truncate(int64(size)); err != nil {
		cause := fmt.Errorf("truncate shared memory file: %w", err)
		return nil, joinCleanupError(cause, cleanup())
	}

	mapping, err := ops.mapRegion(file, size)
	if err != nil {
		cause := fmt.Errorf("map shared memory file: %w", err)
		return nil, joinCleanupError(cause, cleanup())
	}

	handle, err := handleForPath(path)
	if err != nil {
		mappingErr := mapping.close()
		cleanupErr := cleanup()
		return nil, errors.Join(fmt.Errorf("build shared memory handle: %w", err), mappingErr, cleanupErr)
	}

	return &Region{
		file:    file,
		path:    path,
		handle:  handle,
		size:    size,
		mapping: mapping,
	}, nil
}

// Handle returns the file URL accepted by the emulator SharedMemory
// implementation. It returns an empty string after Close.
func (r *Region) Handle() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ""
	}
	return r.handle
}

// Bytes returns the mapped region. It returns nil after Close.
func (r *Region) Bytes() []byte {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.mapping == nil {
		return nil
	}
	return r.mapping.bytes()
}

// Size returns the requested region size in bytes.
func (r *Region) Size() int {
	if r == nil {
		return 0
	}
	return r.size
}

// Close unmaps the region, closes all handles, and removes the backing file.
// It is safe to call more than once; subsequent calls return nil.
func (r *Region) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	mapping := r.mapping
	file := r.file
	path := r.path
	r.mapping = nil
	r.file = nil
	r.path = ""
	r.handle = ""
	r.mu.Unlock()

	return cleanupCreatedFile(file, path, mapping)
}

func temporaryPattern(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("shared memory name must not be empty")
	}
	if name == "." || name == ".." || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("shared memory name must not contain path separators: %q", name)
	}
	return name + "-*", nil
}

func validateRegionSize(size int) error {
	if size <= 0 {
		return fmt.Errorf("shared memory size must be positive: %d", size)
	}
	if int64(size) > maxRegionSize {
		return fmt.Errorf("shared memory size exceeds 32-bit limit: %d > %d", size, maxRegionSize)
	}
	return nil
}

func joinCleanupError(cause, cleanupErr error) error {
	if cleanupErr == nil {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("clean up shared memory file: %w", cleanupErr))
}

func cleanupCreatedFile(file *os.File, path string, mappings ...*platformMapping) error {
	var errs []error

	for _, mapping := range mappings {
		if mapping == nil {
			continue
		}
		if err := mapping.close(); err != nil {
			errs = append(errs, fmt.Errorf("unmap shared memory file: %w", err))
		}
	}
	if file != nil {
		if err := file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close shared memory file: %w", err))
		}
	}
	if path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove shared memory file: %w", err))
		}
	}
	return errors.Join(errs...)
}
