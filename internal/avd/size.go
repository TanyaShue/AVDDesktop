// AVD 配置里的容量值解析。
//
// 同一个配置项在真实 config.ini 里有两种写法（同一台机器上实测同时存在）：
//
//	hw.ramSize=2G      新版 avdmanager 写出的「字节单位」写法
//	hw.ramSize=2048    旧格式与本应用写入的写法：纯数字，单位是 MB
//	vm.heapSize=228M   同上（堆上限）
//
// 因此解析规则是：**带单位后缀按字节单位换算，纯数字按 MB 处理**。
// 只按前导数字截断（旧实现）会把 2G 读成 2，界面就会显示成「2 MB」——差 1024 倍。
//
// 无法解析时返回 0：调用方按「未设置 / 跟随设备档案」处理，绝不猜测取值。
package avd

import (
	"math"
	"strconv"
	"strings"
)

// parseSizeMB 解析 AVD 配置里的容量值并换算成 MB；无法解析时返回 0。
//
// 支持的写法：2048、2G、2GB、1.5G、228M、228MB、2048K、2684354560B、1T（大小写与空格不敏感）。
func parseSizeMB(value string) int {
	number, unit, ok := splitSize(value)
	if !ok {
		return 0
	}
	switch unit {
	case "":
		// 纯数字：AVD 的 hw.ramSize / vm.heapSize 约定，单位就是 MB。
		return roundToInt(number)
	case "B":
		return roundToInt(number / (1 << 20))
	case "K", "KB":
		return roundToInt(number / 1024)
	case "M", "MB":
		return roundToInt(number)
	case "G", "GB":
		return roundToInt(number * 1024)
	case "T", "TB":
		return roundToInt(number * (1 << 20))
	default:
		return 0
	}
}

// splitSize 把 "2G" / "228M" / "2048" / "1.5 GB" 拆成数值与单位。
//
// 单位统一成大写、去掉空格，非容量单位（例如 2X）与非法数值都返回 ok=false。
func splitSize(value string) (float64, string, bool) {
	text := strings.ToUpper(strings.TrimSpace(value))
	if text == "" {
		return 0, "", false
	}
	i := 0
	for i < len(text) && (isASCIIDigit(text[i]) || text[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, "", false
	}
	number, err := strconv.ParseFloat(text[:i], 64)
	if err != nil || number <= 0 || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, "", false
	}
	unit := strings.ReplaceAll(text[i:], " ", "")
	switch unit {
	case "", "B", "K", "KB", "M", "MB", "G", "GB", "T", "TB":
		return number, unit, true
	default:
		return 0, "", false
	}
}

// roundToInt 四舍五入到整数（结果不会是负数：调用方已排除非正数输入）。
func roundToInt(v float64) int {
	if v <= 0 {
		return 0
	}
	return int(math.Round(v))
}

// isASCIIDigit 判断是否为 0-9（避免把全角数字当成合法输入）。
func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }
