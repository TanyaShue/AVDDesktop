// 镜像源测速弹窗（对齐 docs/UI-SPEC.md §5.3）。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { AppError, MirrorSource, SpeedResult } from "../bridge/types";
import { useWailsEvent } from "../hooks/useApp";
import { humanSize, Modal, ScoreBar } from "./ui";

interface Props {
  activeSourceId: string;
  onClose: () => void;
  onUseSource: (sourceId: string) => Promise<void> | void;
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
}

export function SpeedTestModal({ activeSourceId, onClose, onUseSource, onToast }: Props) {
  const [sources, setSources] = useState<MirrorSource[]>([]);
  const [results, setResults] = useState<Record<string, SpeedResult>>({});
  const [selected, setSelected] = useState<string>(activeSourceId);
  const [testing, setTesting] = useState(false);
  const [jobId, setJobId] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [newURL, setNewURL] = useState("");
  const [newName, setNewName] = useState("");

  const load = useCallback(async () => {
    const list = api.asArray((await api.Mirror.ListSources()) as MirrorSource[]);
    setSources(list);
    const map: Record<string, SpeedResult> = {};
    for (const s of list) {
      if (s.lastResult) map[s.id] = s.lastResult;
    }
    setResults(map);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<SpeedResult>(EVENTS.speedtestResult, (r) => {
    setResults((prev) => ({ ...prev, [r.sourceId]: r }));
  });

  useWailsEvent<{ id: string; status: string }>(EVENTS.jobDone, (info) => {
    if (info?.id === jobId) setTesting(false);
  });
  useWailsEvent<{ id: string; status: string; error?: AppError }>(EVENTS.jobFailed, (info) => {
    if (info?.id === jobId) {
      setTesting(false);
      onToast("danger", "测速失败", info.error?.message);
    }
  });

  const startTest = async (ids?: string[]) => {
    setTesting(true);
    try {
      const id = await api.Mirror.TestSources({ sourceIds: ids ?? [], maxThroughputMB: 4, quick: false });
      setJobId(id);
    } catch (err) {
      setTesting(false);
      onToast("danger", "无法启动测速", errorText(err));
    }
  };

  const addCustom = async () => {
    if (!newURL.trim()) return;
    try {
      const src = (await api.Mirror.AddSource({
        name: newName.trim(),
        baseURL: newURL.trim(),
        probe: true,
      })) as MirrorSource;
      onToast("success", "已添加镜像源", src.name);
      setAdding(false);
      setNewURL("");
      setNewName("");
      await load();
    } catch (err) {
      onToast("danger", "添加失败", errorText(err));
    }
  };

  return (
    <Modal
      title="镜像源测速"
      size="lg"
      onClose={onClose}
      footer={
        <>
          <span className="muted">测速会下载少量数据用于估算速度，不会修改任何 SDK 文件。</span>
          <div className="modal__foot-right">
            <button className="btn btn--ghost" onClick={() => setAdding((v) => !v)}>
              {adding ? "取消添加" : "+ 添加自定义源"}
            </button>
            <button className="btn btn--secondary" disabled={testing} onClick={() => void startTest()}>
              {testing ? "测速中…" : "全部重测"}
            </button>
            <button
              className="btn btn--primary"
              disabled={!selected}
              onClick={() => {
                void onUseSource(selected);
                onClose();
              }}
            >
              使用该源
            </button>
          </div>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0 }}>
        为 SDK、模拟器与系统镜像选择下载源。评分 = 延迟 25% + 下载速度 50% + 稳定性 25%。
      </p>

      {adding ? (
        <div className="card" style={{ padding: 16, marginBottom: 16 }}>
          <div className="field">
            <div className="field__label">镜像地址</div>
            <div className="field__control">
              <input
                className="input"
                placeholder="https://mirrors.example.com/AndroidSDK/"
                value={newURL}
                onChange={(e) => setNewURL(e.target.value)}
              />
              <div className="field__hint">
                需要是 `dl.google.com/android/repository/` 的目录镜像（包含 repository2-3.xml 与各 zip）
              </div>
            </div>
          </div>
          <div className="field">
            <div className="field__label">名称（可选）</div>
            <div className="field__control">
              <input
                className="input"
                placeholder="例如：公司内网镜像"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
              />
            </div>
          </div>
          <button className="btn btn--primary" onClick={() => void addCustom()}>
            添加并预检
          </button>
        </div>
      ) : null}

      <table className="table">
        <thead>
          <tr>
            <th style={{ width: 36 }} />
            <th>源</th>
            <th style={{ width: 110 }}>状态</th>
            <th style={{ width: 90 }}>延迟</th>
            <th style={{ width: 110 }}>下载速度</th>
            <th style={{ width: 120 }}>评分</th>
            <th style={{ width: 90 }} />
          </tr>
        </thead>
        <tbody>
          {sources.map((src) => {
            const r = results[src.id];
            const pending = testing && !r;
            return (
              <tr key={src.id}>
                <td>
                  <input
                    type="radio"
                    name="mirror"
                    checked={selected === src.id}
                    onChange={() => setSelected(src.id)}
                    aria-label={`选择 ${src.name}`}
                  />
                </td>
                <td>
                  <div style={{ fontWeight: 500 }}>
                    {src.name}
                    {src.id === activeSourceId ? <span className="chip chip--sm chip--info" style={{ marginLeft: 8 }}>当前</span> : null}
                    {src.kind === "official" ? <span className="chip chip--sm" style={{ marginLeft: 8 }}>官方</span> : null}
                  </div>
                  <div className="mono truncate" style={{ color: "var(--text-tertiary)", maxWidth: 360 }}>
                    {src.baseURL}
                  </div>
                  {src.note ? (
                    <div className="muted" style={{ fontSize: 12 }}>
                      {src.note}
                    </div>
                  ) : null}
                  {r?.error ? (
                    <div className="muted" style={{ fontSize: 12, color: "var(--danger)" }}>
                      {r.error}
                    </div>
                  ) : null}
                </td>
                <td>
                  {pending ? (
                    <span className="row muted">
                      <span className="spinner" /> 测速中
                    </span>
                  ) : (
                    <GradeChip result={r} />
                  )}
                </td>
                <td className="nums">{r ? `${r.ttfbMs} ms` : "—"}</td>
                <td className="nums">{r && r.throughputMBps > 0 ? `${r.throughputMBps.toFixed(2)} MB/s` : "—"}</td>
                <td>
                  {r ? (
                    <div className="row">
                      <span className="nums" style={{ width: 24 }}>
                        {r.score}
                      </span>
                      <ScoreBar score={r.score} />
                    </div>
                  ) : (
                    "—"
                  )}
                </td>
                <td>
                  <button
                    className="btn btn--ghost"
                    disabled={testing}
                    onClick={() => void startTest([src.id])}
                  >
                    重测
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>

      <div className="muted" style={{ marginTop: 12 }}>
        提示：镜像可能只同步了索引而缺少包文件。安装时会自动回退到 Google 官方源（可在设置中关闭）。
        {results && Object.values(results).some((r) => r.rangeSupported)
          ? `　支持断点续传的源：${Object.values(results)
              .filter((r) => r.rangeSupported)
              .map((r) => sources.find((s) => s.id === r.sourceId)?.name ?? r.sourceId)
              .join("、")}`
          : ""}
      </div>
    </Modal>
  );
}

function GradeChip({ result }: { result?: SpeedResult }) {
  if (!result) return <span className="chip">未测速</span>;
  switch (result.grade) {
    case "recommended":
      return <span className="chip chip--success">推荐</span>;
    case "usable":
      return <span className="chip chip--info">可用</span>;
    case "index-only":
      return <span className="chip chip--warning">仅索引</span>;
    default:
      return <span className="chip chip--danger">不可用</span>;
  }
}

export function formatBytesForHint(bytes: number): string {
  return humanSize(bytes);
}
