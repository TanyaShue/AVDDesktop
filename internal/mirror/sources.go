// Package mirror 提供 Android SDK 下载镜像表、连通性检测、延迟/速度采样与资源校验。
package mirror

import (
	"sort"
	"strings"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/sdk"
)

// DefaultSourceID 是未保存用户选择时使用的默认镜像。
const DefaultSourceID = "google-cn"

// Builtin 返回内置镜像站列表。
//
// 其中几项可能只同步部分内容，保留它们的目的是让用户能在当前网络下重新验证，
// 而不是把历史结论写死。实际是否可用完全以探测结果为准。
func Builtin() []domain.MirrorSource {
	return []domain.MirrorSource{
		{
			ID:       "google-cn",
			Name:     "Google 中国下载",
			BaseURL:  "https://googledownloads.cn/android/repository/",
			Region:   "中国大陆",
			Note:     "Google 官方中国下载域名，适合国内直连",
			Official: true,
		},
		{
			ID:      "tencent-cloud",
			Name:    "腾讯云",
			BaseURL: "https://mirrors.cloud.tencent.com/AndroidSDK/",
			Region:  "中国大陆",
			Note:    "完整 Android SDK 目录镜像，覆盖命令行工具、emulator 与系统镜像",
		},
		{
			ID:       "google-official",
			Name:     "Google 官方",
			BaseURL:  "https://dl.google.com/android/repository/",
			Region:   "全球",
			Note:     "官方源，内容始终最新；镜像异常时可作为兜底",
			Official: true,
		},
		{
			ID:      "tuna-tsinghua",
			Name:    "清华 TUNA",
			BaseURL: "https://mirrors.tuna.tsinghua.edu.cn/android/repository/",
			Region:  "中国大陆",
			Note:    "历史可用性不稳定，部分网络会返回 403；以实时检测结果为准",
		},
		{
			ID:      "ustc",
			Name:    "中科大 USTC",
			BaseURL: "https://mirrors.ustc.edu.cn/android/repository/",
			Region:  "中国大陆",
			Note:    "当前通常不提供 SDK 仓库路径；仍可按需重新检测",
		},
		{
			ID:      "bfsu",
			Name:    "北外 BFSU",
			BaseURL: "https://mirrors.bfsu.edu.cn/android/repository/",
			Region:  "中国大陆",
			Note:    "当前可能返回 403；以实时检测结果为准",
		},
	}
}

// Find 在内置镜像中查找 ID。
func Find(id string) (domain.MirrorSource, bool) {
	id = strings.TrimSpace(id)
	for _, source := range Builtin() {
		if source.ID == id {
			return source, true
		}
	}
	return domain.MirrorSource{}, false
}

// Resolve 返回指定 ID 的镜像；ID 为空或无效时返回默认源。
func Resolve(id string) domain.MirrorSource {
	if source, ok := Find(id); ok {
		return source
	}
	if source, ok := Find(DefaultSourceID); ok {
		return source
	}
	return Builtin()[0]
}

// MarkActive 返回一份标记了当前默认源的镜像列表。
func MarkActive(sources []domain.MirrorSource, activeID string) []domain.MirrorSource {
	active := Resolve(activeID)
	out := append([]domain.MirrorSource(nil), sources...)
	for i := range out {
		out[i].Active = out[i].ID == active.ID
	}
	return out
}

// NormalizeBaseURL 规范化用户输入的镜像根地址。
func NormalizeBaseURL(raw string) (string, error) {
	return sdk.NormalizeRepositoryBase(raw)
}

// SortForDisplay 把官方/大陆常用源排在前面，但不改变最终推荐逻辑。
func SortForDisplay(sources []domain.MirrorSource) {
	order := map[string]int{}
	for i, source := range Builtin() {
		order[source.ID] = i
	}
	sort.SliceStable(sources, func(i, j int) bool {
		oi, iok := order[sources[i].ID]
		oj, jok := order[sources[j].ID]
		if iok && jok {
			return oi < oj
		}
		if iok != jok {
			return iok
		}
		return sources[i].Name < sources[j].Name
	})
}
