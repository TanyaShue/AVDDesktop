// 设备设置：读取与修改单个 AVD 的配置项（运行内存 / CPU 核心 / 分辨率 / DPI /
// 虚拟机堆上限 / 物理键盘 / root）。
//
// 设计要点：
//   - 硬件类配置写在 AVD 自己的 config.ini 里（模拟器只在启动时读取），写入沿用
//     hardware.go 的「定点修正」策略：只改明确给出的键，注释、顺序与未知键原样保留；
//   - root 属于软件自己的运行偏好，不写进 config.ini（避免模拟器解析未知键），
//     存在 AVD 目录下的 .avddesktop.json 里，随设备目录一起删除；
//   - 校验口径与创建设备时一致，避免出现「能创建但不能修改」的两套规则。
package avd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/platform"
)

// appStateFile 是软件自有的每设备状态文件（位于 AVD 目录内）。
const appStateFile = ".avddesktop.json"

// 设备设置的允许区间。内存 / CPU / 分辨率与 hardware.go 的创建校验保持一致。
const (
	settingsRAMMinMB   = 512
	settingsRAMMaxMB   = 16384
	settingsCPUMin     = 1
	settingsCPUMax     = 16
	settingsPixelMin   = 240
	settingsPixelMax   = 7680
	settingsDensityMin = 72
	settingsDensityMax = 720
	settingsHeapMinMB  = 16
	settingsHeapMaxMB  = 4096
)

// appState 是软件自己维护的每设备状态（当前只有 root 开关）。
type appState struct {
	Version int  `json:"version"`
	Root    bool `json:"root,omitempty"`
}

// appStatePath 返回设备的软件状态文件路径。
func (s *Store) appStatePath(name string) string {
	return filepath.Join(s.Resolve(name).Dir, appStateFile)
}

// ReadSettings 读取设备当前生效的设置。
//
// 数值项来自 config.ini（未设置时为零值，界面按「跟随设备档案」展示），
// root 来自软件自有的状态文件。config.ini 缺失时返回错误。
func (s *Store) ReadSettings(name string) (domain.AvdSettings, error) {
	config, err := s.ReadConfig(name)
	if err != nil {
		return domain.AvdSettings{}, err
	}
	return settingsFromConfig(config, s.ReadAppState(name).Root), nil
}

// ReadAppState 读取软件自有的设备状态；文件缺失或损坏时返回零值（不阻塞界面）。
func (s *Store) ReadAppState(name string) appState {
	data, err := os.ReadFile(s.appStatePath(name))
	if err != nil {
		return appState{}
	}
	var state appState
	if err := json.Unmarshal(data, &state); err != nil {
		// 损坏的状态文件不应让整个设置面板不可用：按「未开启 root」处理。
		return appState{}
	}
	return state
}

// RootEnabled 返回设备是否已开启 root。
func (s *Store) RootEnabled(name string) bool { return s.ReadAppState(name).Root }

// writeAppState 原子写入软件自有的设备状态。
func (s *Store) writeAppState(name string, state appState) error {
	path := s.appStatePath(name)
	if !platform.DirExists(filepath.Dir(path)) {
		return domain.Err(domain.CodePathNotFound, "设备目录不存在: "+filepath.Dir(path))
	}
	if state.Version == 0 {
		state.Version = 1
	}
	data, err := json.Marshal(state)
	if err != nil {
		return domain.Wrap(domain.CodeUnknown, "无法序列化设备状态", err)
	}
	if err := writeFileAtomic(path, append(data, '\n')); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法写入设备状态文件", err)
	}
	return nil
}

// Detail 汇总设备设置面板需要的全部信息（只读信息 + 可编辑设置 + 破损原因）。
func (s *Store) Detail(name string) (domain.AvdDetail, error) {
	summary, err := s.summary(name)
	if err != nil {
		return domain.AvdDetail{}, err
	}
	config, err := s.ReadConfig(name)
	if err != nil {
		return domain.AvdDetail{}, err
	}
	supported, reason := rootSupport(config)
	detail := domain.AvdDetail{
		Name:            summary.Name,
		Path:            summary.Path,
		SystemImagePath: config["image.sysdir.1"],
		DeviceProfileID: summary.DeviceProfileID,
		API:             summary.API,
		Tag:             summary.Tag,
		ABI:             summary.ABI,
		Settings:        settingsFromConfig(config, s.ReadAppState(name).Root),
		RootSupported:   supported,
		RootReason:      reason,
		SkinPath:        config["skin.path"],
		Broken:          summary.Broken,
		State:           summary.State,
	}
	return detail, nil
}

// UpdateSettings 保存设备设置并返回最新详情。
//
// 调用方必须确保设备没有在运行：config.ini 只在启动时读取，运行中修改既不会生效，
// 也可能与模拟器退出时的配置回写互相覆盖。
func (s *Store) UpdateSettings(req domain.AvdSettingsUpdate) (domain.AvdDetail, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return domain.AvdDetail{}, domain.Err(domain.CodeInvalidArgument, "未指定设备名称")
	}
	if err := ValidateSettings(req); err != nil {
		return domain.AvdDetail{}, err
	}
	if !s.Exists(name) {
		return domain.AvdDetail{}, domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	layout := s.Resolve(name)
	raw, err := os.ReadFile(layout.ConfigIni)
	if err != nil {
		return domain.AvdDetail{}, domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	config := parseIni(string(raw))
	req.Name = name

	// root 的可行性先校验：不通过时不应已经写进 config.ini（避免"改了一半"）。
	state := s.ReadAppState(name)
	if req.Root != nil && *req.Root && !state.Root {
		supported, reason := rootSupport(config)
		if !supported {
			return domain.AvdDetail{}, domain.Err(domain.CodeInvalidArgument,
				"该设备无法开启 root："+reason).
				WithHint("请改用 Google APIs（非 Play）系统镜像创建设备，或保持 root 关闭")
		}
	}

	changes := settingsChanges(req)
	if req.LCDWidth > 0 && req.LCDHeight > 0 {
		// 分辨率还要处理皮肤：skin.path 指向真实皮肤或另一组数值尺寸时会盖过 hw.lcd.*。
		changes = append(changes, resolutionSkinChanges(config, &domain.AvdHardware{
			LCDWidth: req.LCDWidth, LCDHeight: req.LCDHeight,
		})...)
	}
	if len(changes) > 0 {
		if err := writeFileAtomic(layout.ConfigIni, []byte(applyIniChanges(string(raw), changes))); err != nil {
			return domain.AvdDetail{}, domain.Wrap(domain.CodePermissionDenied, "无法写入 config.ini", err)
		}
	}
	if req.Root != nil {
		state.Root = *req.Root
		if err := s.writeAppState(name, state); err != nil {
			return domain.AvdDetail{}, err
		}
	}
	return s.Detail(name)
}

// ValidateSettings 校验设置项（零值 / nil 表示不修改该项）。
//
// 在写文件之前调用：参数不合法时不应留下「保存了但没生效」的设备。
func ValidateSettings(req domain.AvdSettingsUpdate) error {
	fields := []struct {
		value int
		label string
		unit  string
		min   int
		max   int
	}{
		{req.RAMMB, "内存", "MB", settingsRAMMinMB, settingsRAMMaxMB},
		{req.CPUCores, "CPU 核心", "核", settingsCPUMin, settingsCPUMax},
		{req.LCDDensity, "像素密度", "dpi", settingsDensityMin, settingsDensityMax},
		{req.VMHeapMB, "虚拟机堆上限", "MB", settingsHeapMinMB, settingsHeapMaxMB},
		{req.LCDWidth, "屏幕宽度", "px", settingsPixelMin, settingsPixelMax},
		{req.LCDHeight, "屏幕高度", "px", settingsPixelMin, settingsPixelMax},
	}
	for _, f := range fields {
		if f.value == 0 {
			continue
		}
		if f.value < f.min || f.value > f.max {
			return domain.Err(domain.CodeInvalidArgument,
				f.label+" "+strconv.Itoa(f.value)+" "+f.unit+" 超出允许范围（"+
					strconv.Itoa(f.min)+"–"+strconv.Itoa(f.max)+" "+f.unit+"）").
				WithHint("请修正后重试")
		}
	}
	if (req.LCDWidth == 0) != (req.LCDHeight == 0) {
		return domain.Err(domain.CodeInvalidArgument, "分辨率必须同时给出宽度和高度").
			WithHint("只改宽度或只改高度会让屏幕比例失真，请同时填写")
	}
	return nil
}

// settingsChanges 把非零设置项映射成 config.ini 键值。
func settingsChanges(req domain.AvdSettingsUpdate) []iniChange {
	var out []iniChange
	add := func(value int, key string) {
		if value > 0 {
			out = append(out, iniChange{key: key, value: strconv.Itoa(value)})
		}
	}
	add(req.RAMMB, "hw.ramSize")
	add(req.CPUCores, "hw.cpu.ncore")
	add(req.LCDWidth, "hw.lcd.width")
	add(req.LCDHeight, "hw.lcd.height")
	add(req.LCDDensity, "hw.lcd.density")
	add(req.VMHeapMB, "vm.heapSize")
	if req.Keyboard != nil {
		out = append(out, iniChange{key: "hw.keyboard", value: yesNo(*req.Keyboard)})
	}
	return out
}

// settingsFromConfig 从 config.ini 与软件状态里取出设置值。
//
// 容量类键（hw.ramSize / vm.heapSize）可能是 "2G" / "228M" 这种带单位的写法，
// 统一按容量解析成 MB（见 size.go）；其余数值键是普通整数。
func settingsFromConfig(config map[string]string, root bool) domain.AvdSettings {
	settings := domain.AvdSettings{
		RAMMB:      parseSizeMB(config["hw.ramSize"]),
		CPUCores:   atoi(config["hw.cpu.ncore"]),
		LCDWidth:   atoi(config["hw.lcd.width"]),
		LCDHeight:  atoi(config["hw.lcd.height"]),
		LCDDensity: atoi(config["hw.lcd.density"]),
		VMHeapMB:   parseSizeMB(config["vm.heapSize"]),
		Keyboard:   isYes(config["hw.keyboard"]),
		Root:       root,
	}
	return settings
}

// DescribeSettings 返回设置的可读摘要（用于任务日志与诊断）。
func DescribeSettings(settings domain.AvdSettings) string {
	var parts []string
	if settings.RAMMB > 0 {
		parts = append(parts, "内存 "+formatRAM(settings.RAMMB))
	}
	if settings.CPUCores > 0 {
		parts = append(parts, fmt.Sprintf("CPU %d 核", settings.CPUCores))
	}
	if settings.LCDWidth > 0 && settings.LCDHeight > 0 {
		parts = append(parts, fmt.Sprintf("分辨率 %d×%d", settings.LCDWidth, settings.LCDHeight))
	}
	if settings.LCDDensity > 0 {
		parts = append(parts, fmt.Sprintf("%d dpi", settings.LCDDensity))
	}
	if settings.VMHeapMB > 0 {
		parts = append(parts, fmt.Sprintf("虚拟机堆 %d MB", settings.VMHeapMB))
	}
	if settings.Keyboard {
		parts = append(parts, "物理键盘")
	}
	if settings.Root {
		parts = append(parts, "root")
	}
	return strings.Join(parts, "、")
}

// rootSupport 判断设备的系统镜像是否支持 root。
//
// 依据：Google Play 镜像是正式版（user）构建，adb root 与可写系统分区都不可用；
// 其余镜像（google_apis / default / aosp 等）是 userdebug 构建，可 root。
// 判据同时看 PlayStore.enabled、tag.id 与镜像目录，任一命中即视为 Play 镜像。
func rootSupport(config map[string]string) (bool, string) {
	const reason = "系统镜像是 Google Play 版本（user 构建），镜像本身不开放 root"
	if isYes(config["PlayStore.enabled"]) {
		return false, reason
	}
	tag := strings.ToLower(firstNonEmpty(config["tag.id"], firstTagID(config["tag.ids"])))
	sysdir := strings.ToLower(config["image.sysdir.1"])
	if strings.Contains(tag, "playstore") || strings.Contains(sysdir, "playstore") {
		return false, reason
	}
	if tag == "" && sysdir == "" {
		return false, "配置里缺少系统镜像信息（image.sysdir.1 / tag.id），无法判断该镜像是否支持 root"
	}
	return true, ""
}

// yesNo 把布尔值写成 config.ini 的 yes/no。
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// isYes 判断 config.ini 里的开关是否打开（兼容 yes/true/1）。
func isYes(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}
