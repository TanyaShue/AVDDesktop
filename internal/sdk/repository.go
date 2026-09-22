package sdk

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
)

// RepositoryIndexPath 是 Android SDK 仓库主索引的文件名。
const RepositoryIndexPath = "repository2-3.xml"

// RepositoryArchive 是仓库索引中与当前平台匹配的归档。
type RepositoryArchive struct {
	Path        string
	URL         string
	Size        int64
	SHA1        string
	HostOS      string
	HostArch    string
	DisplayName string
}

// RepositoryPackage 是仓库索引中的一个包。
type RepositoryPackage struct {
	Path        string
	DisplayName string
	Archives    []RepositoryArchive
}

// RepositoryIndex 是解析后的 Android SDK 仓库索引。
type RepositoryIndex struct {
	BaseURL  string
	Packages map[string]RepositoryPackage
	Raw      []byte
}

type repositoryXML struct {
	RemotePackages []repositoryPackageXML `xml:"remotePackage"`
}

type repositoryPackageXML struct {
	Path        string                 `xml:"path,attr"`
	DisplayName string                 `xml:"display-name"`
	Archives    []repositoryArchiveXML `xml:"archives>archive"`
}

type repositoryArchiveXML struct {
	HostOS   string `xml:"host-os"`
	HostArch string `xml:"host-arch"`
	Complete struct {
		Size     int64 `xml:"size"`
		Checksum struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"checksum"`
		URL string `xml:"url"`
	} `xml:"complete"`
}

// FetchRepository 下载并解析镜像站的 repository2-3.xml。
func FetchRepository(ctx context.Context, baseURL string, client *http.Client) (*RepositoryIndex, error) {
	base, err := NormalizeRepositoryBase(baseURL)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	indexURL := base + RepositoryIndexPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, domain.Wrap(domain.CodeInvalidArgument, "仓库索引地址非法", err)
	}
	req.Header.Set("Accept", "application/xml,text/xml,*/*")
	req.Header.Set("User-Agent", "AVDDesktop/0.2")
	resp, err := client.Do(req)
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法连接 SDK 镜像仓库", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, domain.ErrDetail(domain.CodeProcessFailed,
			"镜像仓库索引不可用", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "读取镜像仓库索引失败", err)
	}
	return ParseRepository(data, base)
}

// ParseRepository 解析仓库索引内容；baseURL 用于把相对归档地址解析为绝对地址。
func ParseRepository(data []byte, baseURL string) (*RepositoryIndex, error) {
	base, err := NormalizeRepositoryBase(baseURL)
	if err != nil {
		return nil, err
	}
	var raw repositoryXML
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, domain.Wrap(domain.CodeProcessFailed, "无法解析 SDK 仓库索引", err)
	}
	idx := &RepositoryIndex{
		BaseURL:  base,
		Packages: make(map[string]RepositoryPackage, len(raw.RemotePackages)),
		Raw:      data,
	}
	for _, item := range raw.RemotePackages {
		pkgPath := strings.TrimSpace(item.Path)
		if pkgPath == "" {
			continue
		}
		pkg := RepositoryPackage{Path: pkgPath, DisplayName: strings.TrimSpace(item.DisplayName)}
		for _, archive := range item.Archives {
			rel := strings.TrimSpace(archive.Complete.URL)
			if rel == "" {
				continue
			}
			pkg.Archives = append(pkg.Archives, RepositoryArchive{
				Path:        pkgPath,
				URL:         ResolveRepositoryURL(base, rel),
				Size:        archive.Complete.Size,
				SHA1:        strings.TrimSpace(archive.Complete.Checksum.Value),
				HostOS:      strings.TrimSpace(archive.HostOS),
				HostArch:    strings.TrimSpace(archive.HostArch),
				DisplayName: pkg.DisplayName,
			})
		}
		idx.Packages[pkgPath] = pkg
	}
	if len(idx.Packages) == 0 {
		return nil, domain.Err(domain.CodeProcessFailed,
			"镜像仓库索引中没有可用包（该地址可能不是 Android SDK 仓库）")
	}
	return idx, nil
}

// Find 按包路径查找仓库包。
func (idx *RepositoryIndex) Find(pkgPath string) (RepositoryPackage, bool) {
	if idx == nil {
		return RepositoryPackage{}, false
	}
	pkg, ok := idx.Packages[strings.TrimSpace(pkgPath)]
	return pkg, ok
}

// ArchiveFor 返回指定包在当前平台上的归档。
func (idx *RepositoryIndex) ArchiveFor(pkgPath, goos, goarch string) (RepositoryArchive, error) {
	pkg, ok := idx.Find(pkgPath)
	if !ok {
		return RepositoryArchive{}, domain.ErrDetail(domain.CodeProcessFailed,
			"镜像仓库缺少所需包", pkgPath)
	}
	wantOS := hostOSName(goos)
	wantArch := hostArchName(goarch)
	bestScore := -1
	bestIndex := -1
	for i, archive := range pkg.Archives {
		score := archiveMatchScore(archive, wantOS, wantArch)
		if score > bestScore {
			bestScore = score
			bestIndex = i
		}
	}
	if bestIndex < 0 {
		return RepositoryArchive{}, domain.ErrDetail(domain.CodeProcessFailed,
			"镜像仓库没有当前平台的归档", pkgPath+" ("+goos+"/"+goarch+")")
	}
	return pkg.Archives[bestIndex], nil
}

// NormalizeRepositoryBase 规范化镜像仓库根地址，确保以 / 结尾。
func NormalizeRepositoryBase(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", domain.Err(domain.CodeInvalidArgument, "镜像地址不能为空")
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", domain.ErrDetail(domain.CodeInvalidArgument, "镜像地址非法", value)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", domain.Err(domain.CodeInvalidArgument, "镜像地址只支持 http/https")
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u.String(), nil
}

// ResolveRepositoryURL 把仓库索引中的相对路径解析为绝对地址。
func ResolveRepositoryURL(baseURL, relative string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return relative
	}
	ref, err := url.Parse(strings.TrimSpace(relative))
	if err != nil {
		return relative
	}
	if ref.IsAbs() {
		return ref.String()
	}
	return base.ResolveReference(ref).String()
}

func archiveMatchScore(archive RepositoryArchive, wantOS, wantArch string) int {
	score := -1
	switch {
	case archive.HostOS == "":
		score = 1
	case archive.HostOS == wantOS:
		score = 3
	default:
		return -1
	}
	switch {
	case archive.HostArch == "":
		score += 1
	case archive.HostArch == wantArch:
		score += 3
	default:
		return -1
	}
	return score
}

func hostOSName(goos string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "darwin" {
		return "macosx"
	}
	return goos
}

func hostArchName(goarch string) string {
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	default:
		return goarch
	}
}
