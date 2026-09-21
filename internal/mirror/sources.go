// Package mirror 管理镜像源与测速。
//
// 内置源表的依据是实测（docs/RESEARCH-NOTES.md §1）：
// 目前**只有 Google 官方与腾讯云**能完整镜像 SDK 仓库；其它常见镜像实测 403/404/TLS 失败，
// 因此它们默认禁用但保留在列表中——镜像会恢复，用户可以随时"重新测速"验证。
package mirror

import (
	"sort"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
)

// 内置源 ID。
const (
	SourceGoogleID  = "google-official"
	SourceTencentID = "tencent-cloud"
)

// Builtin 返回内置镜像源表（含实测备注）。
func Builtin() []domain.MirrorSource {
	return []domain.MirrorSource{
		{
			ID:      SourceTencentID,
			Name:    "腾讯云镜像",
			BaseURL: "https://mirrors.cloud.tencent.com/AndroidSDK/",
			Kind:    domain.MirrorMirror,
			Grade:   domain.GradeUnknown,
			Enabled: true,
			Region:  "中国大陆",
			Note:    "dl.google.com/android/repository 的完整目录镜像（含系统镜像），国内速度最佳",
		},
		{
			ID:      SourceGoogleID,
			Name:    "Google 官方",
			BaseURL: "https://dl.google.com/android/repository/",
			Kind:    domain.MirrorOfficial,
			Grade:   domain.GradeUnknown,
			Enabled: true,
			Region:  "全球",
			Note:    "官方源，始终最新；其它镜像异常时自动回退到此处",
		},
		{
			ID:      "tuna-tsinghua",
			Name:    "清华 TUNA",
			BaseURL: "https://mirrors.tuna.tsinghua.edu.cn/android/repository/",
			Kind:    domain.MirrorMirror,
			Grade:   domain.GradeUnknown,
			Enabled: false,
			Region:  "中国大陆",
			Note:    "实测返回 403（该站当前不提供 SDK 目录镜像）",
		},
		{
			ID:      "ustc",
			Name:    "中科大 USTC",
			BaseURL: "https://mirrors.ustc.edu.cn/android/repository/",
			Kind:    domain.MirrorMirror,
			Grade:   domain.GradeUnknown,
			Enabled: false,
			Region:  "中国大陆",
			Note:    "实测 404（路径不存在）",
		},
		{
			ID:      "bfsu",
			Name:    "北外 BFSU",
			BaseURL: "https://mirrors.bfsu.edu.cn/android/repository/",
			Kind:    domain.MirrorMirror,
			Grade:   domain.GradeUnknown,
			Enabled: false,
			Region:  "中国大陆",
			Note:    "实测 403",
		},
		{
			ID:      "neusoft",
			Name:    "东软信息学院",
			BaseURL: "https://mirrors.neusoft.edu.cn/android/repository/",
			Kind:    domain.MirrorMirror,
			Grade:   domain.GradeUnknown,
			Enabled: false,
			Region:  "中国大陆",
			Note:    "实测 TLS 握手失败（证书问题）",
		},
		{
			ID:      "huaweicloud",
			Name:    "华为云",
			BaseURL: "https://repo.huaweicloud.com/android/repository/",
			Kind:    domain.MirrorMirror,
			Grade:   domain.GradeUnknown,
			Enabled: false,
			Region:  "中国大陆",
			Note:    "实测 404（未提供该路径）",
		},
	}
}

// NormalizeBaseURL 规范化用户输入的镜像地址：补 https、补结尾斜杠、去空白。
func NormalizeBaseURL(raw string) (string, error) {
	url := strings.TrimSpace(raw)
	if url == "" {
		return "", domain.Err(domain.CodeInvalidArgument, "镜像地址不能为空")
	}
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return "", domain.Err(domain.CodeInvalidArgument, "只支持 http/https 地址")
	}
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return url, nil
}

// Find 在内置 + 自定义源中查找。
func Find(sources []domain.MirrorSource, id string) (domain.MirrorSource, bool) {
	for _, s := range sources {
		if s.ID == id {
			return s, true
		}
	}
	return domain.MirrorSource{}, false
}

// Merge 合并内置源与用户自定义源（自定义源优先覆盖同 ID 项）。
func Merge(custom []domain.MirrorSource) []domain.MirrorSource {
	out := Builtin()
	index := map[string]int{}
	for i, s := range out {
		index[s.ID] = i
	}
	for _, c := range custom {
		if c.ID == "" {
			continue
		}
		if i, ok := index[c.ID]; ok {
			out[i] = c
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// 启用的、以及"内置优先"排序
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// DefaultSourceID 返回默认源（优先腾讯云，其次官方）。
func DefaultSourceID(sources []domain.MirrorSource) string {
	for _, s := range sources {
		if s.ID == SourceTencentID && s.Enabled {
			return s.ID
		}
	}
	for _, s := range sources {
		if s.ID == SourceGoogleID && s.Enabled {
			return s.ID
		}
	}
	for _, s := range sources {
		if s.Enabled {
			return s.ID
		}
	}
	return SourceGoogleID
}

// GradeFromResult 根据测速结果判定兼容性等级。
func GradeFromResult(r domain.SpeedResult) domain.MirrorGrade {
	switch {
	case !r.OK && r.HTTPStatus == 0 && !r.XMLOK:
		return domain.GradeUnreachable
	case !r.XMLOK:
		return domain.GradeIndexOnly
	case r.HasCmdlineTools && r.HasEmulator:
		return domain.GradeFull
	case r.HasCmdlineTools || r.HasEmulator:
		return domain.GradeFull
	default:
		return domain.GradeIndexOnly
	}
}

// IsFresh 判断缓存的测速结果是否仍可用。
func IsFresh(r domain.SpeedResult, ttl time.Duration) bool {
	if r.At == 0 {
		return false
	}
	return time.Since(time.UnixMilli(r.At)) < ttl
}
