// 领域类型别名：把 Wails 生成的 namespace 类型收敛成扁平名称，便于组件使用。
import type { domain, service } from "../../wailsjs/go/models";

// 注意：Wails 生成的 class 带实例方法 convertValues（只用于“发送”请求），
// 前端消费的响应对象是纯数据，这里去掉该方法，便于用普通对象字面量构造/归一化。
export type EnvReport = Omit<domain.EnvReport, "convertValues">;

// 以下别名型在 Go 侧是 `type X string`，Wails 会退化为 string，因此在前端显式声明联合类型。
export type AvdState = "stopped" | "starting" | "booting" | "running" | "stopping" | "error";

export type SystemImage = domain.SystemImage;
export type DeviceProfile = domain.DeviceProfile;
export type AvdSpec = domain.AvdSpec;
export type AvdSummary = domain.AvdSummary;
export type EmulatorInstance = domain.EmulatorInstance;

export type JobInfo = domain.JobInfo;
export type LogLine = domain.LogLine;
export type AppSettings = domain.AppSettings;

export type ResolvedPaths = service.ResolvedPaths;

/** 导航页签名：只保留核心流程所需的两个页面。 */
export type PageKey = "devices" | "settings";

/** Toast 模型（本地 UI 状态）。 */
export interface Toast {
  id: number;
  level: "info" | "success" | "warning" | "danger";
  title: string;
  text?: string;
}
