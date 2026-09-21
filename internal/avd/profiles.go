package avd

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// listDeviceTimeout 是 `avdmanager list device` 的执行上限。
const listDeviceTimeout = 90 * time.Second

// profileIDRe 匹配 `id: 12 or "medium_phone"`。
var profileIDRe = regexp.MustCompile(`^id:\s*(\d+)\s+or\s+"([^"]+)"`)

// profileFieldRe 匹配档案块里的字段行。
//
// 实测：官方输出是 `Tag : ai-glasses`（冒号前有空格），不是 `Tag: ...`。
var profileFieldRe = regexp.MustCompile(`^(Name|OEM|Tag)\s*:\s*(.*)$`)

// ListProfiles 调用官方 avdmanager 获取设备档案（失败时如实返回错误，不做内置兜底表）。
func ListProfiles(ctx context.Context, tools platform.Tools, env []string) ([]domain.DeviceProfile, error) {
	if !tools.HasAvdmanager() {
		return nil, domain.ErrDetail(domain.CodeToolMissing,
			"未找到软件自带的 avdmanager", tools.Avdmanager)
	}
	res, err := proc.Run(ctx, tools.Avdmanager, []string{"list", "device"}, proc.Options{
		Env:     env,
		Timeout: listDeviceTimeout,
	})
	if err != nil {
		return nil, err
	}
	profiles := ParseDeviceList(res.Combined())
	if len(profiles) == 0 {
		return nil, domain.ErrDetail(domain.CodeProcessFailed,
			"avdmanager 未返回任何设备档案", strings.TrimSpace(res.Combined()))
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

// ParseDeviceList 解析 `avdmanager list device` 的块状输出。
//
// 输出形如（块之间用 "---------" 分隔）：
//
//	id: 12 or "medium_phone"
//	     Name: Medium Phone
//	     OEM : Google
//	     Tag : android-desktop
func ParseDeviceList(out string) []domain.DeviceProfile {
	var (
		profiles []domain.DeviceProfile
		current  *domain.DeviceProfile
	)
	flush := func() {
		if current != nil && current.ID != "" {
			profiles = append(profiles, *current)
		}
		current = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "-----") {
			flush()
			continue
		}
		if m := profileIDRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			current = &domain.DeviceProfile{Index: atoi(m[1]), ID: m[2]}
			continue
		}
		if current == nil {
			continue
		}
		if m := profileFieldRe.FindStringSubmatch(trimmed); m != nil {
			value := strings.TrimSpace(m[2])
			switch m[1] {
			case "Name":
				current.Name = value
			case "OEM":
				current.OEM = value
			case "Tag":
				current.Tag = value
			}
		}
	}
	flush()
	return profiles
}

// atoi 解析十进制整数（非法输入返回 0）。
func atoi(v string) int {
	n := 0
	for _, r := range strings.TrimSpace(v) {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
