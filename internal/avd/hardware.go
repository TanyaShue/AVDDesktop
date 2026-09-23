// 创建 AVD 时的硬件参数覆盖。
//
// avdmanager 先按所选设备档案生成 config.ini，这里只做创建后的定点修正：
// 只改用户显式给出的键，其它内容（注释、顺序、未知键）原样保留。
package avd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"AVDDesktop/internal/domain"
)

// hardwareField 把一个覆盖项绑定到 config.ini 键、可读名称与允许区间。
//
// 校验、写入与日志共用同一份定义，避免多处规则漂移。
// min / max 只是防呆区间：超出范围的取值模拟器通常会直接启动失败或行为异常，
// 因此在创建前就给出明确提示。
type hardwareField struct {
	value func(domain.AvdHardware) int
	key   string
	label string
	unit  string
	min   int
	max   int
	// suffix 是写入 config.ini 时的单位后缀（如 data 分区的 "M"）；空表示纯数字。
	suffix string
}

var hardwareFields = []hardwareField{
	{
		value: func(h domain.AvdHardware) int { return h.RAMMB },
		key:   "hw.ramSize", label: "内存", unit: "MB", min: 512, max: 16384,
	},
	{
		value: func(h domain.AvdHardware) int { return h.HeapMB },
		key:   "vm.heapSize", label: "VM 堆", unit: "MB", min: 16, max: 2048,
	},
	{
		value: func(h domain.AvdHardware) int { return h.CPUCores },
		key:   "hw.cpu.ncore", label: "CPU 核心", unit: "核", min: 1, max: 16,
	},
	{
		value: func(h domain.AvdHardware) int { return h.LCDWidth },
		key:   "hw.lcd.width", label: "屏幕宽度", unit: "px", min: 240, max: 7680,
	},
	{
		value: func(h domain.AvdHardware) int { return h.LCDHeight },
		key:   "hw.lcd.height", label: "屏幕高度", unit: "px", min: 240, max: 7680,
	},
	{
		value: func(h domain.AvdHardware) int { return h.LCDDensity },
		key:   "hw.lcd.density", label: "屏幕密度", unit: "dpi", min: 72, max: 960,
	},
	{
		value: func(h domain.AvdHardware) int { return h.DataPartitionMB },
		key:   "disk.dataPartition.size", label: "数据分区", unit: "MB", min: 512, max: 65536, suffix: "M",
	},
	{
		value: func(h domain.AvdHardware) int { return h.SDCardMB },
		key:   "sdcard.size", label: "SD 卡", unit: "MB", min: 64, max: 65536, suffix: "M",
	},
}

// iniChange 是一条要写入 config.ini 的键值。
type iniChange struct {
	key   string
	value string
}

// ValidateHardware 校验硬件覆盖项（零值表示沿用设备档案默认值，不参与校验）。
//
// 在启动 avdmanager 之前调用：参数不合法时不应留下一个"创建了但没生效"的设备。
func ValidateHardware(hw *domain.AvdHardware) error {
	if hw == nil {
		return nil
	}
	if (hw.LCDWidth == 0) != (hw.LCDHeight == 0) {
		return domain.Err(domain.CodeInvalidArgument,
			"分辨率必须同时给出宽度和高度").
			WithHint("留空表示沿用设备档案的默认分辨率")
	}
	for _, f := range hardwareFields {
		v := f.value(*hw)
		if v == 0 {
			continue
		}
		if v < f.min || v > f.max {
			return domain.Err(domain.CodeInvalidArgument,
				fmt.Sprintf("%s %d %s 超出允许范围（%d–%d %s）", f.label, v, f.unit, f.min, f.max, f.unit)).
				WithHint("请修正后重试，或留空以沿用设备档案的默认值")
		}
	}
	return nil
}

// DescribeHardware 返回硬件覆盖项的可读摘要（用于任务日志）。
func DescribeHardware(hw *domain.AvdHardware) string {
	if hw == nil {
		return ""
	}
	text := func(label string, value int, unit string) string {
		return fmt.Sprintf("%s %d %s", label, value, unit)
	}
	var parts []string
	if hw.RAMMB > 0 {
		parts = append(parts, text("内存", hw.RAMMB, "MB"))
	}
	if hw.HeapMB > 0 {
		parts = append(parts, text("VM 堆", hw.HeapMB, "MB"))
	}
	if hw.CPUCores > 0 {
		parts = append(parts, text("CPU 核心", hw.CPUCores, "核"))
	}
	switch {
	case hw.LCDWidth > 0 && hw.LCDHeight > 0:
		res := fmt.Sprintf("分辨率 %d×%d", hw.LCDWidth, hw.LCDHeight)
		if hw.LCDDensity > 0 {
			res += fmt.Sprintf(" @ %d dpi", hw.LCDDensity)
		}
		parts = append(parts, res)
	case hw.LCDDensity > 0:
		parts = append(parts, text("屏幕密度", hw.LCDDensity, "dpi"))
	}
	if hw.DataPartitionMB > 0 {
		parts = append(parts, "数据分区 "+formatMB(hw.DataPartitionMB))
	}
	if hw.SDCardMB > 0 {
		parts = append(parts, "SD 卡 "+formatMB(hw.SDCardMB))
	}
	return strings.Join(parts, "、")
}

// ApplyHardware 把硬件覆盖项写进设备已生成的 config.ini。
//
// 只更新明确给出的键：avdmanager 是 config.ini 的生成者，这里做的是定点修正，
// 失败时调用方应回滚整个设备（见 Create），避免留下"参数与请求不一致"的设备。
func (s *Store) ApplyHardware(name string, hw *domain.AvdHardware) error {
	if hw == nil {
		return nil
	}
	changes := hardwareChanges(hw)
	// 分辨率还要处理皮肤：模拟器实际使用 skin.path，若它指向真实皮肤或数值尺寸，
	// 会盖过 hw.lcd.*，必须让皮肤尺寸不再参与（见 resolutionSkinChanges）。
	withResolution := hw.LCDWidth > 0 && hw.LCDHeight > 0
	if len(changes) == 0 && !withResolution {
		return nil
	}

	l := s.Resolve(name)
	if !l.Exists {
		return domain.Err(domain.CodeAvdNotFound, "设备不存在: "+name)
	}
	data, err := os.ReadFile(l.ConfigIni)
	if err != nil {
		return domain.Wrap(domain.CodePathNotFound, "无法读取 config.ini", err)
	}
	if withResolution {
		changes = append(changes, resolutionSkinChanges(parseIni(string(data)), hw)...)
	}
	if err := writeFileAtomic(l.ConfigIni, []byte(applyIniChanges(string(data), changes))); err != nil {
		return domain.Wrap(domain.CodePermissionDenied, "无法写入 config.ini", err)
	}
	return nil
}

// hardwareChanges 返回覆盖项对应的 config.ini 键值（不含分辨率相关的皮肤键）。
func hardwareChanges(hw *domain.AvdHardware) []iniChange {
	if hw == nil {
		return nil
	}
	var out []iniChange
	for _, f := range hardwareFields {
		v := f.value(*hw)
		if v == 0 {
			continue
		}
		out = append(out, iniChange{key: f.key, value: fmt.Sprintf("%d%s", v, f.suffix)})
	}
	if hw.SDCardMB > 0 {
		// 只写 sdcard.size 而不打开开关时容量不会生效。
		out = append(out, iniChange{key: "hw.sdCard", value: "yes"})
	}
	return out
}

// resolutionSkinChanges 在自定义分辨率时同步皮肤配置。
//
// 依据 AOSP 的说明：模拟器用的是 skin.path（可以是 "320x480" 这样的数值尺寸，
// 或 _no_skin 表示无皮肤），skin.name 只是给工具看的名字、会被模拟器忽略。
// 因此只有 skin.path 指向真实皮肤或另一组数值尺寸时才需要改成 _no_skin，
// 否则请求的分辨率不会生效。
func resolutionSkinChanges(config map[string]string, hw *domain.AvdHardware) []iniChange {
	size := fmt.Sprintf("%dx%d", hw.LCDWidth, hw.LCDHeight)
	var out []iniChange
	if path := strings.TrimSpace(config["skin.path"]); path != "" && path != "_no_skin" {
		out = append(out,
			iniChange{key: "skin.path", value: "_no_skin"},
			iniChange{key: "skin.dynamic", value: "yes"},
		)
	}
	// skin.name 只在配置里已经存在时同步（避免给无皮肤配置凭空加上皮肤名）。
	if _, ok := config["skin.name"]; ok {
		out = append(out, iniChange{key: "skin.name", value: size})
	}
	return out
}

// applyIniChanges 在保留原有内容的前提下更新 ini 文本。
//
// 已存在的键就地替换值（保留 key= 之前的排版），缺失的键追加到末尾；
// 注释、空行、顺序与未知键都不动。
func applyIniChanges(content string, changes []iniChange) string {
	if len(changes) == 0 {
		return content
	}
	pending := make(map[string]string, len(changes))
	order := make([]string, 0, len(changes))
	for _, c := range changes {
		if _, seen := pending[c.key]; !seen {
			order = append(order, c.key)
		}
		pending[c.key] = c.value
	}

	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	// 丢弃结尾空行：重建时统一补一个换行，避免文件尾部不断变长。
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		key, ok := iniKey(line)
		if !ok {
			continue
		}
		value, wanted := pending[key]
		if !wanted {
			continue
		}
		eq := strings.IndexByte(line, '=')
		lines[i] = line[:eq+1] + value
		delete(pending, key)
	}

	var b strings.Builder
	b.Grow(len(content) + 64)
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, key := range order {
		if value, ok := pending[key]; ok {
			b.WriteString(key)
			b.WriteByte('=')
			b.WriteString(value)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// iniKey 从一行 config.ini 里取出键（注释与空行返回 false）。
func iniKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return "", false
	}
	i := strings.IndexByte(trimmed, '=')
	if i <= 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[:i]), true
}

// writeFileAtomic 先写同目录临时文件再重命名，避免中途失败留下半个 config.ini。
func writeFileAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.ini")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 重命名成功后这里是空操作

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// formatMB 把 MB 数值格式化成 config.ini 里常见的大小写法（≥1024 MB 用 GB）。
func formatMB(mb int) string {
	if mb >= 1024 && mb%1024 == 0 {
		return fmt.Sprintf("%dG", mb/1024)
	}
	return fmt.Sprintf("%dM", mb)
}
