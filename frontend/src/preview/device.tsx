// 独立设备窗口（device.html）预览入口：把辅助进程的 window.go.displayhost.DeviceWindow
// 打桩成假数据，让 DeviceWindowApp 能在普通浏览器里渲染出来做视觉走查。
//
// 用法：`npx vite` 后打开 `device-preview.html`（可加 `?stats=1` 默认展开性能面板）。
import React from "react";
import { createRoot } from "react-dom/client";
import { DeviceWindowApp } from "../components/DeviceWindowApp";
import { ErrorBoundary } from "../components/ErrorBoundary";
import { previewParams } from "./mock";
import "../styles/tokens.css";
import "../styles/app.css";
import "../styles/device-window.css";

/** 设备窗口的假统计：数值只用于看排版，不代表真实链路。 */
const STATS = {
  uptimeMs: 128_000,
  mode: "raw-ws",
  streamWidth: 540,
  streamHeight: 960,
  recvFps: 60.2,
  publishFps: 59.8,
  recvTotal: 7680,
  publishTotal: 7640,
  seq: 7640,
  seqGapFrames: 0,
  copyMsP50: 0.4,
  copyMsP95: 1.2,
  encodeMsP50: 0,
  encodeMsP95: 0,
  subscribers: 1,
  dropBeforeEncode: 0,
  dropForClient: 3,
  lastSeqAtUnixMs: Date.now(),
  rawSubscribers: 1,
  rawPublishFps: 59.8,
  rawDropForClient: 3,
  clientPresentFps: 59.4,
  clientFrameIntervalP95: 17.8,
  clientUploadMsP95: 1.6,
  clientDropped: 3,
  clientReportCount: 128,
};

const SESSION = {
  instanceId: "inst-1",
  avdName: "Pixel_8_API_34",
  serial: "emulator-5554",
  url: "/tools/frame-sample.svg",
  deviceWidth: 1080,
  deviceHeight: 2400,
  streamWidth: 540,
  streamHeight: 960,
  mode: "mjpeg",
};

function installDeviceBridge(): void {
  const w = window as unknown as { go: Record<string, unknown> };
  const ok = <T,>(value: T) => () => Promise.resolve(value);
  w.go = {
    ...(w.go ?? {}),
    displayhost: {
      DeviceWindow: {
        Session: ok(SESSION),
        Stats: ok(STATS),
        SendTouch: ok(undefined),
        SendKey: ok(undefined),
        Screenshot: ok("D:\\AVDDesktop\\screenshots\\shot.png"),
        ReportClientStats: ok(undefined),
        Minimise: ok(undefined),
        ToggleMaximise: ok(false),
        IsMaximised: ok(false),
        SetAlwaysOnTop: ok(false),
        Close: ok(undefined),
      },
    },
  };
}

installDeviceBridge();

const params = previewParams();

const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <ErrorBoundary scope="设备窗口">
      <DeviceWindowApp />
    </ErrorBoundary>
  </React.StrictMode>,
);

// `?stats=1` 时自动点开标题栏的「性能统计」按钮（F8 的等价入口）。
if (params.get("stats") === "1") {
  let frames = 0;
  const tick = () => {
    const btn = document.querySelector<HTMLElement>(".dw__winbtn--stats");
    if (btn) {
      btn.click();
      return;
    }
    if (frames++ > 240) return;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}
