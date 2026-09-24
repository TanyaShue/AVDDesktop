//go:build windows

package sharedmem

import (
	"net/url"
	"testing"
)

func TestHandleForPathWindowsDrive(t *testing.T) {
	const path = `C:\Users\Android User\共享\frame.bin`
	handle, err := handleForPath(path)
	if err != nil {
		t.Fatalf("handleForPath() error = %v", err)
	}
	parsed, err := url.Parse(handle)
	if err != nil {
		t.Fatalf("parse handle %q: %v", handle, err)
	}
	if parsed.Scheme != "file" {
		t.Fatalf("scheme = %q, want file", parsed.Scheme)
	}
	if parsed.Host != "" {
		t.Fatalf("host = %q, want empty", parsed.Host)
	}
	if want := "/C:/Users/Android User/共享/frame.bin"; parsed.Path != want {
		t.Fatalf("path = %q, want %q", parsed.Path, want)
	}
	if !containsEscapedSpace(handle) {
		t.Fatalf("handle %q does not percent-encode spaces", handle)
	}
}

func TestHandleForPathWindowsUNC(t *testing.T) {
	handle, err := handleForPath(`\\server\share name\frame.bin`)
	if err != nil {
		t.Fatalf("handleForPath() error = %v", err)
	}
	parsed, err := url.Parse(handle)
	if err != nil {
		t.Fatalf("parse handle %q: %v", handle, err)
	}
	if parsed.Host != "server" || parsed.Path != "/share name/frame.bin" {
		t.Fatalf("parsed handle = %#v, want UNC file URL", parsed)
	}
}

func containsEscapedSpace(handle string) bool {
	for i := 0; i+2 < len(handle); i++ {
		if handle[i:i+3] == "%20" {
			return true
		}
	}
	return false
}
