// logcat 实时日志面板：订阅 `logcat:line` 事件，支持过滤、快照与停止。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { LogLine } from "../bridge/types";
import { useWailsEvent } from "../hooks/useApp";
import { Modal } from "./ui";

interface Props {
  serial: string;
  avdName: string;
  onClose: () => void;
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
}

const MAX_LINES = 2000;

const FILTER_PRESETS = [
  { label: "全部", value: "" },
  { label: "错误", value: "*:E" },
  { label: "警告以上", value: "*:W" },
  { label: "本应用崩溃", value: "AndroidRuntime:E" },
  { label: "Activity 管理器", value: "ActivityManager:I" },
];

export function LogcatModal({ serial, avdName, onClose, onToast }: Props) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [jobId, setJobId] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [filter, setFilter] = useState("");
  const [keyword, setKeyword] = useState("");
  const [autoScroll, setAutoScroll] = useState(true);
  const consoleRef = useRef<HTMLDivElement>(null);

  const start = useCallback(
    async (filterText: string) => {
      try {
        const id = (await api.Adb.StartLogcat({
          serial,
          filter: filterText,
          buffer: "main",
        })) as string;
        setJobId(id);
        setRunning(true);
        setLines([]);
        onToast("info", "已开始订阅日志", `${avdName} · ${filterText || "全部"}`);
      } catch (err) {
        onToast("danger", "无法启动日志流", errorText(err));
      }
    },
    [serial, avdName, onToast],
  );

  // 初次进入自动订阅
  useEffect(() => {
    void start("");
    return () => {
      // 关闭弹窗时必须停掉长驻进程
      void api.Adb.StopLogcat("");
    };
  }, [start]);

  useWailsEvent<{ jobId: string; serial: string; line: string; level: string; at: number }>(
    EVENTS.logcatLine,
    (entry) => {
      if (entry.serial !== serial) return;
      setLines((prev) => {
        const next = [...prev, { at: entry.at, level: entry.level, source: "logcat", message: entry.line }];
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
      });
    },
  );

  useWailsEvent<{ id: string; status: string }>(EVENTS.jobFailed, (info) => {
    if (info?.id === jobId) setRunning(false);
  });
  useWailsEvent<{ id: string; status: string }>(EVENTS.jobDone, (info) => {
    if (info?.id === jobId) setRunning(false);
  });

  useEffect(() => {
    if (autoScroll && consoleRef.current) {
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
    }
  }, [lines.length, autoScroll]);

  const visible = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    if (!q) return lines;
    return lines.filter((l) => l.message.toLowerCase().includes(q));
  }, [lines, keyword]);

  const stop = async () => {
    if (!jobId) return;
    try {
      await api.Adb.StopLogcat(jobId);
    } catch (err) {
      onToast("warning", "停止日志流失败", errorText(err));
    } finally {
      setRunning(false);
      setJobId(null);
    }
  };

  const snapshot = async () => {
    try {
      const out = api.asArray((await api.Adb.LogcatSnapshot(serial, filter, 300)) as string[]);
      setLines(
        out.map((line) => ({
          at: Date.now(),
          level: / [EWF] /.test(line) ? "warn" : "info",
          source: "snapshot",
          message: line,
        })),
      );
      onToast("success", `已抓取 ${out.length} 行日志快照`);
    } catch (err) {
      onToast("danger", "抓取失败", errorText(err));
    }
  };

  return (
    <Modal
      title={`日志 · ${avdName}`}
      size="xl"
      onClose={() => {
        void stop();
        onClose();
      }}
      footer={
        <>
          <span className="row" style={{ gap: 8 }}>
            <select
              className="input"
              style={{ width: 160, height: 32 }}
              value={filter}
              onChange={(e) => {
                setFilter(e.target.value);
                if (running) void stop().then(() => start(e.target.value));
              }}
            >
              {FILTER_PRESETS.map((p) => (
                <option key={p.value} value={p.value}>
                  {p.label}
                </option>
              ))}
            </select>
            <input
              className="input"
              style={{ width: 180, height: 32 }}
              placeholder="过滤关键字…"
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
          </span>
          <div className="modal__foot-right">
            <button className="btn btn--ghost" onClick={() => void snapshot()}>
              抓取快照
            </button>
            <button
              className="btn btn--ghost"
              onClick={() => {
                void api.Env.CopyToClipboard(visible.map((l) => l.message).join("\n"));
                onToast("success", `已复制 ${visible.length} 行`);
              }}
            >
              复制
            </button>
            <button className="btn btn--ghost" onClick={() => setLines([])}>
              清空
            </button>
            {running ? (
              <button className="btn btn--secondary" onClick={() => void stop()}>
                停止订阅
              </button>
            ) : (
              <button className="btn btn--primary" onClick={() => void start(filter)}>
                开始订阅
              </button>
            )}
          </div>
        </>
      }
    >
      <div className="row" style={{ marginBottom: 8, gap: 12 }}>
        <span className={`chip${running ? " chip--success" : ""}`}>
          <span className="dot" />
          {running ? "实时订阅中" : "已停止"}
        </span>
        <span className="muted nums">
          {serial} · 显示 {visible.length} / {lines.length} 行
        </span>
        <label className="row muted" style={{ gap: 6, marginLeft: "auto" }}>
          <input type="checkbox" checked={autoScroll} onChange={(e) => setAutoScroll(e.target.checked)} />
          自动滚动
        </label>
      </div>
      <div
        className="drawer__console"
        ref={consoleRef}
        style={{ height: 420, borderRadius: "var(--r-md)" }}
      >
        {visible.length === 0 ? (
          <span style={{ opacity: 0.6 }}>{running ? "（等待日志…）" : "（暂无日志，点击“开始订阅”）"}</span>
        ) : (
          visible.map((line, i) => (
            <div key={i} className={lineClass(line)}>
              {line.message}
            </div>
          ))
        )}
      </div>
    </Modal>
  );
}

function lineClass(line: LogLine): string {
  if (line.level === "error") return "console-line--error";
  if (line.level === "warn") return "console-line--warn";
  return "";
}
