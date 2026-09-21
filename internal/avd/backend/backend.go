// Package backend 抽象 AVD 的创建操作，提供两种实现（见 ARCHITECTURE.md ADR-02）：
//
//	AvdManagerBackend —— 调用官方 avdmanager（需要 JDK，保证 device profile 指纹与官方一致）
//	DirectBackend     —— 直接写 .ini / config.ini（无需 JDK，支持 CLI 未暴露的硬件项）
//
// 未来官方推荐的 Android CLI（`android emulator create`）可再加一个实现。
package backend

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"AVDDesktop/internal/avd/store"
	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
	"AVDDesktop/internal/proc"
	"AVDDesktop/internal/sdk/repo"
)

// Deps 是创建 AVD 所需的依赖。
type Deps struct {
	Paths    platform.InstallPaths
	Store    *store.Store
	Env      []string
	OnOutput func(stream, line string)
}

// CreateResult 是创建结果。
type CreateResult struct {
	Summary   domain.AvdSummary
	Backend   string // avdmanager | direct
	Warnings  []string
	FinalArgs []string
}

// Backend 是 AVD 创建后端。
type Backend interface {
	// Kind 返回后端标识（avdmanager | direct）。
	Kind() string
	// Available 判断该后端当前是否可用。
	Available() bool
	// Create 创建 AVD 并返回结果。
	Create(ctx context.Context, spec domain.AvdSpec, deps Deps) (CreateResult, error)
}

// Select 选择可用后端。
//
// 语义（与 UI 的“优先使用 avdmanager”开关对应）：
//   - spec.CreateWithAvdManager=true  且 avdmanager 可用 → 用官方 CLI（device profile 指纹与官方一致）
//   - 否则 → 直写配置文件（无需 JDK，也是无 JDK 环境的降级路径）
func Select(spec domain.AvdSpec, deps Deps) Backend {
	if !spec.CreateWithAvdManager {
		return &DirectBackend{}
	}
	am := &AvdManagerBackend{Paths: deps.Paths}
	if am.Available() {
		return am
	}
	return &DirectBackend{}
}

// ---------------------------------------------------------------- avdmanager 后端

// AvdManagerBackend 通过官方 CLI 创建 AVD。
type AvdManagerBackend struct {
	Paths platform.InstallPaths
}

// Kind 实现 Backend。
func (b *AvdManagerBackend) Kind() string { return "avdmanager" }

// Available 判断 avdmanager 是否可用。
func (b *AvdManagerBackend) Available() bool {
	return platform.FileExists(b.Paths.Avdmanager)
}

// Create 执行 `avdmanager create avd` 并做后处理。
func (b *AvdManagerBackend) Create(ctx context.Context, spec domain.AvdSpec, deps Deps) (CreateResult, error) {
	res := CreateResult{Backend: b.Kind()}
	args := []string{"create", "avd", "-n", spec.Name, "-k", spec.SystemImagePath}
	if spec.ProfileID != "" {
		args = append(args, "-d", spec.ProfileID)
	}
	if strings.TrimSpace(spec.SDCardSize) != "" {
		args = append(args, "-c", spec.SDCardSize)
	}
	if strings.TrimSpace(spec.Path) != "" {
		args = append(args, "-p", spec.Path)
	}
	args = append(args, "-f")
	res.FinalArgs = args

	// avdmanager 会交互询问是否自定义硬件配置，这里自动回答 no
	runResult, err := proc.Run(ctx, b.Paths.Avdmanager, args, proc.Options{
		Env:        deps.Env,
		Timeout:    180 * time.Second,
		StdinLines: []string{"no"},
		OnLine:     deps.OnOutput,
	})
	if err != nil {
		return res, err
	}
	// avdmanager 成功时也可能返回非 0（例如 warning），以目录是否生成作为最终判据
	layout := deps.Store.Resolve(spec.Name)
	if !layout.Exists {
		return res, domain.ErrDetail(domain.CodeProcessFailed,
			"avdmanager 未能创建 AVD", runResult.Combined()).
			WithHint("请检查系统镜像是否已安装、名称是否合法")
	}
	if msg := strings.TrimSpace(runResult.Combined()); msg != "" && runResult.ExitCode != 0 {
		res.Warnings = append(res.Warnings, "avdmanager 返回退出码 "+itoa(runResult.ExitCode))
	}
	return finalize(res, spec, deps)
}

// ---------------------------------------------------------------- 直写后端

// DirectBackend 不依赖 JDK，直接生成 AVD 配置文件。
type DirectBackend struct{}

// Kind 实现 Backend。
func (b *DirectBackend) Kind() string { return "direct" }

// Available 始终可用。
func (b *DirectBackend) Available() bool { return true }

// Create 直接写入 .ini 与 config.ini。
func (b *DirectBackend) Create(ctx context.Context, spec domain.AvdSpec, deps Deps) (CreateResult, error) {
	res := CreateResult{Backend: b.Kind()}
	layout := deps.Store.Resolve(spec.Name)

	api, tag, abi, err := SplitSystemImage(spec.SystemImagePath)
	if err != nil {
		return res, err
	}
	sysDir, err := SystemImageDir(spec.SystemImagePath)
	if err != nil {
		return res, err
	}
	if platform.DirExists(layout.Dir) {
		if err := deps.Store.Delete(spec.Name, true); err != nil {
			return res, err
		}
	}
	if err := platform.EnsureDir(layout.Dir); err != nil {
		return res, domain.Wrap(domain.CodePermissionDenied, "无法创建 AVD 目录", err)
	}

	config := map[string]string{
		"AvdId":                  spec.Name,
		"avd.ini.encoding":       "UTF-8",
		"avd.ini.displayname":    firstNonEmpty(spec.DisplayName, spec.Name),
		"target":                 "android-" + api,
		"abi.type":               abi,
		"hw.cpu.arch":            abi,
		"image.sysdir.1":         sysDir + string(filepath.Separator),
		"tag.id":                 tag,
		"tag.ids":                tag,
		"tag.display":            TagDisplay(tag),
		"tag.displaynames":       TagDisplay(tag),
		"PlayStore.enabled":      boolYes(strings.Contains(tag, "playstore")),
		"hw.device.name":         firstNonEmpty(spec.ProfileID, "medium_phone"),
		"hw.device.manufacturer": "Google",
	}
	store.ApplyHW(config, store.DefaultHW(spec.ProfileID, deps.defaultRAM(), deps.defaultCores()))
	if w, h, d := screenFromProfile(spec.ProfileID); w > 0 {
		config["hw.lcd.width"] = itoa(w)
		config["hw.lcd.height"] = itoa(h)
		config["hw.lcd.density"] = itoa(d)
		config["skin.dynamic"] = "yes"
		config["skin.name"] = fmt.Sprintf("%dx%d", w, h)
		config["skin.path"] = fmt.Sprintf("%dx%d", w, h)
	}
	store.ApplyHW(config, spec.HW)

	if strings.TrimSpace(spec.SDCardSize) != "" {
		config["sdcard.size"] = spec.SDCardSize
	}
	if err := store.WriteIni(layout.ConfigIni, config); err != nil {
		return res, err
	}
	ini := map[string]string{
		"avd.ini.encoding": "UTF-8",
		"path":             layout.Dir,
		"path.rel":         filepath.Join("avd", filepath.Base(layout.Dir)),
		"target":           config["target"],
	}
	if err := store.WriteIni(layout.IniPath, ini); err != nil {
		return res, err
	}
	return finalize(res, spec, deps)
}

func (d Deps) defaultRAM() int {
	if d.Store == nil {
		return 2048
	}
	return 2048
}

func (d Deps) defaultCores() int {
	return 4
}

// ---------------------------------------------------------------- 公共后处理

// finalize 写入显示名、元数据并读回摘要。
func finalize(res CreateResult, spec domain.AvdSpec, deps Deps) (CreateResult, error) {
	layout := deps.Store.Resolve(spec.Name)

	config, err := store.ReadIni(layout.ConfigIni)
	if err != nil {
		return res, domain.Wrap(domain.CodePathNotFound, "无法读取新设备的 config.ini", err)
	}
	if spec.DisplayName != "" {
		config["avd.ini.displayname"] = spec.DisplayName
	} else if config["avd.ini.displayname"] == "" {
		config["avd.ini.displayname"] = spec.Name
	}
	store.ApplyHW(config, spec.HW)
	if err := store.WriteIni(layout.ConfigIni, config); err != nil {
		return res, err
	}

	meta := &store.Meta{
		Name:        spec.Name,
		DisplayName: config["avd.ini.displayname"],
		CreatedAt:   platform.NowMs(),
		UpdatedAt:   platform.NowMs(),
		ProfileID:   spec.ProfileID,
		CreatedBy:   res.Backend,
		Tags:        spec.Tags,
		Note:        spec.Note,
	}
	if spec.LaunchDefaults != nil {
		meta.LaunchPreset = spec.LaunchDefaults
	}
	if err := deps.Store.WriteMeta(spec.Name, meta); err != nil {
		res.Warnings = append(res.Warnings, "元数据写入失败："+err.Error())
	}

	summary, err := deps.Store.Summary(spec.Name)
	if err != nil {
		return res, err
	}
	res.Summary = summary
	return res, nil
}

// SplitSystemImage 拆解系统镜像包路径：
// system-images;android-36.1;google_apis_playstore;x86_64 → ("36.1", "google_apis_playstore", "x86_64")。
//
// 返回的 api 已去掉 android- 前缀（用于 config.ini 的 target=android-XX 与 UI 展示）；
// 若要拼目录请用 SystemImageDir，它会保留原始段（目录名是 android-36.1 而非 36.1）。
func SplitSystemImage(pkgPath string) (api, tag, abi string, err error) {
	parts := strings.Split(pkgPath, ";")
	if len(parts) < 4 || parts[0] != "system-images" {
		return "", "", "", domain.ErrDetail(domain.CodeInvalidArgument,
			"系统镜像路径格式不正确", "期望: system-images;<api>;<tag>;<abi>，实际: "+pkgPath)
	}
	return strings.TrimPrefix(parts[1], "android-"), parts[2], parts[3], nil
}

// SystemImageDir 返回系统镜像在 SDK 下的相对目录（保留原始 API 段）。
//
// 实测：真实目录是 system-images/android-34/android-desktop/x86_64，其中 API 段
// 必须保留 android- 前缀，否则路径不存在（该问题由端到端测试发现）。
func SystemImageDir(pkgPath string) (string, error) {
	parts := strings.Split(pkgPath, ";")
	if len(parts) < 4 || parts[0] != "system-images" {
		return "", domain.ErrDetail(domain.CodeInvalidArgument,
			"系统镜像路径格式不正确", "期望: system-images;<api>;<tag>;<abi>，实际: "+pkgPath)
	}
	return filepath.Join(parts[0], parts[1], parts[2], parts[3]), nil
}

// TagDisplay 返回 tag 的展示名（转发到 repo 的统一定义）。
func TagDisplay(tag string) string { return repo.TagDisplay(tag) }

func screenFromProfile(profileID string) (int, int, int) {
	switch profileID {
	case "medium_phone", "pixel_7", "pixel_6", "pixel_8", "pixel_9":
		return 1080, 2400, 420
	case "medium_tablet":
		return 1600, 2560, 320
	case "desktop_medium":
		return 1920, 1080, 160
	case "desktop_large":
		return 2560, 1440, 160
	case "tv_1080p":
		return 1920, 1080, 320
	case "tv_4k":
		return 3840, 2160, 320
	default:
		return 0, 0, 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func boolYes(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
