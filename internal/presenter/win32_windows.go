//go:build windows

package presenter

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 constants used by the presenter. Values are from the Windows SDK.
const (
	csHRedraw = 0x0002
	csVRedraw = 0x0001

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsBorder           = 0x00800000
	wsTabStop          = 0x00010000
	wsExToolWindow     = 0x00000080

	swShow           = 5
	swShowNoActivate = 4

	swpNoSize     = 0x0001
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	wmDestroy        = 0x0002
	wmMove           = 0x0003
	wmSize           = 0x0005
	wmPaint          = 0x000F
	wmClose          = 0x0010
	wmEraseBkgnd     = 0x0014
	wmSetFont        = 0x0030
	wmKeyDown        = 0x0100
	wmCommand        = 0x0111
	wmMouseMove      = 0x0200
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmCaptureChanged = 0x0215
	wmNCCreate       = 0x0081
	wmNCDestroy      = 0x0082
	wmNCHitTest      = 0x0084
	wmDPICHanged     = 0x02E0
	wmApp            = 0x8000
	wmAppFrame       = wmApp + 1
	wmAppFatal       = wmApp + 2
	wmAppCancel      = wmApp + 3

	bnClicked = 0

	bsPushButton = 0

	biRGB        = 0
	dibRGBColors = 0
	srcCopy      = 0x00CC0020
	halftone     = 4

	blackBrush     = 4
	defaultGUIFont = 17

	colorButtonFacePlusOne = 16
	idcArrow               = 32512

	vkEscape = 0x1B

	monitorDefaultToNearest = 2
	htCaption               = 2
)

type point struct {
	X int32
	Y int32
}

type winMsg struct {
	Hwnd     windows.Handle
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type windowClassEx struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type createStruct struct {
	CreateParams uintptr
	Instance     windows.Handle
	Menu         windows.Handle
	Parent       windows.Handle
	Cy           int32
	Cx           int32
	Y            int32
	X            int32
	Style        int32
	Name         *uint16
	Class        *uint16
	ExStyle      uint32
}

type paintStruct struct {
	HDC       windows.Handle
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type rgbQuad struct {
	Blue     byte
	Green    byte
	Red      byte
	Reserved byte
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]rgbQuad
}

type monitorInfo struct {
	CbSize  uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

var (
	user32DLL   = windows.NewLazySystemDLL("user32.dll")
	gdi32DLL    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32DLL = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW   = user32DLL.NewProc("RegisterClassExW")
	procUnregisterClassW   = user32DLL.NewProc("UnregisterClassW")
	procCreateWindowExW    = user32DLL.NewProc("CreateWindowExW")
	procDestroyWindow      = user32DLL.NewProc("DestroyWindow")
	procDefWindowProcW     = user32DLL.NewProc("DefWindowProcW")
	procGetMessageW        = user32DLL.NewProc("GetMessageW")
	procTranslateMessage   = user32DLL.NewProc("TranslateMessage")
	procDispatchMessageW   = user32DLL.NewProc("DispatchMessageW")
	procPostQuitMessage    = user32DLL.NewProc("PostQuitMessage")
	procPostMessageW       = user32DLL.NewProc("PostMessageW")
	procSendMessageW       = user32DLL.NewProc("SendMessageW")
	procShowWindow         = user32DLL.NewProc("ShowWindow")
	procUpdateWindow       = user32DLL.NewProc("UpdateWindow")
	procInvalidateRect     = user32DLL.NewProc("InvalidateRect")
	procBeginPaint         = user32DLL.NewProc("BeginPaint")
	procEndPaint           = user32DLL.NewProc("EndPaint")
	procGetClientRect      = user32DLL.NewProc("GetClientRect")
	procGetWindowRect      = user32DLL.NewProc("GetWindowRect")
	procMoveWindow         = user32DLL.NewProc("MoveWindow")
	procSetWindowPos       = user32DLL.NewProc("SetWindowPos")
	procSetCapture         = user32DLL.NewProc("SetCapture")
	procReleaseCapture     = user32DLL.NewProc("ReleaseCapture")
	procLoadCursorW        = user32DLL.NewProc("LoadCursorW")
	procAdjustWindowRectEx = user32DLL.NewProc("AdjustWindowRectEx")
	procFillRect           = user32DLL.NewProc("FillRect")
	procMonitorFromWindow  = user32DLL.NewProc("MonitorFromWindow")
	procGetMonitorInfoW    = user32DLL.NewProc("GetMonitorInfoW")

	procGetDpiForWindow           = user32DLL.NewProc("GetDpiForWindow")
	procSetProcessDPIAware        = user32DLL.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwarenessCtx = user32DLL.NewProc("SetProcessDpiAwarenessContext")

	procStretchDIBits     = gdi32DLL.NewProc("StretchDIBits")
	procCreateDIBSection  = gdi32DLL.NewProc("CreateDIBSection")
	procDeleteObject      = gdi32DLL.NewProc("DeleteObject")
	procGetStockObject    = gdi32DLL.NewProc("GetStockObject")
	procSetStretchBltMode = gdi32DLL.NewProc("SetStretchBltMode")
	procSetBrushOrgEx     = gdi32DLL.NewProc("SetBrushOrgEx")

	procGetModuleHandleW = kernel32DLL.NewProc("GetModuleHandleW")

	dpiOnce      sync.Once
	moduleOnce   sync.Once
	moduleHandle windows.Handle
	moduleErr    error
)

func normalizeWinError(err error) error {
	if err == nil {
		return nil
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 0 {
		return nil
	}
	return err
}

func winErrorMessage(label string, err error) error {
	if normalized := normalizeWinError(err); normalized != nil {
		return fmt.Errorf("%s: %w", label, normalized)
	}
	return errors.New(label + " failed")
}

func winCall(proc *windows.LazyProc, args ...uintptr) (uintptr, uintptr, error) {
	if err := proc.Find(); err != nil {
		return 0, 0, err
	}
	return proc.Call(args...)
}

func callHandle(proc *windows.LazyProc, label string, args ...uintptr) (windows.Handle, error) {
	r1, _, err := winCall(proc, args...)
	if r1 == 0 {
		return 0, winErrorMessage(label, err)
	}
	return windows.Handle(r1), nil
}

func callBool(proc *windows.LazyProc, label string, args ...uintptr) error {
	r1, _, err := winCall(proc, args...)
	if r1 == 0 {
		return winErrorMessage(label, err)
	}
	return nil
}

func currentModule() (windows.Handle, error) {
	moduleOnce.Do(func() {
		r1, _, err := winCall(procGetModuleHandleW, 0)
		if r1 == 0 {
			moduleErr = winErrorMessage("GetModuleHandleW", err)
			return
		}
		moduleHandle = windows.Handle(r1)
	})
	return moduleHandle, moduleErr
}

func enableDPIAwareness() {
	dpiOnce.Do(func() {
		if err := procSetProcessDpiAwarenessCtx.Find(); err == nil {
			// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is -4.
			_, _, _ = winCall(procSetProcessDpiAwarenessCtx, ^uintptr(3))
			return
		}
		if err := procSetProcessDPIAware.Find(); err == nil {
			_, _, _ = winCall(procSetProcessDPIAware)
		}
	})
}

func windowDPI(hwnd windows.Handle) uint32 {
	if hwnd == 0 || procGetDpiForWindow.Find() != nil {
		return 96
	}
	r1, _, _ := winCall(procGetDpiForWindow, uintptr(hwnd))
	if r1 == 0 {
		return 96
	}
	return uint32(r1)
}

func registerWindowClass(name string, wndProc uintptr, background windows.Handle) (windows.Handle, error) {
	hInstance, err := currentModule()
	if err != nil {
		return 0, err
	}
	className, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("presenter: invalid window class name: %w", err)
	}
	class := windowClassEx{
		CbSize:     uint32(unsafe.Sizeof(windowClassEx{})),
		Style:      csHRedraw | csVRedraw,
		WndProc:    wndProc,
		Instance:   hInstance,
		Cursor:     loadArrowCursor(),
		Background: background,
		ClassName:  className,
	}
	r1, _, callErr := winCall(procRegisterClassExW, uintptr(unsafe.Pointer(&class)))
	if r1 == 0 {
		return 0, winErrorMessage("RegisterClassExW("+name+")", callErr)
	}
	return hInstance, nil
}

func unregisterWindowClass(name string, hInstance windows.Handle) {
	className, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return
	}
	_, _, _ = winCall(procUnregisterClassW, uintptr(unsafe.Pointer(className)), uintptr(hInstance))
}

func loadArrowCursor() windows.Handle {
	r1, _, _ := winCall(procLoadCursorW, 0, idcArrow)
	return windows.Handle(r1)
}

func createWindow(exStyle uint32, className, title string, style uint32, x, y, width, height int32, parent windows.Handle, menu uintptr, param uintptr) (windows.Handle, error) {
	hInstance, err := currentModule()
	if err != nil {
		return 0, err
	}
	classPtr, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return 0, fmt.Errorf("presenter: invalid window class name: %w", err)
	}
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0, fmt.Errorf("presenter: invalid window title: %w", err)
	}
	r1, _, callErr := winCall(
		procCreateWindowExW,
		uintptr(exStyle),
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(style),
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		uintptr(parent),
		menu,
		uintptr(hInstance),
		param,
	)
	if r1 == 0 {
		return 0, winErrorMessage("CreateWindowExW("+className+")", callErr)
	}
	return windows.Handle(r1), nil
}

func defWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	r1, _, _ := winCall(procDefWindowProcW, hwnd, msg, wparam, lparam)
	return r1
}

func destroyWindow(hwnd windows.Handle) bool {
	if hwnd == 0 {
		return false
	}
	r1, _, _ := winCall(procDestroyWindow, uintptr(hwnd))
	return r1 != 0
}

func postMessage(hwnd windows.Handle, msg uint32, wparam, lparam uintptr) error {
	if hwnd == 0 {
		return errors.New("presenter: window is not available")
	}
	return callBool(procPostMessageW, "PostMessageW", uintptr(hwnd), uintptr(msg), wparam, lparam)
}

func sendMessage(hwnd windows.Handle, msg uint32, wparam, lparam uintptr) uintptr {
	r1, _, _ := winCall(procSendMessageW, uintptr(hwnd), uintptr(msg), wparam, lparam)
	return r1
}

func showWindow(hwnd windows.Handle, command int32) {
	_, _, _ = winCall(procShowWindow, uintptr(hwnd), uintptr(command))
}

func updateWindow(hwnd windows.Handle) {
	_, _, _ = winCall(procUpdateWindow, uintptr(hwnd))
}

func invalidateRect(hwnd windows.Handle, r *rect) {
	var ptr uintptr
	if r != nil {
		ptr = uintptr(unsafe.Pointer(r))
	}
	_, _, _ = winCall(procInvalidateRect, uintptr(hwnd), ptr, 0)
}

func getClientRect(hwnd windows.Handle) (rect, error) {
	var r rect
	if err := callBool(procGetClientRect, "GetClientRect", uintptr(hwnd), uintptr(unsafe.Pointer(&r))); err != nil {
		return rect{}, err
	}
	return r, nil
}

func getWindowRect(hwnd windows.Handle) (rect, error) {
	var r rect
	if err := callBool(procGetWindowRect, "GetWindowRect", uintptr(hwnd), uintptr(unsafe.Pointer(&r))); err != nil {
		return rect{}, err
	}
	return r, nil
}

func adjustWindowRectEx(r *rect, style uint32, menu bool, exStyle uint32) error {
	menuFlag := uintptr(0)
	if menu {
		menuFlag = 1
	}
	return callBool(procAdjustWindowRectEx, "AdjustWindowRectEx", uintptr(unsafe.Pointer(r)), uintptr(style), menuFlag, uintptr(exStyle))
}

func moveWindow(hwnd windows.Handle, x, y, width, height int32, repaint bool) error {
	repaintFlag := uintptr(0)
	if repaint {
		repaintFlag = 1
	}
	return callBool(procMoveWindow, "MoveWindow", uintptr(hwnd), uintptr(x), uintptr(y), uintptr(width), uintptr(height), repaintFlag)
}

func setWindowPos(hwnd, insertAfter windows.Handle, x, y, width, height int32, flags uint32) error {
	return callBool(
		procSetWindowPos,
		"SetWindowPos",
		uintptr(hwnd),
		uintptr(insertAfter),
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		uintptr(flags),
	)
}

func setCapture(hwnd windows.Handle) {
	_, _, _ = winCall(procSetCapture, uintptr(hwnd))
}

func releaseCapture() {
	_, _, _ = winCall(procReleaseCapture)
}

func beginPaint(hwnd windows.Handle) (windows.Handle, paintStruct, error) {
	var paint paintStruct
	hdc, err := callHandle(procBeginPaint, "BeginPaint", uintptr(hwnd), uintptr(unsafe.Pointer(&paint)))
	if err != nil {
		return 0, paintStruct{}, err
	}
	return hdc, paint, nil
}

func endPaint(hwnd windows.Handle, paint *paintStruct) {
	_, _, _ = winCall(procEndPaint, uintptr(hwnd), uintptr(unsafe.Pointer(paint)))
}

func fillRect(hdc windows.Handle, r *rect, brush windows.Handle) {
	_, _, _ = winCall(procFillRect, uintptr(hdc), uintptr(unsafe.Pointer(r)), uintptr(brush))
}

func stockObject(id int32) windows.Handle {
	r1, _, _ := winCall(procGetStockObject, uintptr(id))
	return windows.Handle(r1)
}

func createDIBSection(hdc windows.Handle, info *bitmapInfo) (windows.Handle, uintptr, error) {
	var bits uintptr
	hBitmap, _, callErr := winCall(
		procCreateDIBSection,
		uintptr(hdc),
		uintptr(unsafe.Pointer(info)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if hBitmap == 0 {
		return 0, 0, winErrorMessage("CreateDIBSection", callErr)
	}
	if bits == 0 {
		deleteObject(windows.Handle(hBitmap))
		return 0, 0, errors.New("CreateDIBSection returned a nil bitmap pointer")
	}
	return windows.Handle(hBitmap), bits, nil
}

func deleteObject(object windows.Handle) {
	if object == 0 {
		return
	}
	_, _, _ = winCall(procDeleteObject, uintptr(object))
}

func drawDIB(hdc windows.Handle, destination rect, sourceWidth, sourceHeight int32, bits uintptr, info *bitmapInfo) bool {
	r1, _, _ := winCall(
		procStretchDIBits,
		uintptr(hdc),
		uintptr(destination.Left),
		uintptr(destination.Top),
		uintptr(destination.width()),
		uintptr(destination.height()),
		0,
		0,
		uintptr(sourceWidth),
		uintptr(sourceHeight),
		bits,
		uintptr(unsafe.Pointer(info)),
		dibRGBColors,
		srcCopy,
	)
	return r1 != 0
}

func prepareStretchBlt(hdc windows.Handle) {
	_, _, _ = winCall(procSetStretchBltMode, uintptr(hdc), halftone)
	_, _, _ = winCall(procSetBrushOrgEx, uintptr(hdc), 0, 0, 0)
}

func monitorWorkArea(hwnd windows.Handle) (rect, bool) {
	monitor, _, _ := winCall(procMonitorFromWindow, uintptr(hwnd), monitorDefaultToNearest)
	if monitor == 0 {
		return rect{}, false
	}
	info := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	r1, _, _ := winCall(procGetMonitorInfoW, monitor, uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return rect{}, false
	}
	return info.Work, true
}

func mousePoint(lparam uintptr) (int32, int32) {
	x := int32(int16(uint16(lparam & 0xffff)))
	y := int32(int16(uint16((lparam >> 16) & 0xffff)))
	return x, y
}

func lowWord(value uintptr) uint16 {
	return uint16(value & 0xffff)
}

func highWord(value uintptr) uint16 {
	return uint16((value >> 16) & 0xffff)
}
