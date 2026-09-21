package backend

import (
	"path/filepath"
	"testing"

	"AVDDesktop/internal/domain"
)

// TestSplitSystemImage 验证镜像路径拆解与目录拼接的差异。
//
// 端到端测试发现的真实问题：API 段在 config.ini 里是 36.1（target=android-36.1），
// 但目录名必须保留 android- 前缀（system-images/android-36.1/...）。
func TestSplitSystemImage(t *testing.T) {
	api, tag, abi, err := SplitSystemImage("system-images;android-36.1;google_apis_playstore;x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if api != "36.1" || tag != "google_apis_playstore" || abi != "x86_64" {
		t.Errorf("拆解结果错误：api=%q tag=%q abi=%q", api, tag, abi)
	}

	dir, err := SystemImageDir("system-images;android-36.1;google_apis_playstore;x86_64")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("system-images", "android-36.1", "google_apis_playstore", "x86_64")
	if dir != want {
		t.Errorf("目录错误：got=%q want=%q（API 段必须保留 android- 前缀）", dir, want)
	}

	// 非 android- 前缀的 API（canary 通道）同样按原样保留
	dir, err = SystemImageDir("system-images;android-canary-20260909;google_apis_ps16k;arm64-v8a")
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Join("system-images", "android-canary-20260909", "google_apis_ps16k", "arm64-v8a")
	if dir != want {
		t.Errorf("canary 目录错误：got=%q want=%q", dir, want)
	}

	// 非法输入必须报错
	for _, bad := range []string{"", "platform-tools", "system-images;android-34", "not-a-package;a;b;c"} {
		if _, _, _, err := SplitSystemImage(bad); err == nil {
			t.Errorf("%q 应被拒绝", bad)
		}
		if _, err := SystemImageDir(bad); err == nil {
			t.Errorf("%q 的目录计算应被拒绝", bad)
		}
	}
}

// TestSelectBackend 验证后端选择语义。
func TestSelectBackend(t *testing.T) {
	deps := Deps{} // 未提供 paths → avdmanager 不可用
	direct := &DirectBackend{}

	if got := Select(domain.AvdSpec{CreateWithAvdManager: true}, deps); got.Kind() != direct.Kind() {
		t.Errorf("avdmanager 不可用时应回退到直写后端，实际 %s", got.Kind())
	}
	if got := Select(domain.AvdSpec{CreateWithAvdManager: false}, deps); got.Kind() != "direct" {
		t.Errorf("显式关闭 avdmanager 时必须用直写后端，实际 %s", got.Kind())
	}
}

// TestTagDisplay 验证镜像标签展示名。
func TestTagDisplay(t *testing.T) {
	cases := map[string]string{
		"google_apis":           "Google APIs",
		"google_apis_playstore": "Google Play",
		"android-desktop":       "Desktop",
		"unknown-tag":           "unknown-tag",
	}
	for in, want := range cases {
		if got := TagDisplay(in); got != want {
			t.Errorf("TagDisplay(%q) = %q，期望 %q", in, got, want)
		}
	}
}
