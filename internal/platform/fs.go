package platform

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"AVDDesktop/internal/domain"
)

// FileExists 判断普通文件是否存在。
func FileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func fileExists(path string) bool { return FileExists(path) }

// DirExists 判断目录是否存在。
func DirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// IsWritable 通过实际创建临时文件判断目录可写（比权限位可靠，尤其 Windows）。
func IsWritable(dir string) bool {
	if dir == "" {
		return false
	}
	if !DirExists(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		return IsWritable(parent)
	}
	f, err := os.CreateTemp(dir, ".avddesktop-wtest-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// EnsureDir 创建目录（幂等）。
func EnsureDir(path string) error {
	if path == "" {
		return errors.New("空目录路径")
	}
	return os.MkdirAll(path, 0o755)
}

// DirSize 递归统计目录大小（用于设备卡片显示占用空间）。
//
// 大目录可能较慢，调用方应放入后台任务或使用 DirSizeCached。
func DirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 忽略无法访问的项
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// 目录大小缓存：AVD 与系统镜像目录可能上百 MB 到数 GB，
// 每次刷新都重新遍历会让首页自检耗时数秒。
type dirSizeEntry struct {
	size     int64
	computed time.Time
}

var (
	dirSizeMu    sync.Mutex
	dirSizeCache = map[string]dirSizeEntry{}
)

// dirSizeTTL 是目录大小缓存的有效期。
const dirSizeTTL = 10 * time.Minute

// DirSizeCached 返回带缓存的目录大小。
//
// 缓存未命中时会真的遍历目录（可能较慢），因此敏感路径请用 ListOptions 跳过。
func DirSizeCached(path string) int64 {
	if path == "" {
		return 0
	}
	dirSizeMu.Lock()
	if entry, ok := dirSizeCache[path]; ok && time.Since(entry.computed) < dirSizeTTL {
		dirSizeMu.Unlock()
		return entry.size
	}
	dirSizeMu.Unlock()

	size := DirSize(path)

	dirSizeMu.Lock()
	dirSizeCache[path] = dirSizeEntry{size: size, computed: time.Now()}
	dirSizeMu.Unlock()
	return size
}

// InvalidateDirSize 在目录内容变化（安装/删除/启动）后清除缓存。
func InvalidateDirSize(path string) {
	dirSizeMu.Lock()
	if path == "" {
		dirSizeCache = map[string]dirSizeEntry{}
	} else {
		delete(dirSizeCache, path)
		// 同时清除其子路径的缓存（例如删除某个包目录）
		prefix := filepath.Clean(path) + string(filepath.Separator)
		for key := range dirSizeCache {
			if strings.HasPrefix(key, prefix) {
				delete(dirSizeCache, key)
			}
		}
	}
	dirSizeMu.Unlock()
}

// DiskSpace 返回指定路径所在卷的总量与剩余空间（GB）。
func DiskSpace(path string) domain.DiskInfo {
	info := domain.DiskInfo{Path: path}
	if path == "" {
		return info
	}
	for probe := path; ; probe = filepath.Dir(probe) {
		if DirExists(probe) || FileExists(probe) {
			path = probe
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
	}
	total, free, err := diskUsage(path)
	if err != nil {
		return info
	}
	info.TotalGB = int64(total / (1 << 30))
	info.FreeGB = int64(free / (1 << 30))
	info.Sufficient = info.FreeGB >= 12
	return info
}

// WriteFileAtomic 原子写文件（写临时文件后 rename），避免半截配置。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if perm != 0 {
		_ = os.Chmod(tmpName, perm)
	}
	return os.Rename(tmpName, path)
}

// WriteJSON 原子写入 JSON（带缩进，便于人工排查）。
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(data, '\n'), 0o644)
}

// ReadJSON 读取 JSON；文件不存在时返回 os.ErrNotExist。
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// CopyFile 复制文件并保留大小校验。
func CopyFile(src, dst string) (int64, error) {
	if err := EnsureDir(filepath.Dir(dst)); err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if err != nil {
		_ = out.Close()
		return n, err
	}
	return n, out.Close()
}

// CopyTree 递归复制目录（用于 AVD 克隆/导出）。
func CopyTree(src, dst string, skip func(rel string, isDir bool) bool) error {
	src = filepath.Clean(src)
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return EnsureDir(dst)
		}
		if skip != nil && skip(rel, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return EnsureDir(target)
		}
		_, err = CopyFile(path, target)
		return err
	})
}

// HumanSize 把字节数格式化为人类可读（用于日志与错误信息）。
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return humanItoa(n) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n)
	for _, u := range units {
		v /= unit
		if v < unit {
			return humanFloat(v) + " " + u
		}
	}
	return humanFloat(v) + " PB"
}

func humanItoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func humanFloat(v float64) string {
	// 保留 1 位小数，去掉多余的 .0
	neg := v < 0
	if neg {
		v = -v
	}
	rounded := float64(int64(v*10+0.5)) / 10
	whole := int64(rounded)
	frac := int64(rounded*10) % 10
	s := humanItoa(whole)
	if frac != 0 {
		s += "." + string(rune('0'+frac))
	}
	if neg {
		s = "-" + s
	}
	return s
}

// SplitLines 按行切分并去掉空行。
func SplitLines(s string) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// VersionAtLeast 比较形如 "17.0.6" / "37.2.10" 的版本号，判断 a >= b。
func VersionAtLeast(a, b string) bool {
	am, an, ap := parseVersion(a)
	bm, bn, bp := parseVersion(b)
	if am != bm {
		return am > bm
	}
	if an != bn {
		return an > bn
	}
	return ap >= bp
}

// CompareVersions 返回 -1/0/1。
func CompareVersions(a, b string) int {
	am, an, ap := parseVersion(a)
	bm, bn, bp := parseVersion(b)
	if am != bm {
		if am > bm {
			return 1
		}
		return -1
	}
	if an != bn {
		if an > bn {
			return 1
		}
		return -1
	}
	if ap != bp {
		if ap > bp {
			return 1
		}
		return -1
	}
	return 0
}

func parseVersion(v string) (int, int, int) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, 0, 0
	}
	// 只取前导数字段
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' || r == '_' || r == '+' || r == ' ' })
	nums := make([]int, 0, 3)
	for _, p := range parts {
		n := 0
		got := false
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
			got = true
		}
		if !got {
			break
		}
		nums = append(nums, n)
		if len(nums) == 3 {
			break
		}
	}
	for len(nums) < 3 {
		nums = append(nums, 0)
	}
	return nums[0], nums[1], nums[2]
}

// NowMs 返回当前 Unix 毫秒。
func NowMs() int64 { return time.Now().UnixMilli() }

// InboxTempDir 返回应用级临时目录（用于下载与解压）。
func InboxTempDir(base, sub string) string {
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, sub)
	_ = EnsureDir(dir)
	return dir
}

// SameVolume 判断两个路径是否在同一磁盘卷（决定 rename 是否可原子完成）。
func SameVolume(a, b string) bool {
	va, err1 := volumeOf(a)
	vb, err2 := volumeOf(b)
	return err1 == nil && err2 == nil && strings.EqualFold(va, vb)
}

func volumeOf(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for probe := abs; ; probe = filepath.Dir(probe) {
		if DirExists(probe) {
			return filepath.VolumeName(probe) + string(filepath.Separator), nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return filepath.VolumeName(abs) + string(filepath.Separator), nil
		}
	}
}
