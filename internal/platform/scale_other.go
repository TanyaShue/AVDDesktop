//go:build !windows

package platform

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	displayScaleOnce  sync.Once
	displayScaleValue = 1.0
)

// DisplayScale 返回主显示器的缩放系数；非 Windows 平台目前回退 1.0。
//
// 可用 AVDDESKTOP_DISPLAY_SCALE 覆盖，便于测试与自动化。
func DisplayScale() float64 {
	displayScaleOnce.Do(func() {
		if raw := strings.TrimSpace(os.Getenv("AVDDESKTOP_DISPLAY_SCALE")); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0.5 && v <= 8 {
				displayScaleValue = v
			}
		}
	})
	return displayScaleValue
}
