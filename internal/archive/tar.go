package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/domain"
)

// ExtractTarGz 把 .tar.gz 解压到 destDir（防目录穿越）。
//
// 与 ExtractZip 相同：归档若只有一个顶层目录，会自动剥离该层，便于把
// Temurin 的 jdk-21.x/ 内容直接安装到软件自己的 JDK 目录。
func ExtractTarGz(ctx context.Context, archivePath, destDir string) error {
	prefix, err := tarRootPrefix(ctx, archivePath)
	if err != nil {
		return err
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法打开压缩包", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法读取 gzip 压缩包", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return domain.Err(domain.CodeJobCanceled, "解压已取消")
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return domain.Wrap(domain.CodeArchiveFailed, "读取压缩包条目失败", err)
		}

		if hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeXHeader || hdr.Name == "pax_global_header" {
			continue
		}
		name := stripPrefix(hdr.Name, prefix)
		if strings.TrimSpace(name) == "" {
			continue
		}
		target, err := safeJoin(destDir, name)
		if err != nil {
			return err
		}

		mode := hdr.FileInfo().Mode()
		switch hdr.Typeflag {
		case tar.TypeDir:
			perm := mode.Perm()
			if perm == 0 {
				perm = 0o755
			}
			if err := os.MkdirAll(target, perm); err != nil {
				return domain.Wrap(domain.CodeArchiveFailed, "创建目录失败: "+target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return domain.Wrap(domain.CodeArchiveFailed, "创建目录失败: "+filepath.Dir(target), err)
			}
			if err := writeTarFile(ctx, target, tr, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return domain.Wrap(domain.CodeArchiveFailed, "创建目录失败: "+filepath.Dir(target), err)
			}
			if err := createSafeSymlink(destDir, target, hdr.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return domain.Wrap(domain.CodeArchiveFailed, "创建目录失败: "+filepath.Dir(target), err)
			}
			linkName := stripPrefix(hdr.Linkname, prefix)
			source, err := safeJoin(destDir, linkName)
			if err != nil {
				return err
			}
			if err := os.Link(source, target); err != nil {
				return domain.Wrap(domain.CodeArchiveFailed, "创建硬链接失败: "+target, err)
			}
		default:
			// PAX 头、设备文件等元数据不解压；JDK 归档只需要普通文件、目录与链接。
		}
	}
	return nil
}

func tarRootPrefix(ctx context.Context, archivePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", domain.Wrap(domain.CodeArchiveFailed, "无法打开压缩包", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", domain.Wrap(domain.CodeArchiveFailed, "无法读取 gzip 压缩包", err)
	}
	defer func() { _ = gz.Close() }()

	var names []string
	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return "", domain.Err(domain.CodeJobCanceled, "解压已取消")
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", domain.Wrap(domain.CodeArchiveFailed, "读取压缩包条目失败", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeXHeader || hdr.Name == "pax_global_header" {
			continue
		}
		name := hdr.Name
		if hdr.Typeflag == tar.TypeDir && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		names = append(names, name)
	}
	return singleRootPrefix(names), nil
}

func writeTarFile(ctx context.Context, target string, r io.Reader, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "无法写入: "+target, err)
	}
	if _, err := copyWithContext(ctx, out, r); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// createSafeSymlink 只在链接目标仍位于 destDir 内时创建符号链接。
func createSafeSymlink(destDir, target, linkName string) error {
	if filepath.IsAbs(linkName) || strings.HasPrefix(linkName, "/") || strings.HasPrefix(linkName, `\`) {
		return domain.ErrDetail(domain.CodeArchiveFailed,
			"压缩包包含非法符号链接", linkName)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), filepath.FromSlash(linkName)))
	rel, err := filepath.Rel(destDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return domain.ErrDetail(domain.CodeArchiveFailed,
			"压缩包符号链接越界", linkName)
	}
	if err := os.Symlink(linkName, target); err != nil {
		return domain.Wrap(domain.CodeArchiveFailed, "创建符号链接失败: "+target, err)
	}
	return nil
}
