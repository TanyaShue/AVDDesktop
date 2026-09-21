package sdk

import (
	"runtime"
	"sort"
	"strings"

	"AVDDesktop/internal/domain"
)

// HostABI 返回当前宿主架构对应的 Android ABI 名称。
//
// 非宿主 ABI 的镜像也能创建 AVD，但运行需要翻译层，因此界面上把宿主 ABI 排在前面。
func HostABI() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "arm64-v8a"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

// SortImagesForHost 就地排序系统镜像：已安装优先 → 宿主 ABI 优先 → API 从新到旧 → tag → ABI。
func SortImagesForHost(images []domain.SystemImage) {
	host := HostABI()
	sort.SliceStable(images, func(i, j int) bool {
		a, b := images[i], images[j]
		if a.Installed != b.Installed {
			return a.Installed
		}
		if (a.ABI == host) != (b.ABI == host) {
			return a.ABI == host
		}
		if cmp := compareAPI(a.API, b.API); cmp != 0 {
			return cmp > 0
		}
		if a.Tag != b.Tag {
			return a.Tag < b.Tag
		}
		return a.ABI < b.ABI
	})
}

// compareAPI 比较 API 版本号（形如 "34" / "36.1"），返回 -1/0/1。
func compareAPI(a, b string) int {
	pa, pb := apiParts(a), apiParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va > vb {
				return 1
			}
			return -1
		}
	}
	return 0
}

func apiParts(v string) []int {
	fields := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "android-"), ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		out = append(out, atoiDigits(f))
	}
	return out
}

func atoiDigits(v string) int {
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
