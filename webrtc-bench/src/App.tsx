import { useEffect, useMemo, useState } from "react";
import Emulator from "android-emulator-webrtc/dist/components/emulator/emulator.js";
import logger from "android-emulator-webrtc/dist/components/emulator/net/logger.js";
import {
  type BenchSnapshot,
  recordBenchError,
  scanForVideo,
  setConnectionState,
  subscribeBench,
} from "./bench";

logger.setLevel("warn");

const params = new URLSearchParams(window.location.search);
const defaultGateway = params.get("url") || params.get("uri") || "http://127.0.0.1:8080";
const autoConnect = params.get("autoconnect") !== "0";
const previewWidth = Number(params.get("previewWidth") || 360);
const previewHeight = Number(params.get("previewHeight") || 640);
const targetFps = Math.max(1, Number(params.get("hz") || 60));

function format(value: number | null | undefined, digits = 1, suffix = "") {
  return value == null || !Number.isFinite(value) ? "—" : `${value.toFixed(digits)}${suffix}`;
}

function Metric({
  label,
  value,
  detail,
  accent = false,
}: {
  label: string;
  value: string;
  detail?: string;
  accent?: boolean;
}) {
  return (
    <div className={`metric ${accent ? "metric-accent" : ""}`}>
      <div className="metric-label">{label}</div>
      <div className="metric-value">{value}</div>
      {detail && <div className="metric-detail">{detail}</div>}
    </div>
  );
}

function Sparkline({ values, targetFps }: { values: number[]; targetFps: number }) {
  const points = useMemo(() => {
    if (values.length < 2) return "";
    const max = Math.max(targetFps + 5, ...values);
    return values
      .map((value, index) => {
        const x = (index / (values.length - 1)) * 100;
        const y = 30 - Math.max(0, Math.min(1, value / max)) * 28;
        return `${x.toFixed(2)},${y.toFixed(2)}`;
      })
      .join(" ");
  }, [values, targetFps]);

  return (
    <svg className="sparkline" viewBox="0 0 100 30" preserveAspectRatio="none" aria-label="FPS trend">
      <line x1="0" y1="15" x2="100" y2="15" className="spark-grid" />
      <line x1="0" y1="27" x2="100" y2="27" className="spark-grid" />
      {points && <polyline points={points} className="spark-line" />}
    </svg>
  );
}

export default function App() {
  const [gateway, setGateway] = useState(defaultGateway);
  const [connected, setConnected] = useState(autoConnect);
  const [metrics, setMetrics] = useState<BenchSnapshot>({
    connection: "idle",
    connectedAt: null,
    firstFrameAt: null,
    videoWidth: 0,
    videoHeight: 0,
    renderFps: null,
    inboundFps: null,
    bitrateKbps: null,
    framesDecoded: null,
    framesDropped: null,
    codec: null,
    errors: [],
    samples: [],
  });

  useEffect(() => subscribeBench(setMetrics), []);

  useEffect(() => {
    if (!connected) return;
    const timer = window.setInterval(scanForVideo, 200);
    scanForVideo();
    return () => window.clearInterval(timer);
  }, [connected]);

  const recentRenderFps = metrics.samples
    .slice(-30)
    .map((sample) => sample.renderFps)
    .filter((value): value is number => value != null && Number.isFinite(value));

  const nativeResolution =
    metrics.videoWidth && metrics.videoHeight
      ? `${metrics.videoWidth}×${metrics.videoHeight}`
      : "等待视频";
  const stateLabel = metrics.connection === "connected" ? "已连接" : metrics.connection === "connecting" ? "连接中" : metrics.connection === "disconnected" ? "已断开" : "待机";

  return (
    <main className="page-shell">
      <header className="topbar">
        <div>
          <div className="eyebrow">ANDROID EMULATOR / WEBRTC</div>
          <h1>FPS Lab</h1>
          <p className="subtitle">Android 模拟器 → 官方 WebRTC 组件 → 浏览器实时渲染</p>
        </div>
        <div className={`status status-${metrics.connection}`}>
          <span className="status-dot" />
          {stateLabel}
        </div>
      </header>

      {!connected ? (
        <section className="connect-panel">
          <div>
            <div className="section-kicker">GATEWAY</div>
            <h2>连接到 Emulator Gateway</h2>
            <p>启动无头模拟器和 gateway 后，在这里输入地址。</p>
          </div>
          <form
            className="connect-form"
            onSubmit={(event) => {
              event.preventDefault();
              setConnected(true);
            }}
          >
            <input value={gateway} onChange={(event) => setGateway(event.target.value)} placeholder="http://127.0.0.1:8080" />
            <button type="submit">连接</button>
          </form>
        </section>
      ) : (
        <section className="dashboard">
          <div className="preview-column">
            <div className="preview-frame">
              <div className="device-camera" />
              <div className="preview-screen">
                <Emulator
                  uri={gateway}
                  width={previewWidth}
                  height={previewHeight}
                  muted
                  onStateChange={(state) => {
                    setConnectionState(state as BenchSnapshot["connection"]);
                  }}
                  onError={(error) => recordBenchError(error)}
                />
              </div>
              <div className="device-home" />
            </div>
            <div className="preview-caption">
              <span>浏览器预览</span>
              <span>{previewWidth}×{previewHeight} CSS px</span>
            </div>
          </div>

          <div className="metrics-column">
            <div className="hero-metrics">
              <Metric
                label="浏览器渲染 FPS"
                value={format(metrics.renderFps, 1)}
                detail="requestVideoFrameCallback"
                accent
              />
              <Metric
                label="WebRTC 解码 FPS"
                value={format(metrics.inboundFps, 1)}
                detail="inbound-rtp.framesPerSecond"
                accent
              />
            </div>

            <div className="chart-card">
              <div className="chart-head">
                <span>渲染帧率趋势</span>
                <span className="chart-scale">最近 {recentRenderFps.length}s</span>
              </div>
              <Sparkline values={recentRenderFps} targetFps={targetFps} />
              <div className="chart-footer">
                <span>0</span>
                <span>目标 {targetFps} FPS</span>
                <span>{targetFps}+</span>
              </div>
            </div>

            <div className="metric-grid">
              <Metric label="视频原生分辨率" value={nativeResolution} detail="WebRTC frameWidth × frameHeight" />
              <Metric
                label="视频码率"
                value={format(metrics.bitrateKbps, 0, " kbps")}
                detail={metrics.codec || "等待 codec"}
              />
              <Metric
                label="累计解码帧"
                value={metrics.framesDecoded == null ? "—" : metrics.framesDecoded.toLocaleString()}
                detail={`丢帧 ${metrics.framesDropped ?? "—"}`}
              />
              <Metric
                label="连接后帧延迟"
                value={
                  metrics.connectedAt != null && metrics.firstFrameAt != null
                    ? format(metrics.firstFrameAt - metrics.connectedAt, 0, " ms")
                    : "—"
                }
                detail="连接 → 首帧"
              />
            </div>

            <div className="connection-card">
              <div>
                <div className="section-kicker">GATEWAY</div>
                <div className="connection-url">{gateway}</div>
              </div>
              <button
                type="button"
                className="ghost-button"
                onClick={() => {
                  setConnected(false);
                  setConnectionState("idle");
                }}
              >
                断开
              </button>
            </div>

            {metrics.errors.length > 0 && (
              <div className="error-card">
                <div className="section-kicker">RECENT ERROR</div>
                <div>{metrics.errors[metrics.errors.length - 1]}</div>
              </div>
            )}
          </div>
        </section>
      )}

      <footer>
        官方组件 <code>android-emulator-webrtc</code> 2.0.1 · 测速时保持页面可见以避免浏览器后台节流
      </footer>
    </main>
  );
}
