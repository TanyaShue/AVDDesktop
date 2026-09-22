// 环境准备弹窗：分别检测 Android SDK 与 JDK 镜像的连接、延迟、下载速度和资源完整性。
import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { MirrorCheck, MirrorResource } from "../bridge/types";
import { formatMs, humanSize, humanSpeed, Modal } from "./ui";

interface Props {
  currentSourceId?: string;
  currentJdkSourceId?: string;
  /** JDK 已可用时，即使 JDK 镜像检测失败也不阻塞 SDK 准备/修复。 */
  jdkReady?: boolean;
  onClose: () => void;
  onStart: (sourceId: string, jdkSourceId: string) => Promise<void>;
  onRepair: (sourceId: string, jdkSourceId: string) => Promise<void>;
}

export function EnvironmentSetupModal({
  currentSourceId = "",
  currentJdkSourceId = "",
  jdkReady = false,
  onClose,
  onStart,
  onRepair,
}: Props) {
  const [sdkChecks, setSdkChecks] = useState<MirrorCheck[]>([]);
  const [jdkChecks, setJdkChecks] = useState<MirrorCheck[]>([]);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [repairing, setRepairing] = useState(false);
  const [error, setError] = useState("");
  const [selectedSDKId, setSelectedSDKId] = useState(currentSourceId);
  const [selectedJDKId, setSelectedJDKId] = useState(currentJdkSourceId);

  const runChecks = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [sdkRaw, jdkRaw] = await Promise.all([api.Mirror.CheckAll(), api.Mirror.CheckJDKAll()]);
      const nextSDK = api.asArray(sdkRaw as MirrorCheck[]);
      const nextJDK = api.asArray(jdkRaw as MirrorCheck[]);
      setSdkChecks(nextSDK);
      setJdkChecks(nextJDK);
      setSelectedSDKId((previous) => chooseSource(nextSDK, previous || currentSourceId));
      setSelectedJDKId((previous) => chooseSource(nextJDK, previous || currentJdkSourceId));
    } catch (err) {
      setError(errorText(err));
    } finally {
      setLoading(false);
    }
  }, [currentSourceId, currentJdkSourceId]);

  useEffect(() => {
    void runChecks();
  }, [runChecks]);

  const selectedSDK = useMemo(
    () => sdkChecks.find((item) => item.sourceId === selectedSDKId) ?? null,
    [sdkChecks, selectedSDKId],
  );
  const selectedJDK = useMemo(
    () => jdkChecks.find((item) => item.sourceId === selectedJDKId) ?? null,
    [jdkChecks, selectedJDKId],
  );
  const sdkUsable = Boolean(selectedSDK?.reachable && selectedSDK?.compatible);
  const jdkUsable = Boolean(selectedJDK?.reachable && selectedJDK?.compatible);
  const canStart = sdkUsable && (jdkReady || jdkUsable) && !loading && !starting && !repairing;

  const start = async () => {
    if (!selectedSDK || !canStart) return;
    setStarting(true);
    setError("");
    try {
      await onStart(selectedSDK.sourceId, selectedJDK?.sourceId || selectedJDKId);
      onClose();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setStarting(false);
    }
  };

  const repair = async () => {
    if (!selectedSDK || !canStart) return;
    setRepairing(true);
    setError("");
    try {
      await onRepair(selectedSDK.sourceId, selectedJDK?.sourceId || selectedJDKId);
      onClose();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setRepairing(false);
    }
  };

  return (
    <Modal
      title="下载与补充环境"
      size="xl"
      onClose={onClose}
      footer={
        <div className="mirror-modal__foot">
          <div className="mirror-modal__selection mirror-modal__selection--wrap">
            <Selection label="SDK" check={selectedSDK} />
            <Selection label="JDK" check={selectedJDK} optional={jdkReady} />
          </div>
          <div className="row row--wrap">
            <button className="btn btn--secondary" onClick={onClose} disabled={starting || repairing}>
              取消
            </button>
            <button className="btn btn--secondary" onClick={() => void repair()} disabled={!canStart}>
              {repairing ? <span className="spinner" /> : "一键修复"}
            </button>
            <button className="btn btn--primary" onClick={() => void start()} disabled={!canStart}>
              {starting ? <span className="spinner" /> : "准备 / 补装"}
            </button>
          </div>
        </div>
      }
    >
      <div className="mirror-modal">
        <div className="banner banner--info mirror-modal__notice">
          <div className="banner__icon">⇄</div>
          <div className="banner__body">
            <div className="banner__title">Android SDK 与 JDK 分别选择下载镜像</div>
            <div className="banner__text">
              JDK 国内镜像与 GitHub 官方发布同一 Eclipse Temurin 21 归档，下载后始终使用程序内置 SHA-256
              校验。准备环境时只会下载缺失组件；「一键修复」会重装核心 SDK 工具链，并保留许可、系统镜像和 AVD。
            </div>
          </div>
        </div>

        <div className="toolbar mirror-modal__toolbar">
          <div>
            <div className="mirror-modal__heading">下载源检测</div>
            <div className="muted">并发检测连接与延迟，并抽样下载验证速度和文件是否可获取。</div>
          </div>
          <div className="toolbar__right">
            <button className="btn btn--secondary btn--sm" disabled={loading} onClick={() => void runChecks()}>
              {loading ? <span className="spinner" /> : "重新检测"}
            </button>
          </div>
        </div>

        {error ? <div className="mirror-modal__error">{error}</div> : null}

        {loading && sdkChecks.length === 0 && jdkChecks.length === 0 ? (
          <div className="mirror-modal__state">
            <span className="spinner" />
            <span>正在检测 SDK 与 JDK 镜像的连接、延迟和下载速度…</span>
          </div>
        ) : (
          <>
            <MirrorSection
              title="Android SDK 下载镜像"
              description="用于命令行工具、platform-tools、emulator 以及后续系统镜像。"
            >
              <MirrorGrid
                checks={sdkChecks}
                selectedId={selectedSDKId}
                currentSourceId={currentSourceId}
                onSelect={setSelectedSDKId}
              />
            </MirrorSection>

            <MirrorSection
              title="JDK 下载镜像"
              description="仅影响 Eclipse Temurin 21 JDK 下载；所有源使用同一 SHA-256 校验。"
            >
              <MirrorGrid
                checks={jdkChecks}
                selectedId={selectedJDKId}
                currentSourceId={currentJdkSourceId}
                onSelect={setSelectedJDKId}
              />
            </MirrorSection>
          </>
        )}
      </div>
    </Modal>
  );
}

function Selection({ label, check, optional = false }: { label: string; check: MirrorCheck | null; optional?: boolean }) {
  return (
    <span className="mirror-modal__selection-item">
      <span className="muted">{label}</span>
      {check ? <strong>{check.sourceName}</strong> : <strong>请选择</strong>}
      {check && !check.compatible ? <span className="chip chip--sm chip--warning">不可用</span> : null}
      {optional ? <span className="chip chip--sm chip--info">已安装</span> : null}
    </span>
  );
}

function MirrorSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="mirror-modal__section">
      <div className="mirror-modal__section-head">
        <div className="mirror-modal__heading">{title}</div>
        <div className="muted">{description}</div>
      </div>
      {children}
    </section>
  );
}

function MirrorGrid({
  checks,
  selectedId,
  currentSourceId,
  onSelect,
}: {
  checks: MirrorCheck[];
  selectedId: string;
  currentSourceId: string;
  onSelect: (id: string) => void;
}) {
  if (checks.length === 0) {
    return <div className="mirror-modal__empty">没有可检测的镜像源</div>;
  }
  return (
    <div className="mirror-grid">
      {checks.map((check) => (
        <button
          type="button"
          key={check.sourceId}
          className={`mirror-card${selectedId === check.sourceId ? " mirror-card--selected" : ""}`}
          onClick={() => onSelect(check.sourceId)}
        >
          <div className="mirror-card__head">
            <span className={`mirror-radio${selectedId === check.sourceId ? " mirror-radio--checked" : ""}`} />
            <div className="mirror-card__name">
              <div className="mirror-card__title-row">
                <span className="mirror-card__title">{check.sourceName}</span>
                {check.recommended ? <span className="chip chip--sm chip--success">推荐</span> : null}
                {check.sourceId === currentSourceId ? <span className="chip chip--sm chip--info">当前</span> : null}
              </div>
              <div className="mirror-card__region">{check.region || "未知区域"}</div>
            </div>
            <MirrorStatus check={check} />
          </div>

          <div className="mirror-card__metrics">
            <Metric label="连接" value={check.reachable ? "正常" : "失败"} tone={check.reachable ? "ok" : "bad"} />
            <Metric label="延迟" value={check.latencyMs > 0 ? formatMs(check.latencyMs) : "—"} />
            <Metric label="采样速度" value={humanSpeed(check.throughputBps) || "—"} />
            <Metric label="检测耗时" value={formatMs(check.elapsedMs)} />
          </div>

          <div className="mirror-card__resources">
            <div className="mirror-card__resource-head">
              <span>资源校验</span>
              <span className="muted">{resourceSummary(check.resources)}</span>
            </div>
            <div className="mirror-card__chips">
              {check.resources.map((resource) => (
                <ResourceChip key={resource.id} resource={resource} />
              ))}
            </div>
          </div>

          {check.error ? (
            <div className="mirror-card__error" title={check.error}>
              {check.error}
            </div>
          ) : null}
        </button>
      ))}
    </div>
  );
}

function Metric({ label, value, tone = "" }: { label: string; value: string; tone?: "ok" | "bad" | "" }) {
  return (
    <div className="mirror-metric">
      <span className="mirror-metric__label">{label}</span>
      <span className={`mirror-metric__value nums${tone ? ` mirror-metric__value--${tone}` : ""}`}>{value}</span>
    </div>
  );
}

function ResourceChip({ resource }: { resource: MirrorResource }) {
  const title = resource.available
    ? `${resource.name}${resource.sizeBytes ? ` · ${humanSize(resource.sizeBytes)}` : ""}`
    : `${resource.name}: ${resource.error || "不可用"}`;
  return (
    <span
      className={`chip chip--sm ${resource.available ? "chip--success" : resource.required ? "chip--danger" : "chip--warning"}`}
      title={title}
    >
      <span className="dot" />
      {resourceShortName(resource.id)}
      {resource.required ? " *" : ""}
    </span>
  );
}

function MirrorStatus({ check }: { check: MirrorCheck }) {
  if (!check.reachable) return <span className="chip chip--sm chip--danger">无法连接</span>;
  if (check.recommended) return <span className="chip chip--sm chip--success">推荐</span>;
  if (check.compatible) return <span className="chip chip--sm chip--info">可用</span>;
  return <span className="chip chip--sm chip--warning">资源不完整</span>;
}

function resourceShortName(id: string): string {
  switch (id) {
    case "repository":
      return "仓库索引";
    case "cmdline-tools;latest":
      return "cmdline-tools";
    case "platform-tools":
      return "platform-tools";
    case "emulator":
      return "emulator";
    case "system-images:google_apis":
      return "系统镜像索引";
    case "jdk":
      return "JDK 归档";
    default:
      return id;
  }
}

function resourceSummary(resources: MirrorResource[]): string {
  const required = resources.filter((item) => item.required);
  const available = required.filter((item) => item.available).length;
  if (required.length === 0) return `非必需资源 ${resources.filter((item) => item.available).length}/${resources.length}`;
  return `必需资源 ${available}/${required.length} · * 表示当前机器需要`;
}

function chooseSource(checks: MirrorCheck[], preferred: string): string {
  const recommended = checks.find((item) => item.recommended && item.compatible);
  if (recommended) return recommended.sourceId;
  if (preferred) {
    const current = checks.find((item) => item.sourceId === preferred);
    if (current) return current.sourceId;
  }
  const compatible = checks.find((item) => item.compatible);
  if (compatible) return compatible.sourceId;
  return checks.find((item) => item.reachable)?.sourceId ?? checks[0]?.sourceId ?? "";
}
