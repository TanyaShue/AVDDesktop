// 应用壳：标题栏 + 导航 + 页面路由 + 任务抽屉 + Toast。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS } from "../bridge/api";
import type { AppSettings, PageKey } from "../bridge/types";
import { NavRail, TitleBar } from "../components/Chrome";
import { TaskDrawer } from "../components/TaskDrawer";
import { Toasts } from "../components/ui";
import { useJobs, useTheme, useToasts, useWailsEvent } from "../hooks/useApp";
import { DevicesPage } from "../pages/DevicesPage";
import { HomePage } from "../pages/HomePage";
import { SdkPage } from "../pages/SdkPage";
import { SettingsPage } from "../pages/SettingsPage";
import "../styles/tokens.css";
import "../styles/app.css";

export default function App() {
  const [page, setPage] = useState<PageKey>("home");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const { jobs, logs } = useJobs();
  const { toasts, push, dismiss } = useToasts();

  useTheme(settings?.theme ?? "light");

  useEffect(() => {
    void (async () => {
      try {
        setSettings((await api.Settings.Get()) as AppSettings);
      } catch {
        /* 后端未就绪时忽略（首次 wails dev 生成绑定期间） */
      }
    })();
  }, []);

  // 有失败任务时自动展开任务面板，方便用户看到错误
  useWailsEvent<{ status: string; error?: { message?: string } }>(EVENTS.jobFailed, (info) => {
    setDrawerOpen(true);
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
        {page === "home" ? (
          <HomePage onToast={push} onGotoDevices={() => setPage("devices")} />
        ) : page === "devices" ? (
          <DevicesPage onToast={push} />
        ) : page === "sdk" ? (
          <SdkPage onToast={push} />
        ) : (
          <SettingsPage onToast={push} onSettingsChanged={handleSettingsChanged} />
        )}
      </div>
      <TaskDrawer
        jobs={jobs}
        logs={logs}
        expanded={drawerOpen}
        onToggle={(open) => setDrawerOpen(open)}
      />
      <Toasts toasts={toasts} onDismiss={dismiss} />
    </div>
  );
}
