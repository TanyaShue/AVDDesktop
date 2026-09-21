package store

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/archive"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// snapshotSkipDirs 是导出时默认跳过的运行时大文件（可用 IncludeSnapshots 恢复）。
var snapshotSkipDirs = []string{"snapshots", "tmp"}

// ExportProgress 是导出进度回调（done/total 字节，current 为当前条目）。
type ExportProgress func(done, total int64, name string, line string)

// Export 把 AVD 打包为 zip。
//
// 包内结构（便于直接恢复）：
//
//	<name>.ini
//	<name>.avd/config.ini
//	<name>.avd/userdata-qemu.img
//	...
//
// 默认跳过 snapshots/ 与 tmp/（体积大且与宿主强相关）；IncludeSnapshots=true 时全部打包。
func (s *Store) Export(ctx context.Context, name, targetZip string, includeSnapshots bool, onProgress ExportProgress) (int64, error) {
	layout := s.Resolve(name)
	if !layout.Exists {
		return 0, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	if err := platform.EnsureDir(filepath.Dir(targetZip)); err != nil {
		return 0, domain.Wrap(domain.CodePermissionDenied, "无法创建导出目录", err)
	}

	// 先统计总大小，便于进度展示
	var total int64
	_ = filepath.Walk(layout.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !includeSnapshots && isSkipped(path, layout.Dir) {
			return nil
		}
		total += info.Size()
		return nil
	})
	if platform.FileExists(layout.IniPath) {
		if info, err := os.Stat(layout.IniPath); err == nil {
			total += info.Size()
		}
	}

	out, err := os.Create(targetZip)
	if err != nil {
		return 0, domain.Wrap(domain.CodePermissionDenied, "无法创建导出文件", err)
	}
	defer func() { _ = out.Close() }()

	zw := zip.NewWriter(out)
	var done int64

	// 1) .ini 文件
	if platform.FileExists(layout.IniPath) {
		if err := addFileToZip(zw, layout.IniPath, filepath.Base(layout.IniPath)); err != nil {
			_ = zw.Close()
			return 0, err
		}
		if info, statErr := os.Stat(layout.IniPath); statErr == nil {
			done += info.Size()
		}
	}

	// 2) .avd 目录
	baseDir := filepath.Base(layout.Dir)
	err = filepath.Walk(layout.Dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return domain.Err(domain.CodeJobCanceled, "操作已取消")
		}
		if info.IsDir() {
			return nil
		}
		if !includeSnapshots && isSkipped(path, layout.Dir) {
			return nil
		}
		rel, relErr := filepath.Rel(layout.Dir, path)
		if relErr != nil {
			return relErr
		}
		zipName := filepath.ToSlash(filepath.Join(baseDir, rel))
		if err := addFileToZip(zw, path, zipName); err != nil {
			return err
		}
		done += info.Size()
		if onProgress != nil {
			onProgress(done, total, rel, fmt.Sprintf("打包 %s（%s）", rel, platform.HumanSize(info.Size())))
		}
		return nil
	})
	if err != nil {
		_ = zw.Close()
		_ = os.Remove(targetZip)
		return 0, err
	}

	if err := zw.Close(); err != nil {
		_ = os.Remove(targetZip)
		return 0, domain.Wrap(domain.CodeUnknown, "写入 zip 失败", err)
	}
	if onProgress != nil {
		onProgress(done, total, "", fmt.Sprintf("导出完成（%s）", platform.HumanSize(done)))
	}
	return done, nil
}

// Import 从 zip 包恢复 AVD。
//
// 处理要点：
//   - 包内设备名与目标名可能不同 → 解压后统一改写目录名、.ini 与 config.ini 中的身份字段
//   - 同名设备已存在时自动追加 _2/_3 后缀（不覆盖用户数据）
//   - 解压过程复用 archive.ExtractZip 的目录穿越防护
func (s *Store) Import(ctx context.Context, zipPath, targetName string, onProgress func(done, total int64, current string) error) (string, error) {
	entries, err := archive.ListZip(zipPath)
	if err != nil {
		return "", err
	}
	innerName := detectAvdName(entries)
	if innerName == "" {
		return "", domain.ErrDetail(domain.CodeInvalidArgument,
			"这个 zip 不像是 AVDDesktop 导出的设备包",
			"包内应包含 <name>.ini 与 <name>.avd/ 目录")
	}

	name := strings.TrimSpace(targetName)
	if name == "" {
		name = innerName
	}
	if v := s.ValidateName(name); !v.Valid {
		if v.Suggest != "" {
			name = v.Suggest
		} else {
			return "", domain.Err(domain.CodeAvdNameInvalid, v.Reason)
		}
	}

	// 解压到临时目录，避免半成品污染 AVD 主目录
	staging := filepath.Join(s.AvdHome, ".import-"+name+".tmp")
	_ = os.RemoveAll(staging)
	if err := platform.EnsureDir(staging); err != nil {
		return "", domain.Wrap(domain.CodePermissionDenied, "无法创建导入临时目录", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := archive.ExtractZip(zipPath, staging, onProgress); err != nil {
		return "", err
	}

	srcDir := filepath.Join(staging, innerName+".avd")
	srcIni := filepath.Join(staging, innerName+".ini")
	if !platform.DirExists(srcDir) {
		// 兼容不带包装目录的包
		if platform.FileExists(filepath.Join(staging, "config.ini")) {
			srcDir = staging
		} else {
			return "", domain.ErrDetail(domain.CodeArchiveFailed,
				"设备包内容不完整（缺少 .avd 目录）", zipPath)
		}
	}

	dstLayout := s.Resolve(name)
	if platform.DirExists(dstLayout.Dir) {
		return "", domain.Err(domain.CodeAvdExists, "已存在同名设备: "+name)
	}
	if err := os.Rename(srcDir, dstLayout.Dir); err != nil {
		return "", domain.Wrap(domain.CodePermissionDenied, "无法移动导入的设备目录", err)
	}

	// 修正 config.ini 身份字段
	config, err := ReadIni(dstLayout.ConfigIni)
	if err != nil {
		return "", domain.Wrap(domain.CodePathNotFound, "导入的配置无法解析", err)
	}
	config["AvdId"] = name
	if _, ok := config["avd.ini.displayname"]; ok {
		config["avd.ini.displayname"] = name
	}
	if err := WriteIni(dstLayout.ConfigIni, config); err != nil {
		return "", err
	}

	// 写 .ini 指向新目录（旧 .ini 里的绝对路径指向原机器，必须重写）
	ini := map[string]string{
		"avd.ini.encoding": "UTF-8",
		"path":             dstLayout.Dir,
		"path.rel":         filepath.Join("avd", filepath.Base(dstLayout.Dir)),
		"target":           config["target"],
	}
	if err := WriteIni(dstLayout.IniPath, ini); err != nil {
		return "", err
	}
	_ = srcIni

	// 元数据：导入的设备标记来源
	meta, _ := s.ReadMeta(name)
	if meta == nil {
		meta = &Meta{Name: name, CreatedAt: platform.NowMs()}
	}
	meta.UpdatedAt = platform.NowMs()
	meta.LastUsedAt = 0
	meta.DisplayName = firstNonEmpty(config["avd.ini.displayname"], name)
	meta.CreatedBy = "import"
	if meta.Extra == nil {
		meta.Extra = map[string]string{}
	}
	meta.Extra["importedFrom"] = filepath.Base(zipPath)
	_ = s.WriteMeta(name, meta)

	platform.InvalidateDirSize(dstLayout.Dir)
	return name, nil
}

// detectAvdName 从 zip 条目里推断设备名（找 <name>.ini）。
func detectAvdName(entries []string) string {
	for _, e := range entries {
		normalized := filepath.ToSlash(e)
		if strings.Count(normalized, "/") != 0 {
			continue
		}
		if strings.EqualFold(filepath.Ext(normalized), ".ini") {
			return strings.TrimSuffix(normalized, filepath.Ext(normalized))
		}
	}
	// 退化为从 <name>.avd/ 目录推断
	for _, e := range entries {
		normalized := filepath.ToSlash(e)
		idx := strings.IndexByte(normalized, '/')
		if idx <= 0 {
			continue
		}
		top := normalized[:idx]
		if strings.HasSuffix(strings.ToLower(top), ".avd") {
			return strings.TrimSuffix(top, filepath.Ext(top))
		}
	}
	return ""
}

func isSkipped(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		for _, skip := range snapshotSkipDirs {
			if strings.EqualFold(part, skip) {
				return true
			}
		}
	}
	return false
}

func addFileToZip(zw *zip.Writer, srcPath, zipName string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return domain.Wrap(domain.CodePathNotFound, "无法读取文件: "+srcPath, err)
	}
	defer func() { _ = src.Close() }()

	info, err := src.Stat()
	if err != nil {
		return domain.Wrap(domain.CodePathNotFound, "无法读取文件属性", err)
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return domain.Wrap(domain.CodeUnknown, "无法构造 zip 条目", err)
	}
	header.Name = zipName
	header.Method = zip.Deflate

	w, err := zw.CreateHeader(header)
	if err != nil {
		return domain.Wrap(domain.CodeUnknown, "无法写入 zip 条目", err)
	}
	if _, err := io.Copy(w, src); err != nil {
		return domain.Wrap(domain.CodeUnknown, "写入 zip 数据失败", err)
	}
	return nil
}
