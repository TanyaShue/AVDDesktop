# AVDDesktop 接口契约（Go ⇄ TypeScript）

> 所有前后端交互只通过这份契约。Go 侧定义在 `internal/service/*.go`，TS 侧类型在 `frontend/src/bridge/types.ts`。
> Wails 前端调用方式：`import { Detect } from "../wailsjs/go/service/EnvService"`

---

## 1. 约定

| 项 | 约定 |
| --- | --- |
| 绑定结构体 | 一个领域一个 Service 结构体（Wails 允许绑定多个），命名 `XxxService` |
| 方法返回 | `(T, error)`；错误统一包装为 `*domain.AppError`（Wails 会序列化为 `{code, message, hint, detail}`） |
| 长任务 | 返回 `jobID string`，立即返回；进度/日志走事件 |
| 命名 | 方法名为大驼峰；参数用单结构体（便于演进）：`Detect(req DetectRequest)` |
| 事件名 | `<domain>:<event>`，见 §4 |
| 时间 | 统一 Unix 毫秒（`int64` → TS `number`） |
| 空值 | 数值用 0 表示未知，字符串用 `""`；可选字段用 TS `?` |

```go
type AppError struct {
    Code    string   `json:"code"`    // 稳定错误码，前端据此决定 UI
    Message string   `json:"message"` // 面向用户（已本地化前的 key 或中文）
    Hint    string   `json:"hint"`    // 可操作建议
    Detail  string   `json:"detail"`  // 原始输出/堆栈（折叠展示）
    Actions []Action `json:"actions"` // 可点击的修复动作
}
type Action struct{ Kind, Label, Payload string }
```

**错误码表（节选，实现时补齐）**

| code | 含义 | 前端动作 |
| --- | --- | --- |
| `PATH_NOT_FOUND` | SDK/AVD 目录不存在 | 显示"选择目录" |
| `TOOL_MISSING` | 工具未安装 | 显示"安装"CTA |
| `JDK_MISSING` / `JDK_TOO_OLD` | 无 JDK / 版本过低 | 跳设置页设置 JDK 或使用无 JDK 模式 |
| `PERMISSION_DENIED` | 不可写 | 建议换目录 |
| `DISK_FULL` | 空间不足 | 显示所需空间 |
| `MIRROR_UNREACHABLE` / `MIRROR_INDEX_ONLY` | 镜像问题 | 触发测速弹窗 / 回退官方 |
| `CHECKSUM_MISMATCH` | 校验失败 | 重新下载 |
| `FILE_IN_USE` | 被运行实例占用 | 提示停止实例 |
| `ACCEL_UNAVAILABLE` | 无硬件加速 | 打开加速引导面板 |
| `PORT_EXHAUSTED` | 无可用端口 | 提示关闭实例 |
| `AVD_NAME_INVALID` / `AVD_EXISTS` | 名称问题 | 表单内联校验 |
| `IMAGE_NOT_INSTALLED` | 缺系统镜像 | 跳镜像安装 |
| `JOB_CANCELED` / `JOB_BUSY` | 任务取消 / 已有写任务 | 提示 |

---

## 2. EnvService（环境探测与宿主能力）

```go
// Detect 全量自检（并行执行，内部 3s 缓存）
func (s *EnvService) Detect(req DetectRequest) (*domain.EnvReport, error)
type DetectRequest struct {
    Force            bool   `json:"force"`            // 忽略缓存
    SdkRootOverride  string `json:"sdkRootOverride"`  // 临时指定根目录试探测
}

// DetectSdkRoots 返回候选 SDK 根目录及评分（供"选择 SDK 路径"弹窗）
func (s *EnvService) DetectSdkRoots() ([]domain.SdkRootCandidate, error)

func (s *EnvService) DetectJava() (*domain.ToolStatus, error)          // JDK 详情
func (s *EnvService) CheckAcceleration() (*domain.AccelInfo, error)    // emulator -accel-check
func (s *EnvService) EnabledWindowsFeatures() ([]domain.WinFeature, error) // WHPX/Hyper-V/VirtualMachinePlatform
func (s *EnvService) DiskSpace(path string) (*domain.DiskInfo, error)
func (s *EnvService) ResolveAvdHome() (*domain.AvdHomeInfo, error)     // 含解析来源说明
func (s *EnvService) ValidateSdkRoot(path string) (*domain.SdkRootValidation, error)

// 宿主交互
func (s *EnvService) PickDirectory(req PickRequest) (string, error)    // 原生目录选择框
func (s *EnvService) PickFile(req PickRequest) (string, error)
func (s *EnvService) OpenInExplorer(path string) error                 // 目录直接打开、文件打开所在目录并选中
func (s *EnvService) OpenExternalURL(url string) error
func (s *EnvService) CopyToClipboard(text string) error
func (s *EnvService) OpenTerminal(req TerminalRequest) error           // 在 SDK/AVD 目录打开 cmd/pwsh
```

> ⚠️ 宿主目录/终端的打开必须走 `internal/platform`（Windows 用 `explorer.exe` / `ShellExecute`，
> macOS 用 `open`，Linux 用 `xdg-open`）。**不要**用 `wailsruntime.BrowserOpenURL` 拼 `file://`
> URL：Wails v2.16 的 `ValidateAndSanitizeURL` 会拒绝 `file` 方案以及含空格/反斜杠的路径，
> 而 `BrowserOpenURL` 没有返回值 —— 失败只写进 Wails 日志，界面上就是“点了没反应”。
> 路径不存在时退到最近的已存在父目录（不报错、也不创建目录），失败原因通过返回值交给 UI 弹 toast。

```ts
interface EnvReport {
  sdkRoot: string; sdkRootSource: string; jdkPath: string; avdHome: string; avdHomeSource: string;
  components: ToolStatus[];         // 见 §3 状态模型
  accel: AccelInfo; disks: DiskInfo[]; host: HostInfo;
  ready: boolean;                   // 是否能创建并启动 AVD
  blockers: ToolStatus[];           // 阻塞项（按优先级排序）
  scannedAt: number;
}
interface ToolStatus {
  id: "jdk"|"cmdline-tools"|"sdkmanager"|"avdmanager"|"platform-tools"|"emulator"|"system-images"|"accel"|"disk"|"avd-home";
  name: string; state: "unknown"|"missing"|"present"|"outdated"|"broken"|"incompatible";
  version: string; path: string; detail: string;
  fix?: { kind: "install"|"update"|"repair"|"setJdk"|"enableWhpx"|"installAehd"|"downloadImage"|"choosePath"; label: string; payload?: string };
  meta?: Record<string, string|number|boolean>;
}
interface AccelInfo { available: boolean; kind: "whpx"|"aehd"|"haxm"|"gvm"|"none"|"unknown"; raw: string; hints: string[]; }
```

---

## 3. MirrorService（镜像源 + 测速）

```go
func (s *MirrorService) ListSources() ([]domain.MirrorSource, error)
func (s *MirrorService) AddSource(req AddSourceRequest) (*domain.MirrorSource, error)   // 自动规范化 URL + 兼容性预检
func (s *MirrorService) UpdateSource(req UpdateSourceRequest) (*domain.MirrorSource, error)
func (s *MirrorService) RemoveSource(id string) error
func (s *MirrorService) SetActiveSource(id string) error
func (s *MirrorService) TestSources(req TestRequest) (string, error)      // → jobID（流式结果）
func (s *MirrorService) CancelTest(jobID string) error
func (s *MirrorService) GetCachedResults() ([]domain.SpeedResult, error)
func (s *MirrorService) ProbeSource(req ProbeRequest) (*domain.SpeedResult, error)      // 单源同步快检（用于添加时校验）
```

```ts
interface MirrorSource {
  id: string; name: string; baseURL: string;
  kind: "official"|"mirror"|"custom";
  grade: "unknown"|"full"|"index-only"|"invalid"|"unreachable";
  enabled: boolean; note?: string; region?: string;
  lastResult?: SpeedResult;
}
interface SpeedResult {
  sourceId: string; at: number; ok: boolean;
  dnsMs: number; resolveIPs: string[]; connectMs: number; ttfbMs: number;
  httpStatus: number; rangeSupported: boolean; xmlOK: boolean;
  hasCmdlineTools: boolean; hasEmulator: boolean; hasSystemImages: boolean;
  throughputMBps: number; jitterMs: number;
  score: number;             // 0-100
  grade: "recommended"|"usable"|"index-only"|"unusable";
  error?: string;
}
interface TestRequest { sourceIds: string[]; maxThroughputMB: number; timeoutMs: number; }
```

**事件**：`speedtest:started` / `speedtest:result`(每源一条) / `speedtest:done`

---

## 4. SdkService（包管理与安装）

```go
// 索引
func (s *SdkService) ListInstalled() ([]domain.SdkPackage, error)
func (s *SdkService) ListRemote(req ListRemoteRequest) ([]domain.SdkPackage, error)
type ListRemoteRequest struct {
    Kinds    []string `json:"kinds"`    // ["cmdline-tools","platform-tools","emulator","platforms","build-tools","system-images"]
    Channel  string   `json:"channel"`  // stable|beta|canary
    SourceID string   `json:"sourceId"` // 空=当前活动源
    Refresh  bool     `json:"refresh"`
}
func (s *SdkService) ListSystemImages(req ListSystemImagesRequest) ([]domain.SystemImage, error)
func (s *SdkService) SystemImageTags() ([]domain.SysImgTag, error)   // google_apis / playstore / desktop / tv ...

// 许可
func (s *SdkService) ListLicenses(req ListLicensesRequest) ([]domain.License, error)  // 含文本与是否已接受
func (s *SdkService) AcceptLicenses(ids []string) error

// 安装
func (s *SdkService) PlanInstall(req InstallRequest) (*domain.InstallPlan, error)     // 展示依赖/总大小/许可
func (s *SdkService) Install(req InstallRequest) (string, error)                     // → jobID
func (s *SdkService) BootstrapCmdlineTools(req BootstrapRequest) (string, error)      // → jobID（无 cmdline-tools 时的特例路径）
func (s *SdkService) Uninstall(req UninstallRequest) (string, error)                  // → jobID
func (s *SdkService) CancelJob(jobID string) error
func (s *SdkService) VerifyPackage(path string) (*domain.VerifyResult, error)         // 校验已装包完整性
```

```ts
interface SdkPackage {
  path: string;              // "system-images;android-36.1;google_apis_playstore;x86_64"
  displayName: string; kind: string; revision: string; channel: string;
  sizeBytes: number; checksumSHA1: string; url: string;
  installed: boolean; installedRevision?: string; hasUpdate: boolean;
  licenseId: string; dependencies: string[]; obsolete: boolean;
}
interface SystemImage {
  path: string; apiLevel: string; tagId: string; tagDisplay: string; abi: string;
  vendor: string; isPlaystore: boolean; revision: string; sizeBytes: number;
  installed: boolean; hasUpdate: boolean; requiresEmulator?: string;
}
interface InstallPlan { steps: PlanStep[]; totalBytes: number; licenses: domain.License[]; warnings: string[]; }
interface PlanStep { path: string; action: "install"|"update"|"skip"; reason: string; sizeBytes: number; sourceURL: string; }
interface InstallRequest { packages: string[]; sourceId?: string; allowFallbackToOfficial: boolean; autoAcceptLicenses: boolean; }
interface BootstrapRequest { sourceId?: string; withPlatformTools: boolean; withEmulator: boolean; emulatorChannel: string; }
```

**事件**：`job:progress` / `job:log` / `job:done` / `job:failed`（见 §6）

---

## 5. AvdService / EmulatorService / AdbService

### 5.1 AvdService

```go
func (s *AvdService) List() ([]domain.AvdSummary, error)
func (s *AvdService) Get(name string) (*domain.AvdDetail, error)
func (s *AvdService) ListProfiles(refresh bool) ([]domain.DeviceProfile, error)
func (s *AvdService) ListConfigSchema() ([]domain.HwConfigItem, error)   // 硬件项 schema（含默认值/范围/中文名）
func (s *AvdService) ValidateName(name string) (*domain.NameValidation, error)
func (s *AvdService) Create(spec domain.AvdSpec) (string, error)         // → jobID
func (s *AvdService) Update(name string, patch domain.AvdPatch) error
func (s *AvdService) Clone(req CloneRequest) (string, error)             // → jobID
func (s *AvdService) Rename(oldName, newName string) error
func (s *AvdService) Delete(req DeleteRequest) (string, error)           // → jobID（可选含文件）
func (s *AvdService) WipeData(name string) (string, error)
func (s *AvdService) ReadConfigRaw(name string) (string, error)          // 供"ini 直编"
func (s *AvdService) WriteConfigRaw(name string, content string) (*domain.ConfigDiff, error)
func (s *AvdService) Export(req ExportRequest) (string, error)           // → jobID（zip）
func (s *AvdService) Import(req ImportRequest) (string, error)
func (s *AvdService) OpenFolder(name string) error
func (s *AvdService) ComputeCommand(spec domain.AvdSpec) (string, error) // 展示等效 avdmanager/emulator 命令
```

```ts
interface AvdSummary {
  name: string; displayName: string; path: string; target: string;
  apiLevel: string; tagId: string; tagDisplay: string; abi: string;
  deviceProfileId: string; deviceProfileName: string; oem: string;
  ramMB: number; cores: number; dataPartitionGB: string; sdCard: string;
  width: number; height: number; density: number;
  gpuMode: string; playstore: boolean;
  state: "stopped"|"starting"|"booting"|"running"|"stopping"|"error";
  instanceId?: string; serial?: string; port?: number;
  sizeBytes: number; lastUsedAt?: number; createdAt?: number;
  tags?: string[]; note?: string;
}
interface AvdSpec {
  name: string; displayName: string; profileId: string; systemImagePath: string;
  path?: string;                // 默认落 avdHome
  sdcardSize?: string;          // "512M"/"2048M"
  hw: Record<string, string>;   // config.ini 覆盖项
  createWithAvdManager: boolean; // 是否优先使用 avdmanager
  launchDefaults?: LaunchOptions;
}
interface HwConfigItem {
  key: string; label: string; group: string;  // group: basic|compute|storage|display|input|sensors|camera|network|boot
  type: "bool"|"int"|"string"|"enum"|"size";
  default: string; enumValues?: string[]; min?: number; max?: number; unit?: string;
  description: string; advanced: boolean;       // 是否默认折叠
}
```

### 5.2 EmulatorService

```go
func (s *EmulatorService) Start(req StartRequest) (*domain.EmulatorInstance, error)
func (s *EmulatorService) Stop(instanceID string, force bool) error
func (s *EmulatorService) Restart(instanceID string, opts LaunchOptions) error
func (s *EmulatorService) ListRunning() ([]domain.EmulatorInstance, error)
func (s *EmulatorService) GetLog(instanceID string, tail int) ([]domain.LogLine, error)
func (s *EmulatorService) SendKey(instanceID string, key string) error         // 音量/电源/返回...
func (s *EmulatorService) Rotate(instanceID string, orientation string) error
func (s *EmulatorService) SetGps(instanceID string, lat, lon float64) error
func (s *EmulatorService) SetNetwork(instanceID string, speed, delay string) error
func (s *EmulatorService) SetBattery(instanceID string, level int, charging bool) error
func (s *EmulatorService) Screenshot(instanceID string) (string, error)        // base64 PNG
func (s *EmulatorService) OpenEmulatorWindow(instanceID string) error
// 快照
func (s *EmulatorService) Snapshots(avdName string) ([]domain.Snapshot, error)
func (s *EmulatorService) SaveSnapshot(avdName, name string) (string, error)   // → jobID
func (s *EmulatorService) LoadSnapshot(avdName, name string) (string, error)
func (s *EmulatorService) DeleteSnapshot(avdName, name string) error
```

```ts
interface LaunchOptions {
  coldBoot?: boolean; wipeData?: boolean; noWindow?: boolean;
  gpuMode?: "auto"|"host"|"swiftshader_indirect"|"angle_indirect"|"guest";
  snapshotName?: string; writableSystem?: boolean; noBootAnim?: boolean;
  netSpeed?: string; netDelay?: string; dnsServers?: string[]; httpProxy?: string;
  timezone?: string; locale?: string; memoryMB?: number; cores?: number;
  port?: number; extraArgs?: string[];        // 原样追加（带校验）
  scale?: string; noAudio?: boolean;
}
interface EmulatorInstance {
  id: string; avdName: string; serial: string; port: number; adbPort: number; pid: number;
  state: "starting"|"booting"|"running"|"stopping"|"stopped"|"failed";
  startedAt: number; bootCompletedAt?: number; exitCode?: number;
  lastError?: string; args: string[]; logsPath: string;
}
```

### 5.3 AdbService

```go
func (s *AdbService) Devices() ([]domain.AdbDevice, error)
func (s *AdbService) InstallApk(req InstallApkRequest) (string, error)   // → jobID
func (s *AdbService) Push(serial, local, remote string) (string, error)
func (s *AdbService) Pull(serial, remote, local string) (string, error)
func (s *AdbService) Shell(serial, cmd string) (string, error)
func (s *AdbService) Root(serial string) error
func (s *AdbService) Remount(serial string) error
func (s *AdbService) StartLogcat(serial string, filter string) (string, error)  // jobID，持续 push 日志
func (s *AdbService) StopLogcat(jobID string) error
func (s *AdbService) KillServer() error
func (s *AdbService) StartServer() error
```

---

## 6. Job 与事件协议

```ts
type JobInfo = {
  id: string;
  kind: "speedtest"|"download"|"install"|"bootstrap"|"avd-create"|"avd-delete"|"avd-clone"
      | "snapshot"|"emulator-start"|"logcat"|"export"|"diagnostics";
  title: string; subtitle: string; group?: string;     // group 用于"一键准备环境"的步骤分组
  status: "queued"|"running"|"succeeded"|"failed"|"canceled";
  phase: string;                                       // "解析索引" / "下载 emulator" / "解压" / "校验"
  percent: number;                                      // 0-100
  bytesDone: number; bytesTotal: number; speedBps: number; etaSeconds: number;
  itemsDone: number; itemsTotal: number;
  startedAt: number; endedAt?: number;
  error?: AppError;
};
```

| 事件名 | 载荷 | 说明 |
| --- | --- | --- |
| `job:created` | `JobInfo` | 新任务入队 |
| `job:progress` | `JobInfo` | 节流 ≤10 Hz |
| `job:log` | `{jobId, lines: LogLine[]}` | 批量 ≤200 ms |
| `job:done` | `JobInfo` | 成功结束 |
| `job:failed` | `JobInfo` | 失败（含 `error`） |
| `speedtest:result` | `SpeedResult` | 每源一条 |
| `emulator:state` | `EmulatorInstance` | 状态机变化 |
| `avd:changed` | `{action, name}` | AVD 增删改 |
| `sdk:changed` | `{installed?, skipped?, failed?, uninstalled?, repaired?}` | 包安装/卸载/元数据补写 |
| `env:changed` | `EnvReport` | 自检结果变化（文件系统监听或手动触发） |
| `logcat:line` | `{jobId, serial, line}` | 日志流 |

**TS 用法**

```ts
import { EventsOn, EventsOff } from "../wailsjs/runtime/runtime";
const off = EventsOn("job:progress", (j: JobInfo) => store.upsert(j));
// 组件卸载：EventsOff("job:progress")
```

---

## 7. SettingsService / DiagnosticsService

```go
func (s *SettingsService) Get() (*domain.AppSettings, error)
func (s *SettingsService) Update(patch map[string]any) (*domain.AppSettings, error)  // 浅合并 + 校验
func (s *SettingsService) Reset(section string) (*domain.AppSettings, error)
func (s *SettingsService) ExportSettings() (string, error)
func (s *SettingsService) ImportSettings(path string) (*domain.AppSettings, error)

func (s *DiagnosticsService) EnvSummary() (*domain.DiagnosticReport, error)
func (s *DiagnosticsService) ExportReport() (string, error)      // → zip 路径（设置+环境+日志）
func (s *DiagnosticsService) ReadAppLog(tail int) ([]domain.LogLine, error)
func (s *DiagnosticsService) ClearCache() error
func (s *DiagnosticsService) RunSelfCheck() (string, error)      // → jobID：跑一次端到端冒烟检查
```

---

## 8. TS 类型映射约定

| Go | TS |
| --- | --- |
| `string` | `string` |
| `int/int64` | `number` |
| `float64` | `number` |
| `bool` | `boolean` |
| `[]T` | `T[]` |
| `map[string]string` | `Record<string, string>` |
| `*T` 空值 | `T \| undefined`（Wails JSON 会省略 `omitempty` 字段） |
| `time.Time` | `number`（项目统一改为 Unix ms 传递，避免时区问题） |

> **注意**：Go 的 `json:"..."` tag 决定 TS 字段名，统一小驼峰（`sdkRoot`、`bytesDone`）。
> 生成方式：M0 阶段手写 `frontend/src/bridge/types.ts`；后续可加 `go generate` 从 `domain` 自动生成，避免漂移。
