// 自绘标题栏 + 左侧导航栏（对齐 assets/ 参考设计）。
import { Win } from "../bridge/api";
import type { JobInfo, PageKey } from "../bridge/types";
import { Icon, type IconName } from "./icons";

const NAV: Array<{ key: PageKey; label: string; icon: IconName }> = [
  { key: "devices", label: "设备", icon: "devices" },
  { key: "settings", label: "设置", icon: "settings" },
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
      <div className="titlebar__icon">
        <Icon name="devices" size={17} />
      </div>
      <div className="titlebar__title">AVDDesktop</div>
      <div className="titlebar__spacer" />
      {running.length > 0 ? (
        <div className="titlebar__taskpill" onClick={onOpenDrawer} title="打开任务面板">
          <span className="spinner" style={{ width: 12, height: 12, borderWidth: 2 }} />
          {running.length} 个任务进行中
        </div>
      ) : null}
      <div className="titlebar__divider" />
      <div className="titlebar__buttons">
        <button className="winbtn" title="最小化" onClick={() => void Win.Minimise()}>
          <Icon name="minimise" size={16} />
        </button>
        <button className="winbtn" title="最大化/还原" onClick={() => void Win.ToggleMaximise()}>
          <Icon name="maximise" size={15} />
        </button>
        <button className="winbtn winbtn--close" title="关闭" onClick={() => void Win.Close()}>
          <Icon name="close" size={15} />
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
          <Icon name={item.icon} size={22} strokeWidth={1.6} />
          <span className="navrail__label">{item.label}</span>
        </button>
      ))}
      <div className="navrail__spacer" />
    </nav>
  );
}
