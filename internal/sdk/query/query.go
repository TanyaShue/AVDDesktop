// Package query 扫描本地已安装的 SDK 包。
//
// 依据实测（docs/RESEARCH-NOTES.md §4.2）：每个已安装包目录下有 source.properties，
// 其中 Pkg.Revision / Pkg.Path 是权威版本信息来源。
package query

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// Scanner 扫描一个 SDK 根目录。
type Scanner struct {
	SdkRoot string
}

// NewScanner 创建扫描器。
func NewScanner(sdkRoot string) *Scanner { return &Scanner{SdkRoot: sdkRoot} }

// Installed 返回所有已安装包（按 path 排序）。
func (s *Scanner) Installed() []domain.SdkPackage {
	if s.SdkRoot == "" || !platform.DirExists(s.SdkRoot) {
		return nil
	}
	var out []domain.SdkPackage

	// 1) 单例包（目录即包）
	singletons := []struct{ path, dir string }{
		{"platform-tools", filepath.Join(s.SdkRoot, "platform-tools")},
		{"emulator", filepath.Join(s.SdkRoot, "emulator")},
	}
	for _, it := range singletons {
		if props, ok := readProps(it.dir); ok {
			out = append(out, toPackage(it.path, props, it.dir))
		}
	}

	// 2) cmdline-tools;<版本>（latest 是目录名，不是真实版本）
	ctRoot := filepath.Join(s.SdkRoot, "cmdline-tools")
	for _, name := range listDirs(ctRoot) {
		dir := filepath.Join(ctRoot, name)
		if props, ok := readProps(dir); ok {
			pkgPath := "cmdline-tools;" + name
			if name == "latest" {
				// 目录是 latest，但真实版本在 source.properties 里
				pkgPath = "cmdline-tools;latest"
			}
			out = append(out, toPackage(pkgPath, props, dir))
		}
	}

	// 3) platforms;android-XX
	out = append(out, scanVersioned(filepath.Join(s.SdkRoot, "platforms"), "platforms")...)
	// 4) build-tools;<ver>
	out = append(out, scanVersioned(filepath.Join(s.SdkRoot, "build-tools"), "build-tools")...)
	// 5) ndk;<ver>
	out = append(out, scanVersioned(filepath.Join(s.SdkRoot, "ndk"), "ndk")...)
	// 6) system-images;<api>;<tag>;<abi>
	out = append(out, scanSystemImages(filepath.Join(s.SdkRoot, "system-images"))...)
	// 7) sources;android-XX（可选）
	out = append(out, scanVersioned(filepath.Join(s.SdkRoot, "sources"), "sources")...)

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Find 返回指定包路径的已安装信息。
func (s *Scanner) Find(pkgPath string) (domain.SdkPackage, bool) {
	for _, p := range s.Installed() {
		if p.Path == pkgPath {
			return p, true
		}
	}
	return domain.SdkPackage{}, false
}

// SystemImages 返回已安装的系统镜像。
func (s *Scanner) SystemImages() []domain.SystemImage {
	var out []domain.SystemImage
	for _, p := range s.Installed() {
		if p.Kind != "system-images" {
			continue
		}
		out = append(out, ImageFromPackage(p))
	}
	return out
}

// ImageFromPackage 把已安装的系统镜像包转换为视图模型。
func ImageFromPackage(p domain.SdkPackage) domain.SystemImage {
	parts := strings.Split(p.Path, ";") // system-images;android-36.1;google_apis_playstore;x86_64
	img := domain.SystemImage{
		Path:       p.Path,
		Revision:   p.InstalledRevision,
		SizeBytes:  p.SizeBytes,
		Installed:  true,
		TagDisplay: p.DisplayName,
	}
	if len(parts) >= 2 {
		img.APILevel = strings.TrimPrefix(parts[1], "android-")
	}
	if len(parts) >= 3 {
		img.TagID = parts[2]
		img.IsPlaystore = strings.Contains(parts[2], "playstore")
	}
	if len(parts) >= 4 {
		img.ABI = parts[3]
	}
	return img
}

func scanVersioned(root, prefix string) []domain.SdkPackage {
	var out []domain.SdkPackage
	for _, name := range listDirs(root) {
		dir := filepath.Join(root, name)
		props, ok := readProps(dir)
		if !ok {
			continue
		}
		pkgPath := prefix + ";" + name
		out = append(out, toPackage(pkgPath, props, dir))
	}
	return out
}

func scanSystemImages(root string) []domain.SdkPackage {
	var out []domain.SdkPackage
	apis := listDirs(root)
	for _, api := range apis {
		tags := listDirs(filepath.Join(root, api))
		for _, tag := range tags {
			abis := listDirs(filepath.Join(root, api, tag))
			for _, abi := range abis {
				dir := filepath.Join(root, api, tag, abi)
				props, ok := readProps(dir)
				if !ok {
					continue
				}
				pkgPath := strings.Join([]string{"system-images", api, tag, abi}, ";")
				pkg := toPackage(pkgPath, props, dir)
				pkg.DisplayName = firstNonEmpty(props["SystemImage.TagDisplay"], tag)
				out = append(out, pkg)
			}
		}
	}
	return out
}

func toPackage(pkgPath string, props map[string]string, dir string) domain.SdkPackage {
	rev := firstNonEmpty(props["Pkg.Revision"], "0")
	return domain.SdkPackage{
		Path:              pkgPath,
		DisplayName:       firstNonEmpty(props["Pkg.Desc"], filepath.Base(dir)),
		Kind:              kindOf(pkgPath),
		Revision:          rev,
		InstalledRevision: rev,
		Installed:         true,
		SizeBytes:         platform.DirSize(dir),
		URL:               dir,
		Channel:           props["Pkg.Channel"],
	}
}

// readProps 读取目录下的 source.properties。
func readProps(dir string) (map[string]string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "source.properties"))
	if err != nil {
		return nil, false
	}
	props := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		props[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
	}
	return props, true
}

func listDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

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
	case strings.HasPrefix(pkgPath, "sources;"):
		return "sources"
	default:
		return "other"
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
