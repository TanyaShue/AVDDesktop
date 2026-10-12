// 全应用统一的线性图标集。
//
// 约定：
//   - 24×24 viewBox，1.7 描边，currentColor，圆头圆角；
//   - 一律用 SVG，禁止 emoji / 字符图标（字体渲染在 Windows 上不可控）；
//   - 尺寸由 size 控制，默认 18（介于正文与标题之间）。
import type { ReactNode } from "react";

export type IconName =
  // 导航
  | "devices"
  | "settings"
  // 工具栏 / 通用操作
  | "plus"
  | "search"
  | "sort"
  | "grid"
  | "list"
  | "more"
  | "trash"
  | "copy"
  | "refresh"
  | "wrench"
  | "download"
  | "chevronDown"
  | "chevronUp"
  | "chevronRight"
  | "external"
  // 设备
  | "play"
  | "stop"
  | "terminal"
  | "snowflake"
  | "windowOff"
  | "monitor"
  // 状态
  | "check"
  | "alert"
  | "info"
  | "cross"
  | "clock"
  // 环境 / 设置
  | "folder"
  | "image"
  | "globe"
  | "cpu"
  | "memory"
  | "display"
  | "disk"
  | "java"
  | "activity"
  // 窗口控制
  | "minimise"
  | "maximise"
  | "restore"
  | "close"
  // 设备窗口工具栏
  | "back"
  | "home"
  | "recent"
  | "volumeUp"
  | "volumeDown"
  | "screenshot"
  | "pin"
  | "stats";

const PATHS: Record<IconName, ReactNode> = {
  /* ------------------------------------------------------------ 导航 */
  devices: (
    <>
      <rect x="6.5" y="2.8" width="11" height="18.4" rx="2.6" />
      <path d="M10.5 5.6h3" />
      <circle cx="12" cy="18.2" r="0.6" fill="currentColor" stroke="none" />
    </>
  ),
  settings: (
    <>
      <path d="M4 7h9M17 7h3M4 17h3M11 17h9" />
      <circle cx="15" cy="7" r="2.2" />
      <circle cx="9" cy="17" r="2.2" />
    </>
  ),

  /* ------------------------------------------------------------ 通用操作 */
  plus: <path d="M12 5.5v13M5.5 12h13" />,
  search: (
    <>
      <circle cx="11" cy="11" r="6.4" />
      <path d="M19.6 19.6 15.4 15.4" />
    </>
  ),
  sort: (
    <>
      <path d="M4 7h13M4 12h9M4 17h5" />
      <path d="M18 10v9M18 19l-2.6-2.6M18 19l2.6-2.6" />
    </>
  ),
  grid: (
    <>
      <rect x="4" y="4" width="6.6" height="6.6" rx="1.6" />
      <rect x="13.4" y="4" width="6.6" height="6.6" rx="1.6" />
      <rect x="4" y="13.4" width="6.6" height="6.6" rx="1.6" />
      <rect x="13.4" y="13.4" width="6.6" height="6.6" rx="1.6" />
    </>
  ),
  list: (
    <>
      <rect x="4" y="4.5" width="16" height="6" rx="1.8" />
      <rect x="4" y="13.5" width="16" height="6" rx="1.8" />
    </>
  ),
  more: (
    <>
      <circle cx="5.5" cy="12" r="1.4" fill="currentColor" stroke="none" />
      <circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none" />
      <circle cx="18.5" cy="12" r="1.4" fill="currentColor" stroke="none" />
    </>
  ),
  trash: (
    <>
      <path d="M4.5 6.5h15M9.5 6.5V4.8h5v1.7" />
      <path d="M6.5 6.5 7.6 20a1 1 0 0 0 1 .9h6.8a1 1 0 0 0 1-.9l1.1-13.5" />
      <path d="M10.4 10.5v6.4M13.6 10.5v6.4" />
    </>
  ),
  copy: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="2.4" />
      <path d="M15 6.2V5.6A2.6 2.6 0 0 0 12.4 3H5.6A2.6 2.6 0 0 0 3 5.6v6.8A2.6 2.6 0 0 0 5.6 15h.6" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 12a8 8 0 1 1-2.6-5.9" />
      <path d="M20 4.5V10h-5.4" />
    </>
  ),
  wrench: (
    <path d="M14.5 6.2a4.2 4.2 0 0 0 5.4 5.4l-7.3 7.3a2.6 2.6 0 0 1-3.7-3.7zM14.5 6.2 17 3.7M14.5 6.2l2.5 2.5" />
  ),
  download: (
    <>
      <path d="M12 4v10.5" />
      <path d="M8.2 11 12 14.8 15.8 11" />
      <path d="M4.5 18.5h15" />
    </>
  ),
  chevronDown: <path d="M6.5 9.5 12 15l5.5-5.5" />,
  chevronUp: <path d="M6.5 14.5 12 9l5.5 5.5" />,
  chevronRight: <path d="M9.5 6.5 15 12l-5.5 5.5" />,
  external: (
    <>
      <path d="M13.5 4.5H19.5V10.5" />
      <path d="M19.5 4.5 11 13" />
      <path d="M18 14.5v3.9a1.6 1.6 0 0 1-1.6 1.6H5.6A1.6 1.6 0 0 1 4 18.4V7.6A1.6 1.6 0 0 1 5.6 6h3.9" />
    </>
  ),

  /* ------------------------------------------------------------ 设备 */
  play: <path d="M8.5 5.6 18 12l-9.5 6.4z" fill="currentColor" stroke="none" />,
  stop: <rect x="7.2" y="7.2" width="9.6" height="9.6" rx="2" fill="currentColor" stroke="none" />,
  terminal: (
    <>
      <rect x="3.2" y="4.4" width="17.6" height="15.2" rx="3" />
      <path d="M7.4 10 10 12.6 7.4 15.2M12.4 15.4h4.4" />
    </>
  ),
  snowflake: (
    <>
      <path d="M12 3.2v17.6M4.4 7.6l15.2 8.8M19.6 7.6 4.4 16.4" />
      <path d="M9.4 5.2 12 7.8l2.6-2.6M9.4 18.8 12 16.2l2.6 2.6" />
    </>
  ),
  windowOff: (
    <>
      <rect x="3.4" y="5" width="17.2" height="14" rx="2.8" />
      <path d="M6.6 8.6h10.8" />
      <path d="M4.6 19.4 19.4 4.6" />
    </>
  ),
  monitor: (
    <>
      <rect x="3" y="4.4" width="18" height="12.4" rx="2.6" />
      <path d="M9 20.4h6M12 16.8v3.6" />
    </>
  ),

  /* ------------------------------------------------------------ 状态 */
  check: <path d="M5.5 12.6 10 17.1l8.5-8.6" />,
  alert: (
    <>
      <path d="M12 4.6 3.6 19h16.8z" />
      <path d="M12 10v4.2" />
      <circle cx="12" cy="16.9" r="0.7" fill="currentColor" stroke="none" />
    </>
  ),
  info: (
    <>
      <circle cx="12" cy="12" r="8.4" />
      <path d="M12 11v5.2" />
      <circle cx="12" cy="8.2" r="0.8" fill="currentColor" stroke="none" />
    </>
  ),
  cross: <path d="M12 5.6 18.4 12 12 18.4 5.6 12z" />,
  clock: (
    <>
      <circle cx="12" cy="12" r="8.4" />
      <path d="M12 7.4V12l3.2 2" />
    </>
  ),

  /* ------------------------------------------------------------ 环境 / 设置 */
  folder: <path d="M3.4 6.8A2 2 0 0 1 5.4 4.8h3.4l1.8 2.2h8A2 2 0 0 1 20.6 9v8.2a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z" />,
  image: (
    <>
      <rect x="3.4" y="4.6" width="17.2" height="14.8" rx="2.8" />
      <circle cx="9" cy="10" r="1.7" />
      <path d="M5 17.4 10.2 12l3.2 3 2.6-2.2 3 3.4" />
    </>
  ),
  globe: (
    <>
      <circle cx="12" cy="12" r="8.4" />
      <path d="M3.8 9.6h16.4M3.8 14.4h16.4" />
      <path d="M12 3.6c-4 4.4-4 12.4 0 16.8 4-4.4 4-12.4 0-16.8z" />
    </>
  ),
  cpu: (
    <>
      <rect x="6.2" y="6.2" width="11.6" height="11.6" rx="2.4" />
      <rect x="9.8" y="9.8" width="4.4" height="4.4" rx="1" />
      <path d="M9.6 3.4v2.8M14.4 3.4v2.8M9.6 17.8v2.8M14.4 17.8v2.8M3.4 9.6h2.8M3.4 14.4h2.8M17.8 9.6h2.8M17.8 14.4h2.8" />
    </>
  ),
  memory: (
    <>
      <rect x="3.4" y="7.4" width="17.2" height="9.2" rx="2.2" />
      <path d="M7.4 16.6v3M12 16.6v3M16.6 16.6v3" />
      <path d="M7.6 10.8h8.8" />
    </>
  ),
  display: (
    <>
      <rect x="3" y="4" width="18" height="13" rx="2.6" />
      <path d="M8 20.4h8" />
      <path d="M10.4 8.6 14 12l-3.6 3.4z" fill="currentColor" stroke="none" />
    </>
  ),
  disk: (
    <>
      <ellipse cx="12" cy="7" rx="8" ry="3.2" />
      <path d="M4 7v10c0 1.8 3.6 3.2 8 3.2s8-1.4 8-3.2V7" />
      <path d="M4 12c0 1.8 3.6 3.2 8 3.2s8-1.4 8-3.2" />
    </>
  ),
  java: (
    <>
      <path d="M13.6 3.2c2 2.2-3.4 3.4-3.4 5.6 0 1.2 1 1.9 2 2.4" />
      <path d="M17 12.6c0 2.4-2.2 4-5 4s-5-1.6-5-4" />
      <path d="M6 19.6h12" />
    </>
  ),
  activity: <path d="M3.4 12.4h3.8l2.2-5.4 3.2 10.4 2.4-6.4 2 3.2h4.2" />,

  /* ------------------------------------------------------------ 窗口控制 */
  minimise: <path d="M5.5 12h13" />,
  maximise: <rect x="5.2" y="5.2" width="13.6" height="13.6" rx="2" />,
  restore: (
    <>
      <rect x="4.4" y="8.4" width="10" height="10" rx="2" />
      <path d="M8.8 5.2h10v10" />
    </>
  ),
  close: <path d="M6 6l12 12M18 6 6 18" />,

  /* ------------------------------------------------------------ 设备窗口工具栏 */
  back: <path d="M19 12H5m6 6-6-6 6-6" />,
  home: <path d="M4 10.6 12 4l8 6.6V20a1 1 0 0 1-1 1h-4.5v-6h-5v6H5a1 1 0 0 1-1-1z" />,
  recent: <rect x="4" y="4" width="16" height="16" rx="3.5" />,
  volumeUp: (
    <>
      <path d="M5 9.5h3L12 6v12l-4-3.5H5z" />
      <path d="M15.5 9.5a3.5 3.5 0 0 1 0 5M18 7a7 7 0 0 1 0 10" />
    </>
  ),
  volumeDown: (
    <>
      <path d="M5 9.5h3L12 6v12l-4-3.5H5z" />
      <path d="M15.5 9.5a3.5 3.5 0 0 1 0 5" />
    </>
  ),
  screenshot: (
    <>
      <path d="M4 9h3l1.6-2.2h6.8L17 9h3v10.5H4z" />
      <circle cx="12" cy="14" r="3.2" />
    </>
  ),
  pin: (
    <>
      <path d="M9 4h6l-1 6 3 3H7l3-3z" />
      <path d="M12 13v7" />
    </>
  ),
  stats: (
    <>
      <path d="M4 19.4V9.6M10 19.4V4.6M16 19.4v-6M22 19.4H2" />
    </>
  ),
};

export interface IconProps {
  name: IconName;
  /** 图标边长，默认 18。 */
  size?: number;
  /** 描边粗细，默认 1.7（小尺寸可调细到 1.5）。 */
  strokeWidth?: number;
  className?: string;
  /** 仅在图标本身承载语义时使用（此时会输出 role="img"）。 */
  title?: string;
}

/** 线性图标：24×24 viewBox + currentColor，随字号/颜色自动继承。 */
export function Icon({ name, size = 18, strokeWidth = 1.7, className, title }: IconProps) {
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      role={title ? "img" : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      focusable="false"
    >
      {title ? <title>{title}</title> : null}
      {PATHS[name]}
    </svg>
  );
}
