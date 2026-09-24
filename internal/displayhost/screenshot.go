package displayhost

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

const (
	// screenshotTimeout 是一次截图（设备端抓屏 + adb pull）的总上限。
	screenshotTimeout = 30 * time.Second
	// remoteScreenshotPath 是设备端的中转文件；截完即删。
	remoteScreenshotPath = "/data/local/tmp/avddesktop-screenshot.png"
)

// captureScreenshot 抓取设备画面并保存到软件目录的 screenshots/ 下，返回本地路径。
//
// 刻意不用 `adb exec-out screencap -p`：proc 的输出通道按文本行处理，PNG 二进制会被
// 拆行与裁剪。改成「设备端落盘 → adb pull」两段，全程只有文本输出。
func captureScreenshot(ctx context.Context, cfg HelperConfig) (string, error) {
	dir := platform.ScreenshotDir()
	if err := platform.EnsureDir(dir); err != nil {
		return "", domain.ErrDetail(domain.CodeProcessFailed, "无法创建截图目录", err.Error())
	}
	name := fmt.Sprintf("%s-%s.png", sanitizeName(cfg.AVDName), time.Now().Format("20060102-150405-000"))
	local := filepath.Join(dir, name)

	if _, err := runADB(ctx, cfg, "shell", "screencap", "-p", remoteScreenshotPath); err != nil {
		return "", err
	}
	defer func() {
		// 清理设备端中转文件；失败不影响本次结果，下次截图会覆盖。
		cleanupCtx, cancel := context.WithTimeout(context.Background(), adbKeyTimeout)
		defer cancel()
		_, _ = runADB(cleanupCtx, cfg, "shell", "rm", "-f", remoteScreenshotPath)
	}()

	if _, err := runADB(ctx, cfg, "pull", remoteScreenshotPath, local); err != nil {
		return "", err
	}
	return local, nil
}
