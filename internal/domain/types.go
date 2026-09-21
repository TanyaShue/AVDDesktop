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
	ToolSystemImages ToolID = "system-images"
	ToolAcceleration ToolID = "accel"
)

// ToolState 描述组件状态，驱动设置页环境检查的状态徽标。
type ToolState string

const (
	StateMissing  ToolState = "missing"
	StatePresent  ToolState = "present"
	StateOutdated ToolState = "outdated"
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
	Kind      string   `json:"kind"` // whpx | aehd | haxm | gvm | none | unknown
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
	SdkRoot  string `json:"sdkRoot"`
	AvdHome  string `json:"avdHome"`
	JavaPath string `json:"javaPath,omitempty"`

	// Ready 表示创建 AVD 与启动模拟器所需的工具链已齐备。
	Ready bool `json:"ready"`
	// NeedInit 表示软件自己的 SDK 尚未初始化（缺少 sdkmanager）。
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

// ---------------------------------------------------------------- 镜像与测速
// ---------------------------------------------------------------- System Image

// SystemImage 是可以用来创建 AVD 的系统镜像（数据来自官方 sdkmanager 的包列表）。
type SystemImage struct {
	Path        string `json:"path"`
	API         string `json:"api"`
	Tag         string `json:"tag"`
	ABI         string `json:"abi"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Installed   bool   `json:"installed"`
}

// ---------------------------------------------------------------- AVD

// DeviceProfile 是 avdmanager 提供的设备档案。
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

// AvdSummary 是设备列表卡片的数据。
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
}

// AvdSpec 是创建 AVD 的输入（只需要名称、系统镜像与可选的设备档案）。
type AvdSpec struct {
	Name            string `json:"name"`
	SystemImagePath string `json:"systemImagePath"`
	ProfileID       string `json:"profileId,omitempty"`
}

// NameValidation 是 AVD 名称校验结果。
type NameValidation struct {
	Name    string `json:"name"`
	Valid   bool   `json:"valid"`
	Reason  string `json:"reason,omitempty"`
	Suggest string `json:"suggest,omitempty"`
}

// ---------------------------------------------------------------- 模拟器实例

// LaunchOptions 是启动参数（向导与启动按钮共用）。
type LaunchOptions struct {
	ColdBoot       bool     `json:"coldBoot"`
	WipeData       bool     `json:"wipeData"`
	NoWindow       bool     `json:"noWindow"`
	NoAudio        bool     `json:"noAudio"`
	NoBootAnim     bool     `json:"noBootAnim"`
	GPUMode        string   `json:"gpuMode,omitempty"`
	SnapshotName   string   `json:"snapshotName,omitempty"`
	WritableSystem bool     `json:"writableSystem"`
	NetSpeed       string   `json:"netSpeed,omitempty"`
	NetDelay       string   `json:"netDelay,omitempty"`
	DNSServers     []string `json:"dnsServers,omitempty"`
	HTTPProxy      string   `json:"httpProxy,omitempty"`
	Timezone       string   `json:"timezone,omitempty"`
	Locale         string   `json:"locale,omitempty"`
	MemoryMB       int      `json:"memoryMB,omitempty"`
	Cores          int      `json:"cores,omitempty"`
	Port           int      `json:"port,omitempty"`
	Scale          string   `json:"scale,omitempty"`
	ExtraArgs      []string `json:"extraArgs,omitempty"`
}

// EmulatorInstance 是一个运行中的模拟器实例。
type EmulatorInstance struct {
	ID              string   `json:"id"`
	AvdName         string   `json:"avdName"`
	Serial          string   `json:"serial"`
	Port            int      `json:"port"`
	ADBPort         int      `json:"adbPort"`
	PID             int      `json:"pid"`
	State           AvdState `json:"state"`
	StartedAt       int64    `json:"startedAt"`
	BootCompletedAt int64    `json:"bootCompletedAt,omitempty"`
	ExitCode        *int     `json:"exitCode,omitempty"`
	LastError       string   `json:"lastError,omitempty"`
	Args            []string `json:"args"`
	LogsPath        string   `json:"logsPath"`
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
	InstanceID  string `json:"instanceId,omitempty"`
}

// ---------------------------------------------------------------- 任务与设置

// JobKind 任务类型。
type JobKind string

const (
	JobSpeedTest     JobKind = "speedtest"
	JobDownload      JobKind = "download"
	JobInstall       JobKind = "install"
	JobBootstrap     JobKind = "bootstrap"
	JobAvdCreate     JobKind = "avd-create"
	JobAvdDelete     JobKind = "avd-delete"
	JobAvdClone      JobKind = "avd-clone"
	JobSnapshot      JobKind = "snapshot"
	JobEmulatorStart JobKind = "emulator-start"
	JobLogcat        JobKind = "logcat"
	JobExport        JobKind = "export"
	JobDiagnostics   JobKind = "diagnostics"
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
	Group      string    `json:"group,omitempty"`
	Status     JobStatus `json:"status"`
	Phase      string    `json:"phase,omitempty"`
	Percent    float64   `json:"percent"`
	BytesDone  int64     `json:"bytesDone"`
	BytesTotal int64     `json:"bytesTotal"`
	SpeedBps   int64     `json:"speedBps"`
	ETASeconds int       `json:"etaSeconds"`
	ItemsDone  int       `json:"itemsDone"`
	ItemsTotal int       `json:"itemsTotal"`
	StartedAt  int64     `json:"startedAt"`
	EndedAt    int64     `json:"endedAt,omitempty"`
	Error      *AppError `json:"error,omitempty"`
}

// LogLine 是一条日志。
type LogLine struct {
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
}
