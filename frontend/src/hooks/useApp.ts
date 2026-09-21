// 事件订阅与 Toast 的通用 hooks。
import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOff, EventsOn, EVENTS, Jobs } from "../bridge/api";
import type { JobInfo, LogLine, Toast } from "../bridge/types";

/** 订阅一个 Wails 事件；handler 变化时自动重订阅。 */
export function useWailsEvent<T>(name: string, handler: (payload: T) => void): void {
  const ref = useRef(handler);
  ref.current = handler;

  useEffect(() => {
    const off = EventsOn(name, (payload: T) => ref.current(payload));
    return () => {
      if (typeof off === "function") off();
      else EventsOff(name);
    };
  }, [name]);
}

/** 任务列表：初始加载 + 事件增量更新。 */
export function useJobs() {
  const [jobs, setJobs] = useState<JobInfo[]>([]);
  const [logs, setLogs] = useState<Record<string, LogLine[]>>({});

  const upsert = useCallback((info: JobInfo) => {
    setJobs((prev) => {
      const idx = prev.findIndex((j) => j.id === info.id);
      if (idx === -1) return [info, ...prev];
      const next = [...prev];
      next[idx] = info;
      return next;
    });
  }, []);

  useEffect(() => {
    // 首次进入时拉取一次（比如页面刷新后仍有任务在跑）
    void (async () => {
      try {
        const list = (await Jobs.List()) as JobInfo[];
        setJobs(list ?? []);
      } catch {
        /* 忽略：开发模式下后端可能尚未就绪 */
      }
    })();
  }, []);

  useWailsEvent<JobInfo>(EVENTS.jobCreated, upsert);
  useWailsEvent<JobInfo>(EVENTS.jobProgress, upsert);
  useWailsEvent<JobInfo>(EVENTS.jobDone, upsert);
  useWailsEvent<JobInfo>(EVENTS.jobFailed, upsert);
  useWailsEvent<{ jobId: string; lines: LogLine[] }>(EVENTS.jobLog, ({ jobId, lines }) => {
    setLogs((prev) => {
      const merged = [...(prev[jobId] ?? []), ...(lines ?? [])];
      return { ...prev, [jobId]: merged.slice(-500) };
    });
  });

  return { jobs, logs };
}

let toastSeq = 0;

/** Toast 管理。 */
export function useToasts() {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const push = useCallback((level: Toast["level"], title: string, text?: string) => {
    const id = ++toastSeq;
    setToasts((prev) => [...prev, { id, level, title, text }]);
    setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 4200);
  }, []);

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return { toasts, push, dismiss };
}

/** 主题切换：写入 data-theme，并尊重 system 偏好。 */
export function useTheme(theme: string) {
  useEffect(() => {
    const apply = () => {
      const resolved =
        theme === "system"
          ? window.matchMedia("(prefers-color-scheme: dark)").matches
            ? "dark"
            : "light"
          : theme;
      document.documentElement.dataset.theme = resolved;
    };
    apply();
    if (theme === "system") {
      const mq = window.matchMedia("(prefers-color-scheme: dark)");
      mq.addEventListener("change", apply);
      return () => mq.removeEventListener("change", apply);
    }
  }, [theme]);
}
