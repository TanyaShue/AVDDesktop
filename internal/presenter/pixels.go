package presenter

import "fmt"

// rgbaToBGRA converts a top-down RGBA8888 frame into the byte order expected
// by a 32-bit BI_RGB GDI DIB. Alpha is preserved; GDI ignores it for BI_RGB.
func rgbaToBGRA(pix []byte, width, height int) ([]byte, error) {
	total, ok := checkedPixelBytes(width, height)
	if !ok {
		return nil, fmt.Errorf("presenter: invalid frame size %dx%d", width, height)
	}
	if len(pix) < total {
		return nil, fmt.Errorf("presenter: short frame: got %d bytes, need %d", len(pix), total)
	}

	dst := make([]byte, total)
	for src := 0; src < total; src += 4 {
		dst[src+0] = pix[src+2] // B
		dst[src+1] = pix[src+1] // G
		dst[src+2] = pix[src+0] // R
		dst[src+3] = pix[src+3] // A
	}
	return dst, nil
}

func checkedPixelBytes(width, height int) (int, bool) {
	if width <= 0 || height <= 0 {
		return 0, false
	}
	if int64(width) > int64(^uint32(0)>>1) || int64(height) > int64(^uint32(0)>>1) {
		return 0, false
	}
	w, h := uint64(width), uint64(height)
	maxUint := ^uint64(0)
	if w > maxUint/4/h {
		return 0, false
	}
	total := w * h * 4
	if total > uint64(^uint(0)>>1) || total > uint64(^uint32(0)) {
		return 0, false
	}
	return int(total), true
}
