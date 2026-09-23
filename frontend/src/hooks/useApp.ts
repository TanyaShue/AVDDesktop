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

/** 控制台行：日志原文 + 去重与渲染共用的 key。 */
export type ConsoleLine = LogLine & { key: string };

/** 应用日志的命名空间；任务日志各自使用 job:<id>。 */
const APP_NS = "app";

/** 任务日志的命名空间。 */
function jobNs(jobId: string): string {
  return `job:${jobId}`;
}

/**
 * 计算控制台行的唯一 key。
 *
 * 后端保证序号在来源内唯一（应用日志是进程内序号，任务日志是任务内序号），
 * 且实时推送与历史回填携带同一序号，因此同一行必定得到同一个 key：既不会被插入两次，
 * 也不会因为「同一毫秒内文本相同」而被误判成重复行丢弃。
 */
function lineKey(ns: string, line: LogLine): string {
  const seq = line.seq ?? 0;
  if (seq > 0) return `${ns}:${seq}`;
  // 兼容没有序号的日志：退化成 时间 + 来源 + 内容
  return `${ns}:${line.at}|${line.source ?? ""}|${line.message}`;
}

/** 一批待追加的日志及其来源命名空间。 */
type LineBatch = { ns: string; lines: LogLine[] };

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

/** 前端任务记录上限：与后端 Prune(maxFinishedJobs) 的保留量一致，避免长会话无限增长。 */
const JOBS_MAX = 20;

/** 按 id 覆盖/插入一条任务（保持"最新在前"的顺序）。 */
function upsertJob(list: JobInfo[], info: JobInfo): JobInfo[] {
  const idx = list.findIndex((j) => j.id === info.id);
  if (idx === -1) return [info, ...list];
  const next = [...list];
  next[idx] = info;
  return next;
}

/** 裁剪任务记录：优先保留进行中的任务，其余按开始时间从新到旧保留 JOBS_MAX 条。 */
function trimJobs(list: JobInfo[]): JobInfo[] {
  if (list.length <= JOBS_MAX) return list;
  // 先排序再裁剪：不依赖调用方传入的顺序，避免"留下最旧的 20 条"这种隐性错误。
  const ordered = [...list].sort((a, b) => b.startedAt - a.startedAt);
  const running = ordered.filter((j) => j.status === "running" || j.status === "queued");
  const finished = ordered.filter((j) => j.status !== "running" && j.status !== "queued");
  const keep = Math.max(JOBS_MAX - running.length, 0);
  return [...running, ...finished.slice(0, keep)].sort((a, b) => b.startedAt - a.startedAt);
}

/**
 * 把后端快照合并进本地列表：快照里没有的条目（订阅事件期间新创建的任务）必须保留，
 * 否则回填会抹掉刚 upsert 的任务，界面上的任务数会短暂丢失。
 */
function mergeJobs(prev: JobInfo[], snapshot: JobInfo[]): JobInfo[] {
  const byId = new Map(snapshot.map((j) => [j.id, j]));
  const extras = prev.filter((j) => !byId.has(j.id));
  return trimJobs([...extras, ...snapshot].sort((a, b) => b.startedAt - a.startedAt));
}

/** 任务列表 + 统一控制台（应用日志 + 任务日志按时间合并）。 */
export function useJobs() {
  const [jobs, setJobs] = useState<JobInfo[]>([]);
  const [lines, setLines] = useState<ConsoleLine[]>([]);

  /**
   * 追行并去重：历史回填（LogService.Tail / Jobs.Logs）与实时事件（log:line / job:log）
   * 可能包含同一行，按「来源命名空间 + 序号」去重；多来源时按时间合并，保持时间线顺序。
   */
  const append = useCallback((batches: LineBatch[]) => {
    const incoming: { ns: string; line: LogLine }[] = [];
    let sources = 0;
    for (const batch of batches) {
      if (batch.lines.length === 0) continue;
      sources++;
      for (const line of batch.lines) incoming.push({ ns: batch.ns, line });
    }
    if (incoming.length === 0) return;
    if (sources > 1) incoming.sort((a, b) => a.line.at - b.line.at);

    setLines((prev) => {
      const seen = new Set(prev.map((l) => l.key));
      const fresh: ConsoleLine[] = [];
      for (const item of incoming) {
        const key = lineKey(item.ns, item.line);
        if (seen.has(key)) continue;
        seen.add(key);
        fresh.push({ ...item.line, key });
      }
      if (fresh.length === 0) return prev;
      const next = prev.concat(fresh);
      return next.length > CONSOLE_MAX_LINES ? next.slice(next.length - CONSOLE_MAX_LINES) : next;
    });
  }, []);

  const upsert = useCallback((info: JobInfo) => {
    setJobs((prev) => trimJobs(upsertJob(prev, info)));
  }, []);

  useEffect(() => {
    // 首次进入时回填历史（页面刷新后控制台不会空）：应用日志 + 最近几个已结束任务的日志
    void (async () => {
      try {
        const list = ((await Jobs.List()) as JobInfo[]) ?? [];
        setJobs((prev) => mergeJobs(prev, list));
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
        const batches: LineBatch[] = [{ ns: APP_NS, lines: appLines }];
        finished.forEach((j, i) => batches.push({ ns: jobNs(j.id), lines: jobLines[i] }));
        append(batches);
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
  useWailsEvent<{ jobId: string; lines: LogLine[] }>(EVENTS.jobLog, ({ jobId, lines }) => {
    append([{ ns: jobId ? jobNs(jobId) : "job:?", lines: lines ?? [] }]);
  });
  useWailsEvent<LogLine>(EVENTS.logLine, (line) => {
    if (line) append([{ ns: APP_NS, lines: [line] }]);
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
