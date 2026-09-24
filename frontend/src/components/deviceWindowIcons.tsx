// 设备窗口图标：与主窗口自绘标题栏同一风格（线性 SVG，currentColor 描边）。
import type { ReactNode } from "react";

export type IconName =
  | "back"
  | "home"
  | "recent"
  | "volumeUp"
  | "volumeDown"
  | "screenshot"
  | "pin"
  | "minimise"
  | "maximise"
  | "restore"
  | "close";

const PATHS: Record<IconName, ReactNode> = {
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
  minimise: <path d="M5 12h14" />,
  maximise: <rect x="5" y="5" width="14" height="14" rx="2" />,
  restore: (
    <>
      <rect x="4.5" y="8.5" width="10" height="10" rx="2" />
      <path d="M8.5 5.5h11v11" />
    </>
  ),
  close: <path d="M6 6l12 12M18 6 6 18" />,
};

export function Icon({ name, size = 18 }: { name: IconName; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.7}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      {PATHS[name]}
    </svg>
  );
}
