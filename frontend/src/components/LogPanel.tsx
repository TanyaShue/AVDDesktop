// 运行日志面板：实时订阅 `log:line`，支持级别切换与导出。
//
// 与任务抽屉的区别：任务抽屉只显示某个 Job 的日志；
// 这里是应用级日志（含环境自检、镜像测速、下载器、模拟器生命周期、任务转写）。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { LogLine } from "../bridge/types";
import { useWailsEvent } from "../hooks/useApp";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
}

const LEVELS = [
  { value: "debug", label: "调试（最详细）" },
  { value: "info", label: "信息（推荐）" },
  { value: "warn", label: "警告及以上" },
  { value: "error", label: "仅错误" },
];

const MAX_LINES = 1000;

export function LogPanel({ onToast }: Props) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [level, setLevel] = useState("info");
  const [filter, setFilter] = useState("");
  const [autoScroll, setAutoScroll] = useState(true);
  const [expanded, setExpanded] = useState(false);
  const consoleRef = useRef<HTMLDivElement>(null);

  const load = useCallback(async () => {
    try {
      const [entries, currentLevel] = await Promise.all([
        api.Diagnostics.ReadAppLog(300) as Promise<LogLine[] | null>,
        api.Diagnostics.LogLevel() as Promise<string>,
      ]);
      setLines(entries ?? []);
      setLevel(currentLevel || "info");
    } catch (err) {
      onToast("warning", "无法读取运行日志", errorText(err));
    }
  }, [onToast]);

  useEffect(() => {
    void load();
  }, [load]);

  // 只在面板展开时接收实时日志，避免无意义的前端重渲染
  useWailsEvent<{ at: number; level: string; module: string; message: string }>(
    EVENTS.logLine,
    (entry) => {
      if (!expanded) return;
      setLines((prev) => {
        const next = [...prev, { at: entry.at, level: entry.level, source: entry.module, message: entry.message }];
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
      });
    },
  );

  useEffect(() => {
    if (autoScroll && expanded && consoleRef.current) {
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
    }
  }, [lines.length, autoScroll, expanded]);

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return lines;
    return lines.filter(
      (l) => l.message.toLowerCase().includes(q) || (l.source ?? "").toLowerCase().includes(q),
    );
  }, [lines, filter]);

  const changeLevel = async (next: string) => {
    try {
      await api.Diagnostics.SetLogLevel(next);
      setLevel(next);
      onToast("success", `日志级别已切换为 ${next}`, "立即生效，无需重启");
    } catch (err) {
      onToast("danger", "切换失败", errorText(err));
    }
  };

  const copyAll = () => {
    const text = visible.map((l) => `[${l.level}] [${l.source ?? "app"}] ${l.message}`).join("\n");
    void api.Env.CopyToClipboard(text);
    onToast("success", `已复制 ${visible.length} 条日志到剪贴板`);
  };

  return (
    <div className="card" style={{ padding: 20 }}>
      <div className="row" style={{ flexWrap: "wrap", gap: 8, marginBottom: 12 }}>
        <select className="input" style={{ width: 160 }} value={level} onChange={(e) => void changeLevel(e.target.value)}>
          {LEVELS.map((l) => (
            <option key={l.value} value={l.value}>
              {l.label}
            </option>
          ))}
        </select>
        <input
          className="input"
          style={{ flex: 1, minWidth: 160 }}
          placeholder="过滤关键字（模块或内容）…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <button className="btn btn--secondary" onClick={() => setExpanded((v) => !v)}>
          {expanded ? "收起实时日志" : "展开实时日志"}
        </button>
        <button className="btn btn--ghost" onClick={() => void load()}>
          刷新
        </button>
        <button className="btn btn--ghost" onClick={copyAll}>
          复制
        </button>
        <button className="btn btn--ghost" onClick={() => void api.Diagnostics.OpenLogFolder()}>
          打开日志目录
        </button>
      </div>

      {expanded ? (
        <>
          <div className="row" style={{ gap: 12, marginBottom: 8 }}>
            <label className="row muted" style={{ gap: 6 }}>
              <input type="checkbox" checked={autoScroll} onChange={(e) => setAutoScroll(e.target.checked)} />
              自动滚动
            </label>
            <span className="muted nums">
              共 {lines.length} 条，显示 {visible.length} 条
            </span>
            <div style={{ marginLeft: "auto", display: "flex", gap: 8 }}>
              <button
                className="btn btn--ghost"
                onClick={() =>
                  void (async () => {
                    try {
                      const l = await api.Diagnostics.TestLog("来自设置页的测试日志");
                      onToast("info", "已写入一条测试日志", `当前级别：${l}`);
                    } catch (err) {
                      onToast("danger", "写入失败", errorText(err));
                    }
                  })()
                }
              >
                写入测试日志
              </button>
              <button className="btn btn--ghost" onClick={() => setLines([])}>
                清空显示
              </button>
            </div>
          </div>
          <div className="drawer__console" ref={consoleRef} style={{ height: 260, borderRadius: "var(--r-md)" }}>
            {visible.length === 0 ? (
              <span style={{ opacity: 0.6 }}>（暂无日志）</span>
            ) : (
              visible.map((line, i) => (
                <div key={i} className={lineClass(line)}>
                  {line.source ? `[${line.source}] ` : ""}
                  {line.message}
                </div>
              ))
            )}
          </div>
        </>
      ) : (
        <div className="muted">
          日志位于<span className="mono">应用数据目录/logs</span>，按天与 8 MB 滚动，默认保留 7 天。
          展开后可实时查看（含环境自检、镜像测速、下载与模拟器生命周期）。
        </div>
      )}
    </div>
  );
}

function lineClass(line: LogLine): string {
  if (line.level === "error") return "console-line--error";
  if (line.level === "warn") return "console-line--warn";
  return "";
}
