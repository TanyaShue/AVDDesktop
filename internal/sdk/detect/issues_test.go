package detect

import (
	"strings"
	"testing"

	"AVDDesktop/internal/domain"
)

// TestBuildIssuesAcceleration 验证加速问题的三种归因分支。
func TestBuildIssuesAcceleration(t *testing.T) {
	base := func() *domain.EnvReport {
		return &domain.EnvReport{
			Accel: domain.AccelInfo{Available: false, Kind: "none"},
			Components: []domain.ToolStatus{
				{ID: domain.ToolEmulator, State: domain.StatePresent, Version: "36.3.10"},
				{ID: domain.ToolSystemImages, State: domain.StatePresent},
			},
		}
	}

	t.Run("已有 hypervisor 但不可用 → 给 dism 命令", func(t *testing.T) {
		report := base()
		report.Windows = &domain.WindowsInfo{Available: true, HypervisorPresent: true}
		issues := buildIssues(report, nil, Options{})
		issue := findByID(issues, "accel-unavailable")
		if issue == nil {
			t.Fatal("应当产生 accel-unavailable 问题")
		}
		if issue.FixCommand != hypervisorPlatformCommand {
			t.Errorf("应给出开启 WHPX 的命令，实际 %q", issue.FixCommand)
		}
		if !strings.Contains(issue.Detail, "冲突") {
			t.Errorf("应提示 AEHD 冲突可能性，实际：%s", issue.Detail)
		}
	})

	t.Run("无 hypervisor 且无虚拟化能力 → 提示 BIOS", func(t *testing.T) {
		report := base()
		report.Windows = &domain.WindowsInfo{Available: true}
		issues := buildIssues(report, nil, Options{})
		issue := findByID(issues, "accel-unavailable")
		if issue == nil {
			t.Fatal("应当产生 accel-unavailable 问题")
		}
		if !strings.Contains(issue.Detail, "BIOS") {
			t.Errorf("应提示在 BIOS 中开启虚拟化，实际：%s", issue.Detail)
		}
		if issue.FixCommand != "" {
			t.Error("BIOS 分支不应给命令（需用户进固件设置）")
		}
	})

	t.Run("加速可用时不产生问题", func(t *testing.T) {
		report := base()
		report.Accel = domain.AccelInfo{Available: true, Kind: "whpx"}
		issues := buildIssues(report, nil, Options{})
		if findByID(issues, "accel-unavailable") != nil {
			t.Error("加速可用时不应报告加速问题")
		}
	})
}

// TestBuildIssuesImageCompatibility 验证镜像要求更高版本模拟器的判定。
func TestBuildIssuesImageCompatibility(t *testing.T) {
	report := &domain.EnvReport{
		Accel: domain.AccelInfo{Available: true, Kind: "whpx"},
		Components: []domain.ToolStatus{
			{ID: domain.ToolEmulator, State: domain.StatePresent, Version: "34.0.0"},
			{ID: domain.ToolSystemImages, State: domain.StatePresent},
		},
	}
	images := []domain.SystemImage{
		{Path: "system-images;android-36.1;google_apis;x86_64", APILevel: "36.1", RequiresEmulator: "35.4.9"},
	}
	issues := buildIssues(report, images, Options{})
	issue := findByID(issues, "image-requires-newer-emulator:system-images;android-36.1;google_apis;x86_64")
	if issue == nil {
		t.Fatalf("应当报告模拟器版本过低，问题列表：%+v", issues)
	}
	if issue.FixKind != domain.FixUpdate || issue.FixPayload != "emulator" {
		t.Errorf("修复动作应为升级 emulator，实际 kind=%s payload=%s", issue.FixKind, issue.FixPayload)
	}

	// 版本满足时不再提示
	report.Components[0].Version = "36.3.10"
	if findByID(buildIssues(report, images, Options{}), issue.ID) != nil {
		t.Error("模拟器版本满足要求时不应报告兼容性问题")
	}
}

// TestBuildIssuesSystemAndHost 验证长路径、运行实例与缺镜像三类问题。
func TestBuildIssuesSystemAndHost(t *testing.T) {
	report := &domain.EnvReport{
		Accel: domain.AccelInfo{Available: true, Kind: "whpx"},
		Windows: &domain.WindowsInfo{
			Available:         true,
			HypervisorPresent: true,
			LongPathsEnabled:  false,
		},
		Components: []domain.ToolStatus{
			{ID: domain.ToolEmulator, State: domain.StatePresent, Version: "36.3.10"},
			{ID: domain.ToolSystemImages, State: domain.StateMissing},
		},
	}
	issues := buildIssues(report, nil, Options{RunningInstances: 2, AvdIssues: []string{"设备 A：系统镜像已不存在"}})

	if findByID(issues, "long-paths-disabled") == nil {
		t.Error("未开启长路径时应提示")
	}
	if findByID(issues, "instances-running") == nil {
		t.Error("有运行中实例时应提示")
	}
	noImage := findByID(issues, "no-system-image")
	if noImage == nil || noImage.Severity != domain.SeverityBlocker {
		t.Errorf("缺少系统镜像应为 blocker，实际 %+v", noImage)
	}
	avd := findByID(issues, "avd-issue-0")
	if avd == nil || !strings.Contains(avd.Detail, "设备 A") {
		t.Errorf("应包含 AVD 健康问题，实际 %+v", avd)
	}

	// 长路径已开启时不再提示
	report.Windows.LongPathsEnabled = true
	if findByID(buildIssues(report, nil, Options{}), "long-paths-disabled") != nil {
		t.Error("长路径已开启时不应提示")
	}
}

// TestParseSysteminfoVirtualization 验证 systeminfo 关键词判定（中英文文案）。
func TestParseSysteminfoVirtualization(t *testing.T) {
	cases := []struct {
		text     string
		wantVirt bool
	}{
		{"Virtualization Enabled In Firmware: Yes", true},
		{"Virtualization Enabled In Firmware: No", false},
		{"固件中已启用虚拟化: 是", true},
		{"已启用固件中的虚拟化: 否", false},
	}
	for _, c := range cases {
		lower := strings.ToLower(c.text)
		got := !strings.Contains(lower, "virtualization enabled in firmware: no") &&
			!strings.Contains(c.text, "已启用固件中的虚拟化: 否") &&
			!strings.Contains(c.text, "固件中已启用虚拟化: 否")
		if got != c.wantVirt {
			t.Errorf("%q → %v，期望 %v", c.text, got, c.wantVirt)
		}
	}
}

func findByID(issues []domain.EnvIssue, id string) *domain.EnvIssue {
	for i := range issues {
		if issues[i].ID == id {
			return &issues[i]
		}
	}
	return nil
}
