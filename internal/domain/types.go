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
	ToolCmdlineTools ToolID = "cmdline-tools"
	ToolSdkmanager   ToolID = "sdkmanager"
	ToolAvdmanager   ToolID = "avdmanager"
	ToolPlatformTool ToolID = "platform-tools"
	ToolEmulator     ToolID = "emulator"
	ToolSystemImages ToolID = "system-images"
	ToolAcceleration ToolID = "accel"
	ToolDisk         ToolID = "disk"
	ToolAvdHome      ToolID = "avd-home"
)

// ToolState 描述组件状态，驱动首页卡片的状态徽标与修复入口。
type ToolState string

const (
	StateUnknown      ToolState = "unknown"
	StateMissing      ToolState = "missing"
	StatePresent      ToolState = "present"
	StateOutdated     ToolState = "outdated"
	StateBroken       ToolState = "broken"
	StateIncompatible ToolState = "incompatible"
)

// FixKind 是首页卡片上"修复"按钮的动作类型。
type FixKind string

const (
	FixInstall       FixKind = "install"
	FixUpdate        FixKind = "update"
	FixRepair        FixKind = "repair"
	FixSetJDK        FixKind = "setJdk"
	FixEnableWHPX    FixKind = "enableWhpx"
	FixInstallAEHD   FixKind = "installAehd"
	FixDownloadImage FixKind = "downloadImage"
	FixChoosePath    FixKind = "choosePath"
	FixOpenSpeedTest FixKind = "openSpeedTest"
)

// ToolFix 是组件卡片的可执行修复动作。
type ToolFix struct {
	Kind    FixKind `json:"kind"`
	Label   string  `json:"label"`
	Payload string  `json:"payload,omitempty"`
}

// ToolStatus 是一个组件的检测结果。
type ToolStatus struct {
	ID      ToolID            `json:"id"`
	Name    string            `json:"name"`
	State   ToolState         `json:"state"`
	Version string            `json:"version,omitempty"`
	Path    string            `json:"path,omitempty"`
	Detail  string            `json:"detail,omitempty"`
	Fix     *ToolFix          `json:"fix,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// AccelInfo 来自 `emulator -accel-check`。
type AccelInfo struct {
	Available bool     `json:"available"`
	Kind      string   `json:"kind"` // whpx | aehd | haxm | gvm | none | unknown
	Raw       string   `json:"raw"`
	Hints     []string `json:"hints,omitempty"`
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
	Windows  string `json:"windows,omitempty"`
	CPUModel string `json:"cpuModel,omitempty"`
	CPUCores int    `json:"cpuCores,omitempty"`
	MemoryGB int64  `json:"memoryGB,omitempty"`
}

// SdkRootCandidate 是一个候选 SDK 根目录及其有效性评分。
type SdkRootCandidate struct {
	Path             string `json:"path"`
	Source           string `json:"source"`
	Score            int    `json:"score"`
	Exists           bool   `json:"exists"`
	HasCmdlineTools  bool   `json:"hasCmdlineTools"`
	HasPlatformTools bool   `json:"hasPlatformTools"`
	HasEmulator      bool   `json:"hasEmulator"`
	Version          string `json:"version,omitempty"`
}

// SdkRootValidation 手动指定 SDK 路径时的校验结果。
type SdkRootValidation struct {
	Path     string   `json:"path"`
	OK       bool     `json:"ok"`
	Writable bool     `json:"writable"`
	Found    []string `json:"found"`   // 发现的组件
	Missing  []string `json:"missing"` // 缺失的关键组件
	Message  string   `json:"message"`
}

// AvdHomeInfo 是 AVD 主目录的解析结果（含来源说明，供 UI 解释"为什么是这个目录"）。
type AvdHomeInfo struct {
	Path     string `json:"path"`
	Source   string `json:"source"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
	Count    int    `json:"count"`
}

// EnvReport 是首页所需的完整自检结果。
type EnvReport struct {
	SdkRoot       string       `json:"sdkRoot"`
	SdkRootSource string       `json:"sdkRootSource"`
	JdkPath       string       `json:"jdkPath"`
	AvdHome       AvdHomeInfo  `json:"avdHome"`
	Components    []ToolStatus `json:"components"`
	Accel         AccelInfo    `json:"accel"`
	Disks         []DiskInfo   `json:"disks"`
	Host          HostInfo     `json:"host"`
	Ready         bool         `json:"ready"`
	Blockers      []ToolStatus `json:"blockers"`
	ScannedAt     int64        `json:"scannedAt"`
}

// ---------------------------------------------------------------- 镜像与测速

// MirrorKind 区分官方源与第三方镜像。
type MirrorKind string

const (
	MirrorOfficial MirrorKind = "official"
	MirrorMirror   MirrorKind = "mirror"
	MirrorCustom   MirrorKind = "custom"
)

// MirrorGrade 是镜像的兼容性分级（见 ARCHITECTURE.md ADR-04）。
type MirrorGrade string

const (
	GradeUnknown     MirrorGrade = "unknown"
	GradeFull        MirrorGrade = "full"
	GradeIndexOnly   MirrorGrade = "index-only"
	GradeInvalid     MirrorGrade = "invalid"
	GradeUnreachable MirrorGrade = "unreachable"
)

// MirrorSource 是一个下载源。
type MirrorSource struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	BaseURL    string       `json:"baseURL"`
	Kind       MirrorKind   `json:"kind"`
	Grade      MirrorGrade  `json:"grade"`
	Enabled    bool         `json:"enabled"`
	Note       string       `json:"note,omitempty"`
	Region     string       `json:"region,omitempty"`
	LastResult *SpeedResult `json:"lastResult,omitempty"`
}

// SpeedResult 是一次测速的完整结果。
type SpeedResult struct {
	SourceID        string   `json:"sourceId"`
	At              int64    `json:"at"`
	OK              bool     `json:"ok"`
	DNSMs           int64    `json:"dnsMs"`
	ResolveIPs      []string `json:"resolveIPs,omitempty"`
	ConnectMs       int64    `json:"connectMs"`
	TTFBMs          int64    `json:"ttfbMs"`
	HTTPStatus      int      `json:"httpStatus"`
	RangeSupported  bool     `json:"rangeSupported"`
	XMLOK           bool     `json:"xmlOK"`
	HasCmdlineTools bool     `json:"hasCmdlineTools"`
	HasEmulator     bool     `json:"hasEmulator"`
	HasSystemImages bool     `json:"hasSystemImages"`
	ThroughputMBps  float64  `json:"throughputMBps"`
	JitterMs        int64    `json:"jitterMs"`
	Score           int      `json:"score"`
	Grade           string   `json:"grade"` // recommended | usable | index-only | unusable
	Error           string   `json:"error,omitempty"`
}

// ---------------------------------------------------------------- SDK 包

// SdkPackage 是仓库中的一个包（远程或已安装）。
type SdkPackage struct {
	Path              string   `json:"path"`
	DisplayName       string   `json:"displayName"`
	Kind              string   `json:"kind"` // cmdline-tools | platform-tools | emulator | platforms | build-tools | system-images | extras
	Revision          string   `json:"revision"`
	Channel           string   `json:"channel"`
	SizeBytes         int64    `json:"sizeBytes"`
	ChecksumSHA1      string   `json:"checksumSHA1"`
	URL               string   `json:"url"`
	Installed         bool     `json:"installed"`
	InstalledRevision string   `json:"installedRevision,omitempty"`
	HasUpdate         bool     `json:"hasUpdate"`
	LicenseID         string   `json:"licenseId,omitempty"`
	Dependencies      []string `json:"dependencies,omitempty"`
	Obsolete          bool     `json:"obsolete"`
}

// SystemImage 是系统镜像包（系统镜像仓库的独立视图）。
type SystemImage struct {
	Path             string `json:"path"`
	APILevel         string `json:"apiLevel"`
	TagID            string `json:"tagId"`
	TagDisplay       string `json:"tagDisplay"`
	ABI              string `json:"abi"`
	Vendor           string `json:"vendor"`
	IsPlaystore      bool   `json:"isPlaystore"`
	Revision         string `json:"revision"`
	SizeBytes        int64  `json:"sizeBytes"`
	Installed        bool   `json:"installed"`
	HasUpdate        bool   `json:"hasUpdate"`
	RequiresEmulator string `json:"requiresEmulator,omitempty"`
}

// License 是 SDK 许可。
type License struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Accepted bool   `json:"accepted"`
}

// InstallPlan 是安装前的计划预览。
type InstallPlan struct {
	Steps      []PlanStep `json:"steps"`
	TotalBytes int64      `json:"totalBytes"`
	Licenses   []License  `json:"licenses"`
	Warnings   []string   `json:"warnings,omitempty"`
}

// PlanStep 是安装计划中的一步。
type PlanStep struct {
	Path      string `json:"path"`
	Action    string `json:"action"` // install | update | skip
	Reason    string `json:"reason"`
	SizeBytes int64  `json:"sizeBytes"`
	SourceURL string `json:"sourceURL"`
}

// VerifyResult 是已安装包完整性校验结果。
type VerifyResult struct {
	Path    string   `json:"path"`
	OK      bool     `json:"ok"`
	Details []string `json:"details"`
}

// ---------------------------------------------------------------- AVD

// DeviceProfile 是 avdmanager 提供的设备档案。
type DeviceProfile struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Name     string `json:"name"`
	OEM      string `json:"oem"`
	Tag      string `json:"tag"`
	Category string `json:"category"` // phone | tablet | desktop | tv | automotive | wear | xr | other
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Density  int    `json:"density,omitempty"`
	RAMMB    int    `json:"ramMB,omitempty"`
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
type AvdSummary struct {
	Name              string   `json:"name"`
	DisplayName       string   `json:"displayName"`
	Path              string   `json:"path"`
	Target            string   `json:"target"`
	APILevel          string   `json:"apiLevel"`
	TagID             string   `json:"tagId"`
	TagDisplay        string   `json:"tagDisplay"`
	ABI               string   `json:"abi"`
	DeviceProfileID   string   `json:"deviceProfileId"`
	DeviceProfileName string   `json:"deviceProfileName"`
	OEM               string   `json:"oem"`
	RAMMB             int      `json:"ramMB"`
	Cores             int      `json:"cores"`
	DataPartition     string   `json:"dataPartition"`
	SDCard            string   `json:"sdCard"`
	Width             int      `json:"width"`
	Height            int      `json:"height"`
	Density           int      `json:"density"`
	GPUEnabled        bool     `json:"gpuEnabled"`
	GPUMode           string   `json:"gpuMode"`
	Playstore         bool     `json:"playstore"`
	State             AvdState `json:"state"`
	InstanceID        string   `json:"instanceId,omitempty"`
	Serial            string   `json:"serial,omitempty"`
	Port              int      `json:"port,omitempty"`
	SizeBytes         int64    `json:"sizeBytes"`
	CreatedAt         int64    `json:"createdAt,omitempty"`
	LastUsedAt        int64    `json:"lastUsedAt,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Note              string   `json:"note,omitempty"`
	Broken            string   `json:"broken,omitempty"` // 非空表示该 AVD 配置有问题
}

// AvdDetail 是设备详情（含完整 config.ini 键值）。
type AvdDetail struct {
	Summary        AvdSummary        `json:"summary"`
	Config         map[string]string `json:"config"`
	RawConfig      string            `json:"rawConfig"`
	SystemImageDir string            `json:"systemImageDir"`
	MissingImage   bool              `json:"missingImage"`
}

// AvdSpec 是创建 AVD 的完整输入（创建向导提交）。
type AvdSpec struct {
	Name                 string            `json:"name"`
	DisplayName          string            `json:"displayName"`
	ProfileID            string            `json:"profileId"`
	SystemImagePath      string            `json:"systemImagePath"`
	Path                 string            `json:"path,omitempty"`
	SDCardSize           string            `json:"sdcardSize,omitempty"`
	HW                   map[string]string `json:"hw"`
	CreateWithAvdManager bool              `json:"createWithAvdManager"`
	Tags                 []string          `json:"tags,omitempty"`
	Note                 string            `json:"note,omitempty"`
	LaunchDefaults       *LaunchOptions    `json:"launchDefaults,omitempty"`
}

// AvdPatch 是对已有 AVD 的增量修改。
type AvdPatch struct {
	DisplayName *string           `json:"displayName,omitempty"`
	HW          map[string]string `json:"hw,omitempty"`
	RemoveHW    []string          `json:"removeHW,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Note        *string           `json:"note,omitempty"`
	Launch      *LaunchOptions    `json:"launch,omitempty"`
}

// HwConfigItem 是 config.ini 中一个硬件配置项的元数据（schema）。
type HwConfigItem struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Group       string   `json:"group"` // basic|compute|storage|display|input|sensors|camera|network|boot
	Type        string   `json:"type"`  // bool|int|string|enum|size
	Default     string   `json:"default"`
	EnumValues  []string `json:"enumValues,omitempty"`
	Min         int      `json:"min,omitempty"`
	Max         int      `json:"max,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Description string   `json:"description"`
	Advanced    bool     `json:"advanced"`
}

// NameValidation 是 AVD 名称校验结果。
type NameValidation struct {
	Name    string `json:"name"`
	Valid   bool   `json:"valid"`
	Reason  string `json:"reason,omitempty"`
	Suggest string `json:"suggest,omitempty"`
}

// ConfigDiff 是 ini 直编保存时的差异。
type ConfigDiff struct {
	Added    map[string]string `json:"added"`
	Changed  map[string]string `json:"changed"`
	Removed  []string          `json:"removed"`
	Warnings []string          `json:"warnings,omitempty"`
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

// Snapshot 是一个 AVD 快照。
type Snapshot struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"sizeBytes"`
	CreatedAt   int64  `json:"createdAt,omitempty"`
	Description string `json:"description,omitempty"`
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

// ProxyMode 下载代理模式。
type ProxyMode string

const (
	ProxyOff    ProxyMode = "off"
	ProxySystem ProxyMode = "system"
	ProxyCustom ProxyMode = "custom"
)

// AppSettings 是持久化到 settings.json 的用户设置。
type AppSettings struct {
	Version int `json:"version"`

	// 环境
	SdkRoot           string `json:"sdkRoot"`
	JdkPath           string `json:"jdkPath"`
	AvdHome           string `json:"avdHome"`
	InjectEnvForChild bool   `json:"injectEnvForChildren"`

	// 镜像
	ActiveSourceID         string         `json:"activeSourceId"`
	CustomSources          []MirrorSource `json:"customSources"`
	AutoFallbackToOfficial bool           `json:"autoFallbackToOfficial"`

	// 下载
	MaxConnectionsPerFile int       `json:"maxConnectionsPerFile"`
	MaxParallelPackages   int       `json:"maxParallelPackages"`
	TimeoutSeconds        int       `json:"timeoutSeconds"`
	SpeedLimitKBps        int       `json:"speedLimitKBps"`
	ProxyMode             ProxyMode `json:"proxyMode"`
	ProxyURL              string    `json:"proxyURL"`
	DownloadDir           string    `json:"downloadDir"`

	// 许可
	AcceptedLicenseIDs []string `json:"acceptedLicenseIds"`
	AutoAcceptLicenses bool     `json:"autoAcceptLicenses"`

	// AVD 默认值
	DefaultDeviceProfile  string `json:"defaultDeviceProfile"`
	DefaultRAMMB          int    `json:"defaultRamMB"`
	DefaultCores          int    `json:"defaultCores"`
	DefaultDataPartitionG string `json:"defaultDataPartitionGB"`
	DefaultGPUMode        string `json:"defaultGpuMode"`

	// 外观与交互
	Theme               string `json:"theme"`    // light|dark|system
	Language            string `json:"language"` // zh-CN|en-US
	DeviceViewMode      string `json:"deviceViewMode"`
	ConfirmBeforeDelete bool   `json:"confirmBeforeDelete"`
	ShowTaskDrawer      bool   `json:"showTaskDrawer"`

	// 高级
	LogLevel               string `json:"logLevel"`
	KeepLogDays            int    `json:"keepLogDays"`
	AskBeforeDriverInstall bool   `json:"askBeforeDriverInstall"`
}

// DiagnosticReport 是诊断包摘要。
type DiagnosticReport struct {
	GeneratedAt int64         `json:"generatedAt"`
	AppVersion  string        `json:"appVersion"`
	Env         EnvReport     `json:"env"`
	Settings    AppSettings   `json:"settings"`
	Checks      []CheckResult `json:"checks"`
}

// CheckResult 是冒烟自检的一项。
type CheckResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Detail    string `json:"detail"`
	ElapsedMs int64  `json:"elapsedMs"`
}
