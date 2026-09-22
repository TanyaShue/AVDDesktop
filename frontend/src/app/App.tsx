// 应用壳：标题栏 + 导航 + 页面路由 + 任务抽屉 + Toast。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS } from "../bridge/api";
import type { AppSettings, JobInfo, PageKey } from "../bridge/types";
import { NavRail, TitleBar } from "../components/Chrome";
import { ErrorBoundary } from "../components/ErrorBoundary";
import { TaskDrawer } from "../components/TaskDrawer";
import { Toasts } from "../components/ui";
import { useJobs, useTheme, useToasts, useWailsEvent } from "../hooks/useApp";
import { useEnvCheck } from "../hooks/useEnvCheck";
import { DevicesPage } from "../pages/DevicesPage";
import { SettingsPage } from "../pages/SettingsPage";
import "../styles/tokens.css";
import "../styles/app.css";

/** 错误边界提示用：页面中文名。 */
const PAGE_TITLES: Record<PageKey, string> = {
  devices: "设备",
  settings: "设置",
};

export default function App() {
  const [page, setPage] = useState<PageKey>("devices");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const { jobs, lines } = useJobs();
  const { toasts, push, dismiss } = useToasts();
  // 环境检查由应用壳持有：只跑一次，页面切换不重新探测；后端自检结果通过 env:changed 复用
  const env = useEnvCheck(push);

  useEffect(() => {
    void api.Settings.Get()
      .then((next) => setSettings(next as AppSettings))
      .catch(() => {
        /* 后端未就绪时忽略（首次 wails dev 生成绑定期间） */
      });
  }, []);

  useTheme(settings?.theme ?? "light");

  // 底部任务/日志面板只由用户点击展开，任务事件不修改展开状态。
  useWailsEvent<JobInfo>(EVENTS.jobFailed, (info) => {
    push("danger", "任务失败", info?.error?.message);
  });
  useWailsEvent<{ status: string; title?: string }>(EVENTS.jobDone, (info) => {
    if (info?.status === "succeeded") push("success", `${info.title ?? "任务"} 已完成`);
  });

  const handleSettingsChanged = useCallback((next: AppSettings) => setSettings(next), []);

  return (
    <div className="app">
      <TitleBar jobs={jobs} onOpenDrawer={() => setDrawerOpen(true)} />
      <div className="app__body">
        <NavRail page={page} onChange={setPage} />
        {/* key 让切换页面时自动丢弃上一个页面的错误状态 */}
        <ErrorBoundary key={page} scope={PAGE_TITLES[page]}>
          {page === "devices" ? (
            <DevicesPage onToast={push} env={env} confirmBeforeDelete={settings?.confirmBeforeDelete ?? true} />
          ) : (
            <SettingsPage
              onToast={push}
              env={env}
              jobs={jobs}
              onSettingsChanged={handleSettingsChanged}
            />
          )}
        </ErrorBoundary>
      </div>
      <TaskDrawer
        jobs={jobs}
        lines={lines}
        expanded={drawerOpen}
        onToggle={(open) => setDrawerOpen(open)}
      />
      <Toasts toasts={toasts} onDismiss={dismiss} />
    </div>
  );
}
