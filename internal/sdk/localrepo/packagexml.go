// Package localrepo 写入 Android SDK 的本地包元数据 <pkg>/package.xml。
//
// 背景（实测，见 docs/RESEARCH-NOTES.md §9）：sdkmanager / avdmanager / Android Studio
// 判断"某个包是否已安装"的依据是包目录下的 package.xml，source.properties 只是版本载体。
// 本项目自研安装器（internal/sdk/install）直接解压官方归档并整体替换包目录，而官方归档里
// 并不含 package.xml，所以必须由我们补写；否则会出现"文件明明在，官方工具却认为没装"的
// 故障——创建设备时报
//
//	Error: "emulator" package must be installed!
//
// 写入格式复刻官方写入器（com.android.sdklib 的 LocalPackageInstaller）：固定命名空间
// ns2…ns18、xsi:type 指向该类型所在 schema 族的最新版本，让 Android Studio / Gradle /
// sdkmanager / avdmanager 都能一致解析。
package localrepo

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk/repo"
)

// FileName 是本地包元数据文件名（与 sdkmanager / Android Studio 一致）。
const FileName = "package.xml"

// xsiNamespace 是 xsi:type 所在的命名空间。
const xsiNamespace = repo.XSINamespace

// namespacePrefixes 是官方 package.xml 使用的固定命名空间集合（与 sdklib 输出一致）。
var namespacePrefixes = []struct{ Prefix, URI string }{
	{"ns2", "http://schemas.android.com/repository/android/common/02"},
	{"ns3", "http://schemas.android.com/repository/android/common/01"},
	{"ns4", "http://schemas.android.com/repository/android/generic/01"},
	{"ns5", "http://schemas.android.com/repository/android/generic/02"},
	{"ns6", "http://schemas.android.com/sdk/android/repo/repository2/04"},
	{"ns7", "http://schemas.android.com/sdk/android/repo/repository2/01"},
	{"ns8", "http://schemas.android.com/sdk/android/repo/repository2/02"},
	{"ns9", "http://schemas.android.com/sdk/android/repo/repository2/03"},
	{"ns10", "http://schemas.android.com/sdk/android/repo/addon2/01"},
	{"ns11", "http://schemas.android.com/sdk/android/repo/addon2/02"},
	{"ns12", "http://schemas.android.com/sdk/android/repo/addon2/03"},
	{"ns13", "http://schemas.android.com/sdk/android/repo/addon2/04"},
	{"ns14", "http://schemas.android.com/sdk/android/repo/sys-img2/05"},
	{"ns15", "http://schemas.android.com/sdk/android/repo/sys-img2/04"},
	{"ns16", "http://schemas.android.com/sdk/android/repo/sys-img2/03"},
	{"ns17", "http://schemas.android.com/sdk/android/repo/sys-img2/02"},
	{"ns18", "http://schemas.android.com/sdk/android/repo/sys-img2/01"},
}

// schemaFamilyMax 把"某命名空间族的任意版本"归一到该族最新版本。
//
// 官方写入器就是这么选的：platformDetailsType 在 repository2/01…04 里都有定义，
// 官方写出的却是 ns6=repository2/04（实测 platforms/android-36/package.xml）。
var schemaFamilyMax = map[string]string{
	"http://schemas.android.com/repository/android/common":    "http://schemas.android.com/repository/android/common/02",
	"http://schemas.android.com/repository/android/generic":   "http://schemas.android.com/repository/android/generic/02",
	"http://schemas.android.com/sdk/android/repo/repository2": "http://schemas.android.com/sdk/android/repo/repository2/04",
	"http://schemas.android.com/sdk/android/repo/addon2":      "http://schemas.android.com/sdk/android/repo/addon2/04",
	"http://schemas.android.com/sdk/android/repo/sys-img2":    "http://schemas.android.com/sdk/android/repo/sys-img2/05",
}

// prefixByURI 是 namespacePrefixes 的反查表。
var prefixByURI = func() map[string]string {
	m := make(map[string]string, len(namespacePrefixes))
	for _, n := range namespacePrefixes {
		m[n.URI] = n.Prefix
	}
	return m
}()

// genericTypeDetails 是无专有类型的包使用的 type-details（官方对 platform-tools /
// emulator / cmdline-tools / build-tools / cmake / ndk / extras 都写它）。
const genericTypeDetails = `<type-details xmlns:xsi="` + xsiNamespace + `" xsi:type="ns5:genericDetailsType"/>`

// Revision 是一个包版本（major[.minor[.micro]] + 可选 preview）。
type Revision struct {
	Major   int
	Minor   int
	Micro   int
	Preview int
}

// Dependency 是本地元数据里的依赖项。
type Dependency struct {
	Path        string
	MinRevision *Revision
}

// Meta 是一个待写入的本地包描述。
type Meta struct {
	// Path 是包路径（sdkmanager 语法，如 emulator、platforms;android-36）。
	Path string
	// DisplayName 是 UI 展示名；为空时退回 Path。
	DisplayName string
	// Revision 是已安装版本。
	Revision Revision
	// LicenseRef 是 <uses-license ref="…">，为空则不写该元素。
	LicenseRef string
	// LicenseText 是许可文本。sdklib 要求 uses-license 引用的 id 必须在同一个文件里
	// 定义（实测：缺定义时 parsing problem. 未知 ID "android-sdk-license" → 官方工具
	// 直接丢掉该包），所以有 ref 就必须同时写 <license> 元素。
	LicenseText string
	// Dependencies 是依赖列表（系统镜像会写 emulator 的最低版本）。
	Dependencies []Dependency
	// TypeDetails 是原始 <type-details …>；为空时按通用类型写。
	TypeDetails string
	// ABI 是主 ABI（系统镜像必需）。仓库索引可能缺 <abis>，用它补齐（见 ensureImageAbis）。
	ABI string
	// Namespaces 是仓库索引根元素的前缀 → URI（用于改写 type-details 前缀）。
	Namespaces map[string]string
	// Obsolete 对应 localPackage 的 obsolete 属性。
	Obsolete bool
}

// HasMeta 判断包目录下是否已有 package.xml。
func HasMeta(dir string) bool { return platform.FileExists(filepath.Join(dir, FileName)) }

// Write 把元数据原子写入 <dir>/package.xml，返回写入路径。
func Write(dir string, m Meta) (string, error) {
	data, err := Render(m)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, FileName)
	if err := platform.WriteFileAtomic(target, data, 0o644); err != nil {
		return "", domain.Wrap(domain.CodePermissionDenied, "无法写入 "+FileName, err)
	}
	return target, nil
}

// Render 生成 package.xml 内容。
func Render(m Meta) ([]byte, error) {
	path := strings.TrimSpace(m.Path)
	if path == "" {
		return nil, domain.Err(domain.CodeInvalidArgument, "包路径为空，无法写 package.xml")
	}
	typeDetails, extras := rewriteTypeDetails(m.TypeDetails, m.Namespaces)
	if typeDetails == "" {
		typeDetails = genericTypeDetails
	}
	typeDetails = ensureImageAbis(typeDetails, m.ABI)
	display := firstNonEmpty(strings.TrimSpace(m.DisplayName), path)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<ns2:repository`)
	for _, ns := range namespacePrefixes {
		fmt.Fprintf(&b, ` xmlns:%s="%s"`, ns.Prefix, ns.URI)
	}
	for _, prefix := range sortedKeys(extras) {
		fmt.Fprintf(&b, ` xmlns:%s="%s"`, prefix, escapeAttr(extras[prefix]))
	}
	b.WriteString(">\n")
	if ref := strings.TrimSpace(m.LicenseRef); ref != "" {
		fmt.Fprintf(&b, "  <license id=\"%s\" type=\"text\">%s</license>\n",
			escapeAttr(ref), escapeText(m.LicenseText))
	}
	fmt.Fprintf(&b, "  <localPackage path=\"%s\" obsolete=\"%s\">\n",
		escapeAttr(path), boolYes(m.Obsolete))
	fmt.Fprintf(&b, "    %s\n", typeDetails)
	fmt.Fprintf(&b, "    <revision>%s</revision>\n", revisionXML(m.Revision))
	fmt.Fprintf(&b, "    <display-name>%s</display-name>\n", escapeText(display))
	if deps := dependenciesXML(m.Dependencies); deps != "" {
		fmt.Fprintf(&b, "    %s\n", deps)
	}
	if ref := strings.TrimSpace(m.LicenseRef); ref != "" {
		fmt.Fprintf(&b, "    <uses-license ref=\"%s\"/>\n", escapeAttr(ref))
	}
	b.WriteString("  </localPackage>\n")
	b.WriteString("</ns2:repository>\n")
	return []byte(b.String()), nil
}

// ApplyIndex 用仓库索引补全命名空间与许可文本。
//
// 安装与修复路径都要调用它：type-details 的 xsi:type 前缀需要索引的命名空间声明，
// uses-license 引用的许可文本必须与 ref 一起写进同一个文件。
func (m *Meta) ApplyIndex(idx *repo.Index) {
	if idx == nil {
		return
	}
	m.Namespaces = idx.Namespaces
	if m.LicenseRef != "" && m.LicenseText == "" {
		m.LicenseText = idx.Licenses[m.LicenseRef].Text
	}
}

// FromPackage 由仓库索引条目生成元数据（安装路径使用它，字段最完整）。
func FromPackage(p repo.Package) Meta {
	m := Meta{
		Path:        p.Path,
		DisplayName: p.DisplayName,
		Revision: Revision{
			Major:   p.Revision.Major,
			Minor:   p.Revision.Minor,
			Micro:   p.Revision.Micro,
			Preview: p.Revision.Preview,
		},
		LicenseRef: p.LicenseID(),
		ABI:        p.ABI,
	}
	if p.TypeDetails != nil {
		m.TypeDetails = p.TypeDetails.XML
	}
	for _, dep := range p.Dependencies {
		path, rev := splitDependency(dep.Path)
		if path == "" {
			continue
		}
		item := Dependency{Path: path}
		if rev != "" {
			r := ParseRevision(rev)
			item.MinRevision = &r
		} else if dep.MinRevision != nil {
			r := Revision{
				Major:   dep.MinRevision.Major,
				Minor:   dep.MinRevision.Minor,
				Micro:   dep.MinRevision.Micro,
				Preview: dep.MinRevision.Preview,
			}
			item.MinRevision = &r
		}
		m.Dependencies = append(m.Dependencies, item)
	}
	return m
}

// genericKinds 列出"官方也用 genericDetailsType"的包，离线（无索引）时也能正确补写。
//
// 系统镜像走 sysImgTypeDetails（由本地 source.properties 生成）；
// 其余包（platforms / sources）的 type-details 带专有字段，需要仓库索引，
// 否则官方工具解析不出包类型。
var genericKinds = map[string]bool{
	"emulator":       true,
	"platform-tools": true,
	"build-tools":    true,
	"cmake":          true,
	"ndk":            true,
	"extras":         true,
	"cmdline-tools":  true,
	"other":          true,
}

// FromDir 由包目录里的 source.properties 生成元数据（索引不可用时的离线修复路径）。
//
// 系统镜像的 type-details 完全由本地 source.properties 生成（见 sysImgTypeDetails）：
// 镜像站的索引可能缺字段（尤其是 <abis>），而本地文件是官方安装器写的、与实际镜像一致。
func FromDir(pkgPath, dir string) (Meta, error) {
	props, ok := platform.ReadProperties(filepath.Join(dir, "source.properties"))
	if !ok {
		return Meta{}, domain.ErrDetail(domain.CodePathNotFound,
			"包目录里没有 source.properties，无法还原版本信息", dir)
	}
	kind := kindOf(pkgPath)
	if kind != "system-images" && !genericKinds[kind] {
		return Meta{}, domain.ErrDetail(domain.CodeMirrorUnreachable,
			"离线状态下无法还原 "+pkgPath+" 的包类型信息", kind).
			WithHint("请联网后重试（SDK 页刷新一次索引即可）")
	}
	m := Meta{
		Path:        pkgPath,
		DisplayName: firstNonEmpty(strings.TrimSpace(props["Pkg.Desc"]), pkgPath),
		Revision:    ParseRevision(props["Pkg.Revision"]),
	}
	if kind == "system-images" {
		m.ABI = strings.TrimSpace(props["SystemImage.Abi"])
		m.TypeDetails = sysImgTypeDetails(props)
	}
	for _, raw := range strings.Split(props["Pkg.Dependencies"], ",") {
		path, rev := splitDependency(raw)
		if path == "" {
			continue
		}
		item := Dependency{Path: path}
		if rev != "" {
			r := ParseRevision(rev)
			item.MinRevision = &r
		}
		m.Dependencies = append(m.Dependencies, item)
	}
	return m, nil
}

// sysImgTypeDetails 由本地 source.properties 生成系统镜像的 type-details。
func sysImgTypeDetails(props map[string]string) string {
	var b strings.Builder
	b.WriteString(`<type-details xmlns:xsi="` + xsiNamespace + `" xsi:type="ns14:sysImgDetailsType">`)
	write := func(tag, value string) {
		if v := strings.TrimSpace(value); v != "" {
			fmt.Fprintf(&b, "<%s>%s</%s>", tag, escapeText(v), tag)
		}
	}
	write("api-level", props["AndroidVersion.ApiLevel"])
	write("extension-level", props["AndroidVersion.ExtensionLevel"])
	if boolProp(props["AndroidVersion.IsBaseSdk"]) {
		write("base-extension", "true")
	}
	tagID := firstNonEmpty(strings.TrimSpace(props["SystemImage.TagId"]), "default")
	tagDisplay := firstNonEmpty(strings.TrimSpace(props["SystemImage.TagDisplay"]), tagID)
	fmt.Fprintf(&b, "<tag><id>%s</id><display>%s</display></tag>", escapeText(tagID), escapeText(tagDisplay))
	if vendor := strings.TrimSpace(props["Addon.VendorId"]); vendor != "" {
		display := firstNonEmpty(strings.TrimSpace(props["Addon.VendorDisplay"]), vendor)
		fmt.Fprintf(&b, "<vendor><id>%s</id><display>%s</display></vendor>", escapeText(vendor), escapeText(display))
	}
	if abi := strings.TrimSpace(props["SystemImage.Abi"]); abi != "" {
		fmt.Fprintf(&b, "<abi>%s</abi><abis>%s</abis>", escapeText(abi), escapeText(abi))
	}
	b.WriteString("</type-details>")
	return b.String()
}

// ensureImageAbis 给系统镜像的 type-details 补 <abis>。
//
// 实测（avdmanager 20.0）：系统镜像元数据缺 <abis> 时，sdklib 会以空消息抛异常，
// avdmanager 只打印一行 "null" 并返回 1（创建失败）。而部分镜像站索引使用旧版
// sys-img2 schema，不带 <abis>/<translatedAbis>，所以必须自己补上。
// <translatedAbis> 实测可缺失（去掉后创建仍成功）。
func ensureImageAbis(typeDetails, abi string) string {
	if !strings.Contains(typeDetails, "sysImgDetailsType") || strings.Contains(typeDetails, "<abis>") {
		return typeDetails
	}
	if strings.TrimSpace(abi) == "" {
		if m := abiElemRe.FindStringSubmatch(typeDetails); len(m) > 1 {
			abi = strings.TrimSpace(m[1])
		}
	}
	if abi == "" {
		return typeDetails
	}
	abis := "<abis>" + escapeText(abi) + "</abis>"
	if loc := abiElemRe.FindStringIndex(typeDetails); loc != nil {
		return typeDetails[:loc[1]] + abis + typeDetails[loc[1]:]
	}
	return strings.Replace(typeDetails, "</type-details>",
		"<abi>"+escapeText(abi)+"</abi>"+abis+"</type-details>", 1)
}

// abiElemRe 匹配 <abi>…</abi>（不会误匹配 <abis>）。
var abiElemRe = regexp.MustCompile(`<abi>[^<]*</abi>`)

// ParseRevision 解析版本字符串（source.properties 里可能是 "29.0.13113456 rc1"）。
func ParseRevision(raw string) Revision {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Revision{}
	}
	main := s
	preview := 0
	if i := strings.IndexAny(s, " \t"); i > 0 {
		main, preview = s[:i], digits(s[i:])
	} else if v := strings.IndexAny(strings.ToLower(s), "rc"); v > 0 {
		main, preview = s[:v], digits(s[v:])
	}
	parts := strings.Split(strings.Trim(main, ".-_"), ".")
	var r Revision
	if len(parts) > 0 {
		r.Major = atoi(parts[0])
	}
	if len(parts) > 1 {
		r.Minor = atoi(parts[1])
	}
	if len(parts) > 2 {
		r.Micro = atoi(parts[2])
	}
	r.Preview = preview
	return r
}

// ---------------------------------------------------------------- 内部工具

// rewriteTypeDetails 把索引里 type-details 的 xsi:type 前缀改写成官方固定前缀，
// 并补齐 xsi 命名空间声明；返回无法归一化的前缀（需要额外声明）。
func rewriteTypeDetails(raw string, remote map[string]string) (string, map[string]string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "xmlns:xsi") {
		raw = repo.EnsureXSINamespace(raw)
	}
	locs := typeAttrRe.FindStringSubmatchIndex(raw)
	if locs == nil {
		return raw, nil
	}
	value := raw[locs[2]:locs[3]]
	prefix, local := splitQName(value)
	if prefix == "" {
		return raw, nil
	}
	uri := remote[prefix]
	if uri == "" {
		return raw, nil
	}
	target := prefixByURI[uri]
	if family := familyOf(uri); family != "" {
		if max := schemaFamilyMax[family]; max != "" {
			target = firstNonEmpty(prefixByURI[max], target)
		}
	}
	if target == "" || target == prefix {
		return raw, nil
	}
	rewritten := raw[:locs[2]] + target + ":" + local + raw[locs[3]:]
	return rewritten, nil
}

// typeAttrRe 匹配 xsi:type 属性的值。
var typeAttrRe = regexp.MustCompile(`xsi:type="([^"]+)"`)

// familyOf 去掉命名空间 URI 结尾的版本段（…/generic/02 → …/generic）。
func familyOf(uri string) string {
	i := strings.LastIndex(uri, "/")
	if i <= 0 {
		return ""
	}
	if _, err := strconv.Atoi(uri[i+1:]); err != nil {
		return ""
	}
	return uri[:i]
}

func splitQName(v string) (prefix, local string) {
	if i := strings.IndexByte(v, ':'); i > 0 {
		return v[:i], v[i+1:]
	}
	return "", v
}

// splitDependency 拆解 "emulator#35.4.9" 形式的依赖声明。
func splitDependency(raw string) (path, revision string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
	}
	return raw, ""
}

// revisionXML 生成 <revision> 内容；0 值段按 sdklib 的展示规则省略。
func revisionXML(r Revision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<major>%d</major>", r.Major)
	if r.Minor > 0 {
		fmt.Fprintf(&b, "<minor>%d</minor>", r.Minor)
	}
	if r.Micro > 0 {
		fmt.Fprintf(&b, "<micro>%d</micro>", r.Micro)
	}
	if r.Preview > 0 {
		fmt.Fprintf(&b, "<preview>%d</preview>", r.Preview)
	}
	return b.String()
}

func dependenciesXML(deps []Dependency) string {
	if len(deps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<dependencies>")
	for _, d := range deps {
		path := strings.TrimSpace(d.Path)
		if path == "" {
			continue
		}
		fmt.Fprintf(&b, "<dependency path=\"%s\">", escapeAttr(path))
		if d.MinRevision != nil {
			fmt.Fprintf(&b, "<min-revision>%s</min-revision>", revisionXML(*d.MinRevision))
		}
		b.WriteString("</dependency>")
	}
	b.WriteString("</dependencies>")
	return b.String()
}

// kindOf 由包路径推导大类（与 query / repo 的分组保持一致）。
func kindOf(pkgPath string) string {
	switch {
	case strings.HasPrefix(pkgPath, "cmdline-tools"):
		return "cmdline-tools"
	case pkgPath == "platform-tools":
		return "platform-tools"
	case pkgPath == "emulator":
		return "emulator"
	case strings.HasPrefix(pkgPath, "platforms;"):
		return "platforms"
	case strings.HasPrefix(pkgPath, "build-tools;"):
		return "build-tools"
	case strings.HasPrefix(pkgPath, "system-images;"):
		return "system-images"
	case strings.HasPrefix(pkgPath, "ndk;"):
		return "ndk"
	case strings.HasPrefix(pkgPath, "cmake;"):
		return "cmake"
	case strings.HasPrefix(pkgPath, "sources;"):
		return "sources"
	case strings.HasPrefix(pkgPath, "extras;"):
		return "extras"
	default:
		return "other"
	}
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// escapeAttr / escapeText 按 XML 规则转义（xml.EscapeText 对属性与文本都安全）。
func escapeAttr(s string) string { return xmlEscape(s) }

func escapeText(s string) string { return xmlEscape(s) }

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func boolYes(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// boolProp 宽松解析 source.properties 里的布尔值。
func boolProp(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "1":
		return true
	default:
		return false
	}
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// digits 取出字符串里的数字（"rc01" → 1）。
func digits(s string) int {
	start := -1
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			if start < 0 {
				start = i
			}
		case start >= 0:
			return atoi(s[start:i])
		}
	}
	if start >= 0 {
		return atoi(s[start:])
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
