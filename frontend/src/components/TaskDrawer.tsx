// 底部统一日志 / 任务区域。
//
// 常驻显示：折叠态给出进行中任务数与最近一条日志，展开态为任务列表 + 统一控制台
// （应用日志、模拟器输出与任务日志合并成同一条时间线）。
import { useEffect, useMemo, useRef } from "react";
import * as api from "../bridge/api";
import type { JobInfo, LogLine } from "../bridge/types";
import type { ConsoleLine } from "../hooks/useApp";
import { humanSize, humanSpeed, jobStatusText, Progress } from "./ui";

export function TaskDrawer({
  jobs,
  lines,
  expanded,
  onToggle,
}: {
  jobs: JobInfo[];
  lines: ConsoleLine[];
  expanded: boolean;
  onToggle: (open: boolean) => void;
}) {
  const consoleRef = useRef<HTMLDivElement>(null);

  const active = useMemo(
    () => jobs.filter((j) => j.status === "running" || j.status === "queued"),
    [jobs],
  );
  const primary = active[0] ?? null;
  const latest = lines.length > 0 ? lines[lines.length - 1] : null;

  // 跟随最后一行而不是行数：控制台裁剪到 CONSOLE_MAX_LINES 后长度恒定，
  // 用 lines.length 作依赖会让自动滚动在打满上限后彻底失效。
  const lastKey = lines.length > 0 ? lines[lines.length - 1].key : "";

  useEffect(() => {
    if (expanded && consoleRef.current) {
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
    }
  }, [lastKey, expanded]);

  return (
    <div className="drawer">
      <div
        className="drawer__head"
        onClick={() => onToggle(!expanded)}
        role="button"
        aria-expanded={expanded}
      >
        <span className="drawer__title">{expanded ? "▾" : "▴"} 任务</span>
        <span className="drawer__count">
          {active.length > 0 ? `${active.length} 个进行中` : `${jobs.length} 条记录`}
        </span>

        {primary ? (
          <div className="drawer__active">
            <span className="truncate" style={{ maxWidth: 220 }}>
              {primary.title}
            </span>
            <div style={{ flex: 1, maxWidth: 180 }}>
              <Progress
                percent={primary.percent}
                indeterminate={primary.status === "running" && primary.percent <= 0}
              />
            </div>
            <span className="muted nums">
              {Math.round(primary.percent)}%　{humanSpeed(primary.speedBps)}
              {primary.etaSeconds > 0 ? `　剩余 ${formatEta(primary.etaSeconds)}` : ""}
            </span>
          </div>
        ) : null}
        {/* 折叠态始终能看到最近一条日志（含没有任务时的应用日志） */}
        {!expanded ? <span className="drawer__latest truncate">{latestText(latest)}</span> : null}

        <div className="drawer__head-actions" onClick={(e) => e.stopPropagation()}>
          <button
            className="btn btn--ghost drawer__toggle"
            aria-label={expanded ? "收起任务日志" : "展开任务日志"}
            onClick={() => onToggle(!expanded)}
          >
            {expanded ? "收起" : "展开"}
          </button>
        </div>
      </div>

      {expanded ? (
        <div className="drawer__body">
          <div className="drawer__tasks">
            <div className="drawer__list-head">
              <span className="drawer__list-title">任务记录</span>
              {active.length > 0 ? (
                <button
                  className="btn btn--danger-ghost btn--sm"
                  onClick={() => void api.Jobs.CancelAll()}
                >
                  停止全部
                </button>
              ) : null}
            </div>
            <div className="drawer__list">
              {jobs.length === 0 ? (
                <div className="muted drawer__empty">暂无任务</div>
              ) : (
                jobs.map((job) => (
                  <div key={job.id} className="drawer__item">
                    <div className="drawer__item-title">
                      <span className="truncate" style={{ flex: 1 }}>
                        {job.title}
                      </span>
                      <span className={`chip chip--sm${statusChipClass(job)}`}>{jobStatusText(job)}</span>
                      {job.status === "running" || job.status === "queued" ? (
                        <button
                          className="btn btn--danger-ghost drawer__stop"
                          title={`停止任务：${job.title}`}
                          onClick={() => void api.Jobs.Cancel(job.id)}
                        >
                          停止
                        </button>
                      ) : null}
                    </div>
                    {detail(job) ? (
                      <div className="muted" style={{ marginTop: 4, fontSize: 12 }}>
                        {detail(job)}
                      </div>
                    ) : null}
                    <div style={{ marginTop: 6 }}>
                      <Progress
                        percent={job.percent}
                        indeterminate={job.status === "running" && job.percent <= 0}
                        showPercent
                      />
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
          <div className="drawer__console" ref={consoleRef}>
            {lines.length === 0 ? (
              <span style={{ opacity: 0.6 }}>（暂无日志）</span>
            ) : (
              lines.map((line) => (
                <div key={line.key} className={lineClass(line)}>
                  {line.source ? `[${line.source}] ` : ""}
                  {line.message}
                </div>
              ))
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}

/** 任务副行：阶段优先，其次字节进度或子标题。 */
function detail(job: JobInfo): string {
  const parts: string[] = [];
  if (job.phase) parts.push(job.phase);
  if (job.bytesTotal > 0) {
    parts.push(`${humanSize(job.bytesDone)} / ${humanSize(job.bytesTotal)}`);
  } else if (job.bytesDone > 0) {
    // 总量未知（例如仓库未返回归档大小）时也要能看到下载在推进
    parts.push(`已下载 ${humanSize(job.bytesDone)}`);
  } else if (job.subtitle) {
    parts.push(job.subtitle);
  }
  return parts.join("　");
}

/** 折叠态显示最近一条日志，带来源前缀。 */
function latestText(line: LogLine | null): string {
  if (!line) return "（暂无日志）";
  return `${line.source ? `[${line.source}] ` : ""}${line.message}`;
}

function statusChipClass(job: JobInfo): string {
  switch (job.status) {
    case "succeeded":
      return " chip--success";
    case "failed":
      return " chip--danger";
    case "running":
      return " chip--info";
    default:
      return "";
  }
}

function lineClass(line: LogLine): string {
  if (line.level === "error") return "console-line--error";
  if (line.level === "warn") return "console-line--warn";
  return "";
}

function formatEta(seconds: number): string {
  if (seconds < 60) return `${seconds} 秒`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m} 分 ${s} 秒`;
}
