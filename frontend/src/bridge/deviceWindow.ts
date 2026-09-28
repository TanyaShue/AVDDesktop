// 设备窗口（独立 Wails 进程）的绑定层。
//
// 设备窗口由辅助进程承载，它的 Bind 列表与本仓库主程序不同：`wails generate module`
// 只会为主程序生成 frontend/wailsjs，因此这里按生成文件的同一形状手写薄包装。
// 运行期派发完全由 Wails 运行时（window.go）完成，本文件不含任何业务逻辑。
type GoMethod = (...args: unknown[]) => Promise<unknown>;
// window.go[package][struct][method]：包 → 结构体 → 方法三层。
type GoNamespace = Record<string, Record<string, Record<string, GoMethod>>>;

function bridge(): GoNamespace | null {
  const holder = window as unknown as { go?: GoNamespace };
  return holder.go ?? null;
}

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const fn = bridge()?.displayhost?.DeviceWindow?.[method];
  if (typeof fn !== "function") {
    return Promise.reject(new Error("设备窗口桥接不可用：Wails 运行时未就绪"));
  }
  return fn(...args) as Promise<T>;
}

/** 设备窗口会话信息（对应 Go 侧 displayhost.DeviceSession）。 */
export interface DeviceSession {
  instanceId: string;
  avdName: string;
  serial: string;
  /** MJPEG 画面地址（127.0.0.1 随机端口）。 */
  url: string;
  /** 原始 RGBA 帧 WebSocket 地址；旧版辅助进程可能暂不提供。 */
  rawUrl?: string;
  /** 原始帧订阅使用的一次性 token。 */
  rawToken?: string;
  /** 原始帧格式，当前为 rgba8888；缺失时按兼容模式处理。 */
  rawFormat?: string;
  /** 原始帧行序是否自底向上。 */
  rawBottomUp?: boolean;
  /** 设备原生分辨率，触摸映射基准。 */
  deviceWidth: number;
  deviceHeight: number;
  streamWidth: number;
  streamHeight: number;
  mode: string;
}

/** 独立设备窗口的实时性能统计（对应 Go 侧 displayhost.DeviceStats）。 */
export interface DeviceStats {
  uptimeMs: number;
  mode: string;
  streamWidth: number;
  streamHeight: number;
  recvFps: number;
  publishFps: number;
  recvTotal: number;
  publishTotal: number;
  seq: number;
  seqGapFrames: number;
  copyMsP50: number;
  copyMsP95: number;
  encodeMsP50: number;
  encodeMsP95: number;
  subscribers: number;
  dropBeforeEncode: number;
  dropForClient: number;
  lastSeqAtUnixMs: number;
  /** Phase 2 原始帧 Hub 统计。 */
  rawSubscribers?: number;
  rawPublishFps?: number;
  rawDropForClient?: number;
  /** 前端 FrameCanvas 1Hz 上报的呈现统计。 */
  clientPresentFps?: number;
  clientFrameIntervalP95?: number;
  clientUploadMsP95?: number;
  clientDropped?: number;
  clientReportCount?: number;
}

/** 读取会话信息（画面地址与设备分辨率）。 */
export const Session = () => call<DeviceSession>("Session");

/** 读取画面链路的实时性能统计。 */
export const Stats = () => call<DeviceStats>("Stats");

/** 注入触摸：x/y 为画面区域内的归一化坐标（[0,1]）。 */
export const SendTouch = (x: number, y: number, release: boolean) =>
  call<void>("SendTouch", x, y, release);

/** 注入按键：back / home / appswitch / volumeup / volumedown / dpadup ... / enter / delete。 */
export const SendKey = (key: string) => call<void>("SendKey", key);

/** 抓取设备画面并保存到软件目录的 screenshots/，返回本地路径。 */
export const Screenshot = () => call<string>("Screenshot");

/** WebGL 画布每秒上报一次的呈现统计（仅用于诊断与验收）。 */
export const ReportClientStats = (
  presentFps: number,
  frameIntervalP95: number,
  uploadMsP95: number,
  dropped: number,
) => call<void>("ReportClientStats", presentFps, frameIntervalP95, uploadMsP95, dropped);

export const Minimise = () => call<void>("Minimise");

/** 切换最大化，返回切换后的状态。 */
export const ToggleMaximise = () => call<boolean>("ToggleMaximise");

export const IsMaximised = () => call<boolean>("IsMaximised");

/** 设置窗口置顶，返回设置后的状态。 */
export const SetAlwaysOnTop = (on: boolean) => call<boolean>("SetAlwaysOnTop", on);

/** 关闭设备窗口（只关画面，不停模拟器）。 */
export const Close = () => call<void>("Close");



