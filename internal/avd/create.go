package avd

import (
	"context"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sdk"
)

// createTimeout 是 avdmanager 创建命令的执行上限（禁止无限等待）。
const createTimeout = 3 * time.Minute

// Create 通过官方 avdmanager 创建 AVD。
//
// 不自行生成 config.ini：设备目录里的配置由 avdmanager 写入，
// 这里只在命令结束后校验目录是否真的生成。
func (s *Store) Create(ctx context.Context, tools platform.Tools, env []string, spec domain.AvdSpec, onLine sdk.LineFunc) (domain.AvdSummary, error) {
	if !tools.HasAvdmanager() {
		return domain.AvdSummary{}, domain.ErrDetail(domain.CodeToolMissing,
			"未找到软件自带的 avdmanager", tools.Avdmanager)
	}
	if _, err := sdk.ImageDir(spec.SystemImagePath); err != nil {
		return domain.AvdSummary{}, err
	}

	args := []string{"create", "avd", "-n", spec.Name, "-k", spec.SystemImagePath}
	if strings.TrimSpace(spec.ProfileID) != "" {
		args = append(args, "-d", spec.ProfileID)
	}
	args = append(args, "--force")

	// avdmanager 会交互询问是否自定义硬件配置，这里自动回答 no
	res, err := proc.Run(ctx, tools.Avdmanager, args, proc.Options{
		Env:        env,
		Timeout:    createTimeout,
		StdinLines: []string{"no"},
		OnLine:     onLine,
	})
	if err != nil {
		return domain.AvdSummary{}, err
	}

	// avdmanager 成功时也可能返回非 0（只有 warning），以“目录是否生成”为最终判据。
	if !s.Resolve(spec.Name).Exists {
		detail := res.Combined()
		hint := "请确认所选系统镜像已安装、设备名称不冲突"
		switch {
		case strings.Contains(detail, `"emulator" package must be installed`):
			hint = "emulator 组件尚未安装，请先在设置页准备 SDK 环境"
		case strings.Contains(detail, "Package path is not valid"):
			hint = "系统镜像不可用，请重新创建（应用会自动通过 sdkmanager 安装该镜像）"
		case strings.Contains(detail, "already exists"):
			hint = "已存在同名设备，请更换名称或先删除该设备"
		}
		return domain.AvdSummary{}, domain.ErrDetail(domain.CodeProcessFailed,
			"avdmanager 未能创建 AVD", detail).WithHint(hint)
	}
	return s.summary(spec.Name)
}
