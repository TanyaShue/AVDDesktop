// 预览用 Wails 桥接替身：把 window.go / window.runtime 打桩成假数据源，
// 让 `preview.html` 能在普通浏览器（含 headless 截图）里渲染真实组件。
// 只用于开发期视觉走查，不进入打包入口。
import type { EnvReport } from "../bridge/types";
import {
  AVD_DETAILS,
  DEVICES,
  ENV_REPORT,
  INSTANCES,
  JOBS,
  LOG_LINES,
  MIRROR_CHECKS,
  MIRROR_SOURCES,
  PROFILES,
  SETTINGS,
  SYSTEM_IMAGES,
} from "./fixtures";

type Handler = (...args: unknown[]) => unknown;

/** 预览查询参数。 */
export function previewParams(): URLSearchParams {
  return new URLSearchParams(window.location.search);
}

/** 事件订阅表：installPreviewBridge 之后可用 emitPreviewEvent 手动推事件。 */
let previewListeners: Map<string, Set<(payload: unknown) => void>> | null = null;

/** 手动派发一个 Wails 事件（用于预览 toast / 任务状态等由事件驱动的界面）。 */
export function emitPreviewEvent(name: string, payload: unknown): void {
  previewListeners?.get(name)?.forEach((cb) => cb(payload));
}

/** Wails 绑定方法统一返回 Promise（真实后端也是如此）。 */
const ok = <T,>(value: T) => () => Promise.resolve(value);

/**
 * 取某个设备的设置详情。
 *
 * 假数据里没有的设备（例如 Broken_AVD）按「配置缺失」兜底：设置全为跟随档案，
 * 这样设置面板在预览里也能覆盖「读得出但没配置」的分支。
 */
function detailFor(name: string) {
  const found = AVD_DETAILS[name];
  if (found) return found;
  const device = DEVICES.find((d) => d.name === name);
  return {
    name,
    path: device?.path ?? "",
    api: device?.api ?? "",
    tag: device?.tag ?? "",
    abi: device?.abi ?? "",
    settings: { keyboard: false, root: false },
    rootSupported: true,
    state: device?.state ?? "stopped",
    broken: device?.broken,
  } as (typeof AVD_DETAILS)[string];
}

/** 把 window.go.service.* 铺成假实现；未列出的方法一律返回 undefined。 */
function makeServices(
  settings: typeof SETTINGS,
  env: EnvReport,
): Record<string, Record<string, Handler>> {
  return {
    EnvService: {
      Check: ok(env),
      Resolved: ok({
        appRoot: env.appRoot,
        jdkRoot: env.jdkRoot,
        sdkRoot: env.sdkRoot,
        avdHome: env.avdHome,
        javaPath: env.javaPath,
        logDir: env.appRoot + "\\logs",
        cacheDir: env.appRoot + "\\cache",
      }),
      Prepare: ok("job-prepare"),
      Repair: ok("job-repair"),
      CopyToClipboard: ok(undefined),
    },
    SettingsService: {
      Get: ok(settings),
      Update: (next: unknown) => Promise.resolve(next),
      Path: ok(env.appRoot + "\\config\\settings.json"),
      Reset: ok(settings),
    },
    AvdService: {
      List: ok(DEVICES),
      ListImages: ok(SYSTEM_IMAGES),
      ListProfiles: ok(PROFILES),
      ValidateName: (name: unknown) => Promise.resolve({ name, valid: true }),
      Create: ok("job-create"),
      Delete: ok("job-delete"),
      InstallImage: ok("job-install-image"),
      DeleteImage: ok("job-delete-image"),
      // 设备设置：Detail 返回假详情；UpdateSettings 把改动合并回假详情，
      // 这样面板的「保存 → 回读」链路在预览里也是通的（含 root / 键盘这类开关）。
      Detail: (name: unknown) => Promise.resolve(detailFor(String(name))),
      UpdateSettings: (req: unknown) => {
        const update = req as {
          name: string;
          ramMb?: number;
          cpuCores?: number;
          lcdWidth?: number;
          lcdHeight?: number;
          lcdDensity?: number;
          vmHeapMb?: number;
          keyboard?: boolean;
          root?: boolean;
        };
        const base = detailFor(update.name);
        const settings = {
          ...base.settings,
          ...(update.ramMb ? { ramMb: update.ramMb } : {}),
          ...(update.cpuCores ? { cpuCores: update.cpuCores } : {}),
          ...(update.lcdWidth ? { lcdWidth: update.lcdWidth } : {}),
          ...(update.lcdHeight ? { lcdHeight: update.lcdHeight } : {}),
          ...(update.lcdDensity ? { lcdDensity: update.lcdDensity } : {}),
          ...(update.vmHeapMb ? { vmHeapMb: update.vmHeapMb } : {}),
          ...(typeof update.keyboard === "boolean" ? { keyboard: update.keyboard } : {}),
          ...(typeof update.root === "boolean" ? { root: update.root } : {}),
        };
        const next = { ...base, settings };
        AVD_DETAILS[update.name] = next;
        return Promise.resolve(next);
      },
    },
    EmulatorService: {
      ListRunning: ok(INSTANCES),
      Start: ok(INSTANCES[0]),
      Stop: ok(undefined),
      StopByAvd: ok(undefined),
      Shutdown: ok(undefined),
    },
    DisplayService: {
      WindowSupported: ok(false),
      Active: ok([]),
      Open: ok({}),
      OpenWindow: ok(undefined),
      Close: ok(undefined),
      SendKey: ok(undefined),
      SendTouch: ok(undefined),
      Shutdown: ok(undefined),
    },
    JobService: {
      List: ok(JOBS),
      Logs: ok(LOG_LINES),
      Cancel: ok(undefined),
      CancelAll: ok(undefined),
    },
    LogService: {
      Tail: ok(LOG_LINES),
    },
    MirrorService: {
      ListSources: ok(MIRROR_SOURCES),
      Active: ok(MIRROR_SOURCES[0]),
      CheckAll: ok(MIRROR_CHECKS),
      Check: ok(MIRROR_CHECKS[0]),
      ListJDKSources: ok(MIRROR_SOURCES),
      ActiveJDK: ok(MIRROR_SOURCES[3]),
      CheckJDKAll: ok(MIRROR_CHECKS),
      CheckJDK: ok(MIRROR_CHECKS[0]),
      SetActiveSource: ok(undefined),
      SetActiveJDKSource: ok(undefined),
    },
    WindowService: {
      Minimise: ok(undefined),
      Close: ok(undefined),
      ToggleMaximise: ok(undefined),
      IsMaximised: ok(false),
    },
  };
}

/** 安装替身。必须在渲染 React 之前调用。 */
export function installPreviewBridge(): void {
  const params = previewParams();
  const theme = params.get("theme");
  const ready = params.get("env") !== "missing";

  const settings = { ...SETTINGS, theme: theme ?? "light" };
  const env: EnvReport = ready
    ? ENV_REPORT
    : {
        ...ENV_REPORT,
        ready: false,
        needInit: true,
        components: ENV_REPORT.components.map((c, i) =>
          i === 0 ? c : { ...c, state: "missing" },
        ) as EnvReport["components"],
      };

  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  previewListeners = listeners;

  const w = window as unknown as {
    // window.go.<namespace>.<Service>.<Method>
    go: Record<string, Record<string, Record<string, Handler>>>;
    runtime: Record<string, unknown>;
  };

  w.go = {
    main: { App: { GetVersion: ok("0.0.0-preview"), Greet: ok("preview") } },
    service: makeServices(settings, env),
  };

  w.runtime = {
    EventsOn: (name: string, cb: (payload: unknown) => void) => {
      const set = listeners.get(name) ?? new Set();
      set.add(cb);
      listeners.set(name, set);
      return () => set.delete(cb);
    },
    EventsOff: (name: string) => listeners.delete(name),
    EventsOffAll: () => listeners.clear(),
    EventsOnMultiple: (name: string, cb: (payload: unknown) => void) => {
      const set = listeners.get(name) ?? new Set();
      set.add(cb);
      listeners.set(name, set);
      return () => set.delete(cb);
    },
    EventsOnce: (name: string, cb: (payload: unknown) => void) => {
      const set = listeners.get(name) ?? new Set();
      set.add(cb);
      listeners.set(name, set);
      return () => set.delete(cb);
    },
    EventsEmit: () => undefined,
    LogPrint: () => undefined,
    LogTrace: () => undefined,
    LogDebug: () => undefined,
    LogInfo: () => undefined,
    LogWarning: () => undefined,
    LogError: () => undefined,
    WindowSetTitle: () => undefined,
    WindowSetBackgroundColour: () => undefined,
    BrowserOpenURL: () => undefined,
    ClipboardSetText: () => Promise.resolve(true),
    Quit: () => undefined,
  };
}
