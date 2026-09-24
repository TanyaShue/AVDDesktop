package display

import (
	"math"
	"testing"
)

func TestMapTouch(t *testing.T) {
	cases := []struct {
		name  string
		x, y  float64
		w, h  int
		wantX int32
		wantY int32
	}{
		{"左上角", 0, 0, 1080, 2400, 0, 0},
		{"右下角夹取", 1, 1, 1080, 2400, 1079, 2399},
		{"正中", 0.5, 0.5, 1080, 2400, 540, 1200},
		{"通知栏下拉起点", 0.5, 0.002, 1080, 2400, 540, 5},
		{"负数夹取到 0", -0.5, -1, 100, 200, 0, 0},
		{"大于 1 夹取到边界", 2, 1.5, 100, 200, 99, 199},
		{"NaN 归 0", math.NaN(), math.NaN(), 1080, 2400, 0, 0},
		{"Inf 归 0", math.Inf(1), math.Inf(-1), 1080, 2400, 0, 0},
		{"尺寸非正归 0", 0.5, 0.5, 0, -10, 0, 0},
		{"四舍五入", 0.5, 0.5, 3, 3, 2, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotX, gotY := MapTouch(c.x, c.y, c.w, c.h)
			if gotX != c.wantX || gotY != c.wantY {
				t.Fatalf("MapTouch(%v, %v, %d, %d) = (%d, %d)，want (%d, %d)",
					c.x, c.y, c.w, c.h, gotX, gotY, c.wantX, c.wantY)
			}
		})
	}
}

// 越界输入永远不能越出设备像素范围（模拟器对越界坐标的行为未定义）。
func TestMapTouch_AlwaysInsideDevice(t *testing.T) {
	for _, v := range []float64{-1000, -1, -0.0001, 0, 0.25, 0.5, 0.999, 1, 1.0001, 1000} {
		x, y := MapTouch(v, v, 540, 1200)
		if x < 0 || x > 539 || y < 0 || y > 1199 {
			t.Fatalf("MapTouch(%v) = (%d, %d) 越出设备范围", v, x, y)
		}
	}
}
