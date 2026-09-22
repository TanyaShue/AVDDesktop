// 系统镜像选择 / 下载弹窗。
//
// 新建设备与设置页共用同一张表：表头内置筛选，右下角根据 mode 显示「选择」或「下载」。
import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { JobInfo, SystemImage } from "../bridge/types";
import { humanSize, jobStatusText, Modal, Progress } from "./ui";

interface Props {
  mode: "select" | "download";
  images: SystemImage[];
  loading?: boolean;
  error?: string;
  selectedPath?: string;
  installJobs?: Record<string, string>;
  jobs?: JobInfo[];
  busyPath?: string;
  sourceName?: string;
  onClose: () => void;
  onSelect?: (image: SystemImage) => void;
  onDownload?: (image: SystemImage) => void | Promise<void>;
  onReload?: () => void;
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
  sourceName = "",
  onClose,
  onSelect,
  onDownload,
  onReload,
}: Props) {
  const [query, setQuery] = useState("");
  const [apiFilter, setApiFilter] = useState("");
  const [tagFilter, setTagFilter] = useState("");
  const [abiFilter, setAbiFilter] = useState("");
  const [rootFilter, setRootFilter] = useState("");

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
          <span className="muted">
            {sourceName ? `下载源：${sourceName}` : "可按表头下拉框快速筛选"}
          </span>
        </div>

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
                      <td>{imageTagLabel(img.tag)}</td>
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
