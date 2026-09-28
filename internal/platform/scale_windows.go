//go:build windows

package platform

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

var (
	getDpiForSystem   = windows.NewLazySystemDLL("user32.dll").NewProc("GetDpiForSystem")
	displayScaleOnce  sync.Once
	displayScaleValue = 1.0
)

// DisplayScale 返回主显示器的缩放系数（96 DPI = 1.0）。
//
// 可用 AVDDESKTOP_DISPLAY_SCALE 覆盖，便于测试与自动化；取值非法时忽略。
// GetDpiForSystem 不可用（旧版 Windows）时回退 1.0。
func DisplayScale() float64 {
	displayScaleOnce.Do(func() {
		if raw := strings.TrimSpace(os.Getenv("AVDDESKTOP_DISPLAY_SCALE")); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0.5 && v <= 8 {
				displayScaleValue = v
				return
			}
		}
		if err := getDpiForSystem.Find(); err == nil {
			dpi, _, _ := getDpiForSystem.Call()
			if dpi >= 48 && dpi <= 768 {
				displayScaleValue = float64(dpi) / 96.0
			}
		}
	})
	return displayScaleValue
}
