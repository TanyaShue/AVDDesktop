//go:build windows || unix

package sharedmem

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestNewMapsWritableFileBackedRegion(t *testing.T) {
	setTempDir(t)
	const size = 64 * 1024

	region, err := New("frame-buffer", size)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = region.Close() })

	if got := region.Size(); got != size {
		t.Fatalf("Size() = %d, want %d", got, size)
	}
	if handle := region.Handle(); !strings.HasPrefix(handle, "file://") {
		t.Fatalf("Handle() = %q, want file URL", handle)
	}

	path := pathFromHandle(t, region.Handle())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat mapped file: %v", err)
	}
	if got := info.Size(); got != size {
		t.Fatalf("mapped file size = %d, want %d", got, size)
	}

	data := region.Bytes()
	if len(data) != size {
		t.Fatalf("len(Bytes()) = %d, want %d", len(data), size)
	}
	for i := range data {
		data[i] = byte(i % 251)
	}

	reader, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open mapped file independently: %v", err)
	}
	readerMapping, err := mapFile(reader, size)
	if err != nil {
		_ = reader.Close()
		t.Fatalf("map file independently: %v", err)
	}
	got := append([]byte(nil), readerMapping.bytes()...)
	if err := readerMapping.close(); err != nil {
		t.Fatalf("close independent mapping: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close independent file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("independent mapping did not observe writes through Region.Bytes")
	}
}

func TestNewCreatesExclusiveUniqueFiles(t *testing.T) {
	setTempDir(t)

	first, err := New("same-name", 128)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := New("same-name", 128)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	if first.Handle() == second.Handle() {
		t.Fatalf("two regions share handle %q", first.Handle())
	}
	firstPath := pathFromHandle(t, first.Handle())
	secondPath := pathFromHandle(t, second.Handle())
	if firstPath == secondPath {
		t.Fatalf("two regions share path %q", firstPath)
	}
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("stat first region: %v", err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("stat second region: %v", err)
	}
}

func TestCloseIsIdempotentAndCleansUp(t *testing.T) {
	setTempDir(t)

	region, err := New("close-lifecycle", 256)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	path := pathFromHandle(t, region.Handle())

	if err := region.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("backing file still exists after Close: stat error = %v", err)
	}
	if data := region.Bytes(); data != nil {
		t.Fatalf("Bytes() after Close = %v, want nil", data)
	}
	if handle := region.Handle(); handle != "" {
		t.Fatalf("Handle() after Close = %q, want empty", handle)
	}
	if err := region.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestNewValidatesArgumentsBeforeCreatingFile(t *testing.T) {
	var created bool
	ops := defaultRegionFileOps
	ops.createTemp = func(string) (*os.File, error) {
		created = true
		return nil, errors.New("must not be called")
	}

	tests := []struct {
		name string
		size int
	}{
		{name: ""},
		{name: "   "},
		{name: "../escape"},
		{name: "nested/name"},
		{name: `nested\name`},
		{name: "valid", size: 0},
		{name: "valid", size: -1},
	}
	if strconv.IntSize > 32 {
		oversized := maxRegionSize
		oversized++
		tests = append(tests, struct {
			name string
			size int
		}{name: "valid", size: int(oversized)})
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q/%d", tt.name, tt.size), func(t *testing.T) {
			if _, err := newRegion(tt.name, tt.size, ops); err == nil {
				t.Fatal("newRegion() error = nil, want validation error")
			}
		})
	}
	if created {
		t.Fatal("createTemp was called for invalid arguments")
	}
}

func TestNewCleansUpFileWhenMappingFails(t *testing.T) {
	tempDir := t.TempDir()
	mapErr := errors.New("injected mapping failure")
	ops := defaultRegionFileOps
	ops.createTemp = func(pattern string) (*os.File, error) {
		return os.CreateTemp(tempDir, pattern)
	}
	ops.mapRegion = func(*os.File, int) (*platformMapping, error) {
		return nil, mapErr
	}

	if _, err := newRegion("cleanup-on-map-failure", 1024, ops); !errors.Is(err, mapErr) {
		t.Fatalf("newRegion() error = %v, want %v", err, mapErr)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files leaked after mapping failure: %v", entries)
	}
}

func TestNewReturnsCreateFailure(t *testing.T) {
	createErr := errors.New("injected create failure")
	ops := defaultRegionFileOps
	ops.createTemp = func(string) (*os.File, error) {
		return nil, createErr
	}

	if _, err := newRegion("create-failure", 1024, ops); !errors.Is(err, createErr) {
		t.Fatalf("newRegion() error = %v, want %v", err, createErr)
	}
}

func setTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	return dir
}

func pathFromHandle(t *testing.T, handle string) string {
	t.Helper()
	if !strings.HasPrefix(handle, "file://") {
		t.Fatalf("handle %q does not have file:// prefix", handle)
	}
	if runtime.GOOS != "windows" {
		return filepath.FromSlash(strings.TrimPrefix(handle, "file://"))
	}

	parsed, err := url.Parse(handle)
	if err != nil {
		t.Fatalf("parse handle %q: %v", handle, err)
	}
	if parsed.Scheme != "file" || parsed.Host != "" {
		t.Fatalf("parsed handle = %#v, want local file URL", parsed)
	}
	path := parsed.Path
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}
