package platform

import (
	"strings"
	"testing"
)

// TestAccelAdviceIsPlatformSpecific 验证排查建议与宿主平台一致，
// 不会在 macOS / Linux 上给出「开启 Windows 功能」这类无法执行的建议。
func TestAccelAdviceIsPlatformSpecific(t *testing.T) {
	cases := []struct {
		goos     string
		mustHave string
		mustNot  []string
	}{
		{"windows", "Windows 功能", []string{"kvm", "Hypervisor.Framework"}},
		{"linux", "kvm", []string{"Windows 功能", "Hypervisor.Framework"}},
		{"darwin", "Hypervisor.Framework", []string{"Windows 功能"}},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			got := AccelAdvice(tc.goos)
			if strings.TrimSpace(got) == "" {
				t.Fatal("建议为空")
			}
			if !strings.Contains(got, tc.mustHave) {
				t.Errorf("AccelAdvice(%q) = %q，应包含 %q", tc.goos, got, tc.mustHave)
			}
			for _, bad := range tc.mustNot {
				if strings.Contains(got, bad) {
					t.Errorf("AccelAdvice(%q) = %q，不应包含 %q", tc.goos, got, bad)
				}
			}
		})
	}
}
