package service

import "testing"

// TestParseAccelCheck 用各平台 `emulator -accel-check` 的真实输出验证解析。
//
// 输出固定是四行信封：accel: / <状态码> / <说明> / accel，状态码 0 表示加速可用。
// 各平台说明文案完全不同，不能只按 "is installed and usable" 判断：
//   - Windows: "WHPX(...) is installed and usable."、"HAXM version ... (3) is installed and usable."
//   - Linux:   "KVM (version 12) is installed and usable."
//   - macOS:   "Hypervisor.Framework OS X Version 26.6"（不含 usable 字样）
func TestParseAccelCheck(t *testing.T) {
	cases := []struct {
		name      string
		goos      string
		out       string
		available bool
		kind      string
	}{
		{
			name:      "macOS Hypervisor.Framework",
			goos:      "darwin",
			out:       "accel:\n0\nHypervisor.Framework OS X Version 26.6\naccel\n",
			available: true,
			kind:      "hvf",
		},
		{
			name:      "macOS 版本过低",
			goos:      "darwin",
			out:       "accel:\n1\nHypervisor.Framework is only supportedon OS X 10.10 and above\naccel\n",
			available: false,
			kind:      "hvf",
		},
		{
			name:      "Linux KVM 可用",
			goos:      "linux",
			out:       "accel:\n0\nKVM (version 12) is installed and usable.\naccel\n",
			available: true,
			kind:      "kvm",
		},
		{
			name:      "Linux KVM 无权限",
			goos:      "linux",
			out:       "accel:\n3\nThis user doesn't have permissions to use KVM (/dev/kvm)\naccel\n",
			available: false,
			kind:      "kvm",
		},
		{
			name:      "Windows WHPX",
			goos:      "windows",
			out:       "accel:\n0\nWHPX(10.0.19041.1) is installed and usable.\naccel\n",
			available: true,
			kind:      "whpx",
		},
		{
			name:      "Windows AEHD",
			goos:      "windows",
			out:       "accel:\n0\nAEHD (version 2.2) is installed and usable.\naccel\n",
			available: true,
			kind:      "aehd",
		},
		{
			name:      "Windows HAXM",
			goos:      "windows",
			out:       "accel:\n0\nHAXM version 7.5.6 (3) is installed and usable.\naccel\n",
			available: true,
			kind:      "haxm",
		},
		{
			name:      "Windows HAXM 未安装",
			goos:      "windows",
			out:       "accel:\n1\nHAXM is not installed.\naccel\n",
			available: false,
			kind:      "haxm",
		},
		{
			name:      "Windows GVM",
			goos:      "windows",
			out:       "accel:\n0\nGVM is installed and usable.\naccel\n",
			available: true,
			kind:      "gvm",
		},
		{
			name:      "旧式输出无信封（只有 usable 文案）",
			goos:      "linux",
			out:       "KVM (version 12) is installed and usable.\n",
			available: true,
			kind:      "kvm",
		},
		{
			name:      "空输出",
			goos:      "darwin",
			out:       "",
			available: false,
			kind:      "none",
		},
		{
			name:      "无法识别的输出",
			goos:      "windows",
			out:       "accel:\n0\nsome unknown accelerator\naccel\n",
			available: true,
			kind:      "none",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAccelCheck(tc.out)
			if got.Available != tc.available {
				t.Errorf("Available = %v，期望 %v（输出：%q）", got.Available, tc.available, tc.out)
			}
			if got.Kind != tc.kind {
				t.Errorf("Kind = %q，期望 %q", got.Kind, tc.kind)
			}
		})
	}
}
