package mirror

import (
	"context"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
)

// TestEngineLive 对真实镜像执行一次测速（需要网络，默认跳过）。
//
// 运行：go test ./internal/mirror/ -run TestEngineLive -v -timeout 300s
func TestEngineLive(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过网络测试")
	}
	engine := NewEngine(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sources := []domain.MirrorSource{
		{ID: SourceTencentID, Name: "腾讯云镜像", BaseURL: "https://mirrors.cloud.tencent.com/AndroidSDK/"},
		{ID: SourceGoogleID, Name: "Google 官方", BaseURL: "https://dl.google.com/android/repository/"},
	}
	opts := TestOptions{MaxThroughputBytes: 1 << 20, Timeout: 40 * time.Second}.WithDefaults()

	for _, s := range sources {
		r := engine.Test(ctx, s, opts)
		t.Logf("%s(%s)\n  ok=%v grade=%s score=%d\n  dns=%dms connect=%dms ttfb=%dms status=%d range=%v xml=%v\n  吞吐=%.2f MB/s 抖动=%dms\n  cmdlineTools=%v emulator=%v err=%q\n  ips=%v",
			s.Name, s.ID, r.OK, r.Grade, r.Score,
			r.DNSMs, r.ConnectMs, r.TTFBMs, r.HTTPStatus, r.RangeSupported, r.XMLOK,
			r.ThroughputMBps, r.JitterMs, r.HasCmdlineTools, r.HasEmulator, r.Error, r.ResolveIPs)
		if r.SourceID != s.ID {
			t.Errorf("SourceID 不匹配")
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"dl.google.com/android/repository":   "https://dl.google.com/android/repository/",
		"https://example.com/sdk/":           "https://example.com/sdk/",
		"  http://mirror.local/AndroidSDK  ": "http://mirror.local/AndroidSDK/",
	}
	for in, want := range cases {
		got, err := NormalizeBaseURL(in)
		if err != nil {
			t.Fatalf("%q 规范化失败: %v", in, err)
		}
		if got != want {
			t.Errorf("%q → %q，期望 %q", in, got, want)
		}
	}
	if _, err := NormalizeBaseURL("ftp://x/"); err == nil {
		t.Error("ftp 应当被拒绝")
	}
}
