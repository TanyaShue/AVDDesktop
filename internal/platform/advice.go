package platform

// AccelAdvice 返回按平台给出的硬件加速排查建议。
//
// 三个平台的开启方式完全不同：Windows 走「Windows 功能」里的虚拟机监控程序平台，
// Linux 走 KVM 模块与 /dev/kvm 权限，macOS 走 Hypervisor.Framework。
// 统一写「开启 Windows 功能」会让另外两个平台的用户拿到无法执行的指引。
func AccelAdvice(goos string) string {
	switch goos {
	case "windows":
		return "在「Windows 功能」中启用「Windows 虚拟机监控程序平台」（Hyper-V 平台），或安装 AEHD/HAXM"
	case "linux":
		return "确认已加载 kvm 模块（/dev/kvm 存在）且当前用户在 kvm 组"
	case "darwin":
		return "macOS 依赖 Hypervisor.Framework；若宿主本身运行在虚拟机中，嵌套虚拟化通常不可用"
	default:
		return "请确认宿主 CPU 的虚拟化功能已在固件中启用"
	}
}
