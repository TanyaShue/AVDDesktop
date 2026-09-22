// 系统镜像选择 / 下载弹窗。
//
// 新建设备与设置页共用同一张表：表头内置筛选，右下角根据 mode 显示「选择」或「下载」。
// 「下载」模式额外提供镜像源切换：每个源的连接状态直接标在按钮上，切换后会按该源重新加载列表。
import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { JobInfo, MirrorCheck, SystemImage } from "../bridge/types";
import { formatMs, humanSize, jobStatusText, Modal, Progress } from "./ui";

interface Props {
  mode: "select" | "download";
  images: SystemImage[];
  loading?: boolean;
  error?: string;
  selectedPath?: string;
  installJobs?: Record<string, string>;
  jobs?: JobInfo[];
  busyPath?: string;
  /** 当前生效的 SDK 镜像源 ID（下载模式用来标记「当前」）。 */
  activeSourceId?: string;
  onClose: () => void;
  onSelect?: (image: SystemImage) => void;
  onDownload?: (image: SystemImage) => void | Promise<void>;
  onReload?: () => void;
  /** 切换镜像源成功后回调（父级据此重新加载镜像列表）。 */
  onSourceChanged?: (sourceId: string) => void | Promise<void>;
}

export function SystemImageModal({
  mode,
  images,
  loading = false,
  error = "",
  selectedPath = "",
  installJobs = {},
  jobs = [],
  busyPath = "",
  activeSourceId = "",
  onClose,
  onSelect,
  onDownload,
  onReload,
  onSourceChanged,
}: Props) {
  const [query, setQuery] = useState("");
  const [apiFilter, setApiFilter] = useState("");
  const [tagFilter, setTagFilter] = useState("");
  const [abiFilter, setAbiFilter] = useState("");
  const [rootFilter, setRootFilter] = useState("");

  // 下载源：进入弹窗时并发检测一次连接、延迟与资源完整性，之后可随时重新检测或切换。
  const [sources, setSources] = useState<MirrorCheck[]>([]);
  const [sourceId, setSourceId] = useState(activeSourceId);
  const [checking, setChecking] = useState(false);
  const [switching, setSwitching] = useState("");
  const [sourceError, setSourceError] = useState("");

  useEffect(() => {
    setSourceId(activeSourceId);
  }, [activeSourceId]);

  const checkSources = useCallback(async () => {
    setChecking(true);
    setSourceError("");
    try {
      const list = api.asArray((await api.Mirror.CheckAll()) as MirrorCheck[]);
      setSources(list);
    } catch (err) {
      setSourceError(errorText(err));
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => {
    if (mode !== "download") return;
    void checkSources();
  }, [checkSources, mode]);

  const selectSource = useCallback(
    async (next: string) => {
      if (!next || next === sourceId) return;
      setSwitching(next);
      setSourceError("");
      try {
        await api.Mirror.SetActiveSource(next);
        setSourceId(next);
        await onSourceChanged?.(next);
      } catch (err) {
        setSourceError(errorText(err));
      } finally {
        setSwitching("");
      }
    },
    [onSourceChanged, sourceId],
  );

  const selectedSource = useMemo(
    () => sources.find((item) => item.sourceId === sourceId) ?? null,
    [sourceId, sources],
  );

  const apis = useMemo(() => {
    const map = new Map<string, string>();
    images.forEach((img) => {
      if (img.api) map.set(img.api, `${androidVersionLabel(img)}（API ${img.api}）`);
    });
    return [...map.entries()].sort((a, b) => b[0].localeCompare(a[0], undefined, { numeric: true }));
  }, [images]);

  const tags = useMemo(
    () => uniqueOptions(images.map((img) => img.tag), imageTagLabel),
    [images],
  );
  const abis = useMemo(() => uniqueOptions(images.map((img) => img.abi)), [images]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return images.filter((img) => {
      if (apiFilter && img.api !== apiFilter) return false;
      if (tagFilter && img.tag !== tagFilter) return false;
      if (abiFilter && img.abi !== abiFilter) return false;
      if (rootFilter === "yes" && !imageRootSupported(img)) return false;
      if (rootFilter === "no" && imageRootSupported(img)) return false;
      if (!q) return true;
      return [
        img.path,
        img.api,
        img.androidVersion,
        img.tag,
        imageTagLabel(img.tag),
        img.abi,
        img.description,
      ]
        .filter(Boolean)
        .join(" ")
        .toLowerCase()
        .includes(q);
    });
  }, [abiFilter, apiFilter, images, query, rootFilter, tagFilter]);

  return (
    <Modal
      title={mode === "select" ? "选择系统镜像" : "可用系统镜像"}
      size="xl"
      className="modal--fill"
      bodyClassName="modal__body--fill"
      onClose={onClose}
      footer={
        <div className="image-modal__foot">
          <span className="muted">
            显示 {filtered.length} / {images.length} 个镜像
          </span>
          <div className="row">
            {onReload ? (
              <button className="btn btn--secondary" disabled={loading} onClick={onReload}>
                {loading ? "读取中…" : "重新加载"}
              </button>
            ) : null}
            <button className="btn btn--primary" onClick={onClose}>
              关闭
            </button>
          </div>
        </div>
      }
    >
      <div className="image-modal">
        <div className="toolbar image-modal__toolbar">
          <input
            className="search image-modal__search"
            placeholder="搜索镜像、API、架构或类型…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            autoFocus
          />
          <span className="muted">可按表头下拉框快速筛选；表格区域单独滚动。</span>
        </div>

        {mode === "download" ? (
          <SourceBar
            sources={sources}
            selectedId={sourceId}
            switching={switching}
            checking={checking}
            error={sourceError}
            selected={selectedSource}
            onCheck={() => void checkSources()}
            onSelect={(next) => void selectSource(next)}
          />
        ) : null}

        {error ? (
          <div className="image-modal__state image-modal__state--error">
            <div>读取镜像失败：{error}</div>
            {onReload ? (
              <button className="btn btn--secondary btn--sm" onClick={onReload}>
                重试
              </button>
            ) : null}
          </div>
        ) : loading && images.length === 0 ? (
          <div className="image-modal__state">
            <span className="spinner" />
            <span>正在从官方 sdkmanager 读取镜像…</span>
          </div>
        ) : filtered.length === 0 ? (
          <div className="image-modal__state">
            {images.length === 0 ? "暂无可用的系统镜像" : "没有符合当前筛选条件的镜像"}
          </div>
        ) : (
          <div className="table-scroll image-modal__table-scroll">
            <table className="table table--images table--image-picker">
              <colgroup>
                <col style={{ width: "17%" }} />
                <col style={{ width: "21%" }} />
                <col style={{ width: "13%" }} />
                <col style={{ width: "12%" }} />
                <col style={{ width: "12%" }} />
                <col style={{ width: "25%" }} />
              </colgroup>
              <thead>
                <tr>
                  <FilterHeader label="Android 版本" value={apiFilter} onChange={setApiFilter}>
                    <option value="">全部版本</option>
                    {apis.map(([api, label]) => (
                      <option key={api} value={api}>
                        {label}
                      </option>
                    ))}
                  </FilterHeader>
                  <FilterHeader label="类型" value={tagFilter} onChange={setTagFilter}>
                    <option value="">全部类型</option>
                    {tags.map(([tag, label]) => (
                      <option key={tag} value={tag}>
                        {label}
                      </option>
                    ))}
                  </FilterHeader>
                  <FilterHeader label="架构" value={abiFilter} onChange={setAbiFilter}>
                    <option value="">全部架构</option>
                    {abis.map(([abi]) => (
                      <option key={abi} value={abi}>
                        {abi}
                      </option>
                    ))}
                  </FilterHeader>
                  <FilterHeader label="是否支持 Root" value={rootFilter} onChange={setRootFilter}>
                    <option value="">全部</option>
                    <option value="yes">支持</option>
                    <option value="no">不支持</option>
                  </FilterHeader>
                  <th>状态</th>
                  <th className="image-action-head">{mode === "select" ? "选择设备镜像" : "下载"}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((img) => {
                  const selected = img.path === selectedPath;
                  const trackedJob = findTrackedJob(img.path, installJobs, jobs);
                  const activeJob = isActiveJob(trackedJob) ? trackedJob : undefined;
                  const installed = img.installed || trackedJob?.status === "succeeded";
                  const busy = busyPath === img.path;
                  const rootOK = imageRootSupported(img);
                  return (
                    <tr key={img.path}>
                      <td>
                        <div className="image-version">
                          <span>{androidVersionLabel(img)}</span>
                          <span className="muted nums">API {img.api || "?"}</span>
                        </div>
                      </td>
                      <td className="image-tag-cell">
                        <span title={img.tag}>{imageTagLabel(img.tag)}</span>
                      </td>
                      <td className="nums">{img.abi || "—"}</td>
                      <td>
                        <span className={`chip chip--sm ${rootOK ? "chip--success" : "chip--warning"}`}>
                          {rootOK ? "支持" : "不支持"}
                        </span>
                      </td>
                      <td>
                        <span className={`chip chip--sm ${installed ? "chip--success" : ""}`}>
                          {installed ? "已安装" : "未安装"}
                        </span>
                      </td>
                      <td className="image-action-cell">
                        {mode === "select" ? (
                          <button
                            className={`btn btn--sm ${selected ? "btn--secondary" : "btn--primary"}`}
                            disabled={selected}
                            onClick={() => onSelect?.(img)}
                          >
                            {selected ? "已选择" : "选择"}
                          </button>
                        ) : (
                          <ImageDownloadAction
                            image={img}
                            job={trackedJob}
                            activeJob={activeJob}
                            installed={installed}
                            busy={busy}
                            onDownload={onDownload}
                          />
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </Modal>
  );
}

function SourceBar({
  sources,
  selectedId,
  switching,
  checking,
  error,
  selected,
  onCheck,
  onSelect,
}: {
  sources: MirrorCheck[];
  selectedId: string;
  switching: string;
  checking: boolean;
  error: string;
  selected: MirrorCheck | null;
  onCheck: () => void;
  onSelect: (id: string) => void;
}) {
  const busy = switching !== "" || checking;
  return (
    <div className="image-modal__sources">
      <div className="image-modal__sources-head">
        <span className="image-modal__sources-title">下载源</span>
        <span className="muted truncate">
          {error
            ? `检测失败：${error}`
            : checking
              ? "正在并发检测各镜像源的连接、延迟与资源…"
              : "点击切换系统镜像下载源，切换后自动按该源重新加载列表。"}
        </span>
        <button className="btn btn--ghost btn--sm" disabled={busy} onClick={onCheck}>
          {checking ? <span className="spinner" /> : "重新检测"}
        </button>
      </div>

      <div className="image-modal__source-list" role="radiogroup" aria-label="下载源">
        {sources.length === 0 ? (
          <span className="muted">{checking ? "检测中…" : "没有可用的镜像源"}</span>
        ) : (
          sources.map((check) => {
            const active = check.sourceId === selectedId;
            return (
              <button
                key={check.sourceId}
                type="button"
                role="radio"
                aria-checked={active}
                className={`source-chip${active ? " source-chip--active" : ""}`}
                disabled={switching !== ""}
                title={`${check.sourceName}（${check.baseURL}）${check.error ? "\n" + check.error : ""}`}
                onClick={() => onSelect(check.sourceId)}
              >
                {switching === check.sourceId ? (
                  <span className="spinner" />
                ) : (
                  <span className={`source-dot ${sourceDotClass(check)}`} />
                )}
                <span className="source-chip__name">{check.sourceName}</span>
                <span className="source-chip__meta nums">{sourceMetaText(check)}</span>
              </button>
            );
          })
        )}
      </div>

      {selected && (!selected.reachable || !selected.compatible) ? (
        <div className="image-modal__sources-warn">
          {selected.reachable ? "当前下载源资源不完整" : "当前下载源无法连接"}
          {selected.error ? `：${selected.error}` : "，建议切换到其它源后再下载。"}
        </div>
      ) : null}
    </div>
  );
}

function sourceDotClass(check: MirrorCheck): string {
  if (!check.reachable) return "source-dot--bad";
  if (!check.compatible) return "source-dot--warn";
  return "source-dot--ok";
}

function sourceMetaText(check: MirrorCheck): string {
  if (!check.reachable) return "无法连接";
  if (!check.compatible) return "资源不完整";
  if (check.latencyMs > 0) return formatMs(check.latencyMs);
  return "可用";
}

function FilterHeader({
  label,
  value,
  onChange,
  children,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  children: ReactNode;
}) {
  return (
    <th>
      <div className="table-filter">
        <span>{label}</span>
        <select
          className="select table-filter__select"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-label={`筛选${label}`}
        >
          {children}
        </select>
      </div>
    </th>
  );
}

function ImageDownloadAction({
  image,
  job,
  activeJob,
  installed,
  busy,
  onDownload,
}: {
  image: SystemImage;
  job?: JobInfo;
  activeJob?: JobInfo;
  installed: boolean;
  busy: boolean;
  onDownload?: (image: SystemImage) => void | Promise<void>;
}) {
  if (activeJob) {
    const detail = activeJob.bytesTotal > 0
      ? `${humanSize(activeJob.bytesDone)} / ${humanSize(activeJob.bytesTotal)}`
      : activeJob.bytesDone > 0
        ? `已下载 ${humanSize(activeJob.bytesDone)}`
        : jobStatusText(activeJob);
    return (
      <div className="image-download">
        <Progress
          percent={activeJob.percent}
          indeterminate={activeJob.percent <= 0}
          showPercent
        />
        <span className="muted truncate" title={detail}>{detail}</span>
      </div>
    );
  }

  if (installed) {
    return <span className="chip chip--sm chip--success">已安装</span>;
  }

  const failed = job?.status === "failed" || job?.status === "canceled";
  return (
    <div className="image-download">
      <button
        className="btn btn--primary btn--sm"
        disabled={busy}
        onClick={() => void onDownload?.(image)}
      >
        {busy ? <span className="spinner" /> : failed ? "重试" : "下载"}
      </button>
      {failed && job?.error?.message ? (
        <span className="muted truncate image-download__error" title={job.error.message}>
          {job.error.message}
        </span>
      ) : null}
    </div>
  );
}

function findTrackedJob(
  path: string,
  installJobs: Record<string, string>,
  jobs: JobInfo[],
): JobInfo | undefined {
  const mapped = installJobs[path];
  if (mapped) {
    const job = jobs.find((item) => item.id === mapped);
    if (job) return job;
  }
  return jobs.find(
    (job) =>
      job.kind === "install" &&
      (job.subtitle?.includes(path) || job.title.includes(path)),
  );
}

function isActiveJob(job?: JobInfo): boolean {
  return job?.status === "queued" || job?.status === "running";
}

function uniqueOptions(values: string[], labeler?: (value: string) => string): [string, string][] {
  const map = new Map<string, string>();
  values.forEach((value) => {
    if (value) map.set(value, labeler ? labeler(value) : value);
  });
  return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1], "zh-CN"));
}

export function androidVersionLabel(image: SystemImage): string {
  return image.androidVersion?.trim() || `Android API ${image.api || "?"}`;
}

export function imageTagLabel(tag: string): string {
  switch (tag.toLowerCase()) {
    case "default":
      return "默认";
    case "google_apis":
      return "Google APIs";
    case "google_apis_playstore":
      return "Google Play";
    case "aosp_atd":
      return "AOSP ATD";
    case "google_atd":
      return "Google APIs ATD";
    case "android-desktop":
      return "Android Desktop";
    case "google_apis_ps16k":
      return "Google APIs (16 KB)";
    case "google_apis_playstore_ps16k":
      return "Google Play (16 KB)";
    default:
      return tag || "未知";
  }
}

export function imageRootSupported(image: SystemImage): boolean {
  if (typeof image.rootSupported === "boolean") return image.rootSupported;
  return !image.tag.toLowerCase().includes("playstore");
}
