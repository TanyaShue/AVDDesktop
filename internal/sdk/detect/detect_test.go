package detect

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
)

// TestDetectLive 在真实机器上执行一次完整自检（需要网络/外部进程，short 模式跳过）。
//
// 运行：go test ./internal/sdk/detect/ -run TestDetectLive -v -timeout 300s
func TestDetectLive(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过环境自检")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	d := NewDetector(nil)
	report, err := d.Detect(ctx, Options{InjectEnv: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("SDK 根目录: %s（来源：%s）", report.SdkRoot, report.SdkRootSource)
	t.Logf("JDK      : %s", report.JdkPath)
	t.Logf("AVD 目录 : %s（来源：%s，%d 个设备，可写=%v）",
		report.AvdHome.Path, report.AvdHome.Source, report.AvdHome.Count, report.AvdHome.Writable)
	t.Logf("硬件加速 : available=%v kind=%s", report.Accel.Available, report.Accel.Kind)
	if w := report.Windows; w != nil {
		t.Logf("Windows  : %s build=%s hypervisor=%v 固件虚拟化=%v SLAT=%v 长路径=%v HV服务=%v VMCompute=%v source=%s",
			w.Caption, w.Build, w.HypervisorPresent, w.VirtFirmware, w.SLAT, w.LongPathsEnabled,
			w.HyperVHostService, w.VMComputeService, w.Source)
	}
	for _, d := range report.Disks {
		t.Logf("磁盘     : %s 剩余 %d GB / 共 %d GB", d.Path, d.FreeGB, d.TotalGB)
	}
	for _, c := range report.Components {
		t.Logf("  [%-17s] %-8s %-12s %s", c.ID, c.State, c.Version, c.Name)
	}
	for _, issue := range report.Issues {
		t.Logf("  问题[%s] %s：%s｜修复=%s %s", issue.Severity, issue.Title, issue.Detail, issue.FixLabel, issue.FixCommand)
	}
	t.Logf("已接受许可 %d 项，运行中实例 %d 个", len(report.AcceptedLicenses), report.RunningInstances)
	t.Logf("ready=%v 阻塞项=%d 问题=%d 耗时=%dms", report.Ready, len(report.Blockers), len(report.Issues), report.ScanMs)

	if report.SdkRoot == "" {
		t.Error("未能解析出 SDK 根目录")
	}
	if report.AvdHome.Path == "" {
		t.Error("未能解析出 AVD 目录")
	}
	if len(report.Components) < 6 {
		t.Errorf("组件数量异常: %d", len(report.Components))
	}
}

// TestNormalizeReportJSONArrays 锁定“空数组必须序列化成 [] 而不是 null”的契约。
//
// 真实事故：健康环境（无阻塞项）下 EnvReport.Blockers 是 nil 切片，JSON 里成了 null，
// 前端 `report.blockers.length` 直接抛异常 → React 卸载整棵树 → 首页整窗白屏。
func TestNormalizeReportJSONArrays(t *testing.T) {
	report := &domain.EnvReport{} // 零值：所有数组字段都是 nil 切片
	NormalizeReport(report)

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("序列化报告失败: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化报告失败: %v", err)
	}
	for _, key := range []string{"components", "blockers", "disks"} {
		if _, ok := decoded[key].([]any); !ok {
			t.Errorf("%s 的 JSON 值是 %#v；契约要求空数组输出 []（null 会让前端 .length 抛异常）",
				key, decoded[key])
		}
	}
}

// TestParseAccelCheck 验证加速检测输出解析（用实测样本）。
func TestParseAccelCheck(t *testing.T) {
	cases := []struct {
		out       string
		code      int
		wantKind  string
		wantAvail bool
	}{
		{"accel:\n0\nWHPX(10.0.26200) is installed and usable.\naccel", 0, "whpx", true},
		{"accel:\n0\nHAXM is not installed on this machine\naccel", 0, "haxm", false},
		{"accel:\n1\nAEHD is installed but not usable\naccel", 0, "aehd", false},
		{"accel:\n0\nAndroid Emulator hypervisor driver is installed and usable.\naccel", 0, "aehd", true},
	}
	for _, c := range cases {
		kind, avail := ParseAccelCheck(c.code, c.out)
		if kind != c.wantKind || avail != c.wantAvail {
			t.Errorf("ParseAccelCheck(%q) = (%s,%v)，期望 (%s,%v)", c.out, kind, avail, c.wantKind, c.wantAvail)
		}
	}
}

// TestParseVersions 验证各工具版本解析。
func TestParseVersions(t *testing.T) {
	java := []struct{ out, want string }{
		{`openjdk version "17.0.6" 2023-01-17 LTS`, "17.0.6"},
		{`java version "1.8.0_291"`, "8.0.291"},
		{`openjdk version "21.0.1" 2023-10-17`, "21.0.1"},
	}
	for _, c := range java {
		if got := ParseJavaVersion(c.out); got != c.want {
			t.Errorf("ParseJavaVersion(%q) = %q，期望 %q", c.out, got, c.want)
		}
	}
	emu := "Android emulator version 36.3.10.0 (build_id 14472402) (CL:N/A)"
	if got := ParseEmulatorVersion(emu); got != "36.3.10.0" {
		t.Errorf("ParseEmulatorVersion = %q", got)
	}
	adb := "Android Debug Bridge version 1.0.41\nVersion 36.0.2-12345678"
	if got := ParseAdbVersion(adb); got != "36.0.2" {
		t.Errorf("ParseAdbVersion = %q，期望 36.0.2（取 Version 行而非 1.0.41）", got)
	}
}
