package archive

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/domain"
)

// ExtractZip 把 zip 解压到 destDir（防目录穿越）。
//
// 官方 Android / Temurin 归档都套了一层与包同名的顶层目录（如 cmdline-tools/bin/…、
// jdk-21/...），而安装后的目录结构不含这一层，因此这里会自动剥离唯一的顶层目录。
func ExtractZip(ctx context.Context, zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法打开压缩包", err)
	}
	defer func() { _ = r.Close() }()

	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		name := f.Name
		if f.FileInfo().IsDir() && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		names = append(names, name)
	}
	prefix := singleRootPrefix(names)
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return domain.Err(domain.CodeJobCanceled, "解压已取消")
		}
		name := stripPrefix(f.Name, prefix)
		if strings.TrimSpace(name) == "" {
			continue // 包装目录自身
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
		if err := extractZipFile(ctx, f, target); err != nil {
			return err
		}
	}
	return nil
}

// singleRootPrefix 返回所有条目共同的顶层目录前缀（形如 "cmdline-tools/"）。
//
// 存在任何顶层文件、或顶层目录不唯一时返回空串（表示不剥离）。
func singleRootPrefix(names []string) string {
	prefix := ""
	for _, raw := range names {
		name := normalizeArchiveName(raw)
		if name == "" {
			continue
		}
		i := strings.IndexByte(name, '/')
		if i <= 0 {
			return "" // 有顶层文件 → 不能剥离
		}
		top := name[:i+1]
		if strings.TrimSuffix(top, "/") == "." || strings.TrimSuffix(top, "/") == ".." {
			return "" // 交给 safeJoin 做越界校验，不能把越界路径“洗白”
		}
		if prefix == "" {
			prefix = top
			continue
		}
		if top != prefix {
			return ""
		}
	}
	return prefix
}

func normalizeArchiveName(name string) string {
	name = filepath.ToSlash(strings.TrimPrefix(name, "./"))
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	return name
}

func stripPrefix(name, prefix string) string {
	normalized := normalizeArchiveName(name)
	if prefix == "" {
		return normalized
	}
	if normalized == strings.TrimSuffix(prefix, "/") {
		return ""
	}
	if !strings.HasPrefix(normalized, prefix) {
		return normalized
	}
	return strings.TrimPrefix(normalized, prefix)
}

func extractZipFile(ctx context.Context, f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法读取压缩包条目: "+f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	// 保留可执行位：macOS / Linux 下的 sdkmanager、emulator、adb 需要 +x
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法写入: "+target, err)
	}
	if _, err := copyWithContext(ctx, out, rc); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// copyWithContext 拷贝数据并在过程中响应取消。
//
// 条目级别的取消检查不够：单个条目可能有数百 MB（如 JDK 的 lib/modules），
// 必须能在写入途中立刻中断，否则"取消解压"要等整个文件写完才生效。
func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 256*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, domain.Err(domain.CodeJobCanceled, "解压已取消")
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return written, domain.Wrap(domain.CodeArchiveFailed, "解压写入失败", writeErr)
			}
			written += int64(n)
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, domain.Wrap(domain.CodeArchiveFailed, "解压写入失败", readErr)
		}
	}
}

// safeJoin 校验归档条目路径不会逃出 destDir。
//
// 条目必须是相对路径：绝对路径、以 "/" 开头的路径、UNC 路径一律拒绝
// （Windows 上 "/abs" 不算 filepath.IsAbs，因此显式判一次，保证各平台行为一致）。
func safeJoin(destDir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") ||
		strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
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
