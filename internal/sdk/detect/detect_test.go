package detect

import (
	"context"
	"testing"
	"time"
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
