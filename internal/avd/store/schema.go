// Package store 负责 AVD 的文件级读写：目录解析、<name>.ini / config.ini 读写、
// 硬件配置 schema、元数据（avddesktop.json）、克隆与删除。
//
// 依据实测（docs/RESEARCH-NOTES.md §5）：avdmanager 生成的 config.ini 键名与默认值
// 见本包 Schema；hardware-qemu.ini 由 emulator 首次启动时写入，我们只读不写。
package store

import "AVDDesktop/internal/domain"

// 配置分组（UI 折叠面板与编辑器分区使用）。
const (
	GroupBasic   = "basic"
	GroupCompute = "compute"
	GroupStorage = "storage"
	GroupDisplay = "display"
	GroupInput   = "input"
	GroupSensors = "sensors"
	GroupCamera  = "camera"
	GroupNetwork = "network"
	GroupBoot    = "boot"
)

// GroupLabels 是分组的中文名（前端也可自行 i18n）。
var GroupLabels = map[string]string{
	GroupBasic:   "基础信息",
	GroupCompute: "计算资源",
	GroupStorage: "存储",
	GroupDisplay: "显示与外观",
	GroupInput:   "输入",
	GroupSensors: "传感器",
	GroupCamera:  "相机",
	GroupNetwork: "网络",
	GroupBoot:    "启动行为",
}

// Schema 是 config.ini 的完整配置项表。
//
// 类型说明：
//
//	bool   —— yes/no
//	int    —— 整数（Min/Max 为建议范围）
//	size   —— 形如 "6G" / "512M" 的大小
//	string —— 自由文本
//	enum   —— 从 EnumValues 中取值
var Schema = []domain.HwConfigItem{
	// ---------------- 基础信息
	{Key: "AvdId", Label: "AVD 标识", Group: GroupBasic, Type: "string", Default: "", Advanced: true,
		Description: "AVD 的内部 ID，通常与目录名一致，不建议手工修改"},
	{Key: "avd.ini.displayname", Label: "显示名称", Group: GroupBasic, Type: "string", Default: "",
		Description: "在设备列表中显示的名字，支持中文与空格"},
	{Key: "target", Label: "目标平台", Group: GroupBasic, Type: "string", Default: "",
		Description: "编译目标，如 android-34（由系统镜像决定）"},
	{Key: "abi.type", Label: "ABI", Group: GroupBasic, Type: "enum", Default: "x86_64",
		EnumValues:  []string{"x86_64", "x86", "arm64-v8a", "armeabi-v7a"},
		Description: "CPU 指令集；x86_64 在 PC 上最快，arm64 仅在需要 ARM 专有库时使用"},
	{Key: "hw.cpu.arch", Label: "CPU 架构", Group: GroupBasic, Type: "enum", Default: "x86_64",
		EnumValues:  []string{"x86_64", "x86", "arm64", "arm"},
		Description: "模拟器使用的 CPU 架构，必须与系统镜像匹配"},
	{Key: "image.sysdir.1", Label: "系统镜像目录", Group: GroupBasic, Type: "string", Default: "", Advanced: true,
		Description: "相对 SDK 根目录的系统镜像路径，由创建流程写入"},
	{Key: "tag.id", Label: "镜像标签", Group: GroupBasic, Type: "string", Default: "", Advanced: true,
		Description: "如 google_apis / google_apis_playstore"},
	{Key: "tag.ids", Label: "镜像标签列表", Group: GroupBasic, Type: "string", Default: "", Advanced: true,
		Description: "多个候选标签，逗号分隔（通常与 tag.id 相同）"},
	{Key: "tag.display", Label: "镜像标签名称", Group: GroupBasic, Type: "string", Default: "", Advanced: true,
		Description: "标签的展示名，如 Google Play"},
	{Key: "tag.displaynames", Label: "镜像标签名称列表", Group: GroupBasic, Type: "string", Default: "", Advanced: true,
		Description: "标签展示名列表，逗号分隔"},
	{Key: "avd.ini.encoding", Label: "配置文件编码", Group: GroupBasic, Type: "string", Default: "UTF-8", Advanced: true,
		Description: "AVD 配置文件的编码，保持 UTF-8 即可"},
	{Key: "PlayStore.enabled", Label: "启用 Google Play", Group: GroupBasic, Type: "bool", Default: "no",
		Description: "仅在使用 Play 镜像时可用；启用后无法 adb root"},
	{Key: "hw.device.name", Label: "设备档案", Group: GroupBasic, Type: "string", Default: "medium_phone", Advanced: true,
		Description: "创建时选择的设备型号，如 pixel_7 / medium_phone"},
	{Key: "hw.device.manufacturer", Label: "厂商", Group: GroupBasic, Type: "string", Default: "Google", Advanced: true,
		Description: "设备档案中的厂商名"},
	{Key: "hw.device.hash2", Label: "档案指纹", Group: GroupBasic, Type: "string", Default: "", Advanced: true,
		Description: "设备档案的 MD5 指纹，用于判断档案是否被修改"},

	// ---------------- 计算资源
	{Key: "hw.ramSize", Label: "内存 (RAM)", Group: GroupCompute, Type: "int", Default: "2048", Min: 512, Max: 16384, Unit: "MB",
		Description: "模拟设备的内存；内存越大越流畅，但会占用更多宿主内存"},
	{Key: "vm.heapSize", Label: "Java 堆上限", Group: GroupCompute, Type: "int", Default: "336", Min: 16, Max: 2048, Unit: "MB",
		Description: "虚拟机堆大小，影响应用内存上限（Android Studio 会自动提示）"},
	{Key: "hw.cpu.ncore", Label: "CPU 核心数", Group: GroupCompute, Type: "int", Default: "4", Min: 1, Max: 16, Unit: "核",
		Description: "模拟的 CPU 核心数，一般设为宿主机物理核心数的一半到全部"},
	{Key: "hw.cpu.model", Label: "CPU 型号", Group: GroupCompute, Type: "string", Default: "", Advanced: true,
		Description: "覆盖上报的 CPU 型号字符串，留空使用默认值"},
	{Key: "hw.gpu.enabled", Label: "启用 GPU 加速", Group: GroupCompute, Type: "bool", Default: "yes",
		Description: "开启后使用硬件图形加速，关闭会影响界面渲染性能"},
	{Key: "hw.gpu.mode", Label: "GPU 模式", Group: GroupCompute, Type: "enum", Default: "auto",
		EnumValues:  []string{"auto", "host", "swiftshader_indirect", "angle_indirect", "guest"},
		Description: "auto 自动选择；host 用宿主 GPU；swiftshader_indirect 纯软件渲染（兼容性最好、最慢）"},
	{Key: "hw.gpu.option", Label: "GPU 附加参数", Group: GroupCompute, Type: "string", Default: "", Advanced: true,
		Description: "传给 GPU 后端的附加选项，一般留空"},

	// ---------------- 存储
	{Key: "disk.dataPartition.size", Label: "内部存储", Group: GroupStorage, Type: "size", Default: "6G",
		Description: "用户数据分区大小，决定可安装多少应用；常用 2G / 6G / 8G / 16G"},
	{Key: "disk.cachePartition", Label: "启用缓存分区", Group: GroupStorage, Type: "bool", Default: "yes", Advanced: true,
		Description: "是否创建 /cache 分区"},
	{Key: "disk.cachePartition.size", Label: "缓存分区大小", Group: GroupStorage, Type: "size", Default: "66MB", Advanced: true,
		Description: "缓存分区容量，一般保持默认"},
	{Key: "hw.sdCard", Label: "启用 SD 卡", Group: GroupStorage, Type: "bool", Default: "yes",
		Description: "是否挂载可用的 SD 卡分区（部分应用需要）"},
	{Key: "sdcard.size", Label: "SD 卡容量", Group: GroupStorage, Type: "size", Default: "512M",
		Description: "SD 卡镜像大小，常用 512M / 1G / 2G"},
	{Key: "hw.sdCard.path", Label: "SD 卡镜像路径", Group: GroupStorage, Type: "string", Default: "", Advanced: true,
		Description: "自定义 SD 卡镜像文件位置，留空则使用 AVD 目录内的 sdcard.img"},
	{Key: "disk.systemPartition.size", Label: "系统分区大小", Group: GroupStorage, Type: "size", Default: "", Advanced: true,
		Description: "覆盖 system 分区大小（配合 -writable-system 使用）"},
	{Key: "disk.vendorPartition.size", Label: "Vendor 分区大小", Group: GroupStorage, Type: "size", Default: "", Advanced: true,
		Description: "覆盖 vendor 分区大小"},

	// ---------------- 显示与外观
	{Key: "hw.lcd.width", Label: "屏幕宽度", Group: GroupDisplay, Type: "int", Default: "1080", Min: 320, Max: 4096, Unit: "px",
		Description: "横向像素数；常见机型 1080×2400、1440×3120、1280×720"},
	{Key: "hw.lcd.height", Label: "屏幕高度", Group: GroupDisplay, Type: "int", Default: "2400", Min: 320, Max: 4096, Unit: "px",
		Description: "纵向像素数"},
	{Key: "hw.lcd.density", Label: "屏幕密度", Group: GroupDisplay, Type: "int", Default: "420", Min: 120, Max: 640, Unit: "dpi",
		Description: "像素密度；手机常见 420/440/480，平板 320"},
	{Key: "hw.initialOrientation", Label: "初始方向", Group: GroupDisplay, Type: "enum", Default: "portrait",
		EnumValues:  []string{"portrait", "landscape"},
		Description: "启动时的屏幕方向"},
	{Key: "showDeviceFrame", Label: "显示设备边框", Group: GroupDisplay, Type: "bool", Default: "yes",
		Description: "在模拟器窗口中显示手机外壳图案"},
	{Key: "skin.dynamic", Label: "动态皮肤", Group: GroupDisplay, Type: "bool", Default: "yes", Advanced: true,
		Description: "按分辨率自动生成皮肤，无需外部皮肤文件"},
	{Key: "skin.name", Label: "皮肤名称", Group: GroupDisplay, Type: "string", Default: "", Advanced: true,
		Description: "皮肤名，动态皮肤下等于「宽x高」"},
	{Key: "skin.path", Label: "皮肤路径", Group: GroupDisplay, Type: "string", Default: "", Advanced: true,
		Description: "自定义皮肤目录"},

	// ---------------- 输入
	{Key: "hw.keyboard", Label: "硬件键盘", Group: GroupInput, Type: "bool", Default: "yes",
		Description: "开启后可直接用电脑键盘输入中文/英文，强烈建议开启"},
	{Key: "hw.dPad", Label: "方向键 (D-Pad)", Group: GroupInput, Type: "bool", Default: "no", Advanced: true,
		Description: "是否模拟实体方向键"},
	{Key: "hw.mainKeys", Label: "实体导航键", Group: GroupInput, Type: "bool", Default: "no", Advanced: true,
		Description: "是否使用实体返回/主页键（现代设备为虚拟导航栏）"},
	{Key: "hw.trackBall", Label: "轨迹球", Group: GroupInput, Type: "bool", Default: "no", Advanced: true,
		Description: "仅老设备使用"},
	{Key: "hw.multitouch", Label: "多点触控", Group: GroupInput, Type: "enum", Default: "", Advanced: true,
		EnumValues:  []string{"", "none", "basic", "full"},
		Description: "多点触控能力，留空使用档案默认值"},

	// ---------------- 传感器
	{Key: "hw.accelerometer", Label: "加速度计", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "影响屏幕旋转与摇一摇类应用"},
	{Key: "hw.gyroscope", Label: "陀螺仪", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "影响 VR/AR 与体感类应用"},
	{Key: "hw.gps", Label: "GPS 定位", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "是否提供定位能力（可在运行时用扩展控制台改坐标）"},
	{Key: "hw.sensors.proximity", Label: "距离传感器", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "通话时息屏等场景使用"},
	{Key: "hw.sensors.light", Label: "光线传感器", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "自动亮度场景使用"},
	{Key: "hw.sensors.magnetic_field", Label: "磁场传感器", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "电子罗盘依赖"},
	{Key: "hw.sensors.orientation", Label: "方向传感器", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "屏幕方向判定依赖"},
	{Key: "hw.sensors.pressure", Label: "气压计", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "海拔类应用依赖"},
	{Key: "hw.sensors.temperature", Label: "温度传感器", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "模拟环境温度"},
	{Key: "hw.sensors.heart_rate", Label: "心率传感器", Group: GroupSensors, Type: "bool", Default: "", Advanced: true,
		Description: "穿戴设备常用"},
	{Key: "hw.battery", Label: "电池状态", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "提供电池信息接口"},
	{Key: "hw.audioInput", Label: "麦克风输入", Group: GroupSensors, Type: "bool", Default: "yes",
		Description: "是否允许模拟器使用宿主麦克风"},
	{Key: "hw.audioOutput", Label: "音频输出", Group: GroupSensors, Type: "bool", Default: "yes", Advanced: true,
		Description: "是否输出音频到宿主"},

	// ---------------- 相机
	{Key: "hw.camera.back", Label: "后置摄像头", Group: GroupCamera, Type: "enum", Default: "emulated",
		EnumValues:  []string{"none", "emulated", "virtualscene", "webcam0"},
		Description: "emulated 为静态模拟画面；virtualscene 为可交互 3D 场景；webcam0 使用宿主摄像头"},
	{Key: "hw.camera.front", Label: "前置摄像头", Group: GroupCamera, Type: "enum", Default: "emulated",
		EnumValues:  []string{"none", "emulated", "virtualscene", "webcam0"},
		Description: "同后置摄像头"},
	{Key: "hw.camera.back.orientation", Label: "后置摄像头方向", Group: GroupCamera, Type: "int", Default: "", Advanced: true,
		Description: "摄像头旋转角度"},

	// ---------------- 网络
	{Key: "runtime.network.latency", Label: "网络延迟", Group: GroupNetwork, Type: "enum", Default: "none",
		EnumValues:  []string{"none", "gprs", "edge", "umts", "hsdpa", "full"},
		Description: "模拟不同网络延迟，用于弱网测试"},
	{Key: "runtime.network.speed", Label: "网络速度", Group: GroupNetwork, Type: "enum", Default: "full",
		EnumValues:  []string{"full", "gsm", "hscsd", "gprs", "edge", "umts", "hsdpa", "lte"},
		Description: "模拟不同网络带宽，用于弱网测试"},
	{Key: "hw.dns1", Label: "DNS 服务器", Group: GroupNetwork, Type: "string", Default: "", Advanced: true,
		Description: "覆盖模拟器内使用的 DNS"},
	{Key: "hw.dns2", Label: "备用 DNS", Group: GroupNetwork, Type: "string", Default: "", Advanced: true,
		Description: "备用 DNS 服务器"},
	{Key: "hw.httpProxy", Label: "HTTP 代理", Group: GroupNetwork, Type: "string", Default: "", Advanced: true,
		Description: "模拟器内的 HTTP 代理地址，如 127.0.0.1:8888（用于抓包）"},

	// ---------------- 启动行为
	{Key: "fastboot.forceColdBoot", Label: "冷启动", Group: GroupBoot, Type: "bool", Default: "no",
		Description: "每次启动都从零开机（最干净但最慢）"},
	{Key: "fastboot.forceFastBoot", Label: "快速启动（快照）", Group: GroupBoot, Type: "bool", Default: "yes",
		Description: "从上次保存的快照恢复，秒级启动"},
	{Key: "fastboot.forceChosenSnapshotBoot", Label: "从指定快照启动", Group: GroupBoot, Type: "bool", Default: "no", Advanced: true,
		Description: "使用 fastboot.chosenSnapshotFile 指定的快照"},
	{Key: "fastboot.chosenSnapshotFile", Label: "指定快照文件", Group: GroupBoot, Type: "string", Default: "", Advanced: true,
		Description: "快照名称/文件"},
	{Key: "hw.arc", Label: "ARC 模式", Group: GroupBoot, Type: "bool", Default: "false", Advanced: true,
		Description: "ChromeOS ARC 相关，桌面场景一般不需要"},
}

// SchemaByKey 返回 key → 配置项 的映射。
func SchemaByKey() map[string]domain.HwConfigItem {
	m := make(map[string]domain.HwConfigItem, len(Schema))
	for _, it := range Schema {
		m[it.Key] = it
	}
	return m
}

// SchemaGroups 按分组返回配置项（保持 Schema 中的顺序）。
func SchemaGroups() map[string][]domain.HwConfigItem {
	out := map[string][]domain.HwConfigItem{}
	for _, it := range Schema {
		out[it.Group] = append(out[it.Group], it)
	}
	return out
}

// GroupOrder 是 UI 中分组的展示顺序。
var GroupOrder = []string{GroupBasic, GroupCompute, GroupStorage, GroupDisplay, GroupInput, GroupSensors, GroupCamera, GroupNetwork, GroupBoot}

// ScreenPresets 是常见分辨率预设（创建向导用）。
var ScreenPresets = []struct {
	Label   string
	Width   int
	Height  int
	Density int
}{
	{"手机 1080 × 2400 (420dpi)", 1080, 2400, 420},
	{"手机 1080 × 1920 (420dpi)", 1080, 1920, 420},
	{"手机 720 × 1280 (320dpi)", 720, 1280, 320},
	{"手机 1440 × 3120 (560dpi)", 1440, 3120, 560},
	{"平板 1600 × 2560 (320dpi)", 1600, 2560, 320},
	{"平板 1200 × 1920 (240dpi)", 1200, 1920, 240},
	{"桌面 1920 × 1080 (160dpi)", 1920, 1080, 160},
	{"电视 1920 × 1080 (320dpi)", 1920, 1080, 320},
	{"手表 454 × 454 (320dpi)", 454, 454, 320},
}

// DefaultHW 返回新建 AVD 的推荐硬件配置（向导的初始值）。
//
// 取值参考实测的本机 AVD（medium_phone / android-36.1）并做了保守化处理。
func DefaultHW(profileID string, ramMB, cores int) map[string]string {
	if ramMB <= 0 {
		ramMB = 2048
	}
	if cores <= 0 {
		cores = 4
	}
	return map[string]string{
		"hw.ramSize":                       itoa(ramMB),
		"vm.heapSize":                      itoa(defaultHeap(ramMB)),
		"hw.cpu.ncore":                     itoa(cores),
		"hw.gpu.enabled":                   "yes",
		"hw.gpu.mode":                      "auto",
		"disk.dataPartition.size":          "6G",
		"disk.cachePartition":              "yes",
		"disk.cachePartition.size":         "66MB",
		"hw.sdCard":                        "yes",
		"sdcard.size":                      "512M",
		"hw.lcd.width":                     "1080",
		"hw.lcd.height":                    "2400",
		"hw.lcd.density":                   "420",
		"hw.initialOrientation":            "portrait",
		"showDeviceFrame":                  "yes",
		"skin.dynamic":                     "yes",
		"hw.keyboard":                      "yes",
		"hw.accelerometer":                 "yes",
		"hw.gyroscope":                     "yes",
		"hw.gps":                           "yes",
		"hw.sensors.proximity":             "yes",
		"hw.sensors.light":                 "yes",
		"hw.sensors.magnetic_field":        "yes",
		"hw.sensors.orientation":           "yes",
		"hw.sensors.pressure":              "yes",
		"hw.battery":                       "yes",
		"hw.audioInput":                    "yes",
		"hw.camera.back":                   "virtualscene",
		"hw.camera.front":                  "emulated",
		"runtime.network.latency":          "none",
		"runtime.network.speed":            "full",
		"fastboot.forceColdBoot":           "no",
		"fastboot.forceFastBoot":           "yes",
		"fastboot.forceChosenSnapshotBoot": "no",
	}
}

// defaultHeap 依据 RAM 推荐 Java 堆（与官方启发式一致：约为 RAM 的 1/6）。
func defaultHeap(ramMB int) int {
	if ramMB < 512 {
		return 48
	}
	heap := ramMB / 6
	if heap < 64 {
		return 64
	}
	if heap > 512 {
		return 512
	}
	return heap
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
