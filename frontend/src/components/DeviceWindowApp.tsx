// 独立设备窗口（MuMu 风格）：自绘标题栏 + 画面区 + 工具栏。
//
// 与主窗口内的 DeviceWindow 浮层不同，这个页面运行在辅助进程自己的 Wails 窗口里：
//   - 画面：MJPEG（<img> 直接消费 multipart/x-mixed-replace）
//   - 输入：指针 → 归一化坐标 → 辅助进程 → gRPC sendTouch；按键 → adb keyevent
//   - 窗口：无边框，拖拽区由 --wails-draggable 提供，按钮走辅助进程绑定
import { useCallback, useEffect, useRef, useState } from "react";
import type { CSSProperties, PointerEvent as ReactPointerEvent } from "react";
import * as win from "../bridge/deviceWindow";
import type { DeviceSession } from "../bridge/deviceWindow";
import { Icon } from "./deviceWindowIcons";

/** 拖动时两次触摸上报的最小间隔：约 60Hz，避免把 gRPC 通道打满。 */
const TOUCH_INTERVAL_MS = 16;

/** 工具栏按键（映射到 adb keyevent）。 */
const TOOL_KEYS = [
  { key: "back", label: "返回", icon: "back" as const },
  { key: "home", label: "主页", icon: "home" as const },
  { key: "appswitch", label: "多任务", icon: "recent" as const },
];

/** 音量键：设备音量由系统 UI 呈现，这里只做按键注入。 */
const VOLUME_KEYS = [
  { key: "volumedown", label: "音量 -", icon: "volumeDown" as const },
  { key: "volumeup", label: "音量 +", icon: "volumeUp" as const },
];

/** 键盘快捷键：模拟器窗口直接转发常用键，省去用鼠标点导航条。 */
const KEYMAP: Record<string, string> = {
  Escape: "back",
  ArrowUp: "dpadup",
  ArrowDown: "dpaddown",
  ArrowLeft: "dpadleft",
  ArrowRight: "dpadright",
  Enter: "enter",
  Backspace: "delete",
};

interface ToastState {
  level: "info" | "danger";
  text: string;
  id: number;
}

export function DeviceWindowApp() {
  const [session, setSession] = useState<DeviceSession | null>(null);
  const [error, setError] = useState("");
  const [frameReady, setFrameReady] = useState(false);
  const [frameBroken, setFrameBroken] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [pinned, setPinned] = useState(false);
  const [maximised, setMaximised] = useState(false);
  const [toast, setToast] = useState<ToastState | null>(null);
  const screenRef = useRef<HTMLImageElement | null>(null);
  const lastTouchAt = useRef(0);
  const touchFailed = useRef(false);
  const toastSeq = useRef(0);

  const notify = useCallback((level: ToastState["level"], text: string) => {
    toastSeq.current += 1;
    const id = toastSeq.current;
    setToast({ level, text, id });
    window.setTimeout(() => {
      setToast((current) => (current && current.id === id ? null : current));
    }, 3600);
  }, []);

  // 会话信息只读一次：画面地址与设备分辨率在窗口生命周期内不变。
  useEffect(() => {
    let cancelled = false;
    void win
      .Session()
      .then((value) => {
        if (!cancelled) setSession(value);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errText(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const sendTouch = useCallback(
    (x: number, y: number, release: boolean) => {
      void win.SendTouch(x, y, release).catch((err: unknown) => {
        if (touchFailed.current) return;
        touchFailed.current = true;
        notify("danger", `触摸注入失败：${errText(err)}`);
      });
    },
    [notify],
  );

  /** 指针位置 → 画面归一化坐标：元素盒子就是画面区域，直接按元素宽度换算。 */
  const normalized = (e: ReactPointerEvent<HTMLImageElement>) => {
    const el = screenRef.current;
    if (!el) return null;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return null;
    return {
      x: Math.min(Math.max((e.clientX - rect.left) / rect.width, 0), 1),
      y: Math.min(Math.max((e.clientY - rect.top) / rect.height, 0), 1),
    };
  };

  const handlePointerDown = (e: ReactPointerEvent<HTMLImageElement>) => {
    if (e.button !== 0) return;
    const p = normalized(e);
    if (!p) return;
    // 捕获指针：拖出画面后仍能收到 move/up，抬起时才能把 release 发出去。
    e.currentTarget.setPointerCapture(e.pointerId);
    lastTouchAt.current = performance.now();
    sendTouch(p.x, p.y, false);
  };

  const handlePointerMove = (e: ReactPointerEvent<HTMLImageElement>) => {
    if (!e.currentTarget.hasPointerCapture(e.pointerId)) return;
    const now = performance.now();
    if (now - lastTouchAt.current < TOUCH_INTERVAL_MS) return;
    lastTouchAt.current = now;
    const p = normalized(e);
    if (p) sendTouch(p.x, p.y, false);
  };

  const handlePointerEnd = (e: ReactPointerEvent<HTMLImageElement>) => {
    if (!e.currentTarget.hasPointerCapture(e.pointerId)) return;
    const p = normalized(e);
    if (p) sendTouch(p.x, p.y, true);
  };

  const sendKey = useCallback(
    (key: string, label: string) => {
      void win.SendKey(key).catch((err: unknown) => notify("danger", `${label}失败：${errText(err)}`));
    },
    [notify],
  );

  // 键盘快捷键：焦点不在输入控件上时把常用键转发给设备。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const key = KEYMAP[e.key];
      if (!key) return;
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) return;
      e.preventDefault();
      void win.SendKey(key).catch(() => undefined);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const togglePin = useCallback(() => {
    const next = !pinned;
    setPinned(next);
    void win.SetAlwaysOnTop(next).catch((err: unknown) => {
      setPinned(!next);
      notify("danger", `置顶设置失败：${errText(err)}`);
    });
  }, [pinned, notify]);

  const toggleMaximise = useCallback(() => {
    void win
      .ToggleMaximise()
      .then((value) => setMaximised(value))
      .catch((err: unknown) => notify("danger", `窗口操作失败：${errText(err)}`));
  }, [notify]);

  const takeScreenshot = useCallback(() => {
    notify("info", "正在截图…");
    void win
      .Screenshot()
      .then((path) => notify("info", `截图已保存：${path}`))
      .catch((err: unknown) => notify("danger", `截图失败：${errText(err)}`));
  }, [notify]);

  const aspect = session ? session.deviceWidth / session.deviceHeight : 0;
  const screenStyle = session
    ? ({ "--device-ar": String(aspect) } as CSSProperties)
    : undefined;

  return (
    <div className="dw">
      <header className="dw__titlebar">
        <span className="dw__brand" title={session?.avdName ?? ""}>
          <span className="dw__logo" aria-hidden>
            A
          </span>
          <span className="dw__name truncate">{session?.avdName ?? "设备窗口"}</span>
        </span>

        <span className="dw__meta nums">
          {session ? `${session.deviceWidth} × ${session.deviceHeight}` : error ? "连接失败" : "连接中…"}
        </span>

        <div className="dw__winbtns">
          <button
            type="button"
            className={`dw__winbtn${pinned ? " is-active" : ""}`}
            title={pinned ? "取消置顶" : "窗口置顶"}
            onClick={togglePin}
          >
            <Icon name="pin" />
          </button>
          <button
            type="button"
            className="dw__winbtn"
            title="最小化"
            onClick={() => void win.Minimise().catch(() => undefined)}
          >
            <Icon name="minimise" />
          </button>
          <button
            type="button"
            className="dw__winbtn"
            title={maximised ? "还原" : "最大化"}
            onClick={toggleMaximise}
          >
            <Icon name={maximised ? "restore" : "maximise"} />
          </button>
          <button
            type="button"
            className="dw__winbtn dw__winbtn--close"
            title="关闭设备窗口（不会停止模拟器）"
            onClick={() => void win.Close().catch(() => undefined)}
          >
            <Icon name="close" />
          </button>
        </div>
      </header>

      <main className="dw__stage">
        {session ? (
          // 画面元素本身就是画面区域：指针坐标按它的盒子换算，因此信箱边框不会影响映射。
          <img
            ref={screenRef}
            className="dw__screen"
            style={screenStyle}
            src={`${session.url}?r=${reloadKey}`}
            alt={`${session.avdName} 的设备画面`}
            draggable={false}
            onLoad={() => {
              setFrameReady(true);
              setFrameBroken(false);
            }}
            onError={() => {
              if (frameReady) setFrameBroken(true);
            }}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerEnd}
            onPointerCancel={handlePointerEnd}
            onContextMenu={(e) => e.preventDefault()}
          />
        ) : null}

        {!frameReady && !error && !frameBroken ? (
          <div className="dw__overlay">
            <span className="spinner" />
            <div className="dw__overlay-title">正在连接设备画面…</div>
            <div className="dw__overlay-text">冷启动需要一点时间，画面就绪后会自动显示。</div>
          </div>
        ) : null}

        {error ? (
          <div className="dw__overlay">
            <div className="dw__overlay-title">无法打开设备窗口</div>
            <div className="dw__overlay-text">{error}</div>
          </div>
        ) : null}

        {frameBroken ? (
          <div className="dw__overlay">
            <div className="dw__overlay-title">画面已中断</div>
            <div className="dw__overlay-text">模拟器可能已停止；重新连接会重新订阅画面流。</div>
            <button
              type="button"
              className="dw__action"
              onClick={() => {
                setFrameReady(false);
                setFrameBroken(false);
                setReloadKey((v) => v + 1);
              }}
            >
              重新连接
            </button>
          </div>
        ) : null}
      </main>

      <footer className="dw__toolbar">
        <div className="dw__group">
          {TOOL_KEYS.map((item) => (
            <button
              key={item.key}
              type="button"
              className="dw__tool"
              title={item.label}
              onClick={() => sendKey(item.key, item.label)}
            >
              <Icon name={item.icon} />
            </button>
          ))}
        </div>

        <span className="dw__divider" />

        <div className="dw__group">
          {VOLUME_KEYS.map((item) => (
            <button
              key={item.key}
              type="button"
              className="dw__tool"
              title={item.label}
              onClick={() => sendKey(item.key, item.label)}
            >
              <Icon name={item.icon} />
            </button>
          ))}
        </div>

        <span className="dw__divider" />

        <button type="button" className="dw__tool" title="截图" onClick={takeScreenshot}>
          <Icon name="screenshot" />
        </button>
      </footer>

      {toast ? (
        <div className={`dw__toast dw__toast--${toast.level}`} role="status">
          {toast.text}
        </div>
      ) : null}
    </div>
  );
}

/** 把绑定层抛出的错误渲染成可读文本。 */
function errText(err: unknown): string {
  if (!err) return "未知错误";
  if (typeof err === "string") return err;
  const e = err as { message?: string; hint?: string };
  return [e.message, e.hint].filter(Boolean).join("　") || "未知错误";
}
