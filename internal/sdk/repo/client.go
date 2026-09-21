// Package repo 是 Android SDK 仓库索引客户端。
//
// 依据实测（docs/RESEARCH-NOTES.md §2）：
//   - repository2-3.xml              覆盖 cmdline-tools / platform-tools / emulator / platforms / build-tools / extras
//   - sys-img/<tag>/sys-img2-3.xml   系统镜像，每个 tag 一个索引文件
//   - 归档 url 是相对路径：repository 索引相对于仓库根，sys-img 索引相对于该 XML 所在目录
//   - emulator/cmdline-tools 的归档带 host-os/host-arch，系统镜像不带
//   - 每个归档有 size 与 sha1，安装前必须校验
package repo

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
)

// 索引相对路径常量。
const (
	IndexRepository = "repository2-3.xml"
	IndexAddon      = "addon2-3.xml"
	IndexSysImgTmpl = "sys-img/%s/sys-img2-3.xml"
)

// SysImgTag 是系统镜像的 tag（对应镜像站上的目录名）。
type SysImgTag struct {
	ID      string `json:"id"`
	Display string `json:"display"`
	Note    string `json:"note"`
}

// KnownSysImgTags 是常用系统镜像 tag 及中文说明。
//
// 注意：这里只做"候选列表"，实际可用性由索引请求结果决定（不同 API 支持的 tag 不同）。
var KnownSysImgTags = []SysImgTag{
	{ID: "google_apis", Display: "Google APIs", Note: "含 Google 服务，可 adb root，适合大多数调试"},
	{ID: "google_apis_playstore", Display: "Google Play", Note: "含 Play 商店，不可 adb root"},
	{ID: "aosp_atd", Display: "AOSP ATD", Note: "自动化测试优化镜像，无界面、启动快、占用小"},
	{ID: "google_atd", Display: "Google APIs ATD", Note: "自动化测试优化镜像（含 Google APIs）"},
	{ID: "android-desktop", Display: "Desktop", Note: "桌面模式（自由窗口），适合桌面应用调试"},
	{ID: "android-tv", Display: "Android TV", Note: "电视设备"},
	{ID: "android-automotive", Display: "Automotive", Note: "车机设备"},
	{ID: "android-automotive-playstore", Display: "Automotive with Play", Note: "车机设备（含 Play 商店）"},
	{ID: "android-wear", Display: "Wear OS", Note: "手表设备"},
	{ID: "android-xr", Display: "Android XR", Note: "XR 设备"},
	{ID: "google_apis_ps16k", Display: "Google APIs (16KB page)", Note: "16KB 内存页模拟，较新平台"},
}

// TagDisplay 返回 sys-img tag 的展示名。
func TagDisplay(tag string) string {
	for _, t := range KnownSysImgTags {
		if t.ID == tag {
			return t.Display
		}
	}
	return tag
}

// KnownSysImgTagSet 返回 tag id 集合，便于校验。
func KnownSysImgTagSet() map[string]SysImgTag {
	m := make(map[string]SysImgTag, len(KnownSysImgTags))
	for _, t := range KnownSysImgTags {
		m[t.ID] = t
	}
	return m
}

// Revision 是包版本。
type Revision struct {
	Major int `xml:"major" json:"major"`
	Minor int `xml:"minor" json:"minor"`
	Micro int `xml:"micro" json:"micro"`
}

// String 返回 "37.2.10" 形式（0 段会被省略）。
func (r Revision) String() string {
	switch {
	case r.Micro > 0:
		return fmt.Sprintf("%d.%d.%d", r.Major, r.Minor, r.Micro)
	case r.Minor > 0:
		return fmt.Sprintf("%d.%d", r.Major, r.Minor)
	default:
		return fmt.Sprintf("%d", r.Major)
	}
}

// Archive 是一个可下载归档。
type Archive struct {
	HostOS   string `xml:"host-os" json:"hostOS"`
	HostArch string `xml:"host-arch" json:"hostArch"`
	Size     int64  `xml:"complete>size" json:"size"`
	URL      string `xml:"complete>url" json:"url"`
	SHA1     string `xml:"complete>checksum" json:"sha1"`
}

// Dependency 是包依赖。
type Dependency struct {
	Path string `xml:"path,attr" json:"path"`
}

// UsesLicense 是 <uses-license ref="…"/> 子元素。
type UsesLicense struct {
	Ref string `xml:"ref,attr"`
}

// ChannelRef 是 <channelRef ref="…"/> 子元素。
type ChannelRef struct {
	Ref string `xml:"ref,attr"`
}

// Package 是仓库中的一个包。
type Package struct {
	Path         string       `xml:"path,attr" json:"path"`
	DisplayName  string       `xml:"display-name" json:"displayName"`
	Revision     Revision     `xml:"revision" json:"revision"`
	UsesLicense  UsesLicense  `xml:"uses-license" json:"usesLicense"`
	ChannelRef   ChannelRef   `xml:"channelRef" json:"channelRef"`
	Archives     []Archive    `xml:"archives>archive" json:"archives"`
	Dependencies []Dependency `xml:"dependencies>dependency" json:"dependencies"`

	// 系统镜像专有（type-details）
	APILevel       string `xml:"type-details>api-level" json:"apiLevel"`
	ExtensionLevel string `xml:"type-details>extension-level" json:"extensionLevel"`
	TagID          string `xml:"type-details>tag>id" json:"tagId"`
	TagDisplay     string `xml:"type-details>tag>display" json:"tagDisplay"`
	ABI            string `xml:"type-details>abi" json:"abi"`
	VendorID       string `xml:"type-details>vendor>id" json:"vendorId"`
	VendorDisplay  string `xml:"type-details>vendor>display" json:"vendorDisplay"`
	IsBaseSDK      string `xml:"type-details>is-base-sdk" json:"isBaseSdk"`

	// 安装后填充
	BaseURL string `xml:"-" json:"baseURL"`
}

// LicenseID 返回该包声明的许可 id（例如 android-sdk-license）。
func (p Package) LicenseID() string { return p.UsesLicense.Ref }

// Kind 返回包的大类（用于 UI 分组）。
func (p Package) Kind() string {
	switch {
	case strings.HasPrefix(p.Path, "cmdline-tools"):
		return "cmdline-tools"
	case p.Path == "platform-tools":
		return "platform-tools"
	case p.Path == "emulator":
		return "emulator"
	case strings.HasPrefix(p.Path, "platforms;"):
		return "platforms"
	case strings.HasPrefix(p.Path, "build-tools;"):
		return "build-tools"
	case strings.HasPrefix(p.Path, "system-images;"):
		return "system-images"
	case strings.HasPrefix(p.Path, "ndk;"):
		return "ndk"
	case strings.HasPrefix(p.Path, "extras;"):
		return "extras"
	default:
		return "other"
	}
}

// PickArchive 选择适配当前平台的归档。
//
// 规则（实测）：
//   - 带 host-os 的归档（emulator / cmdline-tools）：必须匹配当前 OS，且 host-arch 只在有值时比较
//   - 不带 host-os 的归档（系统镜像）：直接可用
func (p Package) PickArchive(goos, goarch string) (Archive, error) {
	var fallback *Archive
	for i := range p.Archives {
		a := p.Archives[i]
		if a.URL == "" {
			continue
		}
		if a.HostOS == "" {
			if fallback == nil {
				fallback = &a
			}
			continue
		}
		if !strings.EqualFold(a.HostOS, mapGOOS(goos)) {
			continue
		}
		if a.HostArch == "" || strings.EqualFold(a.HostArch, mapGOARCH(goarch)) {
			return a, nil
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return Archive{}, domain.ErrDetail(domain.CodeDownloadFailed,
		fmt.Sprintf("包 %s 没有适配 %s/%s 的归档", p.Path, goos, goarch), "")
}

// 仓库 XML 使用 sdk: 前缀，但 Go 的 xml 包按本地名匹配即可（已用真实索引验证）。
func mapGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "macosx"
	default:
		return goos
	}
}

func mapGOARCH(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "arm64":
		return "aarch64"
	default:
		return goarch
	}
}

// License 是索引里携带的许可文本。
type License struct {
	ID   string `xml:"id,attr"`
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

type indexXML struct {
	// 不限定根元素名：repository2-3.xml 的根是 sdk-repository，
	// 而 sys-img2-3.xml 的根是 sdk-sys-img（实测）。
	XMLName        xml.Name    `xml:""`
	Licenses       []License   `xml:"license"`
	RemotePackages []Package   `xml:"remotePackage"`
	Channels       []channelXM `xml:"channel"`
}

type channelXM struct {
	ID string `xml:"id,attr"`
}

// Index 是一个已解析的仓库索引。
type Index struct {
	SourceURL string // 索引文件 URL
	BaseURL   string // 归档相对 URL 的基准（= 索引所在目录）
	RootName  string // 根元素名（sdk-repository / sdk-sys-img）
	FetchedAt int64
	Licenses  map[string]License
	Packages  []Package
	byPath    map[string]int
}

// Build 建立路径索引（解析后必须调用）。
func (idx *Index) Build() {
	idx.byPath = make(map[string]int, len(idx.Packages))
	for i := range idx.Packages {
		idx.Packages[i].BaseURL = idx.BaseURL
		idx.byPath[idx.Packages[i].Path] = i
	}
}

// Find 按包路径查找。
func (idx *Index) Find(pkgPath string) (Package, bool) {
	if idx.byPath == nil {
		idx.Build()
	}
	if i, ok := idx.byPath[pkgPath]; ok {
		return idx.Packages[i], true
	}
	return Package{}, false
}

// PackageURL 把归档相对路径解析为绝对 URL。
func (idx *Index) PackageURL(a Archive) string {
	return ResolveURL(idx.SourceURL, a.URL)
}

// ResolveURL 依据索引文件 URL 解析相对归档 URL（base = 索引所在目录）。
func ResolveURL(indexURL, rel string) string {
	if rel == "" {
		return ""
	}
	if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
		return rel
	}
	if i := strings.LastIndex(indexURL, "/"); i >= 0 {
		return indexURL[:i+1] + strings.TrimPrefix(path.Clean("/"+rel), "/")
	}
	return rel
}

// ParseIndex 解析仓库索引 XML。
func ParseIndex(data []byte, sourceURL string) (*Index, error) {
	var raw indexXML
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, domain.Wrap(domain.CodeMirrorIndexOnly, "无法解析仓库索引（可能不是 Android SDK 索引）", err)
	}
	if len(raw.RemotePackages) == 0 {
		return nil, domain.ErrDetail(domain.CodeMirrorIndexOnly,
			"仓库索引中没有可用包（该地址可能只是普通网页或镜像不完整）", sourceURL)
	}
	idx := &Index{
		SourceURL: sourceURL,
		BaseURL:   dirOf(sourceURL),
		RootName:  raw.XMLName.Local,
		FetchedAt: time.Now().UnixMilli(),
		Licenses:  make(map[string]License, len(raw.Licenses)),
		Packages:  raw.RemotePackages,
	}
	for _, l := range raw.Licenses {
		idx.Licenses[l.ID] = l
	}
	idx.Build()
	return idx, nil
}

func dirOf(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[:i+1]
	}
	return url
}

// ---------------------------------------------------------------- 网络抓取 + 磁盘缓存

// Fetcher 负责带缓存地抓取索引与任意文件。
type Fetcher struct {
	Client    *http.Client
	CacheDir  string
	TTL       time.Duration
	UserAgent string
}

// NewFetcher 创建索引抓取器。
func NewFetcher(cacheDir string, timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Fetcher{
		Client:    &http.Client{Timeout: timeout},
		CacheDir:  cacheDir,
		TTL:       30 * time.Minute,
		UserAgent: "AVDDesktop/0.1 (+https://github.com/) Wails",
	}
}

// GetIndex 抓取并解析索引（带内存/磁盘缓存与条件请求）。
//
// cacheKey 用于区分不同源与不同 tag 的缓存文件。
func (f *Fetcher) GetIndex(ctx context.Context, cacheKey, url string, force bool) (*Index, error) {
	data, err := f.fetch(ctx, cacheKey, url, force)
	if err != nil {
		return nil, err
	}
	return ParseIndex(data, url)
}
func (f *Fetcher) fetch(ctx context.Context, cacheKey, url string, force bool) ([]byte, error) {
	cachePath := ""
	if f.CacheDir != "" && cacheKey != "" {
		cachePath = filepath.Join(f.CacheDir, cacheKey+".xml")
		if !force && f.fresh(cachePath) {
			if data, err := os.ReadFile(cachePath); err == nil {
				return data, nil
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, domain.Wrap(domain.CodeInvalidArgument, "索引地址非法", err)
	}
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	req.Header.Set("Accept", "application/xml,text/xml,*/*")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, domain.ErrDetail(domain.CodeMirrorUnreachable, "无法连接镜像源", url+"\n"+err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, domain.ErrDetail(domain.CodeMirrorIndexOnly,
			fmt.Sprintf("该镜像没有提供 SDK 索引（HTTP %d）", resp.StatusCode), url).
			WithHint("请更换镜像源，或使用 Google 官方源")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, domain.ErrDetail(domain.CodeMirrorUnreachable,
			fmt.Sprintf("镜像返回异常状态码 HTTP %d", resp.StatusCode), url)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, domain.Wrap(domain.CodeMirrorUnreachable, "读取索引内容失败", err)
	}
	if cachePath != "" {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		_ = os.WriteFile(cachePath, data, 0o644)
	}
	return data, nil
}

func (f *Fetcher) fresh(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	ttl := f.TTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return time.Since(info.ModTime()) < ttl
}

// Platform 返回当前平台标识（用于日志与 UI 提示）。
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }
