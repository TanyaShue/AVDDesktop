//go:build windows

package presenter

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	presenterWindowClassPrefix = "AVDDesktopPresenterWindow_"
	toolbarWindowClassPrefix   = "AVDDesktopPresenterToolbar_"

	buttonBackID      = 1001
	buttonHomeID      = 1002
	buttonAppSwitchID = 1003
	buttonCloseID     = 1004
)

type dibSection struct {
	bitmap windows.Handle
	info   bitmapInfo
	pixels []byte
	width  int32
	height int32
}

type renderedFrame struct {
	pixels []byte
	width  int32
	height int32
	seq    uint32
}

type presenterWindow struct {
	cfg    Config
	runCtx context.Context
	cancel context.CancelFunc

	mu sync.Mutex

	presenterHwnd windows.Handle
	toolbarHwnd   windows.Handle
	buttons       [4]windows.Handle

	pending *renderedFrame
	dib     *dibSection

	frameWidth  int32
	frameHeight int32

	touching    bool
	lastDeviceX int32
	lastDeviceY int32

	initErr    error
	fatalErr   error
	contextErr error
	stopped    bool
}

type runResult struct {
	started bool
	err     error
}

var (
	presenterCallbacks = struct {
		presenter uintptr
		toolbar   uintptr
	}{
		presenter: syscall.NewCallback(presenterWindowProc),
		toolbar:   syscall.NewCallback(toolbarWindowProc),
	}

	registryMu       sync.RWMutex
	presenterWindows = map[windows.Handle]*presenterWindow{}
	toolbarWindows   = map[windows.Handle]*presenterWindow{}

	classSequence atomic.Uint64
)

// Supported reports whether this build has a native presenter backend.
func Supported() bool { return true }

// Run creates and runs the native presenter until the window is closed, the
// frame source fails, or ctx is canceled. Native window creation is performed
// on a dedicated OS thread so the function can also be used by an in-process
// host that already owns its own UI thread.
func Run(ctx context.Context, cfg Config) error {
	if ctx == nil {
		return errors.New("presenter: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	result := make(chan runResult, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		result <- runOnWindowThread(ctx, cfg)
	}()

	outcome := <-result
	if outcome.started && cfg.OnClose != nil {
		cfg.OnClose()
	}
	return outcome.err
}

func runOnWindowThread(ctx context.Context, cfg Config) (result runResult) {
	enableDPIAwareness()

	presenterClass := uniqueWindowClass(presenterWindowClassPrefix, cfg.InstanceID)
	toolbarClass := uniqueWindowClass(toolbarWindowClassPrefix, cfg.InstanceID)

	hInstance, err := registerWindowClass(presenterClass, presenterCallbacks.presenter, 0)
	if err != nil {
		return runResult{err: err}
	}
	defer unregisterWindowClass(presenterClass, hInstance)

	hInstance, err = registerWindowClass(toolbarClass, presenterCallbacks.toolbar, windows.Handle(colorButtonFacePlusOne))
	if err != nil {
		return runResult{err: err}
	}
	defer unregisterWindowClass(toolbarClass, hInstance)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	p := &presenterWindow{
		cfg:    cfg,
		runCtx: runCtx,
		cancel: cancel,
	}
	defer func() {
		p.destroyWindows()
		p.releaseDIB()
	}()

	if err := p.createPresenter(presenterClass); err != nil {
		return runResult{err: err}
	}
	if err := p.createToolbar(toolbarClass); err != nil {
		return runResult{err: err}
	}
	if err := p.createToolbarButtons(); err != nil {
		return runResult{err: err}
	}

	p.positionToolbar()
	showWindow(p.presenterHandle(), swShow)
	showWindow(p.toolbarHandle(), swShowNoActivate)
	p.invalidatePresenter()

	var (
		done    = make(chan struct{})
		wait    sync.WaitGroup
		loopErr error
	)
	wait.Add(2)
	go func() {
		defer wait.Done()
		p.consumeFrames()
	}()
	go func() {
		defer wait.Done()
		select {
		case <-ctx.Done():
			p.requestContextClose(ctx.Err())
		case <-done:
		}
	}()

	result.started = true
	loopErr = p.messageLoop()
	cancel()
	close(done)
	wait.Wait()
	p.destroyWindows()
	p.releaseDIB()

	if fatal := p.fatalError(); fatal != nil {
		return runResult{started: true, err: fatal}
	}
	if ctxErr := p.contextError(); ctxErr != nil {
		return runResult{started: true, err: ctxErr}
	}
	return runResult{started: true, err: loopErr}
}

func uniqueWindowClass(prefix, instanceID string) string {
	suffix := sanitizeClassName(instanceID)
	if suffix == "" {
		suffix = fmt.Sprintf("instance-%d", classSequence.Add(1))
	} else {
		suffix = fmt.Sprintf("%s-%d", suffix, classSequence.Add(1))
	}
	if len(suffix) > 96 {
		suffix = suffix[:96]
	}
	return prefix + suffix
}

func sanitizeClassName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (p *presenterWindow) createPresenter(className string) error {
	clientWidth, clientHeight := initialClientSize(int32(p.cfg.DeviceWidth), int32(p.cfg.DeviceHeight))
	outer := rect{0, 0, clientWidth, clientHeight}
	if err := adjustWindowRectEx(&outer, wsOverlappedWindow, false, 0); err != nil {
		return err
	}
	_, err := createWindow(
		0,
		className,
		p.cfg.windowTitle(),
		wsOverlappedWindow,
		100,
		100,
		outer.width(),
		outer.height(),
		0,
		0,
		uintptr(unsafe.Pointer(p)),
	)
	return err
}

func (p *presenterWindow) createToolbar(className string) error {
	metrics := toolbarMetricsForDPI(windowDPI(p.presenterHandle()))
	outer := rect{0, 0, metrics.clientWidth, metrics.clientHeight}
	if err := adjustWindowRectEx(&outer, wsPopup|wsBorder, false, wsExToolWindow); err != nil {
		return err
	}
	_, err := createWindow(
		wsExToolWindow,
		className,
		"Device controls",
		wsPopup|wsBorder,
		0,
		0,
		outer.width(),
		outer.height(),
		0,
		0,
		uintptr(unsafe.Pointer(p)),
	)
	return err
}

func (p *presenterWindow) createToolbarButtons() error {
	labels := [...]string{"Back", "Home", "Apps", "Close"}
	ids := [...]uintptr{buttonBackID, buttonHomeID, buttonAppSwitchID, buttonCloseID}
	font := stockObject(defaultGUIFont)

	for i := range labels {
		button, err := createWindow(
			0,
			"BUTTON",
			labels[i],
			wsChild|wsVisible|wsTabStop|bsPushButton,
			0,
			0,
			10,
			10,
			p.toolbarHandle(),
			ids[i],
			0,
		)
		if err != nil {
			return err
		}
		p.mu.Lock()
		p.buttons[i] = button
		p.mu.Unlock()
		if font != 0 {
			sendMessage(button, wmSetFont, uintptr(font), 1)
		}
	}
	p.layoutToolbarButtons()
	return nil
}

type toolbarMetrics struct {
	clientWidth  int32
	clientHeight int32
	margin       int32
	gap          int32
	widths       [4]int32
}

func toolbarMetricsForDPI(dpi uint32) toolbarMetrics {
	scale := func(v int32) int32 { return scaleDPI(v, dpi) }
	widths := [4]int32{scale(58), scale(58), scale(58), scale(64)}
	margin, gap := scale(4), scale(4)
	width := margin*2 + gap*3
	for _, w := range widths {
		width += w
	}
	return toolbarMetrics{
		clientWidth:  width,
		clientHeight: scale(32),
		margin:       margin,
		gap:          gap,
		widths:       widths,
	}
}

func scaleDPI(value int32, dpi uint32) int32 {
	if dpi == 0 {
		dpi = 96
	}
	if value < 1 {
		return value
	}
	scaled := (int64(value)*int64(dpi) + 48) / 96
	if scaled < 1 {
		scaled = 1
	}
	return int32(scaled)
}

func (p *presenterWindow) layoutToolbarButtons() {
	hwnd := p.toolbarHandle()
	if hwnd == 0 {
		return
	}
	client, err := getClientRect(hwnd)
	if err != nil || client.width() <= 0 || client.height() <= 0 {
		return
	}
	metrics := toolbarMetricsForDPI(windowDPI(hwnd))
	available := client.width() - metrics.margin*2 - metrics.gap*3
	if available <= 0 {
		return
	}

	widths := metrics.widths
	total := int32(0)
	for _, width := range widths {
		total += width
	}
	if total > available {
		for i := range widths {
			widths[i] = widths[i] * available / total
			if widths[i] < 1 {
				widths[i] = 1
			}
		}
	}
	height := client.height() - metrics.margin*2
	if height < 1 {
		height = client.height()
	}

	p.mu.Lock()
	buttons := p.buttons
	p.mu.Unlock()

	x := metrics.margin
	for i, button := range buttons {
		if button == 0 {
			continue
		}
		_ = moveWindow(button, x, metrics.margin, widths[i], height, true)
		x += widths[i] + metrics.gap
	}
}

func (p *presenterWindow) messageLoop() error {
	var message winMsg
	for {
		r1, _, callErr := winCall(procGetMessageW, uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(uint32(r1)) == -1 {
			if err := normalizeWinError(callErr); err != nil {
				return fmt.Errorf("presenter: GetMessageW: %w", err)
			}
			return errors.New("presenter: GetMessageW failed")
		}
		if r1 == 0 {
			return nil
		}
		_, _, _ = winCall(procTranslateMessage, uintptr(unsafe.Pointer(&message)))
		_, _, _ = winCall(procDispatchMessageW, uintptr(unsafe.Pointer(&message)))
	}
}

func (p *presenterWindow) consumeFrames() {
	for {
		if p.runCtx == nil || p.runCtx.Err() != nil {
			return
		}
		frame, err := p.cfg.Source.Next(p.runCtx)
		if err != nil {
			if p.runCtx.Err() != nil {
				return
			}
			p.fail(fmt.Errorf("presenter: frame source: %w", err))
			return
		}
		if frame.Width <= 0 || frame.Height <= 0 || int64(frame.Width) > int64(^uint32(0)>>1) || int64(frame.Height) > int64(^uint32(0)>>1) {
			p.fail(fmt.Errorf("presenter: invalid frame size %dx%d", frame.Width, frame.Height))
			return
		}
		pixels, err := rgbaToBGRA(frame.Pix, frame.Width, frame.Height)
		if err != nil {
			p.fail(err)
			return
		}
		p.setPending(&renderedFrame{
			pixels: pixels,
			width:  int32(frame.Width),
			height: int32(frame.Height),
			seq:    frame.Seq,
		})
	}
}

func (p *presenterWindow) setPending(frame *renderedFrame) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.pending = frame
	hwnd := p.presenterHwnd
	p.mu.Unlock()
	if hwnd != 0 {
		_ = postMessage(hwnd, wmAppFrame, 0, 0)
	}
}

func (p *presenterWindow) takePending() *renderedFrame {
	p.mu.Lock()
	defer p.mu.Unlock()
	frame := p.pending
	p.pending = nil
	return frame
}

func (p *presenterWindow) paint(hwnd windows.Handle) {
	hdc, paint, err := beginPaint(hwnd)
	if err != nil {
		p.fail(err)
		return
	}
	defer endPaint(hwnd, &paint)

	client, err := getClientRect(hwnd)
	if err != nil {
		p.fail(err)
		return
	}
	if brush := stockObject(blackBrush); brush != 0 {
		fillRect(hdc, &client, brush)
	}

	if pending := p.takePending(); pending != nil {
		if err := p.ensureDIB(hdc, pending.width, pending.height); err != nil {
			p.fail(err)
			return
		}
		if len(p.dib.pixels) < len(pending.pixels) {
			p.fail(errors.New("presenter: DIB buffer is smaller than the frame"))
			return
		}
		copy(p.dib.pixels, pending.pixels)
		p.mu.Lock()
		p.frameWidth = pending.width
		p.frameHeight = pending.height
		p.mu.Unlock()
	}

	dib := p.dib
	if dib == nil {
		return
	}
	destination, ok := fitRect(client.width(), client.height(), dib.width, dib.height)
	if !ok {
		return
	}
	prepareStretchBlt(hdc)
	if !drawDIB(hdc, destination, dib.width, dib.height, uintptr(unsafe.Pointer(&dib.pixels[0])), &dib.info) {
		p.fail(errors.New("presenter: StretchDIBits failed"))
	}
}

func (p *presenterWindow) ensureDIB(hdc windows.Handle, width, height int32) error {
	if p.dib != nil && p.dib.width == width && p.dib.height == height {
		return nil
	}
	p.releaseDIB()

	total, ok := checkedPixelBytes(int(width), int(height))
	if !ok {
		return fmt.Errorf("presenter: invalid DIB size %dx%d", width, height)
	}
	info := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:       width,
			Height:      -height, // top-down DIB
			Planes:      1,
			BitCount:    32,
			Compression: biRGB,
			SizeImage:   uint32(total),
		},
	}
	bitmap, bits, err := createDIBSection(hdc, &info)
	if err != nil {
		return err
	}
	p.dib = &dibSection{
		bitmap: bitmap,
		info:   info,
		pixels: unsafe.Slice((*byte)(unsafe.Pointer(bits)), total),
		width:  width,
		height: height,
	}
	return nil
}

func (p *presenterWindow) releaseDIB() {
	if p.dib == nil {
		return
	}
	deleteObject(p.dib.bitmap)
	p.dib = nil
}

func (p *presenterWindow) invalidatePresenter() {
	if hwnd := p.presenterHandle(); hwnd != 0 {
		invalidateRect(hwnd, nil)
	}
}

func (p *presenterWindow) positionToolbar() {
	presenter := p.presenterHandle()
	toolbar := p.toolbarHandle()
	if presenter == 0 || toolbar == 0 {
		return
	}
	presenterRect, err := getWindowRect(presenter)
	if err != nil {
		return
	}
	toolbarRect, err := getWindowRect(toolbar)
	if err != nil || toolbarRect.width() <= 0 || toolbarRect.height() <= 0 {
		return
	}

	gap := scaleDPI(6, windowDPI(presenter))
	x := presenterRect.Left
	y := presenterRect.Top - toolbarRect.height() - gap

	if work, ok := monitorWorkArea(presenter); ok {
		if y < work.Top {
			y = presenterRect.Bottom + gap
		}
		if x+toolbarRect.width() > work.Right {
			x = work.Right - toolbarRect.width()
		}
		if x < work.Left {
			x = work.Left
		}
		if y+toolbarRect.height() > work.Bottom {
			y = work.Bottom - toolbarRect.height()
		}
		if y < work.Top {
			y = work.Top
		}
	}
	_ = setWindowPos(toolbar, 0, x, y, 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func (p *presenterWindow) updateTouchPosition(hwnd windows.Handle, lparam uintptr) (int32, int32, bool, bool) {
	client, err := getClientRect(hwnd)
	if err != nil || client.width() <= 0 || client.height() <= 0 {
		return 0, 0, false, false
	}
	deviceWidth, deviceHeight := p.deviceSize()
	clientX, clientY := mousePoint(lparam)
	x, y, inside := clientToDevice(clientX, clientY, client.width(), client.height(), deviceWidth, deviceHeight)
	return x, y, inside, true
}

func (p *presenterWindow) deviceSize() (int32, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.frameWidth > 0 && p.frameHeight > 0 {
		return p.frameWidth, p.frameHeight
	}
	return int32(p.cfg.DeviceWidth), int32(p.cfg.DeviceHeight)
}

func (p *presenterWindow) sendTouch(x, y int32, release bool, fatal bool) {
	if p.cfg.SendTouch == nil {
		return
	}
	if err := p.cfg.SendTouch(x, y, release); err != nil && fatal {
		p.fail(fmt.Errorf("presenter: send touch: %w", err))
	}
}

func (p *presenterWindow) releaseTouch(fatal bool) {
	p.mu.Lock()
	if !p.touching {
		p.mu.Unlock()
		return
	}
	p.touching = false
	x, y := p.lastDeviceX, p.lastDeviceY
	p.mu.Unlock()
	p.sendTouch(x, y, true, fatal)
}

func (p *presenterWindow) handleMouseDown(hwnd windows.Handle, lparam uintptr) {
	if p.cfg.SendTouch == nil {
		return
	}
	x, y, inside, valid := p.updateTouchPosition(hwnd, lparam)
	if !valid || !inside {
		return
	}
	p.mu.Lock()
	p.touching = true
	p.lastDeviceX, p.lastDeviceY = x, y
	p.mu.Unlock()
	setCapture(hwnd)
	p.sendTouch(x, y, false, true)
}

func (p *presenterWindow) handleMouseMove(hwnd windows.Handle, lparam uintptr) {
	p.mu.Lock()
	touching := p.touching
	p.mu.Unlock()
	if !touching {
		return
	}
	x, y, inside, valid := p.updateTouchPosition(hwnd, lparam)
	if !valid {
		p.releaseTouch(false)
		releaseCapture()
		return
	}
	if !inside {
		p.mu.Lock()
		p.touching = false
		p.mu.Unlock()
		releaseCapture()
		p.sendTouch(x, y, true, false)
		return
	}
	p.mu.Lock()
	p.lastDeviceX, p.lastDeviceY = x, y
	p.mu.Unlock()
	p.sendTouch(x, y, false, true)
}

func (p *presenterWindow) handleMouseUp(hwnd windows.Handle, lparam uintptr) {
	p.mu.Lock()
	touching := p.touching
	p.mu.Unlock()
	if !touching {
		releaseCapture()
		return
	}
	x, y, _, valid := p.updateTouchPosition(hwnd, lparam)
	if !valid {
		p.releaseTouch(false)
		releaseCapture()
		return
	}
	p.mu.Lock()
	p.touching = false
	p.mu.Unlock()
	releaseCapture()
	p.sendTouch(x, y, true, true)
}

func (p *presenterWindow) handleCaptureChanged() {
	p.releaseTouch(false)
}

func (p *presenterWindow) handleDPIChanged(hwnd windows.Handle, lparam uintptr) {
	if lparam == 0 {
		return
	}
	suggested := (*rect)(unsafe.Pointer(lparam))
	_ = setWindowPos(hwnd, 0, suggested.Left, suggested.Top, suggested.width(), suggested.height(), swpNoZOrder|swpNoActivate)
	if hwnd == p.toolbarHandle() {
		p.layoutToolbarButtons()
	} else {
		p.positionToolbar()
		p.invalidatePresenter()
	}
}

func (p *presenterWindow) handleToolbarCommand(id uint16) {
	switch id {
	case buttonBackID:
		p.sendKey("Back")
	case buttonHomeID:
		p.sendKey("Home")
	case buttonAppSwitchID:
		p.sendKey("AppSwitch")
	case buttonCloseID:
		p.shutdown()
	}
}

func (p *presenterWindow) sendKey(key string) {
	if p.cfg.SendKey == nil {
		return
	}
	if err := p.cfg.SendKey(key); err != nil {
		p.fail(fmt.Errorf("presenter: send key %s: %w", key, err))
	}
}

func (p *presenterWindow) fail(err error) {
	if err == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	if p.fatalErr == nil {
		p.fatalErr = err
	}
	hwnd := p.presenterHwnd
	p.mu.Unlock()
	if hwnd != 0 {
		_ = postMessage(hwnd, wmAppFatal, 0, 0)
	}
}

func (p *presenterWindow) requestContextClose(err error) {
	if err == nil {
		err = context.Canceled
	}
	p.mu.Lock()
	if p.contextErr == nil {
		p.contextErr = err
	}
	hwnd := p.presenterHwnd
	p.mu.Unlock()
	if hwnd != 0 {
		_ = postMessage(hwnd, wmAppCancel, 0, 0)
	}
}

func (p *presenterWindow) fatalError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fatalErr
}

func (p *presenterWindow) contextError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.contextErr
}

func (p *presenterWindow) presenterHandle() windows.Handle {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.presenterHwnd
}

func (p *presenterWindow) toolbarHandle() windows.Handle {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.toolbarHwnd
}

func (p *presenterWindow) shutdown() {
	p.releaseTouch(false)
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	p.destroyWindows()
}

func (p *presenterWindow) destroyWindows() {
	if toolbar := p.toolbarHandle(); toolbar != 0 {
		destroyWindow(toolbar)
	}
	if presenter := p.presenterHandle(); presenter != 0 {
		if !destroyWindow(presenter) {
			_, _, _ = winCall(procPostQuitMessage, 0)
		}
	}
}

func (p *presenterWindow) attachPresenter(hwnd windows.Handle) error {
	p.mu.Lock()
	if p.presenterHwnd != 0 {
		p.mu.Unlock()
		return errors.New("presenter: presenter window already attached")
	}
	p.presenterHwnd = hwnd
	p.mu.Unlock()

	registryMu.Lock()
	presenterWindows[hwnd] = p
	registryMu.Unlock()
	return nil
}

func (p *presenterWindow) attachToolbar(hwnd windows.Handle) error {
	p.mu.Lock()
	if p.toolbarHwnd != 0 {
		p.mu.Unlock()
		return errors.New("presenter: toolbar window already attached")
	}
	p.toolbarHwnd = hwnd
	p.mu.Unlock()

	registryMu.Lock()
	toolbarWindows[hwnd] = p
	registryMu.Unlock()
	return nil
}

func (p *presenterWindow) setInitError(err error) {
	p.mu.Lock()
	if p.initErr == nil {
		p.initErr = err
	}
	p.mu.Unlock()
}

func (p *presenterWindow) initializationError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initErr
}

func lookupPresenter(hwnd windows.Handle) *presenterWindow {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return presenterWindows[hwnd]
}

func lookupToolbar(hwnd windows.Handle) *presenterWindow {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return toolbarWindows[hwnd]
}

func detachPresenter(hwnd windows.Handle) {
	registryMu.Lock()
	delete(presenterWindows, hwnd)
	registryMu.Unlock()
}

func detachToolbar(hwnd windows.Handle) {
	registryMu.Lock()
	delete(toolbarWindows, hwnd)
	registryMu.Unlock()
}

func presenterWindowProc(hwnd, msg, wparam, lparam uintptr) (result uintptr) {
	window := windows.Handle(hwnd)
	if msg == wmNCCreate {
		create := (*createStruct)(unsafe.Pointer(lparam))
		p := (*presenterWindow)(unsafe.Pointer(create.CreateParams))
		if p == nil {
			return 0
		}
		if err := p.attachPresenter(window); err != nil {
			p.setInitError(err)
			return 0
		}
		return 1
	}

	p := lookupPresenter(window)
	if p == nil {
		return defWindowProc(hwnd, msg, wparam, lparam)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			p.fail(fmt.Errorf("presenter: window callback panic: %v", recovered))
			result = 0
		}
	}()

	switch msg {
	case wmClose:
		p.shutdown()
		return 0
	case wmDestroy:
		p.handlePresenterDestroy()
		return 0
	case wmNCDestroy:
		detachPresenter(window)
	case wmMove, wmSize:
		p.positionToolbar()
		p.invalidatePresenter()
		return 0
	case wmPaint:
		p.paint(window)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmLButtonDown:
		p.handleMouseDown(window, lparam)
		return 0
	case wmMouseMove:
		p.handleMouseMove(window, lparam)
		return 0
	case wmLButtonUp:
		p.handleMouseUp(window, lparam)
		return 0
	case wmCaptureChanged:
		p.handleCaptureChanged()
		return 0
	case wmKeyDown:
		if wparam == vkEscape {
			p.shutdown()
			return 0
		}
	case wmDPICHanged:
		p.handleDPIChanged(window, lparam)
		return 0
	case wmAppFrame:
		p.invalidatePresenter()
		return 0
	case wmAppFatal:
		p.shutdown()
		return 0
	case wmAppCancel:
		p.shutdown()
		return 0
	}
	return defWindowProc(hwnd, msg, wparam, lparam)
}

func toolbarWindowProc(hwnd, msg, wparam, lparam uintptr) (result uintptr) {
	window := windows.Handle(hwnd)
	if msg == wmNCCreate {
		create := (*createStruct)(unsafe.Pointer(lparam))
		p := (*presenterWindow)(unsafe.Pointer(create.CreateParams))
		if p == nil {
			return 0
		}
		if err := p.attachToolbar(window); err != nil {
			p.setInitError(err)
			return 0
		}
		return 1
	}

	p := lookupToolbar(window)
	if p == nil {
		return defWindowProc(hwnd, msg, wparam, lparam)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			p.fail(fmt.Errorf("presenter: toolbar callback panic: %v", recovered))
			result = 0
		}
	}()

	switch msg {
	case wmCommand:
		if highWord(wparam) == bnClicked {
			p.handleToolbarCommand(lowWord(wparam))
		}
		return 0
	case wmClose:
		p.shutdown()
		return 0
	case wmNCDestroy:
		detachToolbar(window)
	case wmDestroy:
		p.handleToolbarDestroy()
		return 0
	case wmDPICHanged:
		p.handleDPIChanged(window, lparam)
		return 0
	case wmNCHitTest:
		return htCaption
	}
	return defWindowProc(hwnd, msg, wparam, lparam)
}

func (p *presenterWindow) handlePresenterDestroy() {
	p.releaseTouch(false)
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	p.presenterHwnd = 0
	p.pending = nil
	p.stopped = true
	toolbar := p.toolbarHwnd
	p.mu.Unlock()
	if toolbar != 0 {
		destroyWindow(toolbar)
	}
	_, _, _ = winCall(procPostQuitMessage, 0)
}

func (p *presenterWindow) handleToolbarDestroy() {
	p.mu.Lock()
	p.toolbarHwnd = 0
	p.buttons = [4]windows.Handle{}
	p.mu.Unlock()
}

func initialClientSize(deviceWidth, deviceHeight int32) (int32, int32) {
	fitted, ok := fitRect(960, 640, deviceWidth, deviceHeight)
	if !ok {
		return 640, 480
	}
	width, height := fitted.width(), fitted.height()
	if width < 320 {
		height = height * 320 / width
		width = 320
	}
	if height < 240 {
		width = width * 240 / height
		height = 240
	}
	return width, height
}
