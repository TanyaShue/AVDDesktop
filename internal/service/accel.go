package service

import (
	"strconv"
	"strings"

	"AVDDesktop/internal/platform"
)

// AccelCheck 是 `emulator -accel-check` 的解析结果。
type AccelCheck struct {
	Available bool   // 状态码为 0 表示加速可用
	Kind      string // hvf | kvm | whpx | haxm | aehd | gvm | none
	Message   string // 官方说明文本（原样保留，供界面展示）
}

// accelKinds 按优先级匹配加速器名称。
//
// 同一份说明文本里不会同时出现两个加速器，顺序只影响匹配歧义时的取舍。
var accelKinds = []struct {
	kind   string
	tokens []string
}{
	{"hvf", []string{"hypervisor.framework", "hvf"}}, // macOS
	{"whpx", []string{"whpx", "windows hypervisor platform"}},
	{"aehd", []string{"aehd"}},
	{"haxm", []string{"haxm"}},
	{"gvm", []string{"gvm"}},
	{"kvm", []string{"kvm"}}, // Linux
}

// parseAccelCheck 解析 `emulator -accel-check` 的输出。
//
// 官方输出是固定四行信封，状态码 0 表示加速可用：
//
//	accel:
//	0
//	Hypervisor.Framework OS X Version 26.6
//	accel
//
// 说明文本各平台完全不同：Windows 是 "WHPX(...) is installed and usable."，
// Linux 是 "KVM (version 12) is installed and usable."，而 macOS 只说
// "Hypervisor.Framework OS X Version x.y"——不含 usable 字样。
// 因此判定必须以状态码为准，不能只匹配英文文案（那正是 macOS 被误判为不可用的原因）。
//
// 对不含信封的旧输出，退回按官方固定文案 "is installed and usable" 判定。
func parseAccelCheck(out string) AccelCheck {
	lines := nonEmptyLines(out)
	info := AccelCheck{Kind: "none"}
	parsed := false

	for i, line := range lines {
		if !strings.EqualFold(line, "accel:") || i+1 >= len(lines) {
			continue
		}
		code, err := strconv.Atoi(lines[i+1])
		if err != nil {
			continue
		}
		info.Available = code == 0
		if i+2 < len(lines) && !strings.EqualFold(lines[i+2], "accel") {
			info.Message = lines[i+2]
		}
		parsed = true
		break
	}

	if !parsed {
		info.Message = strings.Join(lines, "\n")
		info.Available = strings.Contains(strings.ToLower(info.Message), "is installed and usable")
	}

	info.Kind = accelKind(info.Message)
	return info
}

// accelKind 从说明文本里识别加速器类型。
func accelKind(message string) string {
	lower := strings.ToLower(message)
	for _, item := range accelKinds {
		for _, token := range item.tokens {
			if strings.Contains(lower, token) {
				return item.kind
			}
		}
	}
	return "none"
}

// accelHints 生成加速不可用时的可操作提示。
func accelHints(goos, message string, available bool) []string {
	if available {
		return nil
	}
	lower := strings.ToLower(message)
	hints := []string{"未检测到可用的硬件加速：" + platform.AccelAdvice(goos)}
	if strings.Contains(lower, "not supported") || strings.Contains(lower, "not enabled") {
		hints = append(hints, "宿主 CPU 的虚拟化功能可能未开启（需在 BIOS/UEFI 中启用 VT-x / AMD-V）")
	}
	return append(hints, "没有硬件加速时模拟器可以启动，但会明显变慢")
}

// nonEmptyLines 按行切分并丢弃空白行（同时兼容 \r\n 与 \r）。
func nonEmptyLines(out string) []string {
	text := strings.ReplaceAll(out, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		if line := strings.TrimSpace(raw); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
