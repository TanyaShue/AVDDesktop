// 前后端桥接层：统一 re-export Wails 生成的服务绑定。
//
// 约定：
//   - 页面/组件只从本文件导入后端能力，绝不直接 import wailsjs 深层路径
//   - wailsjs 由 `wails generate module` / `wails dev` 自动生成，勿手改

export * as Env from "../../wailsjs/go/service/EnvService";
export * as Avd from "../../wailsjs/go/service/AvdService";
export * as Emulator from "../../wailsjs/go/service/EmulatorService";
export * as Settings from "../../wailsjs/go/service/SettingsService";
export * as Logs from "../../wailsjs/go/service/LogService";
export * as Jobs from "../../wailsjs/go/service/JobService";
export * as Win from "../../wailsjs/go/service/WindowService";

import type { EnvReport } from "./types";

export { EventsOn, EventsOff, EventsEmit } from "../../wailsjs/runtime/runtime";

/**
 * 把可能为 null 的数组兜底成空数组。
 *
 * Wails 绑定层把 Go 的 nil 切片序列化成 null，前端一旦直接 `arr.length` / `arr.map(...)`
 * 就会抛异常，React 会卸载整棵树（表现为整窗白屏）。
 * 后端也做了 NonNil 归一化，这里是第二道防线。
 */
export function asArray<T>(value: readonly T[] | null | undefined): T[] {
  return Array.isArray(value) ? (value as T[]) : [];
}

/** EnvReport 归一化：补齐全部数组字段，页面可以安全地当数组使用。 */
export function normalizeEnvReport(report: EnvReport): EnvReport {
  return {
    ...report,
    components: asArray(report.components),
    issues: asArray(report.issues),
    accel: report.accel ? { ...report.accel, hints: asArray(report.accel.hints) } : report.accel,
  };
}

/** 事件名常量。 */
export const EVENTS = {
  jobCreated: "job:created",
  jobProgress: "job:progress",
  jobLog: "job:log",
  jobDone: "job:done",
  jobFailed: "job:failed",
  emulatorState: "emulator:state",
  avdChanged: "avd:changed",
  envChanged: "env:changed",
  logLine: "log:line",
} as const;

/** 把 AppError 渲染成可读文本。 */
export function errorText(err: unknown): string {
  if (!err) return "";
  if (typeof err === "string") return err;
  const e = err as {
    code?: string;
    message?: string;
    hint?: string;
    detail?: string;
  };
  const parts = [e.message ?? "操作失败"];
  if (e.hint) parts.push(e.hint);
  return parts.join("　");
}
