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
// 判据是镜像目录、source.properties 与 sdkmanager 写入的 package.xml 都完整；
// 缺任一项或发现中断安装残留时，交给官方 sdkmanager 清理旧目录并重新安装。
// 返回 alreadyInstalled 表示是否本来就已经装好。
func EnsureImage(ctx context.Context, tools platform.Tools, env []string, pkgPath string, onLine sdk.LineFunc) (alreadyInstalled bool, err error) {
	if sdk.VerifyPackage(tools, pkgPath) == nil {
		return true, nil
	}
	relDir, err := sdk.ImageDir(pkgPath)
	if err != nil {
		return false, err
	}
	dir := filepath.Join(tools.SdkRoot, relDir)
	if err := sdk.InstallPackages(ctx, tools, env, []string{pkgPath}, onLine); err != nil {
		return false, err
	}
	if err := sdk.VerifyPackage(tools, pkgPath); err != nil {
		return false, domain.ErrDetail(domain.CodeProcessFailed,
			"系统镜像安装后仍不完整", dir+"："+err.Error())
	}
	return false, nil
}
