package avd

import (
	"context"
	"path/filepath"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/sdk"
)

// EnsureImage 确保所选的系统镜像已安装到软件自己的 SDK 目录。
//
// 判据是镜像目录与 sdkmanager 写入的 package.xml 都存在；
// 缺任一项就交给官方 sdkmanager 安装（这就是“镜像不存在时自动下载”）。
// 返回 alreadyInstalled 表示是否本来就已经装好。
func EnsureImage(ctx context.Context, tools platform.Tools, env []string, pkgPath string, onLine sdk.LineFunc) (alreadyInstalled bool, err error) {
	relDir, err := sdk.ImageDir(pkgPath)
	if err != nil {
		return false, err
	}
	dir := filepath.Join(tools.SdkRoot, relDir)
	if platform.DirExists(dir) && platform.FileExists(filepath.Join(dir, "package.xml")) {
		return true, nil
	}
	if err := sdk.InstallPackages(ctx, tools, env, []string{pkgPath}, onLine); err != nil {
		return false, err
	}
	if !platform.DirExists(dir) {
		return false, domain.ErrDetail(domain.CodeProcessFailed,
			"系统镜像安装后仍未找到目录", dir)
	}
	return false, nil
}
