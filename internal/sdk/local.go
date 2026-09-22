package sdk

import (
	"io/fs"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/platform"
)

// ScanInstalledPackages 直接扫描软件自带 SDK 目录，返回完整且可用的本地包。
//
// 新版 sdkmanager 委托给 Android CLI 后，--list_installed 既可能输出完全不同的表格，
// 也可能在输出完成后异常退出。本地扫描不依赖 CLI 退出行为，并且会用 source.properties
// 与关键可执行文件过滤中断安装留下的残目录。
func ScanInstalledPackages(tools platform.Tools) []Package {
	seen := map[string]bool{}
	var out []Package
	add := func(pkgPath, dir string) {
		if seen[pkgPath] || VerifyPackage(tools, pkgPath) != nil {
			return
		}
		meta, _ := ReadSourceProperties(dir)
		seen[pkgPath] = true
		out = append(out, Package{
			Path:        pkgPath,
			Version:     meta.Revision,
			Description: meta.Path,
			Installed:   true,
		})
	}

	add("cmdline-tools;latest", tools.CmdlineTools)
	add("platform-tools", tools.PlatformTools)
	add("emulator", tools.EmulatorDir)

	_ = filepath.WalkDir(tools.SystemImages, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "source.properties" {
			return nil
		}
		dir := filepath.Dir(path)
		rel, relErr := filepath.Rel(tools.SdkRoot, dir)
		if relErr != nil {
			return nil
		}
		pkgPath := strings.ReplaceAll(filepath.ToSlash(rel), "/", ";")
		if strings.HasPrefix(pkgPath, "system-images;") {
			add(pkgPath, dir)
		}
		return nil
	})
	return out
}
