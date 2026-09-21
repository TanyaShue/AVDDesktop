//go:build windows

package platform

import (
	"os"
	"syscall"
	"unsafe"
)

// kernel32/user32 过程调用句柄（延迟加载，避免额外依赖）。
var (
	kernel32Proc      = syscall.NewLazyDLL("kernel32.dll")
	user32Proc        = syscall.NewLazyDLL("user32.dll")
	procCreateMutexW  = kernel32Proc.NewProc("CreateMutexW")
	procReleaseMutex  = kernel32Proc.NewProc("ReleaseMutex")
	procCloseHandle   = kernel32Proc.NewProc("CloseHandle")
	procMessageBoxW   = user32Proc.NewProc("MessageBoxW")
	procFindWindowW   = user32Proc.NewProc("FindWindowW")
	procSetForeground = user32Proc.NewProc("SetForegroundWindow")
	procShowWindow    = user32Proc.NewProc("ShowWindow")
)

const (
	errorAlreadyExists = 183
	swRestore          = 9
	mbIconInformation  = 0x00000040
	mbOK               = 0x00000000
)

// instanceLock 持有 Windows 命名互斥体句柄。
type instanceLock struct {
	handle syscall.Handle
}

// AcquireSingleInstance 通过命名互斥体实现单实例。
//
// 返回的 release 必须在退出前调用；alreadyRunning 为 true 时表示已有实例在运行。
func AcquireSingleInstance(name string) (release func(), alreadyRunning bool, err error) {
	mutexName := "Global\\" + name + ".SingleInstance"
	namePtr, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return func() {}, false, err
	}
	handle, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if handle == 0 {
		return func() {}, false, callErr
	}
	// CreateMutex 成功但已存在同名对象时，GetLastError 会返回 ERROR_ALREADY_EXISTS
	if callErr == syscall.Errno(errorAlreadyExists) {
		_, _, _ = procCloseHandle.Call(handle)
		return func() {}, true, nil
	}

	lock := &instanceLock{handle: syscall.Handle(handle)}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_, _, _ = procReleaseMutex.Call(uintptr(lock.handle))
		_, _, _ = procCloseHandle.Call(uintptr(lock.handle))
	}, false, nil
}

// FocusExistingInstance 尝试把已有实例的窗口带到前台（尽力而为）。
func FocusExistingInstance(title string) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	_, _, _ = procShowWindow.Call(hwnd, swRestore)
	_, _, _ = procSetForeground.Call(hwnd)
}

// NotifyAlreadyRunning 弹出原生提示框（此时 Wails 还未启动，不能用 runtime 对话框）。
//
// 设置 AVDDESKTOP_NO_UI_DIALOG=1 时只写标准错误（自动化测试/静默启动用）。
func NotifyAlreadyRunning(title, message string) {
	if os.Getenv("AVDDESKTOP_NO_UI_DIALOG") == "1" {
		_, _ = os.Stderr.WriteString(title + ": " + message + "\n")
		return
	}
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	_, _, _ = procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(msgPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbOK|mbIconInformation))
}
