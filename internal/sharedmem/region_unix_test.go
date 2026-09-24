//go:build unix

package sharedmem

import "testing"

func TestHandleForPathUnixPreservesRawPath(t *testing.T) {
	const path = "/tmp/android frame/frame.bin"
	handle, err := handleForPath(path)
	if err != nil {
		t.Fatalf("handleForPath() error = %v", err)
	}
	if want := "file://" + path; handle != want {
		t.Fatalf("handleForPath() = %q, want %q", handle, want)
	}
}
