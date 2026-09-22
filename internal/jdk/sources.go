package jdk

import (
	"runtime"
	"strings"

	"AVDDesktop/internal/domain"
)

const (
	// OfficialSourceID 是 Adoptium 官方 GitHub Release 源。
	OfficialSourceID = "github-official"
	// DefaultSourceID 是首次使用时优先选择的 JDK 下载源。
	DefaultSourceID = "nju"
)

type sourceLayout uint8

const (
	layoutGitHubRelease sourceLayout = iota
	layoutAdoptiumMirror
)

type sourceDefinition struct {
	source domain.MirrorSource
	layout sourceLayout
}

// sourceDefinitions 中的国内镜像与官方 Release 提供同一批 Eclipse Temurin
// 归档文件，因此共用程序内置的 SHA-256，镜像站无法篡改下载内容。
var sourceDefinitions = []sourceDefinition{
	{
		source: domain.MirrorSource{
			ID:      "tuna",
			Name:    "清华 TUNA",
			BaseURL: "https://mirrors.tuna.tsinghua.edu.cn/Adoptium/",
			Region:  "中国大陆",
			Note:    "Eclipse Temurin 完整镜像，文件与官方 SHA-256 一致",
		},
		layout: layoutAdoptiumMirror,
	},
	{
		source: domain.MirrorSource{
			ID:      "bfsu",
			Name:    "北外 BFSU",
			BaseURL: "https://mirrors.bfsu.edu.cn/Adoptium/",
			Region:  "中国大陆",
			Note:    "Eclipse Temurin 完整镜像，适合教育网与国内直连",
		},
		layout: layoutAdoptiumMirror,
	},
	{
		source: domain.MirrorSource{
			ID:      "nju",
			Name:    "南京大学 NJU",
			BaseURL: "https://mirrors.nju.edu.cn/adoptium/",
			Region:  "中国大陆",
			Note:    "Adoptium 官方制品镜像，更新及时，适合作为国内默认源",
		},
		layout: layoutAdoptiumMirror,
	},
	{
		source: domain.MirrorSource{
			ID:       OfficialSourceID,
			Name:     "GitHub 官方",
			BaseURL:  temurinBaseURL,
			Region:   "全球",
			Note:     "Adoptium 官方发布地址；国内镜像异常时作为兜底",
			Official: true,
		},
		layout: layoutGitHubRelease,
	},
}

// Sources 返回 JDK 内置镜像源。
func Sources() []domain.MirrorSource {
	out := make([]domain.MirrorSource, 0, len(sourceDefinitions))
	for _, def := range sourceDefinitions {
		out = append(out, def.source)
	}
	return out
}

// FindSource 按 ID 查找 JDK 镜像源。
func FindSource(id string) (domain.MirrorSource, bool) {
	id = strings.TrimSpace(id)
	for _, def := range sourceDefinitions {
		if def.source.ID == id {
			return def.source, true
		}
	}
	return domain.MirrorSource{}, false
}

// ResolveSource 返回指定 ID 的 JDK 源；ID 为空或无效时返回默认源。
func ResolveSource(id string) domain.MirrorSource {
	if source, ok := FindSource(id); ok {
		return source
	}
	if source, ok := FindSource(DefaultSourceID); ok {
		return source
	}
	return Sources()[0]
}

// MarkSourcesActive 返回一份标记了当前默认源的 JDK 镜像列表。
func MarkSourcesActive(sources []domain.MirrorSource, activeID string) []domain.MirrorSource {
	active := ResolveSource(activeID)
	out := append([]domain.MirrorSource(nil), sources...)
	for i := range out {
		out[i].Active = out[i].ID == active.ID
	}
	return out
}

// ArtifactURL 返回当前平台从指定源下载 JDK 的完整地址。
func ArtifactURL(sourceID string) (string, error) {
	source, ok := FindSource(sourceID)
	if !ok {
		return "", domain.Err(domain.CodeInvalidArgument, "JDK 镜像源不存在: "+strings.TrimSpace(sourceID))
	}
	spec, err := artifactForSource(source, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	return spec.URL, nil
}

// artifactForSource 根据镜像源生成当前平台 JDK 归档的下载地址。
// 未知 ID 且提供 BaseURL 时，按“BaseURL 已指向归档目录”处理，便于测试与扩展。
func artifactForSource(source domain.MirrorSource, goos, goarch string) (artifact, error) {
	spec, err := artifactFor(goos, goarch)
	if err != nil {
		return artifact{}, err
	}

	def, known := sourceDefinitionByID(source.ID)
	if known && def.layout == layoutGitHubRelease {
		return spec, nil
	}

	root := strings.TrimSpace(source.BaseURL)
	if known {
		if root == "" {
			root = def.source.BaseURL
		}
	}
	if root == "" {
		return artifact{}, domain.ErrDetail(domain.CodeInvalidArgument, "JDK 镜像地址为空", source.ID)
	}

	rel := ""
	if known && def.layout == layoutAdoptiumMirror {
		rel, err = adoptiumMirrorPath(goos, goarch)
		if err != nil {
			return artifact{}, err
		}
	}
	spec.URL = joinURL(root, rel+spec.Name)
	return spec, nil
}

func sourceDefinitionByID(id string) (sourceDefinition, bool) {
	id = strings.TrimSpace(id)
	for _, def := range sourceDefinitions {
		if def.source.ID == id {
			return def, true
		}
	}
	return sourceDefinition{}, false
}

// adoptiumMirrorPath 返回 NJU/TUNA/BFSU 等 Adoptium 镜像的版本/平台目录。
func adoptiumMirrorPath(goos, goarch string) (string, error) {
	arch := ""
	switch goarch {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "aarch64"
	default:
		return "", domain.ErrDetail(domain.CodeInvalidArgument,
			"当前平台暂不支持自动安装 JDK", goos+"/"+goarch)
	}
	osName := ""
	switch goos {
	case "windows":
		osName = "windows"
	case "darwin":
		osName = "mac"
	case "linux":
		osName = "linux"
	default:
		return "", domain.ErrDetail(domain.CodeInvalidArgument,
			"当前平台暂不支持自动安装 JDK", goos+"/"+goarch)
	}
	return "21/jdk/" + arch + "/" + osName + "/", nil
}

func joinURL(root, rel string) string {
	return strings.TrimRight(root, "/") + "/" + strings.TrimLeft(rel, "/")
}
