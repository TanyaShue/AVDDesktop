package display

import "math"

// MapTouch 把前端传来的归一化坐标（[0,1]，相对设备窗口的显示区域）映射为设备像素坐标。
//
// 规则：
//   - 正常值按 round(v * size) 换算到像素，越界（含负数、>1）夹取到 [0, size-1]
//   - NaN / ±Inf 视为无效输入，归 0（前端在窗口尺寸未知时可能传入这类值）
//   - 设备尺寸非正时归 0
func MapTouch(x, y float64, w, h int) (int32, int32) {
	return mapAxis(x, w), mapAxis(y, h)
}

func mapAxis(v float64, size int) int32 {
	if size <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	px := math.Round(v * float64(size))
	if px <= 0 {
		return 0
	}
	if max := float64(size - 1); px > max {
		return int32(max)
	}
	return int32(px)
}
