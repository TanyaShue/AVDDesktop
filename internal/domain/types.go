// Package domain 定义整个应用的领域模型。
//
// 约束：
//   - 本包不允许依赖任何其它内部包（保证可被前端类型生成工具消费）
//   - 所有对外字段使用小驼峰 json tag，与 frontend/src/bridge/types.ts 一一对应
//   - 时间统一使用 Unix 毫秒（int64），避免时区序列化差异
package domain

// ---------------------------------------------------------------- 环境 / 工具链

// ToolID 标识一个可检测的工具链组件。
type ToolID string

const (
	ToolJDK          ToolID = "jdk"
	ToolSdkmanager   ToolID = "sdkmanager"
	ToolAvdmanager   ToolID = "avdmanager"
	ToolAdb          ToolID = "adb"
	ToolEmulator     ToolID = "emulator"
	ToolAcceleration ToolID = "accel"
)

// ToolState 描述组件状态，驱动设置页环境检查的状态徽标。
type ToolState string

const (
	StateMissing ToolState = "missing"
	StatePresent ToolState = "present"
)

// FixKind 是环境检查项上的修复动作类型。
type FixKind string

const (
	// FixPrepare 自动准备 SDK（下载命令行工具 / 接受许可 / 安装基础组件）。
	FixPrepare FixKind = "prepare"
	// FixInstall 安装指定 SDK 包（Payload 为包路径，例如 system-images;android-34;google_apis;x86_64）。
	FixInstall FixKind = "install"
)

// ToolFix 是可执行的修复动作。
type ToolFix struct {
	Kind    FixKind `json:"kind"`
	Label   string  `json:"label"`
	Payload string  `json:"payload,omitempty"`
	// Command 是可复制到终端的手工命令（例如 sdkmanager 安装命令）。
	Command string `json:"command,omitempty"`
}

// ToolStatus 是一个组件的检测结果。
type ToolStatus struct {
	ID      ToolID    `json:"id"`
	Name    string    `json:"name"`
	State   ToolState `json:"state"`
	Version string    `json:"version,omitempty"`
	Path    string    `json:"path,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	Fix     *ToolFix  `json:"fix,omitempty"`
}

// AccelInfo 来自 `emulator -accel-check`。
type AccelInfo struct {
	Available bool     `json:"available"`
	Kind      string   `json:"kind"` // hvf | kvm | whpx | aehd | haxm | gvm | none
	Raw       string   `json:"raw"`
	Hints     []string `json:"hints,omitempty"`
}

// IssueSeverity 是环境问题的严重程度。
type IssueSeverity string

const (
	SeverityInfo    IssueSeverity = "info"
	SeverityWarning IssueSeverity = "warning"
	SeverityBlocker IssueSeverity = "blocker"
)

// EnvIssue 是一条可操作的环境问题。
type EnvIssue struct {
	ID         string        `json:"id"`
	Severity   IssueSeverity `json:"severity"`
	Title      string        `json:"title"`
	Detail     string        `json:"detail"`
	FixLabel   string        `json:"fixLabel,omitempty"`
	FixCommand string        `json:"fixCommand,omitempty"`
	FixKind    FixKind       `json:"fixKind,omitempty"`
	FixPayload string        `json:"fixPayload,omitempty"`
}

// MirrorSource 是一个可用于软件组件下载的镜像站（Android SDK 或 JDK）。
type MirrorSource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"baseURL"`
	Region   string `json:"region,omitempty"`
	Note     string `json:"note,omitempty"`
	Official bool   `json:"official,omitempty"`
	Active   bool   `json:"active,omitempty"`
}

// MirrorResource 是镜像站上一次资源探测的结果。
//
// Required 表示当前这台机器是否真的需要它；Available 表示镜像站是否提供了它。
// 这样同一张镜像表既能指导首次安装，也能用于已有环境的补装。
type MirrorResource struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	URL        string `json:"url,omitempty"`
	Required   bool   `json:"required"`
	Available  bool   `json:"available"`
	StatusCode int    `json:"statusCode,omitempty"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	LatencyMs  int64  `json:"latencyMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

// MirrorCheck 是一次镜像站连通性、延迟、吞吐与资源完整性检测。
type MirrorCheck struct {
	SourceID      string           `json:"sourceId"`
	SourceName    string           `json:"sourceName"`
	BaseURL       string           `json:"baseURL"`
	Region        string           `json:"region,omitempty"`
	Reachable     bool             `json:"reachable"`
	Compatible    bool             `json:"compatible"`
	Recommended   bool             `json:"recommended"`
	LatencyMs     int64            `json:"latencyMs"`
	ThroughputBps int64            `json:"throughputBps"`
	Resources     []MirrorResource `json:"resources"`
	CheckedAt     int64            `json:"checkedAt"`
	ElapsedMs     int64            `json:"elapsedMs"`
	Error         string           `json:"error,omitempty"`
}

// DiskInfo 描述一个分区/目录所在磁盘的空间。
type DiskInfo struct {
	Path       string `json:"path"`
	TotalGB    int64  `json:"totalGB"`
	FreeGB     int64  `json:"freeGB"`
	Sufficient bool   `json:"sufficient"`
}

// HostInfo 宿主环境摘要。
type HostInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	CPUCores int    `json:"cpuCores,omitempty"`
	MemoryGB int64  `json:"memoryGB,omitempty"`
}

// EnvReport 是唯一的环境检查结果（设置页与自动准备共用）。
type EnvReport struct {
	AppRoot  string `json:"appRoot"`
	JdkRoot  string `json:"jdkRoot"`
	SdkRoot  string `json:"sdkRoot"`
	AvdHome  string `json:"avdHome"`
	JavaPath string `json:"javaPath,omitempty"`
	// MirrorSourceID / MirrorSourceName 是当前 Android SDK 组件下载源。
	MirrorSourceID   string `json:"mirrorSourceId,omitempty"`
	MirrorSourceName string `json:"mirrorSourceName,omitempty"`
	// JDKMirrorSourceID / JDKMirrorSourceName 是当前 JDK 下载源。
	JDKMirrorSourceID   string `json:"jdkMirrorSourceId,omitempty"`
	JDKMirrorSourceName string `json:"jdkMirrorSourceName,omitempty"`

	// Ready 表示创建 AVD 与启动模拟器所需的工具链已齐备。
	Ready bool `json:"ready"`
	// NeedInit 表示软件自带 JDK 或 SDK 尚未初始化（缺少 JDK / sdkmanager）。
	NeedInit bool `json:"needInit"`

	Components []ToolStatus `json:"components"`
	Accel      *AccelInfo   `json:"accel,omitempty"`
	Disk       DiskInfo     `json:"disk"`
	Host       HostInfo     `json:"host"`

	// Images 是已安装的系统镜像数量，Avds 是已有设备数量。
	Images int `json:"images"`
	Avds   int `json:"avds"`

	Issues    []EnvIssue `json:"issues"`
	CheckedAt int64      `json:"checkedAt"`
	ElapsedMs int64      `json:"elapsedMs"`
}

// ---------------------------------------------------------------- System Image

// SystemImage 是可以用来创建 AVD 的系统镜像（数据来自官方 sdkmanager 的包列表）。
type SystemImage struct {
	Path        string `json:"path"`
	API         string `json:"api"`
	Tag         string `json:"tag"`
	ABI         string `json:"abi"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`

	// AndroidVersion 是可读的 Android 版本（例如 Android 14），供界面直接展示。
	AndroidVersion string `json:"androidVersion"`
	// RootSupported 表示该镜像是否支持 `adb root`。Google Play 镜像不可 root，其余镜像按可 root 展示。
	RootSupported bool `json:"rootSupported"`
	Installed     bool `json:"installed"`
}

// ---------------------------------------------------------------- AVD

// DeviceProfile 是 avdmanager 提供的设备档案。
type DeviceProfile struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
	Name  string `json:"name"`
	OEM   string `json:"oem"`
	Tag   string `json:"tag"`
}

// AvdState 是 AVD 的运行状态（由 EmulatorService 维护）。
type AvdState string

const (
	AvdStopped  AvdState = "stopped"
	AvdStarting AvdState = "starting"
	AvdBooting  AvdState = "booting"
	AvdRunning  AvdState = "running"
	AvdStopping AvdState = "stopping"
	AvdError    AvdState = "error"
)

// AvdSummary 是设备列表里的一行（只保留界面真正展示的信息）。
type AvdSummary struct {
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	API             string   `json:"api"`
	Tag             string   `json:"tag"`
	ABI             string   `json:"abi"`
	DeviceProfileID string   `json:"deviceProfileId"`
	State           AvdState `json:"state"`
	InstanceID      string   `json:"instanceId,omitempty"`
	Serial          string   `json:"serial,omitempty"`
	Port            int      `json:"port,omitempty"`
	Broken          string   `json:"broken,omitempty"` // 非空表示该 AVD 配置有问题

	// 以下字段来自 config.ini，用于在设备卡片上直接显示当前生效的硬件参数。
	RAMMB     int `json:"ramMb,omitempty"`
	CPUCores  int `json:"cpuCores,omitempty"`
	LCDWidth  int `json:"lcdWidth,omitempty"`
	LCDHeight int `json:"lcdHeight,omitempty"`
}

// AvdHardware 是创建 AVD 时可覆盖的设备档案硬件参数。
//
// 只保留最常用的三项：内存、CPU 核心与屏幕分辨率。零值表示「沿用设备档案的默认值」：
// 只有显式给出的字段才会写进 config.ini，其余参数仍由 avdmanager 按所选设备档案生成。
type AvdHardware struct {
	// RAMMB 是设备内存（MB），对应 config.ini 的 hw.ramSize。
	RAMMB int `json:"ramMb,omitempty"`
	// CPUCores 是虚拟 CPU 核心数，对应 hw.cpu.ncore。
	CPUCores int `json:"cpuCores,omitempty"`
	// LCDWidth / LCDHeight 是屏幕分辨率（px），对应 hw.lcd.width / hw.lcd.height。
	LCDWidth  int `json:"lcdWidth,omitempty"`
	LCDHeight int `json:"lcdHeight,omitempty"`
}

// AvdSpec 是创建 AVD 的输入（名称、系统镜像、可选设备档案与硬件覆盖项）。
type AvdSpec struct {
	Name            string       `json:"name"`
	SystemImagePath string       `json:"systemImagePath"`
	ProfileID       string       `json:"profileId,omitempty"`
	Hardware        *AvdHardware `json:"hardware,omitempty"`
}

// NameValidation 是 AVD 名称校验结果。
type NameValidation struct {
	Name    string `json:"name"`
	Valid   bool   `json:"valid"`
	Reason  string `json:"reason,omitempty"`
	Suggest string `json:"suggest,omitempty"`
}

// ---------------------------------------------------------------- 模拟器实例

// LaunchOptions 是启动参数（界面只暴露冷启动与无窗口两个开关；端口由启动器内部决定）。
type LaunchOptions struct {
	ColdBoot bool `json:"coldBoot"`
	NoWindow bool `json:"noWindow"`
}

// EmulatorInstance 是一个模拟器实例（只保留界面展示与诊断需要的字段）。
type EmulatorInstance struct {
	ID        string   `json:"id"`
	AvdName   string   `json:"avdName"`
	Serial    string   `json:"serial"`
	Port      int      `json:"port"`
	PID       int      `json:"pid"`
	State     AvdState `json:"state"`
	StartedAt int64    `json:"startedAt"`
	EndedAt   int64    `json:"endedAt,omitempty"`
	ExitCode  *int     `json:"exitCode,omitempty"`
	LastError string   `json:"lastError,omitempty"`
	Args      []string `json:"args"`
}

// AdbDevice 是 `adb devices -l` 中的一行。
type AdbDevice struct {
	Serial      string `json:"serial"`
	State       string `json:"state"` // device | offline | unauthorized | bootloader
	Product     string `json:"product,omitempty"`
	Model       string `json:"model,omitempty"`
	Device      string `json:"device,omitempty"`
	TransportID string `json:"transportId,omitempty"`
	IsEmulator  bool   `json:"isEmulator"`
}

// ---------------------------------------------------------------- 任务与设置

// JobKind 任务类型。
type JobKind string

const (
	JobInstall       JobKind = "install"
	JobBootstrap     JobKind = "bootstrap"
	JobAvdCreate     JobKind = "avd-create"
	JobAvdDelete     JobKind = "avd-delete"
	JobImageDelete   JobKind = "image-delete"
	JobEmulatorStart JobKind = "emulator-start"
)

// JobStatus 任务状态。
type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCanceled  JobStatus = "canceled"
)

// JobInfo 是推送给前端的任务快照。
type JobInfo struct {
	ID         string    `json:"id"`
	Kind       JobKind   `json:"kind"`
	Title      string    `json:"title"`
	Subtitle   string    `json:"subtitle,omitempty"`
	Status     JobStatus `json:"status"`
	Phase      string    `json:"phase,omitempty"`
	Percent    float64   `json:"percent"`
	BytesDone  int64     `json:"bytesDone"`
	BytesTotal int64     `json:"bytesTotal"`
	SpeedBps   int64     `json:"speedBps"`
	ETASeconds int       `json:"etaSeconds"`
	StartedAt  int64     `json:"startedAt"`
	EndedAt    int64     `json:"endedAt,omitempty"`
	Error      *AppError `json:"error,omitempty"`
}

// LogLine 是一条日志。
//
// Seq 是日志在其来源内的唯一序号：应用日志是进程内递增序号，
// 任务日志是该任务内的递增序号。前端控制台用它作为行 id（配合来源命名空间），
// 使历史回填与实时推送的同一行不会被重复插入，也不会误删同一时刻的相同内容。
type LogLine struct {
	Seq     uint64 `json:"seq,omitempty"`
	At      int64  `json:"at"`
	Level   string `json:"level"` // debug|info|warn|error
	Source  string `json:"source,omitempty"`
	Message string `json:"message"`
}

// AppSettings 是持久化到 settings.json 的用户设置。
type AppSettings struct {
	Version int `json:"version"`

	// 外观与交互
	Theme               string `json:"theme"` // light|dark|system
	ConfirmBeforeDelete bool   `json:"confirmBeforeDelete"`
	ShowTaskDrawer      bool   `json:"showTaskDrawer"`

	// 日志
	LogLevel    string `json:"logLevel"`
	KeepLogDays int    `json:"keepLogDays"`

	// MirrorSourceID 是 Android SDK 组件默认下载源；空值时使用内置默认源。
	MirrorSourceID string `json:"mirrorSourceId"`
	// JDKMirrorSourceID 是 JDK 默认下载源；空值时使用内置默认源。
	JDKMirrorSourceID string `json:"jdkMirrorSourceId"`
}
