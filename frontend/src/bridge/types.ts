// 领域类型别名：把 Wails 生成的 namespace 类型收敛成扁平名称，便于组件使用。
import type { domain, service } from "../../wailsjs/go/models";

export type { domain, service };

// 注意：Wails 生成的 class 带实例方法 convertValues（只用于“发送”请求），
// 前端消费的响应对象是纯数据，这里去掉该方法，便于用普通对象字面量构造/归一化。
export type EnvReport = Omit<domain.EnvReport, "convertValues">;
export type ToolStatus = Omit<domain.ToolStatus, "convertValues">;
export type AccelInfo = domain.AccelInfo;
export type DiskInfo = domain.DiskInfo;
export type AvdHomeInfo = domain.AvdHomeInfo;
export type SdkRootCandidate = domain.SdkRootCandidate;
export type SdkRootValidation = domain.SdkRootValidation;
export type WindowsInfo = domain.WindowsInfo;
export type EnvIssue = domain.EnvIssue;

// 以下别名型在 Go 侧是 `type X string`，Wails 会退化为 string，因此在前端显式声明联合类型。
export type ToolState = "unknown" | "missing" | "present" | "outdated" | "broken" | "incompatible";
export type IssueSeverity = "info" | "warning" | "blocker";
export type MirrorGrade = "unknown" | "full" | "index-only" | "invalid" | "unreachable";
export type AvdState = "stopped" | "starting" | "booting" | "running" | "stopping" | "error";
export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "canceled";

export type MirrorSource = domain.MirrorSource;
export type SpeedResult = domain.SpeedResult;

export type SdkPackage = domain.SdkPackage;
export type SystemImage = domain.SystemImage;
export type License = domain.License;
export type InstallPlan = domain.InstallPlan;

export type AvdSummary = domain.AvdSummary;
export type AvdDetail = domain.AvdDetail;
export type AvdSpec = domain.AvdSpec;
export type DeviceProfile = domain.DeviceProfile;
export type HwConfigItem = domain.HwConfigItem;
export type NameValidation = domain.NameValidation;
export type ConfigDiff = domain.ConfigDiff;
export type Snapshot = domain.Snapshot;

export type EmulatorInstance = domain.EmulatorInstance;
export type LaunchOptions = domain.LaunchOptions;
export type AdbDevice = domain.AdbDevice;

export type JobInfo = domain.JobInfo;
export type LogLine = domain.LogLine;
export type AppSettings = domain.AppSettings;
export type AppError = domain.AppError;

export type ResolvedPaths = service.ResolvedPaths;
export type DetectRequest = service.DetectRequest;
export type TestRequest = service.TestRequest;
export type InstallRequest = service.InstallRequest;
export type BootstrapRequest = service.BootstrapRequest;
export type CloneRequest = service.CloneRequest;
export type DeleteRequest = service.DeleteRequest;
export type ListRemoteRequest = service.ListRemoteRequest;
export type SysImgTagAvailability = service.SysImgTagAvailability;

/** 导航页签名。 */
export type PageKey = "home" | "devices" | "sdk" | "settings";

/** Toast 模型（本地 UI 状态）。 */
export interface Toast {
  id: number;
  level: "info" | "success" | "warning" | "danger";
  title: string;
  text?: string;
}
