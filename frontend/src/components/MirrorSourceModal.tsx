// 镜像源设置弹窗：独立负责「检测 + 设置」 Android SDK 与 JDK 的下载镜像源。
//
// 这是镜像检测/切换的唯一入口：环境准备弹窗与系统镜像弹窗只读取当前生效的源，
// 不再各自内置一套检测与切换 UI（避免同一功能散落在多个弹窗里）。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { MirrorCheck, MirrorResource } from "../bridge/types";
import { formatMs, humanSize, humanSpeed, Modal } from "./ui";

/** 保存成功后的结果（父级据此刷新展示与依赖镜像源的列表）。 */
export interface MirrorSourceSelection {
  sourceId: string;
  sourceName: string;
  jdkSourceId: string;
  jdkSourceName: string;
}

interface Props {
  /** 当前生效的 SDK / JDK 镜像源 ID，用于标记「当前」。 */
  currentSourceId: string;
  currentJdkSourceId: string;
  onClose: () => void;
  /** 保存成功后的回调（弹窗自身负责调用 SetActiveSource / SetActiveJDKSource）。 */
  onApplied: (selection: MirrorSourceSelection) => void | Promise<void>;
}

export function MirrorSourceModal({
  currentSourceId = "",
  currentJdkSourceId = "",
  onClose,
  onApplied,
}: Props) {
  const [sdkChecks, setSdkChecks] = useState<MirrorCheck[]>([]);
  const [jdkChecks, setJdkChecks] = useState<MirrorCheck[]>([]);
  const [selectedSDKId, setSelectedSDKId] = useState(currentSourceId);
  const [selectedJDKId, setSelectedJDKId] = useState(currentJdkSourceId);
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState("");

  // 只在挂载时固定一次：currentSourceId 可能晚于弹窗打开才到达（首份环境报告返回后），
  // 若作为依赖，整套联网检测会在用户即将点「保存」时重跑并再次禁用按钮。
  const initialSources = useRef({ sdk: currentSourceId, jdk: currentJdkSourceId });

  const runChecks = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [sdkRaw, jdkRaw] = await Promise.all([api.Mirror.CheckAll(), api.Mirror.CheckJDKAll()]);
      const nextSDK = api.asArray(sdkRaw as MirrorCheck[]);
      const nextJDK = api.asArray(jdkRaw as MirrorCheck[]);
      setSdkChecks(nextSDK);
      setJdkChecks(nextJDK);
      setSelectedSDKId((previous) => resolveSelection(nextSDK, previous, initialSources.current.sdk));
      setSelectedJDKId((previous) => resolveSelection(nextJDK, previous, initialSources.current.jdk));
    } catch (err) {
      setError(errorText(err));
    } finally {
      setLoading(false);
    }
  }, []);

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
  const sdkChanged = Boolean(selectedSDKId) && selectedSDKId !== currentSourceId;
  const jdkChanged = Boolean(selectedJDKId) && selectedJDKId !== currentJdkSourceId;
  // 只校验「要改动」的那一侧：当前生效的源本来就已经保存，即使这次检测不可用，
  // 也不应该拦住用户只改另一侧（例如 JDK 源探测失败时仍要换 SDK 源）。
  const blockReason = unusableReason(sdkChanged, sdkUsable, jdkChanged, jdkUsable);
  const canApply = !loading && !applying && blockReason === "";

  const apply = async () => {
    if (!canApply) return;
    setApplying(true);
    setError("");
    try {
      if (sdkChanged && selectedSDK) await api.Mirror.SetActiveSource(selectedSDK.sourceId);
      if (jdkChanged && selectedJDK) await api.Mirror.SetActiveJDKSource(selectedJDK.sourceId);
      await onApplied({
        sourceId: selectedSDK?.sourceId ?? currentSourceId,
        sourceName: selectedSDK?.sourceName ?? "",
        jdkSourceId: selectedJDK?.sourceId ?? selectedJDKId,
        jdkSourceName: selectedJDK?.sourceName ?? "",
      });
      onClose();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setApplying(false);
    }
  };

  return (
    <Modal
      title="下载镜像源"
      size="xl"
      onClose={onClose}
      footer={
        <div className="mirror-modal__foot">
          <div className="mirror-modal__selection mirror-modal__selection--wrap">
            <Selection label="SDK" check={selectedSDK} changed={sdkChanged} />
            <Selection label="JDK" check={selectedJDK} changed={jdkChanged} />
          </div>
          <div className="row row--wrap">
            <button className="btn btn--secondary" onClick={onClose} disabled={applying}>
              取消
            </button>
            <button className="btn btn--primary" onClick={() => void apply()} disabled={!canApply}>
              {applying ? <span className="spinner" /> : sdkChanged || jdkChanged ? "保存镜像源" : "保存"}
            </button>
          </div>
        </div>
      }
    >
      <div className="mirror-modal">
        <div className="banner banner--info mirror-modal__notice">
          <div className="banner__icon">⇄</div>
          <div className="banner__body">
            <div className="banner__title">Android SDK 与 JDK 分别检测并选择下载镜像</div>
            <div className="banner__text">
              SDK 镜像用于命令行工具、platform-tools、emulator 与系统镜像下载；JDK 镜像仅在需要下载
              Eclipse Temurin 21 时使用。JDK 国内镜像与 GitHub 官方发布同一批归档，下载后始终使用程序内置
              SHA-256 校验。保存后立即生效。
            </div>
          </div>
        </div>

        <div className="toolbar mirror-modal__toolbar">
          <div>
            <div className="mirror-modal__heading">镜像源检测</div>
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

        {!loading && blockReason ? <div className="mirror-modal__hint">{blockReason}</div> : null}
      </div>
    </Modal>
  );
}

/** 哪一侧「要改动但不可用」就返回对应原因；两侧都可保存时返回空串。 */
function unusableReason(
  sdkChanged: boolean,
  sdkUsable: boolean,
  jdkChanged: boolean,
  jdkUsable: boolean,
): string {
  if (sdkChanged && !sdkUsable) {
    return "所选 Android SDK 镜像不可用（无法连接或必需资源缺失），请换一个源后再保存。";
  }
  if (jdkChanged && !jdkUsable) {
    return "所选 JDK 镜像不可用（无法连接或缺少 JDK 归档），请换一个源后再保存。";
  }
  return "";
}

function Selection({
  label,
  check,
  changed,
}: {
  label: string;
  check: MirrorCheck | null;
  changed: boolean;
}) {
  return (
    <span className="mirror-modal__selection-item">
      <span className="muted">{label}</span>
      {check ? <strong>{check.sourceName}</strong> : <strong>请选择</strong>}
      {check && !check.compatible ? <span className="chip chip--sm chip--warning">不可用</span> : null}
      {changed ? <span className="chip chip--sm chip--info">待保存</span> : null}
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

/**
 * 决定重新检测后保留哪个选择。
 *
 * 优先保留用户在当前弹窗里的选择（重新检测不该改变他的选择），其次是当前生效的源，
 * 最后才回退到推荐/可用源。注意不能一上来就选「推荐」：弹窗的默认值必须是正在使用的源，
 * 否则用户会误以为镜像源被自动换掉。
 */
function resolveSelection(checks: MirrorCheck[], previous: string, current: string): string {
  if (previous && checks.some((item) => item.sourceId === previous)) return previous;
  if (current && checks.some((item) => item.sourceId === current)) return current;
  const recommended = checks.find((item) => item.recommended && item.compatible);
  if (recommended) return recommended.sourceId;
  const compatible = checks.find((item) => item.compatible);
  if (compatible) return compatible.sourceId;
  return checks.find((item) => item.reachable)?.sourceId ?? checks[0]?.sourceId ?? "";
}
