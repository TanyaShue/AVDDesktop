package platform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
)

// FileExists 判断普通文件是否存在。
func FileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

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

// NowMs 返回当前 Unix 毫秒。
func NowMs() int64 { return time.Now().UnixMilli() }
