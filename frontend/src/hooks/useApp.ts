// 事件订阅与 Toast 的通用 hooks。
import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOff, EventsOn, EVENTS, Jobs, Logs } from "../bridge/api";
import type { JobInfo, LogLine, Toast } from "../bridge/types";

/** 统一控制台的行数上限（避免长任务把前端状态撑爆）。 */
const CONSOLE_MAX_LINES = 800;
/** 初始回填的应用日志行数。 */
const CONSOLE_TAIL = 200;
/** 初始回填日志的已结束任务数。 */
const CONSOLE_BACKFILL_JOBS = 5;

/** 控制台去重键 / React key：应用日志用后端唯一序号，任务日志（无序号）用时间 + 来源 + 内容。
 *  同一条日志无论是实时推送还是历史回填，都得到同一个 id，因此不可能被插入两次。 */
export function lineId(line: LogLine): string {
  // seq 只在应用日志上有值（任务日志不带序号），因此按可选处理
  const seq = line.seq ?? 0;
  if (seq > 0) return `a:${seq}`;
  return `j:${line.at}|${line.source ?? ""}|${line.message}`;
}

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

/** 任务列表 + 统一控制台（应用日志 + 任务日志按时间合并）。 */
export function useJobs() {
  const [jobs, setJobs] = useState<JobInfo[]>([]);
  const [lines, setLines] = useState<LogLine[]>([]);

  /**
   * 追行并去重：历史回填（LogService.Tail / Jobs.Logs）与实时事件（log:line / job:log）
   * 可能包含同一行，按 lineId 去重（含同一批次内部），避免启动瞬间同一行出现两次。
   */
  const append = useCallback((incoming: LogLine[]) => {
    if (incoming.length === 0) return;
    setLines((prev) => {
      const seen = new Set(prev.map(lineId));
      const fresh: LogLine[] = [];
      for (const line of incoming) {
        const id = lineId(line);
        if (seen.has(id)) continue;
        seen.add(id);
        fresh.push(line);
      }
      if (fresh.length === 0) return prev;
      const next = prev.concat(fresh);
      return next.length > CONSOLE_MAX_LINES ? next.slice(next.length - CONSOLE_MAX_LINES) : next;
    });
  }, []);

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
    // 首次进入时回填历史（页面刷新后控制台不会空）：应用日志 + 最近几个已结束任务的日志
    void (async () => {
      try {
        const list = ((await Jobs.List()) as JobInfo[]) ?? [];
        setJobs(list);
        const appLines = ((await Logs.Tail(CONSOLE_TAIL)) as LogLine[]) ?? [];
        const finished = list
          .filter((j) => j.status !== "running" && j.status !== "queued")
          .slice(0, CONSOLE_BACKFILL_JOBS);
        const jobLines = await Promise.all(
          finished.map(async (j) => {
            try {
              return ((await Jobs.Logs(j.id, CONSOLE_TAIL)) as LogLine[]) ?? [];
            } catch {
              return []; // 任务刚被清理时忽略，不影响其它历史回填
            }
          }),
        );
        append(appLines.concat(...jobLines).sort((a, b) => a.at - b.at));
      } catch {
        /* 忽略：开发模式下后端可能尚未就绪 */
      }
    })();
  }, [append]);

  useWailsEvent<JobInfo>(EVENTS.jobCreated, upsert);
  useWailsEvent<JobInfo>(EVENTS.jobProgress, upsert);
  useWailsEvent<JobInfo>(EVENTS.jobDone, upsert);
  useWailsEvent<JobInfo>(EVENTS.jobFailed, upsert);
  // 任务日志与应用日志（含 module=emulator 的模拟器输出）汇入同一条控制台时间线
  useWailsEvent<{ jobId: string; lines: LogLine[] }>(EVENTS.jobLog, ({ lines }) => {
    append(lines ?? []);
  });
  useWailsEvent<LogLine>(EVENTS.logLine, (line) => {
    if (line) append([line]);
  });

  return { jobs, lines };
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
