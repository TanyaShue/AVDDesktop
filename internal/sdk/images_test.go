package sdk

import (
	"testing"

	"AVDDesktop/internal/domain"
)

func TestCompareAPI(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"34", "33", 1},
		{"34", "34", 0},
		{"36.1", "36", 1},
		{"36.1", "36.1", 0},
		{"36.1", "36.2", -1},
		{"android-34", "34", 0},
	}
	for _, c := range cases {
		if got := compareAPI(c.a, c.b); got != c.want {
			t.Errorf("compareAPI(%q,%q) = %d，期望 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSortImagesForHost(t *testing.T) {
	host := HostABI()
	if host == "" {
		t.Fatal("HostABI 不应为空")
	}
	other := "arm64-v8a"
	if host == other {
		other = "x86_64"
	}

	images := []domain.SystemImage{
		{Path: "a", API: "33", Tag: "google_apis", ABI: other},
		{Path: "b", API: "30", Tag: "google_apis", ABI: host, Installed: true},
		{Path: "c", API: "36", Tag: "google_apis", ABI: host},
		{Path: "d", API: "36.1", Tag: "google_apis", ABI: host},
		{Path: "e", API: "36.1", Tag: "google_apis_playstore", ABI: host},
	}
	SortImagesForHost(images)

	wantPaths := []string{"b", "d", "e", "c", "a"}
	for i, want := range wantPaths {
		if images[i].Path != want {
			t.Fatalf("排序结果第 %d 项 = %q，期望 %q（完整：%+v）", i, images[i].Path, want, images)
		}
	}
}
