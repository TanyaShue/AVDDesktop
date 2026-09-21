// 通用 UI 组件：弹窗、Toast 容器、状态徽标、进度条。
import type { ReactNode } from "react";
import type { JobInfo, Toast, ToolState } from "../bridge/types";

export function Modal({
  title,
  size = "md",
  onClose,
  children,
  footer,
}: {
  title: string;
  size?: "sm" | "md" | "lg" | "xl";
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div
      className="modal-mask"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        className={`modal modal--${size}`}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="modal__head">
          <div className="modal__title">{title}</div>
          <button className="modal__close" onClick={onClose} aria-label="关闭">
            ✕
          </button>
        </div>
        <div className="modal__body">{children}</div>
        {footer ? <div className="modal__foot">{footer}</div> : null}
      </div>
    </div>
  );
}

export function Toasts({ toasts, onDismiss }: { toasts: Toast[]; onDismiss: (id: number) => void }) {
  if (toasts.length === 0) return null;
  return (
    <div className="toasts">
      {toasts.map((t) => (
        <div key={t.id} className="toast" onClick={() => onDismiss(t.id)} role="status">
          <span className={`tool__icon tool__icon--${iconTone(t.level)}`} style={{ width: 28, height: 28, fontSize: 14 }}>
            {iconChar(t.level)}
          </span>
          <div style={{ minWidth: 0 }}>
            <div className="toast__title">{t.title}</div>
            {t.text ? <div className="toast__text">{t.text}</div> : null}
          </div>
        </div>
      ))}
    </div>
  );
}

function iconChar(level: Toast["level"]) {
  switch (level) {
    case "success":
      return "✓";
    case "warning":
      return "!";
    case "danger":
      return "✕";
    default:
      return "i";
  }
}

function iconTone(level: Toast["level"]) {
  switch (level) {
    case "success":
      return "ok";
    case "warning":
      return "warn";
    case "danger":
      return "bad";
    default:
      return "";
  }
}

/** 组件状态 → chip 样式与文案。 */
export function stateChip(state: ToolState | undefined) {
  switch (state) {
    case "present":
      return { cls: "chip chip--success", text: "就绪" };
    case "missing":
      return { cls: "chip chip--danger", text: "缺失" };
    case "broken":
      return { cls: "chip chip--danger", text: "异常" };
    case "incompatible":
      return { cls: "chip chip--warning", text: "需升级" };
    case "outdated":
      return { cls: "chip chip--warning", text: "可更新" };
    default:
      return { cls: "chip", text: "未知" };
  }
}

/** 组件状态 → 图标样式。 */
export function stateTone(state: ToolState | undefined) {
  switch (state) {
    case "present":
      return { cls: "tool__icon tool__icon--ok", char: "✓" };
    case "missing":
    case "broken":
      return { cls: "tool__icon tool__icon--bad", char: "✕" };
    case "incompatible":
    case "outdated":
      return { cls: "tool__icon tool__icon--warn", char: "!" };
    default:
      return { cls: "tool__icon", char: "—" };
  }
}

export function Progress({ percent, indeterminate }: { percent: number; indeterminate?: boolean }) {
  return (
    <div className={`progress${indeterminate ? " progress--indeterminate" : ""}`}>
      <div className="progress__bar" style={{ width: `${Math.max(0, Math.min(100, percent))}%` }} />
    </div>
  );
}

export function ScoreBar({ score }: { score: number }) {
  const color = score >= 70 ? "var(--speed-fast)" : score >= 35 ? "var(--speed-mid)" : "var(--speed-slow)";
  return (
    <div className="scorebar" title={`评分 ${score}`}>
      <div className="scorebar__fill" style={{ width: `${score}%`, background: color }} />
    </div>
  );
}

/** 任务标题后缀：进度百分比或状态。 */
export function jobStatusText(job: JobInfo): string {
  switch (job.status) {
    case "queued":
      return "排队中";
    case "running":
      return job.phase || "进行中";
    case "succeeded":
      return "已完成";
    case "failed":
      return job.error?.message ?? "失败";
    case "canceled":
      return "已取消";
    default:
      return "";
  }
}

/** 字节格式化（与后端 HumanSize 保持一致的展示习惯）。 */
export function humanSize(bytes: number): string {
  if (!bytes || bytes <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value >= 100 || i === 0 ? Math.round(value) : value.toFixed(1)} ${units[i]}`;
}

/** 速度格式化。 */
export function humanSpeed(bps: number): string {
  if (!bps || bps <= 0) return "";
  return `${(bps / (1 << 20)).toFixed(1)} MB/s`;
}
