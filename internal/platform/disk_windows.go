//go:build windows

package platform

import (
	"syscall"
	"unsafe"
)

// diskUsage 在 Windows 上通过 GetDiskFreeSpaceExW 查询卷容量。
func diskUsage(path string) (total, free uint64, err error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	r, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r == 0 {
		return 0, 0, callErr
	}
	return totalBytes, totalFreeBytes, nil
}
