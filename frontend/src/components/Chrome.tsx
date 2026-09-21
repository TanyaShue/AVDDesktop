// 自绘标题栏 + 左侧导航栏（对齐 assets/ 参考设计）。
import { Win } from "../bridge/api";
import type { JobInfo, PageKey } from "../bridge/types";

const NAV: Array<{ key: PageKey; label: string; icon: string }> = [
  { key: "home", label: "首页", icon: "🏠" },
  { key: "devices", label: "设备", icon: "📱" },
  { key: "sdk", label: "SDK", icon: "🧩" },
  { key: "settings", label: "设置", icon: "⚙️" },
];

export function TitleBar({
  jobs,
  onOpenDrawer,
}: {
  jobs: JobInfo[];
  onOpenDrawer: () => void;
}) {
  const running = jobs.filter((j) => j.status === "running" || j.status === "queued");
  return (
    <div className="titlebar">
      <div className="titlebar__icon">A</div>
      <div className="titlebar__title">AVDDesktop</div>
      <div className="titlebar__spacer" />
      {running.length > 0 ? (
        <div className="titlebar__taskpill" onClick={onOpenDrawer} title="打开任务面板">
          <span className="spinner" style={{ width: 10, height: 10, borderWidth: 2 }} />
          {running.length} 个任务进行中
        </div>
      ) : null}
      <div className="titlebar__buttons">
        <button className="winbtn" title="最小化" onClick={() => void Win.Minimise()}>
          <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden>
            <rect x="2" y="5.5" width="8" height="1" fill="currentColor" />
          </svg>
        </button>
        <button className="winbtn" title="最大化/还原" onClick={() => void Win.ToggleMaximise()}>
          <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden>
            <rect x="2.5" y="2.5" width="7" height="7" fill="none" stroke="currentColor" />
          </svg>
        </button>
        <button className="winbtn winbtn--close" title="关闭" onClick={() => void Win.Close()}>
          <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden>
            <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.2" />
          </svg>
        </button>
      </div>
    </div>
  );
}

export function NavRail({
  page,
  onChange,
}: {
  page: PageKey;
  onChange: (page: PageKey) => void;
}) {
  return (
    <nav className="navrail" aria-label="主导航">
      {NAV.map((item) => (
        <button
          key={item.key}
          className={`navrail__item${page === item.key ? " navrail__item--active" : ""}`}
          onClick={() => onChange(item.key)}
          aria-current={page === item.key}
        >
          <span style={{ fontSize: 20, lineHeight: 1 }} aria-hidden>
            {item.icon}
          </span>
          <span className="navrail__label">{item.label}</span>
        </button>
      ))}
      <div className="navrail__spacer" />
    </nav>
  );
}
