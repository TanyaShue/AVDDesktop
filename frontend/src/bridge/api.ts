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

import { service as serviceModels } from "../../wailsjs/go/models";
import type { domain } from "../../wailsjs/go/models";
import type { EnvReport } from "./types";

/**
 * 构造带嵌套字段的请求对象。
 *
 * Wails 为含嵌套结构的 Go struct 生成的 TS class 带有 convertValues 实例方法，
 * 因此普通对象字面量无法直接传入；统一用 createFrom 构造，保持类型安全。
 */
export const req = {
  startEmulator: (v: { avdName: string; options: domain.LaunchOptions }) =>
    serviceModels.StartRequest.createFrom(v),
};

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
  emulatorLog: "emulator:log",
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
