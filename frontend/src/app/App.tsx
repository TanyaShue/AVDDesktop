// 应用壳：标题栏 + 导航 + 页面路由 + 任务抽屉 + Toast。
import { useCallback, useEffect, useRef, useState } from "react";
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

  // 设置只加载一次；启动自检的任务可能早于设置到达，因此用同一个 Promise 等结果
  const settingsRef = useRef<AppSettings | null>(null);
  const settingsPromise = useRef<Promise<AppSettings | null> | null>(null);
  const loadSettings = useCallback(() => {
    if (!settingsPromise.current) {
      settingsPromise.current = api.Settings.Get()
        .then((next) => {
          settingsRef.current = next as AppSettings;
          return settingsRef.current;
        })
        .catch(() => null); // 后端未就绪时忽略（首次 wails dev 生成绑定期间）
    }
    return settingsPromise.current;
  }, []);

  useTheme(settings?.theme ?? "light");

  useEffect(() => {
    void loadSettings().then((next) => {
      if (next) setSettings(next);
    });
  }, [loadSettings]);

  // 「自动展开任务区域」设置：任务开始/失败时是否自动展开
  // （先等首次加载完成，设置页的修改由 handleSettingsChanged 同步进 ref，始终按最新值判断）
  const shouldAutoExpand = useCallback(async () => {
    await loadSettings();
    return settingsRef.current?.showTaskDrawer ?? true;
  }, [loadSettings]);

  useWailsEvent<JobInfo>(EVENTS.jobCreated, (info) => {
    // 启动模拟器属于用户主动操作，进度在设备卡片上有明确状态；
    // 不要因为这个短任务自动展开底部日志面板。
    if (info?.kind === "emulator-start") return;
    void shouldAutoExpand().then((open) => {
      if (open) setDrawerOpen(true);
    });
  });
  useWailsEvent<JobInfo>(EVENTS.jobFailed, (info) => {
    // 启动失败用 Toast 提示即可；用户没有主动要求查看日志时不要打断当前页面布局。
    if (info?.kind !== "emulator-start") {
      void shouldAutoExpand().then((open) => {
        if (open) setDrawerOpen(true);
      });
    }
    push("danger", "任务失败", info?.error?.message);
  });

  // 启动自检的任务可能在前端订阅事件之前就已开始：首次看到进行中的任务时补一次自动展开
  const autoExpandApplied = useRef(false);
  useEffect(() => {
    if (autoExpandApplied.current || !settings) return;
    if (!jobs.some((j) => (j.status === "running" || j.status === "queued") && j.kind !== "emulator-start")) return;
    autoExpandApplied.current = true;
    if (settings.showTaskDrawer) setDrawerOpen(true);
  }, [settings, jobs]);
  useWailsEvent<{ status: string; title?: string }>(EVENTS.jobDone, (info) => {
    if (info?.status === "succeeded") push("success", `${info.title ?? "任务"} 已完成`);
  });

  const handleSettingsChanged = useCallback((next: AppSettings) => {
    settingsRef.current = next;
    setSettings(next);
  }, []);

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
