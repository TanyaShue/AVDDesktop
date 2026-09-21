//go:build windows

package platform

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/windows/registry"

	"AVDDesktop/internal/proc"
)

// VirtualizationInfo 描述宿主机的虚拟化能力（非管理员可读取）。
//
// 数据来源优先级：
//  1. WMI/CIM（Win32_ComputerSystem.HypervisorPresent + Win32_Processor 虚拟化标志）
//  2. 回退解析 `systeminfo` 的 "Hyper-V Requirements" 段
//
// 之所以需要这些信息：Android 模拟器在 Windows 上依赖 WHPX 或 AEHD，
// 而 `emulator -accel-check` 只告诉我们"现在能不能用"，不告诉我们"为什么不能用"。
type VirtualizationInfo struct {
	Available bool `json:"available"` // 是否成功采集到信息

	HypervisorPresent bool `json:"hypervisorPresent"` // 已有 hypervisor 在运行（Hyper-V/WHPX/VBS）
	VirtFirmware      bool `json:"virtualizationFirmwareEnabled"`
	SLAT              bool `json:"slat"`
	VMMonitor         bool `json:"vmmMonitor"`
	DataExecPrev      bool `json:"dataExecutionPrevention"`

	CPU         string `json:"cpu,omitempty"`
	ProductName string `json:"productName,omitempty"`
	Caption     string `json:"caption,omitempty"`
	Version     string `json:"version,omitempty"`
	Build       string `json:"build,omitempty"`

	// 常见 Windows 功能/服务的启发式探测结果（注册表，非管理员）
	HyperVHostService bool `json:"hyperVHostService,omitempty"`
	VMComputeService  bool `json:"vmComputeService,omitempty"`

	LongPathsEnabled bool   `json:"longPathsEnabled"`
	Source           string `json:"source,omitempty"`
	Error            string `json:"error,omitempty"`
}

var (
	virtOnce   sync.Once
	virtCache  VirtualizationInfo
	virtExpiry time.Time
	virtMu     sync.Mutex
)

// VirtualizationInfoCached 带 10 分钟缓存的虚拟化信息采集（该查询较重）。
func VirtualizationInfoCached(ctx context.Context) VirtualizationInfo {
	virtMu.Lock()
	defer virtMu.Unlock()
	if time.Now().Before(virtExpiry) {
		return virtCache
	}
	info := collectVirtualization(ctx)
	virtCache = info
	virtExpiry = time.Now().Add(10 * time.Minute)
	return info
}

func collectVirtualization(ctx context.Context) VirtualizationInfo {
	info := VirtualizationInfo{LongPathsEnabled: LongPathsEnabled()}

	if fromPS, err := virtualizationFromPowerShell(ctx); err == nil {
		info = fromPS
		info.LongPathsEnabled = LongPathsEnabled()
		info.Source = "cim"
	} else if fromSys, sysErr := virtualizationFromSysteminfo(ctx); sysErr == nil {
		info = fromSys
		info.LongPathsEnabled = LongPathsEnabled()
		info.Source = "systeminfo"
	} else {
		info.Error = "无法读取虚拟化信息：" + err.Error()
	}

	info.HyperVHostService = serviceExists("HvHost")
	info.VMComputeService = serviceExists("vmcompute")
	return info
}

// virtualizationFromPowerShell 通过 CIM 一次性取回所需字段（约 0.3-3s）。
//
// 注意：中文 Windows 上 PowerShell 5.1 默认用 GBK 输出，会把中文系统名称变成非法 UTF-8。
// 因此必须显式把控制台输出编码强制为 UTF-8（实测本机不设置时“Windows 11 专业版”会乱码）。
func virtualizationFromPowerShell(ctx context.Context) (VirtualizationInfo, error) {
	const script = `$ErrorActionPreference='Stop';` +
		`$OutputEncoding=[Console]::OutputEncoding=[Text.Encoding]::UTF8;` +
		`$cs=Get-CimInstance Win32_ComputerSystem;` +
		`$p=Get-CimInstance Win32_Processor | Select-Object -First 1;` +
		`$os=Get-CimInstance Win32_OperatingSystem;` +
		`[pscustomobject]@{` +
		`hypervisorPresent=$cs.HypervisorPresent;` +
		`virtFirmware=$p.VirtualizationFirmwareEnabled;` +
		`slat=$p.SecondLevelAddressTranslationExtensions;` +
		`vmm=$p.VMMonitorModeExtensions;` +
		`dep=$p.DataExecutionPrevention_Available;` +
		`cpu=$p.Name;model=$cs.Model;` +
		`caption=$os.Caption;version=$os.Version;build=$os.BuildNumber` +
		`}|ConvertTo-Json -Compress`

	res, err := proc.Run(ctx, "powershell.exe",
		[]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script},
		proc.Options{Timeout: 25 * time.Second})
	if err != nil && strings.TrimSpace(res.Stdout) == "" {
		return VirtualizationInfo{}, err
	}

	var raw struct {
		HypervisorPresent *bool  `json:"hypervisorPresent"`
		VirtFirmware      *bool  `json:"virtFirmware"`
		SLAT              *bool  `json:"slat"`
		VMM               *bool  `json:"vmm"`
		DEP               *bool  `json:"dep"`
		CPU               string `json:"cpu"`
		Model             string `json:"model"`
		Caption           string `json:"caption"`
		Version           string `json:"version"`
		Build             string `json:"build"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &raw); err != nil {
		return VirtualizationInfo{}, err
	}
	return VirtualizationInfo{
		Available:         true,
		HypervisorPresent: boolValue(raw.HypervisorPresent),
		VirtFirmware:      boolValue(raw.VirtFirmware),
		SLAT:              boolValue(raw.SLAT),
		VMMonitor:         boolValue(raw.VMM),
		DataExecPrev:      boolValue(raw.DEP),
		CPU:               cleanUTF8(strings.TrimSpace(raw.CPU)),
		ProductName:       cleanUTF8(strings.TrimSpace(raw.Model)),
		Caption:           cleanUTF8(strings.TrimSpace(raw.Caption)),
		Version:           strings.TrimSpace(raw.Version),
		Build:             strings.TrimSpace(raw.Build),
	}, nil
}

// cleanUTF8 丢弃非法 UTF-8 字节，避免乱码字符进入日志与 UI。
func cleanUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "")
}

// virtualizationFromSysteminfo 解析 `systeminfo` 的 Hyper-V Requirements 段。
func virtualizationFromSysteminfo(ctx context.Context) (VirtualizationInfo, error) {
	res, err := proc.Run(ctx, "systeminfo.exe", nil, proc.Options{Timeout: 60 * time.Second})
	out := res.Stdout
	if strings.TrimSpace(out) == "" {
		out = res.Stderr
	}
	if strings.TrimSpace(out) == "" {
		return VirtualizationInfo{}, err
	}
	info := VirtualizationInfo{Available: true}
	lower := strings.ToLower(out)
	// 中文/英文系统文案都做兼容
	info.HypervisorPresent = containsAny(lower, "a hypervisor has been detected", "检测到虚拟机监控程序", "hyper-v 要求")
	info.VirtFirmware = !containsAny(lower, "virtualization enabled in firmware: no", "已启用固件中的虚拟化: 否", "固件中已启用虚拟化: 否")
	info.SLAT = !containsAny(lower, "second level address translation: no", "第二级地址转换: 否")
	info.VMMonitor = !containsAny(lower, "vm monitor mode extensions: no", "vm 监控模式扩展: 否")
	info.DataExecPrev = !containsAny(lower, "data execution prevention available: no", "数据执行保护可用: 否")
	return info, nil
}

// LongPathsEnabled 读取注册表中的长路径支持开关（Win10 1607+）。
func LongPathsEnabled() bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\FileSystem`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = key.Close() }()
	v, _, err := key.GetIntegerValue("LongPathsEnabled")
	return err == nil && v == 1
}

// serviceExists 判断服务/驱动键是否存在（用于推断 Hyper-V 相关功能是否已启用）。
func serviceExists(name string) bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\`+name, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	_ = key.Close()
	return true
}

// WindowsBuild 返回当前 Windows 构建号（供加速引导判断版本下限）。
func WindowsBuild() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer func() { _ = key.Close() }()
	build, _, err := key.GetStringValue("CurrentBuildNumber")
	if err != nil {
		return ""
	}
	return build
}

func boolValue(v *bool) bool { return v != nil && *v }

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
