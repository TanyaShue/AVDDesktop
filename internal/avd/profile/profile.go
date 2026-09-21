// Package profile 解析 `avdmanager list device` 的设备档案。
//
// 依据实测（docs/RESEARCH-NOTES.md §4.1）：输出是块状文本，块之间用 "---------" 分隔，
// 每块含 id / Name / OEM / Tag。新版本会包含 ai_glasses_device、desktop_large 等新档案，
// 因此分类采用"tag 前缀 + 名称关键字"启发式，未知归入 other。
package profile

import (
	"context"
	"regexp"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
)

// idRe 匹配 `id: 12 or "medium_phone"`。
var idRe = regexp.MustCompile(`^id:\s*(\d+)\s+or\s+"([^"]+)"`)

// ParseDeviceList 解析 `avdmanager list device` 的输出。
func ParseDeviceList(out string) []domain.DeviceProfile {
	var (
		profiles []domain.DeviceProfile
		current  *domain.DeviceProfile
	)
	flush := func() {
		if current != nil && current.ID != "" {
			current.Category = classify(*current)
			applyKnownSpecs(current)
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
		if m := idRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			current = &domain.DeviceProfile{Index: atoi(m[1]), ID: m[2]}
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "Name:"):
			current.Name = strings.TrimSpace(strings.TrimPrefix(trimmed, "Name:"))
		case strings.HasPrefix(trimmed, "OEM"):
			current.OEM = strings.TrimSpace(strings.TrimPrefix(trimmed, "OEM"))
			current.OEM = strings.TrimSpace(strings.TrimPrefix(current.OEM, ":"))
		case strings.HasPrefix(trimmed, "Tag:"):
			current.Tag = strings.TrimSpace(strings.TrimPrefix(trimmed, "Tag:"))
		}
	}
	flush()
	return profiles
}

// List 调用 avdmanager 获取设备档案；失败时返回内置兜底列表。
//
// useAvdManager 为 false（无 JDK）时直接返回兜底列表。
func List(ctx context.Context, avdmanagerPath string, env []string) ([]domain.DeviceProfile, error) {
	if avdmanagerPath != "" && platform.FileExists(avdmanagerPath) {
		res, err := proc.Run(ctx, avdmanagerPath, []string{"list", "device"}, proc.Options{
			Env: env, Timeout: 90 * time.Second,
		})
		if err == nil && strings.TrimSpace(res.Stdout) != "" {
			if profiles := ParseDeviceList(res.Stdout); len(profiles) > 0 {
				return profiles, nil
			}
		}
	}
	return Fallback(), nil
}

// classify 依据 tag 与名称推断分类。
func classify(p domain.DeviceProfile) string {
	tag := strings.ToLower(p.Tag)
	name := strings.ToLower(p.Name + " " + p.ID)
	switch {
	case strings.Contains(tag, "wear") || strings.Contains(name, "wear"):
		return "wear"
	case strings.Contains(tag, "automotive") || strings.Contains(name, "automotive") || strings.Contains(name, "car"):
		return "automotive"
	case strings.Contains(tag, "tv") || strings.Contains(name, "tv"):
		return "tv"
	case strings.Contains(tag, "xr") || strings.Contains(name, "xr") || strings.Contains(name, "glasses"):
		return "xr"
	case strings.Contains(tag, "desktop") || strings.Contains(name, "desktop"):
		return "desktop"
	case strings.Contains(name, "tablet"):
		return "tablet"
	case strings.Contains(name, "pixel") || strings.Contains(name, "nexus") || strings.Contains(name, "phone"):
		return "phone"
	default:
		return "other"
	}
}

// knownSpecs 是常见档案的分辨率/密度/内存（avdmanager list device 不输出这些信息）。
var knownSpecs = map[string][3]int{ // width, height, density
	"medium_phone":               {1080, 2400, 420},
	"medium_tablet":              {1600, 2560, 320},
	"desktop_medium":             {1920, 1080, 160},
	"desktop_large":              {2560, 1440, 160},
	"pixel_9":                    {1080, 2424, 420},
	"pixel_9_pro":                {1344, 2992, 480},
	"pixel_8":                    {1080, 2400, 420},
	"pixel_8_pro":                {1344, 2992, 480},
	"pixel_7":                    {1080, 2400, 420},
	"pixel_7_pro":                {1440, 3120, 560},
	"pixel_6":                    {1080, 2400, 420},
	"pixel_6_pro":                {1440, 3120, 560},
	"pixel_5":                    {1080, 2340, 440},
	"pixel_4":                    {1080, 2280, 440},
	"pixel_3a":                   {1080, 2220, 440},
	"pixel_2":                    {1080, 1920, 420},
	"pixel":                      {1080, 1920, 420},
	"Nexus 5X":                   {1080, 1920, 420},
	"Nexus 5":                    {1080, 1920, 480},
	"Nexus 6":                    {1440, 2560, 560},
	"Nexus 6P":                   {1440, 2560, 560},
	"Nexus 7":                    {1200, 1920, 320},
	"Nexus 9":                    {1536, 2048, 320},
	"Nexus 10":                   {2560, 1600, 320},
	"Galaxy Nexus":               {720, 1280, 320},
	"tv_1080p":                   {1920, 1080, 320},
	"tv_4k":                      {3840, 2160, 320},
	"wearos_small_round":         {454, 454, 320},
	"wearos_large_round":         {454, 454, 320},
	"wearos_square":              {320, 320, 320},
	"automotive_1024p_landscape": {1024, 768, 160},
	"automotive_1080p_landscape": {1920, 1080, 240},
	"automotive_1408p_landscape_with_google_apis": {1408, 792, 240},
	"automotive_ultrawide":                        {2560, 720, 240},
	"ai_glasses_device":                           {640, 480, 240},
}

func applyKnownSpecs(p *domain.DeviceProfile) {
	if p.Width > 0 {
		return
	}
	if spec, ok := knownSpecs[p.ID]; ok {
		p.Width, p.Height, p.Density = spec[0], spec[1], spec[2]
	}
}

// Fallback 返回内置的常用档案（avdmanager 不可用时使用，覆盖主流场景）。
func Fallback() []domain.DeviceProfile {
	ids := []string{
		"medium_phone", "medium_tablet", "desktop_medium", "desktop_large",
		"pixel_7", "pixel_7_pro", "pixel_6", "pixel_5",
		"Nexus 5X", "Nexus 6", "Nexus 7", "Nexus 10",
		"tv_1080p", "tv_4k", "wearos_small_round", "wearos_square",
		"automotive_1080p_landscape", "ai_glasses_device",
	}
	out := make([]domain.DeviceProfile, 0, len(ids))
	for i, id := range ids {
		p := domain.DeviceProfile{Index: i, ID: id, Name: humanName(id), OEM: "Google"}
		applyKnownSpecs(&p)
		p.Category = classify(p)
		out = append(out, p)
	}
	return out
}

func humanName(id string) string {
	switch id {
	case "medium_phone":
		return "Medium Phone"
	case "medium_tablet":
		return "Medium Tablet"
	case "desktop_medium":
		return "Medium Desktop"
	case "desktop_large":
		return "Large Desktop"
	case "wearos_small_round":
		return "Wear OS Small Round"
	case "wearos_square":
		return "Wear OS Square"
	case "ai_glasses_device":
		return "AI Glasses"
	default:
		return id
	}
}

func atoi(v string) int {
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
