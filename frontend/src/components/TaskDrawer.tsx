// 底部任务抽屉：折叠显示最活跃任务，展开显示任务列表 + 日志控制台。
import { useEffect, useMemo, useRef, useState } from "react";
import * as api from "../bridge/api";
import type { JobInfo, LogLine } from "../bridge/types";
import { humanSize, humanSpeed, jobStatusText, Progress } from "./ui";

export function TaskDrawer({
  jobs,
  logs,
  expanded,
  onToggle,
}: {
  jobs: JobInfo[];
  logs: Record<string, LogLine[]>;
  expanded: boolean;
  onToggle: (open: boolean) => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  const consoleRef = useRef<HTMLDivElement>(null);

  const active = useMemo(
    () => jobs.filter((j) => j.status === "running" || j.status === "queued"),
    [jobs],
  );

  useEffect(() => {
    if (!selected && active.length > 0) setSelected(active[0].id);
  }, [active, selected]);

  const selectedJob = jobs.find((j) => j.id === selected) ?? null;
  const lines = selected ? logs[selected] ?? [] : [];

  useEffect(() => {
    if (consoleRef.current && expanded) {
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
    }
  }, [lines.length, expanded]);

  if (jobs.length === 0) return null;

  const primary = active[0] ?? jobs[0];

  return (
    <div className="drawer">
      <div
        className="drawer__head"
        onClick={() => onToggle(!expanded)}
        role="button"
        aria-expanded={expanded}
      >
        <span className="drawer__title">{expanded ? "▾" : "▴"} 任务（{jobs.length}）</span>
        <div className="drawer__active">
          <span className="truncate" style={{ maxWidth: 260 }}>
            {primary.title}
          </span>
          <div style={{ flex: 1, maxWidth: 220 }}>
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
        <div style={{ marginLeft: "auto", display: "flex", gap: 8 }} onClick={(e) => e.stopPropagation()}>
          {active.length > 0 ? (
            <button className="btn btn--ghost" onClick={() => void api.Jobs.CancelAll()}>
              全部取消
            </button>
          ) : null}
        </div>
      </div>

      {expanded ? (
        <div className="drawer__body">
          <div className="drawer__list">
            {jobs.map((job) => (
              <div
                key={job.id}
                className={`drawer__item${job.id === selected ? " drawer__item--active" : ""}`}
                onClick={() => setSelected(job.id)}
              >
                <div className="drawer__item-title">
                  <span className="truncate" style={{ flex: 1 }}>
                    {job.title}
                  </span>
                  <span className={`chip chip--sm${statusChipClass(job)}`}>{jobStatusText(job)}</span>
                </div>
                <div className="muted" style={{ marginTop: 4, fontSize: 12 }}>
                  {job.bytesTotal > 0
                    ? `${humanSize(job.bytesDone)} / ${humanSize(job.bytesTotal)}`
                    : job.subtitle ?? ""}
                </div>
                <div style={{ marginTop: 6 }}>
                  <Progress
                    percent={job.percent}
                    indeterminate={job.status === "running" && job.percent <= 0}
                  />
                </div>
              </div>
            ))}
          </div>
          <div className="drawer__console" ref={consoleRef}>
            {lines.length === 0 ? (
              <span style={{ opacity: 0.6 }}>（暂无日志）</span>
            ) : (
              lines.map((line, i) => (
                <div key={i} className={lineClass(line)}>
                  {line.source ? `[${line.source}] ` : ""}
                  {line.message}
                </div>
              ))
            )}
          </div>
          {selectedJob && (selectedJob.status === "running" || selectedJob.status === "queued") ? (
            <div style={{ padding: 12, borderLeft: "1px solid var(--border-subtle)" }}>
              <button className="btn btn--secondary" onClick={() => void api.Jobs.Cancel(selectedJob.id)}>
                取消该任务
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
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
