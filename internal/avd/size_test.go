package avd

import "testing"

// TestParseSizeMB 覆盖真实 config.ini 里出现的两套容量写法。
//
// 回归点：旧实现用 atoi 只读前导数字，"2G" 会变成 2，界面显示成「2 MB」（差 1024 倍）。
func TestParseSizeMB(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		// 字节单位写法（新版 avdmanager）
		{"2G", 2048},
		{"2g", 2048},
		{"2GB", 2048},
		{"1.5G", 1536},
		{"228M", 228},
		{"228MB", 228},
		{"256m", 256},
		{"2048K", 2},
		{"2684354560B", 2560},
		{"1T", 1 << 20},
		// 纯数字写法（旧格式与本应用写入）：单位是 MB
		{"2048", 2048},
		{"1536", 1536},
		{"  4096  ", 4096},
		{"512", 512},
		// 空格容错
		{"2 G", 2048},
		{"228 MB", 228},
		// 无法解析：按「未设置」处理，不猜测
		{"", 0},
		{"   ", 0},
		{"0", 0},
		{"G", 0},
		{"2X", 0},
		{"abc", 0},
		{"-2048", 0},
		{"2.5.5G", 0},
		{"１５３６", 0}, // 全角数字
	}
	for _, c := range cases {
		if got := parseSizeMB(c.value); got != c.want {
			t.Errorf("parseSizeMB(%q) = %d，期望 %d", c.value, got, c.want)
		}
	}
}

// TestParseSizeMBFromRealConfig 用实测到的 config.ini 片段核对端到端取值。
func TestParseSizeMBFromRealConfig(t *testing.T) {
	config := parseIni("hw.ramSize=2G\nvm.heapSize=228M\ndisk.dataPartition.size=6G\n")
	if got := parseSizeMB(config["hw.ramSize"]); got != 2048 {
		t.Fatalf("hw.ramSize=2G 应解析为 2048 MB，实际 %d", got)
	}
	if got := parseSizeMB(config["vm.heapSize"]); got != 228 {
		t.Fatalf("vm.heapSize=228M 应解析为 228 MB，实际 %d", got)
	}
	// 同机另一台设备是旧格式（纯数字 MB）
	legacy := parseIni("hw.ramSize=2048\nvm.heapSize=192\n")
	if got := parseSizeMB(legacy["hw.ramSize"]); got != 2048 {
		t.Fatalf("hw.ramSize=2048 应解析为 2048 MB，实际 %d", got)
	}
	if got := parseSizeMB(legacy["vm.heapSize"]); got != 192 {
		t.Fatalf("vm.heapSize=192 应解析为 192 MB，实际 %d", got)
	}
}
