package service

import (
	"context"

	"AVDDesktop/internal/job"
	"AVDDesktop/internal/sdk"
)

// watchInstallProgress 在安装期间把 sdkmanager 的下载字节数写进任务进度，返回停止函数。
//
// sdkmanager 在非交互环境下不打印任何进度（实测连 TTY 下也没有百分比），
// 所以进度来自它下载临时目录里归档文件的增长，总量通过官方仓库的 HEAD 请求获得；
// 拿不到总量时只显示已下载字节数，不推算假百分比。
func watchInstallProgress(ctx context.Context, j *job.Job, sdkRoot string, packages []string) func() {
	watchCtx, cancel := context.WithCancel(ctx)

	go sdk.WatchInstallProgress(watchCtx, sdkRoot, packages, func(p sdk.DownloadProgress) {
		// 速度由任务内部采样估算；这里只上报字节数
		j.SetBytes(p.Done, p.Total)
	})

	return cancel
}
