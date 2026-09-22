// 通用 UI 组件：弹窗、Toast 容器、状态徽标、进度条。
import { useEffect } from "react";
import type { ReactNode } from "react";
import type { JobInfo, Toast } from "../bridge/types";

export function Modal({
  title,
  size = "md",
  className = "",
  bodyClassName = "",
  onClose,
  children,
  footer,
}: {
  title: string;
  size?: "sm" | "md" | "lg" | "xl";
  /** 追加在 modal 根节点上的类名（用于需要固定高度、内部自行滚动的弹窗）。 */
  className?: string;
  /** 追加在 modal__body 上的类名（用于把滚动交给内部表格等元素）。 */
  bodyClassName?: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  // 弹窗打开期间锁住底层页面滚动：否则弹窗背后会多出一条滚动条。
  useEffect(() => {
    document.body.classList.add("modal-open");
    return () => document.body.classList.remove("modal-open");
  }, []);

  return (
    <div
      className="modal-mask"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        className={`modal modal--${size}${className ? ` ${className}` : ""}`}
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
        <div className={`modal__body${bodyClassName ? ` ${bodyClassName}` : ""}`}>{children}</div>
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

/** 耗时格式化（环境检查/任务耗时展示）。 */
export function formatMs(ms: number): string {
  if (!ms || ms < 0) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

/** 进度条。 */
/**
 * 进度条。
 *
 * showPercent 为真且进度确定时，在右侧给出百分比数字——下载/安装这类有明确总量的
 * 任务需要它；总量未知（不确定进度）时不显示数字，避免出现假的百分比。
 */
export function Progress({
  percent,
  indeterminate,
  showPercent,
}: {
  percent: number;
  indeterminate?: boolean;
  showPercent?: boolean;
}) {
  const bar = (
    <div className={`progress${indeterminate ? " progress--indeterminate" : ""}`}>
      <div className="progress__bar" style={{ width: `${Math.max(0, Math.min(100, percent))}%` }} />
    </div>
  );
  if (!showPercent || indeterminate) return bar;
  return (
    <div className="progress-line">
      {bar}
      <span className="progress-line__value nums">{Math.round(percent)}%</span>
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
