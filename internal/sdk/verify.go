package sdk

import (
	"os"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// PackageMetadata 是 source.properties 中我们需要的字段。
type PackageMetadata struct {
	Path     string
	Revision string
}

// PackageDirectory 返回 SDK 包对应的落盘目录。
func PackageDirectory(tools platform.Tools, pkgPath string) (string, error) {
	pkgPath = strings.TrimSpace(pkgPath)
	switch {
	case pkgPath == "cmdline-tools;latest" || strings.HasPrefix(pkgPath, "cmdline-tools;"):
		return tools.CmdlineTools, nil
	case pkgPath == "platform-tools":
		return tools.PlatformTools, nil
	case pkgPath == "emulator":
		return tools.EmulatorDir, nil
	case strings.HasPrefix(pkgPath, "system-images;"):
		rel, err := ImageDir(pkgPath)
		if err != nil {
			return "", err
		}
		return filepath.Join(tools.SdkRoot, rel), nil
	case pkgPath == "":
		return "", domain.Err(domain.CodeInvalidArgument, "SDK 包路径为空")
	default:
		return filepath.Join(tools.SdkRoot, filepath.FromSlash(strings.ReplaceAll(pkgPath, ";", "/"))), nil
	}
}

// VerifyPackage 校验 SDK 包是否完整、官方工具是否还能识别它。
//
// 这个校验只读本地文件，不发网络请求，供环境检查和安装后的最终判定共用。
func VerifyPackage(tools platform.Tools, pkgPath string) error {
	dir, err := PackageDirectory(tools, pkgPath)
	if err != nil {
		return err
	}
	if !platform.DirExists(dir) {
		return domain.ErrDetail(domain.CodePathNotFound, "SDK 包目录不存在", dir)
	}

	meta, err := ReadSourceProperties(dir)
	if err != nil {
		return err
	}
	if strings.TrimSpace(meta.Revision) == "" {
		return domain.ErrDetail(domain.CodeArchiveFailed,
			"SDK 包缺少版本信息（可能是旧版本残留）", filepath.Join(dir, "source.properties"))
	}

	switch {
	case pkgPath == "cmdline-tools;latest" || strings.HasPrefix(pkgPath, "cmdline-tools;"):
		for _, name := range []string{"sdkmanager", "avdmanager"} {
			if !platform.FileExists(filepath.Join(dir, "bin", scriptName(name))) {
				return domain.ErrDetail(domain.CodeToolMissing,
					"命令行工具不完整", filepath.Join(dir, "bin", scriptName(name)))
			}
		}
	case pkgPath == "platform-tools":
		if !platform.FileExists(filepath.Join(dir, "package.xml")) {
			return domain.ErrDetail(domain.CodeArchiveFailed,
				"platform-tools 缺少 package.xml（可能是旧版本残留）", filepath.Join(dir, "package.xml"))
		}
		if !platform.FileExists(tools.Adb) {
			return domain.ErrDetail(domain.CodeToolMissing, "platform-tools 缺少 adb", tools.Adb)
		}
	case pkgPath == "emulator":
		if !platform.FileExists(filepath.Join(dir, "package.xml")) {
			return domain.ErrDetail(domain.CodeArchiveFailed,
				"emulator 缺少 package.xml（可能是旧版本残留）", filepath.Join(dir, "package.xml"))
		}
		if !platform.FileExists(tools.Emulator) {
			return domain.ErrDetail(domain.CodeToolMissing, "emulator 缺少主程序", tools.Emulator)
		}
	case strings.HasPrefix(pkgPath, "system-images;"):
		// sdkmanager / avdmanager 把 package.xml 作为「已安装」的依据。
		if !platform.FileExists(filepath.Join(dir, "package.xml")) {
			return domain.ErrDetail(domain.CodeArchiveFailed,
				"系统镜像缺少 package.xml（可能是中断安装残留）", filepath.Join(dir, "package.xml"))
		}
	default:
		if !platform.FileExists(filepath.Join(dir, "package.xml")) {
			return domain.ErrDetail(domain.CodeArchiveFailed,
				"SDK 包缺少 package.xml（可能是旧版本残留）", filepath.Join(dir, "package.xml"))
		}
	}
	return nil
}

// VerifyPackages 校验一组包，并返回可读的聚合错误。
func VerifyPackages(tools platform.Tools, packages []string) error {
	var failures []string
	for _, pkgPath := range packages {
		if strings.TrimSpace(pkgPath) == "" {
			continue
		}
		if err := VerifyPackage(tools, pkgPath); err != nil {
			failures = append(failures, pkgPath+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return domain.ErrDetail(domain.CodeArchiveFailed,
			"SDK 组件安装后完整性校验失败", strings.Join(failures, "；"))
	}
	return nil
}

// ReadSourceProperties 读取包目录下的 source.properties。
func ReadSourceProperties(dir string) (PackageMetadata, error) {
	path := filepath.Join(dir, "source.properties")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PackageMetadata{}, domain.ErrDetail(domain.CodePathNotFound,
				"SDK 包缺少 source.properties（可能是旧版本残留）", path)
		}
		return PackageMetadata{}, domain.Wrap(domain.CodePermissionDenied, "无法读取 SDK 包版本信息", err)
	}
	var meta PackageMetadata
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Pkg.Path":
			meta.Path = strings.TrimSpace(value)
		case "Pkg.Revision":
			meta.Revision = strings.TrimSpace(value)
		}
	}
	return meta, nil
}
