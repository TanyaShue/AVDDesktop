// 独立设备窗口的性能诊断浮层。
//
// 只在标题栏的 FPS 按钮或 F8 打开时轮询 Go 侧统计；关闭后立即停止定时器，
// 不接触 MJPEG 画面链路，也不会在画面帧到达时触发 React 更新。
import { useEffect, useState } from "react";
import * as win from "../bridge/deviceWindow";

interface DeviceStatsOverlayProps {
  visible: boolean;
}

/** 数值统一保留 1 位小数；缺失或非有限值显示为不可用。 */
function metric(value: number | null | undefined): string {
  return typeof value === "number" && Number.isFinite(value) ? value.toFixed(1) : "--";
}

/** 成对耗时显示为 p50 / p95，单位固定为毫秒。 */
function metricPair(p50: number | null | undefined, p95: number | null | undefined): string {
  return `${metric(p50)} / ${metric(p95)} ms`;
}

/** 流尺寸用整数展示（像素没有小数含义），任一维无效时整体显示不可用。 */
function streamSize(stats: win.DeviceStats | null): string {
  if (!stats || !Number.isFinite(stats.streamWidth) || !Number.isFinite(stats.streamHeight)) {
    return "--";
  }
  if (stats.streamWidth <= 0 || stats.streamHeight <= 0) return "--";
  return `${Math.round(stats.streamWidth)} × ${Math.round(stats.streamHeight)}`;
}

export function DeviceStatsOverlay({ visible }: DeviceStatsOverlayProps) {
  const [stats, setStats] = useState<win.DeviceStats | null>(null);

  useEffect(() => {
    if (!visible) return;

    let disposed = false;
    let inFlight = false;

    const poll = async () => {
      if (disposed || inFlight) return;
      inFlight = true;
      try {
        const value = await win.Stats();
        if (!disposed) setStats(value);
      } catch {
        if (!disposed) setStats(null);
      } finally {
        inFlight = false;
      }
    };

    void poll();
    const timer = window.setInterval(() => void poll(), 1000);
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, [visible]);

  if (!visible) return null;

  return (
    <section className="dw__stats" aria-label="性能诊断">
      <div className="dw__stats-head">
        <span>性能诊断</span>
        <span className={`dw__stats-live${stats ? " is-live" : ""}`}>{stats ? "LIVE" : "--"}</span>
      </div>

      <dl className="dw__stats-grid">
        <div className="dw__stats-item">
          <dt>模式</dt>
          <dd title={stats?.mode || "--"}>{stats?.mode || "--"}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>接收 FPS</dt>
          <dd>{metric(stats?.recvFps)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>发布 FPS</dt>
          <dd>{metric(stats?.publishFps)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>Seq 缺口</dt>
          <dd>{metric(stats?.seqGapFrames)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>Copy p50 / p95</dt>
          <dd>{metricPair(stats?.copyMsP50, stats?.copyMsP95)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>Encode p50 / p95</dt>
          <dd>{metricPair(stats?.encodeMsP50, stats?.encodeMsP95)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>订阅者</dt>
          <dd>{metric(stats?.subscribers)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>画面尺寸</dt>
          <dd>{streamSize(stats)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>编码前丢弃</dt>
          <dd>{metric(stats?.dropBeforeEncode)}</dd>
        </div>
        <div className="dw__stats-item">
          <dt>客户端丢弃</dt>
          <dd>{metric(stats?.dropForClient)}</dd>
        </div>
      </dl>
    </section>
  );
}
