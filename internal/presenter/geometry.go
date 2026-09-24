package presenter

// rect is an integer rectangle in Win32 coordinates.
type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (r rect) width() int32  { return r.Right - r.Left }
func (r rect) height() int32 { return r.Bottom - r.Top }

// fitRect returns the largest rect with the device aspect ratio that fits in
// the client area and is centered in it.
func fitRect(clientW, clientH, deviceW, deviceH int32) (rect, bool) {
	if clientW <= 0 || clientH <= 0 || deviceW <= 0 || deviceH <= 0 {
		return rect{}, false
	}

	cw, ch := int64(clientW), int64(clientH)
	dw, dh := int64(deviceW), int64(deviceH)

	var width, height int64
	if cw*dh <= ch*dw {
		width = cw
		height = roundDiv(dh*cw, dw)
	} else {
		height = ch
		width = roundDiv(dw*ch, dh)
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if width > cw {
		width = cw
	}
	if height > ch {
		height = ch
	}

	return rect{
		Left:   int32((cw - width) / 2),
		Top:    int32((ch - height) / 2),
		Right:  int32((cw-width)/2 + width),
		Bottom: int32((ch-height)/2 + height),
	}, true
}

// clientToDevice maps a client-space point to absolute device coordinates.
//
// The returned inside value is false for points in the letterbox area. Even
// when outside, x and y are clamped to the nearest device pixel so the caller
// can always use them for a final touch release.
func clientToDevice(clientX, clientY, clientW, clientH, deviceW, deviceH int32) (x, y int32, inside bool) {
	r, ok := fitRect(clientW, clientH, deviceW, deviceH)
	if !ok {
		return 0, 0, false
	}

	inside = clientX >= r.Left && clientX < r.Right &&
		clientY >= r.Top && clientY < r.Bottom

	dx := int64(clientX) - int64(r.Left)
	dy := int64(clientY) - int64(r.Top)
	if dx < 0 {
		dx = 0
	} else if dx >= int64(r.width()) {
		dx = int64(r.width()) - 1
	}
	if dy < 0 {
		dy = 0
	} else if dy >= int64(r.height()) {
		dy = int64(r.height()) - 1
	}

	x = int32(dx * int64(deviceW) / int64(r.width()))
	y = int32(dy * int64(deviceH) / int64(r.height()))
	if x < 0 {
		x = 0
	} else if x >= deviceW {
		x = deviceW - 1
	}
	if y < 0 {
		y = 0
	} else if y >= deviceH {
		y = deviceH - 1
	}
	return x, y, inside
}

func roundDiv(n, d int64) int64 {
	if d <= 0 {
		return 0
	}
	return (n + d/2) / d
}
