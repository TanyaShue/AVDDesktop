// Package archive 提供安全的压缩包解压（防目录穿越）与校验工具。
//
// 安全约束（见 ARCHITECTURE.md §12）：
//   - 拒绝绝对路径、`..` 越界、以及指向目标目录之外的符号链接
//   - 解压到临时目录后再原子 rename，避免半成品 SDK 目录
package archive

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/domain"
)

// Progress 是解压进度回调。
type Progress func(done, total int64, current string)

// ExtractZip 把 zip 解压到 destDir。
//
// 自动处理 Android SDK 归档的“包装目录”约定：
// 官方包（platform-tools / cmdline-tools / emulator / system-images …）的 zip 里都套了一层
// 与包同名的顶层目录（如 platform-tools/adb.exe），而 sdkmanager 会把它剥掉。
// 若 zip 中所有条目同属一个顶层目录，本函数会按同样的规则剥离；否则原样解压。
//
// onProgress 可为 nil。返回错误即中止（用于响应 ctx 取消）。
func ExtractZip(zipPath, destDir string, onProgress func(done, total int64, current string) error) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法打开压缩包", err)
	}
	defer func() { _ = r.Close() }()

	prefix := SingleRootPrefix(r.File)

	var total int64
	for _, f := range r.File {
		total += int64(f.UncompressedSize64)
	}

	var done int64
	for _, f := range r.File {
		name := stripPrefix(f.Name, prefix)
		if name == "" {
			continue // 包装目录本身
		}
		target, err := safeJoin(destDir, name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return domain.Wrap(domain.CodeArchiveFailed, "创建目录失败: "+target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return domain.Wrap(domain.CodeArchiveFailed, "创建目录失败: "+filepath.Dir(target), err)
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
		done += int64(f.UncompressedSize64)
		if onProgress != nil {
			if err := onProgress(done, total, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// SingleRootPrefix 返回 zip 中所有条目共同的顶层目录前缀（形如 "platform-tools/"）。
//
// 只要存在任何顶层文件、或顶层目录不唯一，就返回空串（表示不应剥离）。
func SingleRootPrefix(files []*zip.File) string {
	prefix := ""
	for _, f := range files {
		name := filepath.ToSlash(strings.TrimPrefix(f.Name, "./"))
		if strings.TrimSpace(name) == "" {
			continue
		}
		i := strings.IndexByte(name, '/')
		if i <= 0 {
			return "" // 有顶层文件 → 不能剥离
		}
		top := name[:i+1]
		// 不把 "./" 或 "../" 当作可剥离的包装目录：保留原条目名，
		// 让 safeJoin 去做越界校验（不能把越界路径“洗白”成正常路径）。
		if strings.TrimSuffix(top, "/") == "." || strings.TrimSuffix(top, "/") == ".." {
			return ""
		}
		if prefix == "" {
			prefix = top
			continue
		}
		if top != prefix {
			return "" // 多个顶层目录 → 不能剥离
		}
	}
	return prefix
}

func stripPrefix(name, prefix string) string {
	if prefix == "" {
		return name
	}
	normalized := filepath.ToSlash(strings.TrimPrefix(name, "./"))
	if normalized == strings.TrimSuffix(prefix, "/") {
		return "" // 包装目录自身
	}
	if !strings.HasPrefix(normalized, prefix) {
		return name
	}
	return strings.TrimPrefix(normalized, prefix)
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法读取压缩包条目: "+f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	// 保留可执行位（Linux/macOS 的 SDK 工具需要 +x）
	if (mode&0o111) == 0 && strings.HasSuffix(f.Name, "/") {
		mode = 0o755
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法写入: "+target, err)
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return domain.Wrap(domain.CodeArchiveFailed, "解压写入失败: "+target, err)
	}
	return out.Close()
}

// safeJoin 校验 zip 条目路径不会逃出 destDir。
func safeJoin(destDir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", domain.ErrDetail(domain.CodeArchiveFailed,
			"压缩包包含非法路径，已中止解压", name)
	}
	target := filepath.Join(destDir, clean)
	rel, err := filepath.Rel(destDir, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", domain.ErrDetail(domain.CodeArchiveFailed,
			"压缩包条目越界，已中止解压", name)
	}
	return target, nil
}

// ListZip 返回压缩包内的条目名（用于探测目录结构，例如 cmdline-tools 的内层目录）。
func ListZip(zipPath string) ([]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, domain.Wrap(domain.CodeArchiveFailed, "无法打开压缩包", err)
	}
	defer func() { _ = r.Close() }()
	out := make([]string, 0, len(r.File))
	for _, f := range r.File {
		out = append(out, f.Name)
	}
	return out, nil
}

// TopLevelDirs 返回压缩包内的一级目录名（去重）。
func TopLevelDirs(zipPath string) ([]string, error) {
	names, err := ListZip(zipPath)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		n = filepath.ToSlash(n)
		if i := strings.IndexByte(n, '/'); i > 0 {
			top := n[:i]
			if !seen[top] {
				seen[top] = true
				out = append(out, top)
			}
		}
	}
	return out, nil
}

// SHA1File 计算文件 SHA-1（小写十六进制）。
func SHA1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySHA1 校验文件哈希；expected 为空表示跳过校验。
func VerifySHA1(path, expected string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}
	got, err := SHA1File(path)
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法计算文件校验值", err)
	}
	if !strings.EqualFold(got, expected) {
		return domain.ErrDetail(domain.CodeChecksumMismatch, "下载内容校验失败（可能被篡改或下载不完整）",
			fmt.Sprintf("期望 sha1=%s 实际 sha1=%s\n文件: %s", expected, got, path))
	}
	return nil
}

// RenameAtomic 在同卷内原子替换目录；跨卷时退化为"先删后移"。
func RenameAtomic(src, dst string) error {
	if err := os.RemoveAll(dst + ".old"); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, dst+".old"); err != nil {
			return domain.Wrap(domain.CodeFileInUse, "目标目录被占用，无法替换（可能有模拟器正在运行）", err).
				WithHint("请先停止使用该目录的模拟器实例后重试")
		}
	}
	if err := os.Rename(src, dst); err != nil {
		// 回滚
		if _, statErr := os.Stat(dst + ".old"); statErr == nil {
			_ = os.Rename(dst+".old", dst)
		}
		return domain.Wrap(domain.CodeArchiveFailed, "替换目标目录失败", err)
	}
	_ = os.RemoveAll(dst + ".old")
	return nil
}
