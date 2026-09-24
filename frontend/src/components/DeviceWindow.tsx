// 设备窗口：应用内全屏浮层（Wails v2 没有多窗口 API），显示「自定义 UI」实例的实时画面并回注触摸。
//
// 画面来自后端 display.Session 的 MJPEG 流（<img> 直接消费 multipart/x-mixed-replace），
// 触摸按归一化坐标经 gRPC 回注设备，导航键走 adb。关闭只停画面，不停模拟器。
import { useCallback, useEffect, useRef, useState } from "react";
import type { CSSProperties, PointerEvent as ReactPointerEvent } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { DisplaySession } from "../bridge/types";
import { useWailsEvent } from "../hooks/useApp";

/** 拖动时两次触摸上报的最小间隔：约 60Hz，避免把 gRPC 通道打满。 */
const TOUCH_INTERVAL_MS = 16;

/** 导航键（后端映射到 adb keyevent：back=4 / home=3 / appswitch=187）。 */
const NAV_KEYS = [
  { key: "back", label: "返回" },
  { key: "home", label: "主页" },
  { key: "appswitch", label: "多任务" },
] as const;

type NavKey = (typeof NAV_KEYS)[number]["key"];

/**
 * 进行中的 Open 请求（按实例去重）。
 *
 * React StrictMode 在开发模式下会重复执行挂载副作用，同一实例会同时发出两次 Open：
 * 后端虽然用键锁串行化，但「连接中用户点关闭」只会让第一次 Open 放弃，第二次仍会建好会话，
 * 留下一个无人订阅的画面流。这里让重复的调用复用同一个请求，从根上避免这种情况。
 */
const inflightOpen = new Map<string, Promise<DisplaySession>>();

function openSession(instanceId: string): Promise<DisplaySession> {
  const existing = inflightOpen.get(instanceId);
  if (existing) return existing;
  const pending = api.Display.Open(instanceId).finally(() => inflightOpen.delete(instanceId));
  inflightOpen.set(instanceId, pending);
  return pending;
}

interface Props {
  instanceId: string;
  avdName: string;
  onClose: () => void;
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
}

export function DeviceWindow({ instanceId, avdName, onClose, onToast }: Props) {
  const [session, setSession] = useState<DisplaySession | null>(null);
  const [error, setError] = useState("");
  const screenRef = useRef<HTMLDivElement | null>(null);
  /** 上一次触摸上报的时间戳（节流用）。 */
  const lastTouchAt = useRef(0);
  /** 触摸注入失败只提示一次，否则拖动过程中会刷屏。 */
  const touchFailed = useRef(false);

  /** 打开画面：后端会自己等待 gRPC 就绪（≤3 分钟，冷启动可能几十秒），这里只负责连接中/失败两种界面状态。 */
  const connect = useCallback(async () => {
    setError("");
    setSession(null);
    try {
      setSession(await openSession(instanceId));
    } catch (err) {
      setError(errorText(err));
    }
  }, [instanceId]);

  useEffect(() => {
    void connect();
  }, [connect]);

  const close = useCallback(() => {
    // 只关画面，不停模拟器；会话可能已随实例结束被后端清理，关闭失败无需打扰用户。
    void api.Display.Close(instanceId).catch(() => undefined);
    onClose();
  }, [instanceId, onClose]);

  // 实例退出（进程结束/列表里消失）时自动关闭：画面不可能再更新，留着只会误导。
  const handleInstanceChanged = useCallback(() => {
    void api.Emulator.ListRunning()
      .then((list) => {
        const inst = api.asArray(list).find((it) => it.id === instanceId);
        // endedAt 非空表示进程已退出；列表里找不到该实例同样说明已经结束。
        if (inst && !inst.endedAt) return;
        onToast("warning", `${avdName} 已停止`, "设备窗口已自动关闭");
        close();
      })
      .catch(() => undefined); // 列表读取失败时保持窗口，等下一次事件
  }, [instanceId, avdName, close, onToast]);

  useWailsEvent<unknown>(EVENTS.emulatorState, handleInstanceChanged);
  useWailsEvent<unknown>(EVENTS.avdChanged, handleInstanceChanged);

  const sendTouch = (x: number, y: number, release: boolean) => {
    void api.Display.SendTouch(instanceId, x, y, release).catch((err) => {
      if (touchFailed.current) return;
      touchFailed.current = true;
      onToast("danger", "触摸注入失败", errorText(err));
    });
  };

  /** 指针位置 → 画面归一化坐标：元素盒子就是画面区域，直接按元素宽度换算即可。 */
  const normalized = (e: ReactPointerEvent<HTMLDivElement>) => {
    const el = screenRef.current;
    if (!el) return null;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return null;
    return {
      x: Math.min(Math.max((e.clientX - rect.left) / rect.width, 0), 1),
      y: Math.min(Math.max((e.clientY - rect.top) / rect.height, 0), 1),
    };
  };

  const handlePointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!session || e.button !== 0) return;
    const p = normalized(e);
    if (!p) return;
    // 捕获指针：拖出画面后仍能收到 move/up，抬起时才能把 release 发出去。
    e.currentTarget.setPointerCapture(e.pointerId);
    lastTouchAt.current = performance.now();
    sendTouch(p.x, p.y, false);
  };

  const handlePointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!session || !e.currentTarget.hasPointerCapture(e.pointerId)) return;
    const now = performance.now();
    if (now - lastTouchAt.current < TOUCH_INTERVAL_MS) return;
    lastTouchAt.current = now;
    const p = normalized(e);
    if (p) sendTouch(p.x, p.y, false);
  };

  const handlePointerEnd = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!session || !e.currentTarget.hasPointerCapture(e.pointerId)) return;
    const p = normalized(e);
    if (p) sendTouch(p.x, p.y, true);
  };

  const sendKey = (key: NavKey) => {
    void api.Display.SendKey(instanceId, key).catch((err) => onToast("danger", "按键注入失败", errorText(err)));
  };

  return (
    <div className="devicewindow" role="dialog" aria-modal="true" aria-label={`设备窗口 ${avdName}`}>
      <div className="devicewindow__head">
        <span className="devicewindow__title truncate" title={avdName}>
          {avdName}
        </span>
        <span className="devicewindow__status">{statusLabel(session, error)}</span>
      </div>

      <div className="devicewindow__frame">
        {session ? (
          <div
            className="devicewindow__screen"
            ref={screenRef}
            // CSS 自定义属性不在 CSSProperties 的类型里，只能断言传入（宽高比同时用于宽度上限计算）。
            style={
              {
                "--device-ar": `calc(${session.deviceWidth} / ${session.deviceHeight})`,
              } as CSSProperties
            }
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerEnd}
            onPointerCancel={handlePointerEnd}
          >
            <img src={session.url} alt={`${avdName} 的设备画面`} draggable={false} />
          </div>
        ) : error ? (
          <div className="devicewindow__state">
            <div className="devicewindow__state-icon">⚠️</div>
            <div className="devicewindow__state-title">无法打开设备画面</div>
            <div className="devicewindow__state-text">{error}</div>
            <button className="btn btn--primary" onClick={() => void connect()}>
              重试
            </button>
          </div>
        ) : (
          <div className="devicewindow__state">
            <span className="spinner" />
            <div className="devicewindow__state-title">正在连接设备画面…</div>
            <div className="devicewindow__state-text">模拟器就绪需要一点时间，连接成功后会自动显示画面。</div>
          </div>
        )}
      </div>

      <div className="devicewindow__toolbar">
        {NAV_KEYS.map((item) => (
          <button
            key={item.key}
            className="btn btn--secondary"
            disabled={!session}
            onClick={() => sendKey(item.key)}
          >
            {item.label}
          </button>
        ))}
        <span className="devicewindow__toolbar-gap" />
        <button className="btn btn--danger-ghost" onClick={close}>
          关闭
        </button>
      </div>
    </div>
  );
}

/** 顶部状态文案：已连接时显示设备原生分辨率（触摸映射基准）。 */
function statusLabel(session: DisplaySession | null, error: string): string {
  if (session) return `${session.deviceWidth} × ${session.deviceHeight}`;
  return error ? "连接失败" : "正在连接…";
}
