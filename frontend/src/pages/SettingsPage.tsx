// 设置页：环境检查（唯一入口）+ 下载镜像源（检测/设置独立一项）+ 系统镜像 + 基础偏好。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { AppSettings, JobInfo, MirrorSource, ResolvedPaths, SystemImage } from "../bridge/types";
import type { EnvCheck } from "../hooks/useEnvCheck";
import { EnvironmentSetupModal } from "../components/EnvironmentSetupModal";
import { MirrorSourceModal } from "../components/MirrorSourceModal";
import type { MirrorSourceSelection } from "../components/MirrorSourceModal";
import {
  androidVersionLabel,
  imageRootSupported,
  imageTagLabel,
  SystemImageModal,
} from "../components/SystemImageModal";
import { formatMs } from "../components/ui";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
  onSettingsChanged: (next: AppSettings) => void;
  env: EnvCheck;
  jobs: JobInfo[];
}

/** 镜像安装 / 删除任务的标题前缀（与后端 job.Spec.Title 一致）。 */
const INSTALL_TITLE_PREFIX = "安装系统镜像 ";
const DELETE_TITLE_PREFIX = "删除系统镜像 ";

/**
 * 已处理过的任务 id。
 *
 * 放在模块作用域：设置页切走会卸载，组件内的 ref 会重建，导致同一个任务被重复处理
 * （重复弹 toast、重复移除列表项）。
 */
const handledInstallJobs = new Set<string>();
const handledDeleteJobs = new Set<string>();

/** 该镜像是否已有未结束的安装任务（本地跟踪表在切页后会丢失，用任务标题兜底）。 */
function hasPendingImageJob(jobs: JobInfo[], path: string, prefix: string): boolean {
  return jobs.some(
    (job) =>
      (job.status === "running" || job.status === "queued") && job.title === prefix + path,
  );
}

export function SettingsPage({ onToast, onSettingsChanged, env, jobs }: Props) {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [paths, setPaths] = useState<ResolvedPaths | null>(null);
  // 当前生效的镜像源：设置页只读展示，检测与更换在 MirrorSourceModal 中完成。
  const [sdkSource, setSdkSource] = useState<MirrorSource | null>(null);
  const [jdkSource, setJdkSource] = useState<MirrorSource | null>(null);
  const [installedImages, setInstalledImages] = useState<SystemImage[]>([]);
  const [allImages, setAllImages] = useState<SystemImage[]>([]);
  const [loadingInstalled, setLoadingInstalled] = useState(false);
  const [loadingAll, setLoadingAll] = useState(false);
  const [installedError, setInstalledError] = useState("");
  const [allImagesError, setAllImagesError] = useState("");
  const [imagePickerOpen, setImagePickerOpen] = useState(false);
  const [environmentSetupOpen, setEnvironmentSetupOpen] = useState(false);
  const [mirrorSettingsOpen, setMirrorSettingsOpen] = useState(false);
  const [installJobs, setInstallJobs] = useState<Record<string, string>>({});
  const [deleteJobs, setDeleteJobs] = useState<Record<string, string>>({});
  const [startingPath, setStartingPath] = useState("");
  const [deletingPath, setDeletingPath] = useState("");

  const load = useCallback(async () => {
    try {
      const [nextSettings, nextPaths, sdkSources, jdkSources] = await Promise.all([
        api.Settings.Get() as Promise<AppSettings>,
        api.Env.Resolved() as Promise<ResolvedPaths>,
        api.Mirror.ListSources() as Promise<MirrorSource[]>,
        api.Mirror.ListJDKSources() as Promise<MirrorSource[]>,
      ]);
      setSettings(nextSettings);
      setPaths(nextPaths);
      setSdkSource(activeSource(sdkSources));
      setJdkSource(activeSource(jdkSources));
    } catch (err) {
      onToast("danger", "读取设置失败", errorText(err));
    }
  }, [onToast]);

  const loadInstalledImages = useCallback(
    async (notifyError = false) => {
      setLoadingInstalled(true);
      setInstalledError("");
      try {
        const list = api.asArray((await api.Avd.ListImages(true)) as SystemImage[]);
        setInstalledImages(list);
        setInstalledError("");
      } catch (err) {
        const text = errorText(err);
        setInstalledError(text);
        if (notifyError) onToast("danger", "读取已安装镜像失败", text);
      } finally {
        setLoadingInstalled(false);
      }
    },
    [onToast],
  );

  const loadAllImages = useCallback(async () => {
    setLoadingAll(true);
    setAllImagesError("");
    try {
      const list = api.asArray((await api.Avd.ListImages(false)) as SystemImage[]);
      setAllImages(list);
      setAllImagesError("");
    } catch (err) {
      const text = errorText(err);
      setAllImagesError(text);
      onToast("danger", "读取可用镜像失败", text);
    } finally {
      setLoadingAll(false);
    }
  }, [onToast]);

  useEffect(() => {
    void load();
  }, [load]);

  // 设置页默认展示已安装镜像；初始化尚未完成时不发起 sdkmanager 查询。
  useEffect(() => {
    if (!env.report || env.report.needInit) return;
    void loadInstalledImages(false);
  }, [env.report?.checkedAt, env.report?.needInit, loadInstalledImages]);

  // 镜像下载任务完成后刷新本地列表，并同步更新弹窗中的安装状态。
  useEffect(() => {
    const succeeded = new Set<string>();
    for (const [path, jobId] of Object.entries(installJobs)) {
      if (handledInstallJobs.has(jobId)) continue;
      const job = jobs.find((item) => item.id === jobId);
      if (!job || job.status === "queued" || job.status === "running") continue;
      handledInstallJobs.add(jobId);
      if (job.status === "succeeded") succeeded.add(path);
    }
    if (succeeded.size === 0) return;
    setAllImages((prev) =>
      prev.map((img) => (succeeded.has(img.path) ? { ...img, installed: true } : img)),
    );
    setInstalledImages((prev) => {
      const byPath = new Map(prev.map((img) => [img.path, img]));
      allImages.forEach((img) => {
        if (succeeded.has(img.path)) byPath.set(img.path, { ...img, installed: true });
      });
      return [...byPath.values()];
    });
    void loadInstalledImages(false);
  }, [allImages, installJobs, jobs, loadInstalledImages]);

  // 镜像删除任务完成后把条目移出本地列表，并同步弹窗中的安装状态。
  //
  // 跟踪关系以任务标题为准（而不是本地 deleteJobs 表）：设置页切走会卸载，
  // 本地表随之丢失——那样删除按钮会提前恢复可点、列表也不会更新。
  useEffect(() => {
    const removed = new Set<string>();
    for (const job of jobs) {
      if (job.status === "running" || job.status === "queued") continue;
      if (!job.title.startsWith(DELETE_TITLE_PREFIX)) continue;
      if (handledDeleteJobs.has(job.id)) continue;
      handledDeleteJobs.add(job.id);
      const path = job.title.slice(DELETE_TITLE_PREFIX.length);
      setDeleteJobs((prev) => {
        if (!(path in prev)) return prev;
        const next = { ...prev };
        delete next[path];
        return next;
      });
      if (job.status !== "succeeded") continue;
      removed.add(path);
      onToast("success", "系统镜像已删除", path);
    }
    if (removed.size === 0) return;
    setInstalledImages((prev) => prev.filter((img) => !removed.has(img.path)));
    setAllImages((prev) =>
      prev.map((img) => (removed.has(img.path) ? { ...img, installed: false } : img)),
    );
  }, [deleteJobs, jobs, onToast]);

  const patch = async (next: Partial<AppSettings>) => {
    try {
      const updated = (await api.Settings.Update(next)) as AppSettings;
      setSettings(updated);
      onSettingsChanged(updated);
    } catch (err) {
      onToast("danger", "保存设置失败", errorText(err));
    }
  };

  const openImagePicker = () => {
    setImagePickerOpen(true);
    if (allImages.length === 0 && !loadingAll) void loadAllImages();
  };

  const startEnvironmentSetup = async () => {
    try {
      // 镜像源已在「下载镜像源」中保存，这里直接用当前生效的源准备环境。
      await api.Env.Prepare();
      onToast("info", "已开始准备环境", "SDK / JDK 下载与补装进度已同步到底部任务区域。");
    } catch (err) {
      onToast("danger", "准备环境失败", errorText(err));
      throw err;
    }
  };

  const repairEnvironment = async () => {
    try {
      await api.Env.Repair();
      onToast("warning", "已开始修复环境", "将重装核心 SDK 工具链，并在缺失时补装 JDK，进度已同步到底部任务区域。");
    } catch (err) {
      onToast("danger", "修复环境失败", errorText(err));
      throw err;
    }
  };

  const deleteImage = async (image: SystemImage) => {
    const label = `${androidVersionLabel(image)} · ${imageTagLabel(image.tag)} · ${image.abi || "未知架构"}`;
    const confirmed = window.confirm(
      `确认删除本机的系统镜像「${label}」？\n\n` +
        "删除后如需再次使用必须重新下载（约 1-2 GB）。仍在被设备使用的镜像会被拒绝删除。",
    );
    if (!confirmed) return;
    setDeletingPath(image.path);
    try {
      const id = await api.Avd.DeleteImage(image.path);
      setDeleteJobs((prev) => ({ ...prev, [image.path]: id }));
      onToast("info", "正在删除系统镜像", `任务 ${id}，进度已同步到底部任务区域`);
    } catch (err) {
      onToast("danger", "删除镜像失败", errorText(err));
    } finally {
      setDeletingPath("");
    }
  };

  // 保存镜像源后：刷新当前源展示；SDK 源变化时按新源重建镜像列表（列表来自所选仓库），
  // 并重新检查环境报告里的当前镜像源。两侧都没改动时不重复跑一次完整的联网环境检查。
  const handleMirrorApplied = useCallback(
    async (next: MirrorSourceSelection) => {
      const sdkChanged = Boolean(next.sourceId) && next.sourceId !== sdkSource?.id;
      const jdkChanged = Boolean(next.jdkSourceId) && next.jdkSourceId !== jdkSource?.id;
      await load();
      if (sdkChanged) await loadAllImages();
      if (sdkChanged || jdkChanged) void env.reload();
      onToast(
        sdkChanged || jdkChanged ? "success" : "info",
        sdkChanged || jdkChanged ? "镜像源已更新" : "镜像源未变化",
        `SDK：${next.sourceName || next.sourceId}　JDK：${next.jdkSourceName || next.jdkSourceId}`,
      );
    },
    [env, jdkSource?.id, load, loadAllImages, onToast, sdkSource?.id],
  );

  const downloadImage = async (image: SystemImage) => {
    setStartingPath(image.path);
    try {
      const id = await api.Avd.InstallImage(image.path);
      setInstallJobs((prev) => ({ ...prev, [image.path]: id }));
      onToast("info", "已开始下载系统镜像", `任务 ${id}，进度已同步到底部任务日志`);
    } catch (err) {
      onToast("danger", "下载失败", errorText(err));
    } finally {
      setStartingPath("");
    }
  };

  const report = env.report;
  const jdkReady = report?.components.some((item) => item.id === "jdk" && item.state === "present") ?? false;
  // 「尚未初始化」已有横幅（含一个「立即准备」按钮），issue 列表不再重复渲染同类动作
  const issues = report
    ? report.issues.filter((issue) => !(report.needInit && issue.fixKind === "prepare"))
    : [];

  return (
    <div className="page page--settings">
      <div className="pageheader">
        <div className="pageheader__text">
          <div className="pageheader__title">设置</div>
          <div className="pageheader__subtitle">
            <span>软件自带 JDK / SDK 与模拟器环境</span>
          </div>
        </div>
      </div>

      <div className="pagecontent">
        <div className="settings-content">
        {/* ---------------------------------------------------------- 环境检查 */}
        <div className="section">
          <div className="section__header">
            <div className="section__title">环境检查</div>
            <div className="row row--wrap">
              {/* 「准备 / 补装」与「一键修复」都在同一个弹窗里选择，这里只保留一个入口。 */}
              <button className="btn btn--secondary" onClick={() => setEnvironmentSetupOpen(true)}>
                准备 / 修复环境
              </button>
              <button className="btn btn--secondary" disabled={env.loading} onClick={() => void env.reload()}>
                {env.loading ? "检查中…" : "重新检查"}
              </button>
            </div>
          </div>
          <div className="card">
            {report ? (
              <>
                <div className="row row--wrap" style={{ justifyContent: "space-between", alignItems: "center" }}>
                  <span className={`chip ${report.ready ? "chip--success" : "chip--warning"}`}>
                    {report.ready ? "环境就绪" : report.needInit ? "尚未初始化" : "存在缺失组件"}
                  </span>
                  <span className="muted nums" style={{ fontSize: 12 }}>
                    用时 {formatMs(report.elapsedMs)}
                  </span>
                </div>

                <div className="table-scroll">
                  <table className="table table--environment">
                  <thead>
                    <tr>
                      <th>组件</th>
                      <th>状态</th>
                      <th>版本</th>
                      <th>位置</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.components.map((c) => (
                      <tr key={c.id}>
                        <td>
                          <div>{c.name}</div>
                          {c.detail ? <div className="muted" style={{ fontSize: 11 }}>{c.detail}</div> : null}
                        </td>
                        <td>
                          <span
                            className={`chip ${c.state === "present" ? "chip--success" : "chip--warning"}`}
                          >
                            {c.state === "present" ? "可用" : "缺失"}
                          </span>
                        </td>
                        <td className="nums">{c.version || "—"}</td>
                        <td className="mono" style={{ fontSize: 12, wordBreak: "break-all" }}>
                          {c.path || "—"}
                        </td>
                      </tr>
                    ))}
                    </tbody>
                  </table>
                </div>

                <div className="muted" style={{ fontSize: 12, marginTop: 8 }}>
                  JDK 只使用软件目录下的版本；系统 JAVA_HOME / PATH 中的 JDK 不参与检测与运行。
                </div>

                <div className="row row--wrap" style={{ gap: 16, marginTop: 12 }}>
                  <span className="muted">
                    已安装系统镜像：<span className="nums">{report.images}</span> 个
                  </span>
                  <span className="muted">
                    已有设备：<span className="nums">{report.avds}</span> 个
                  </span>
                  <span className="muted">
                    可用空间：<span className="nums">{report.disk.freeGB}</span> GB
                    {report.disk.sufficient ? "" : "（不足）"}
                  </span>
                  {report.accel ? (
                    <span className="muted">
                      硬件加速：<span className="nums">{report.accel.available ? "可用" : "不可用"}</span>
                    </span>
                  ) : null}
                </div>

                {report.needInit ? (
                  <div className="banner banner--info" style={{ marginTop: 12 }}>
                    <div className="banner__icon">⬇️</div>
                    <div className="banner__body">
                      <div className="banner__title">需要初始化软件自带环境</div>
                      <div className="banner__text">
                        将按当前 JDK 镜像下载 Eclipse Temurin 21（约 200 MB，缺失或版本过低时）与官方命令行工具（约 150 MB），并安装 platform-tools 与 emulator。所有 JDK 下载都会校验 SHA-256，进度显示在底部任务区域。
                      </div>
                    </div>
                    <button className="btn btn--primary" onClick={() => setEnvironmentSetupOpen(true)}>
                      立即准备
                    </button>
                  </div>
                ) : null}

                {issues.length > 0 ? (
                  <div style={{ marginTop: 12 }}>
                    {issues.map((issue) => (
                      <div
                        key={issue.id}
                        className={`banner ${issue.severity === "blocker" ? "banner--danger" : "banner--info"}`}
                        style={{ marginTop: 8 }}
                      >
                        <div className="banner__icon">{issue.severity === "blocker" ? "⛔" : "ℹ️"}</div>
                        <div className="banner__body">
                          <div className="banner__title">{issue.title}</div>
                          <div className="banner__text">{issue.detail}</div>
                          {issue.fixCommand ? (
                            <div className="mono" style={{ fontSize: 12, marginTop: 6 }}>
                              {issue.fixCommand}
                            </div>
                          ) : null}
                        </div>
                        {issue.fixLabel && (issue.fixKind === "prepare" || issue.fixKind === "install") ? (
                          <button className="btn btn--secondary" onClick={() => setEnvironmentSetupOpen(true)}>
                            {issue.fixLabel}
                          </button>
                        ) : null}
                        {issue.fixCommand ? (
                          <button
                            className="btn btn--ghost"
                            onClick={() => void api.Env.CopyToClipboard(issue.fixCommand!)}
                          >
                            复制命令
                          </button>
                        ) : null}
                      </div>
                    ))}
                  </div>
                ) : null}
              </>
            ) : (
              <div className="muted">正在检查环境…</div>
            )}
          </div>
        </div>

        {/* ---------------------------------------------------------- 下载镜像源 */}
        <div className="section">
          <div className="section__header section__header--start">
            <div>
              <div className="section__title">下载镜像源</div>
              <div className="muted">
                Android SDK 与 JDK 分别使用独立的下载镜像；连接、延迟、采样速度与资源完整性
                都在弹窗内检测，检测结果直接用来选择并保存镜像源。
              </div>
            </div>
            <button className="btn btn--secondary" onClick={() => setMirrorSettingsOpen(true)}>
              检测 / 更换镜像源
            </button>
          </div>
          <div className="card">
            <div className="mirror-summary">
              <MirrorSummary
                title="Android SDK 镜像源"
                description="命令行工具、platform-tools、emulator 与系统镜像"
                source={sdkSource}
              />
              <MirrorSummary
                title="JDK 镜像源"
                description="Eclipse Temurin 21（下载后强制校验 SHA-256）"
                source={jdkSource}
              />
            </div>
          </div>
        </div>

        {/* ---------------------------------------------------------- 系统镜像 */}
        <div className="section">
          <div className="section__header section__header--start">
            <div>
              <div className="section__title">系统镜像</div>
              <div className="muted">
                默认展示已安装镜像，可直接删除释放磁盘空间；点击「查看全部镜像」
                可按版本、架构与 Root 支持筛选下载，下载使用「下载镜像源」中的 SDK 镜像。
              </div>
            </div>
            <button className="btn btn--secondary" disabled={loadingAll} onClick={openImagePicker}>
              {loadingAll ? "读取中…" : "查看全部镜像"}
            </button>
          </div>
          <div className="card">
            {loadingInstalled && installedImages.length === 0 ? (
              <div className="image-list-state">
                <span className="spinner" />
                <span>正在读取已安装镜像…</span>
              </div>
            ) : installedError ? (
              <div className="image-list-state image-list-state--error">
                <span>读取已安装镜像失败：{installedError}</span>
                <button className="btn btn--secondary btn--sm" onClick={() => void loadInstalledImages(true)}>
                  重试
                </button>
              </div>
            ) : installedImages.length > 0 ? (
              <div className="table-scroll">
                <table className="table table--images table--installed">
                  <thead>
                    <tr>
                      <th>Android 版本</th>
                      <th>类型</th>
                      <th>架构</th>
                      <th>是否支持 Root</th>
                      <th>状态</th>
                      <th className="image-delete-head">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {installedImages.map((img) => (
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
                          <span className={`chip chip--sm ${imageRootSupported(img) ? "chip--success" : "chip--warning"}`}>
                            {imageRootSupported(img) ? "支持" : "不支持"}
                          </span>
                        </td>
                        <td>
                          <span className="chip chip--sm chip--success">已安装</span>
                        </td>
                        <td className="image-delete-cell">
                          <button
                            className="btn btn--danger-ghost btn--sm"
                            disabled={
                              deletingPath === img.path ||
                              Boolean(deleteJobs[img.path]) ||
                              hasPendingImageJob(jobs, img.path, DELETE_TITLE_PREFIX)
                            }
                            title="删除本机已安装的镜像"
                            onClick={() => void deleteImage(img)}
                          >
                            {deletingPath === img.path ||
                            deleteJobs[img.path] ||
                            hasPendingImageJob(jobs, img.path, DELETE_TITLE_PREFIX) ? (
                              <span className="spinner" />
                            ) : (
                              "删除"
                            )}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="muted">
                {report?.needInit ? "初始化 SDK 后将在这里显示已安装镜像。" : "尚未安装系统镜像。"}
              </div>
            )}
          </div>
        </div>

        {/* ---------------------------------------------------------- 目录 */}
        <div className="section">
          <div className="section__title">目录</div>
          <div className="card">
            {[
              { label: "软件目录", value: paths?.appRoot },
              { label: "SDK", value: paths?.sdkRoot },
              { label: "AVD", value: paths?.avdHome },
              { label: "JDK", value: paths?.jdkRoot },
              { label: "日志", value: paths?.logDir },
            ].map((row) => (
              <div className="field" key={row.label}>
                <div className="field__label">{row.label}</div>
                <div className="field__control">
                  <input className="input mono" readOnly value={row.value ?? "（未解析）"} />
                </div>
                {row.value ? (
                  <button
                    className="btn btn--ghost btn--icon"
                    title="复制路径"
                    onClick={() => void api.Env.CopyToClipboard(row.value!)}
                  >
                    ⧉
                  </button>
                ) : null}
              </div>
            ))}
          </div>
        </div>

        {/* ---------------------------------------------------------- 偏好 */}
        <div className="section">
          <div className="section__title">偏好</div>
          <div className="card">
            <div className="field">
              <div className="field__label">主题</div>
              <div className="field__control">
                <select
                  className="select"
                  value={settings?.theme ?? "light"}
                  onChange={(e) => void patch({ theme: e.target.value })}
                >
                  <option value="light">浅色</option>
                  <option value="dark">深色</option>
                  <option value="system">跟随系统</option>
                </select>
              </div>
            </div>
            <div className="field">
              <div className="field__label">日志级别</div>
              <div className="field__control">
                <select
                  className="select"
                  value={settings?.logLevel ?? "info"}
                  onChange={(e) => void patch({ logLevel: e.target.value })}
                >
                  <option value="debug">debug</option>
                  <option value="info">info</option>
                  <option value="warn">warn</option>
                  <option value="error">error</option>
                </select>
                <div className="field__hint">用于底部日志区域与日志文件</div>
              </div>
            </div>
            <div className="field">
              <div className="field__label">删除设备前确认</div>
              <div className="field__control">
                <input
                  type="checkbox"
                  checked={settings?.confirmBeforeDelete ?? true}
                  onChange={(e) => void patch({ confirmBeforeDelete: e.target.checked })}
                />
              </div>
            </div>
          </div>
        </div>
        </div>
      </div>

      {imagePickerOpen ? (
        <SystemImageModal
          mode="download"
          images={allImages}
          loading={loadingAll}
          error={allImagesError}
          installJobs={installJobs}
          jobs={jobs}
          busyPath={startingPath}
          activeSourceName={sdkSource?.name || report?.mirrorSourceName || ""}
          onClose={() => setImagePickerOpen(false)}
          onReload={() => void loadAllImages()}
          onDownload={downloadImage}
        />
      ) : null}

      {environmentSetupOpen ? (
        <EnvironmentSetupModal
          jdkReady={jdkReady}
          onClose={() => setEnvironmentSetupOpen(false)}
          onStart={startEnvironmentSetup}
          onRepair={repairEnvironment}
          onOpenMirrorSettings={() => {
            setEnvironmentSetupOpen(false);
            setMirrorSettingsOpen(true);
          }}
        />
      ) : null}

      {mirrorSettingsOpen ? (
        <MirrorSourceModal
          currentSourceId={sdkSource?.id || report?.mirrorSourceId || settings?.mirrorSourceId || ""}
          currentJdkSourceId={
            jdkSource?.id || report?.jdkMirrorSourceId || settings?.jdkMirrorSourceId || ""
          }
          onClose={() => setMirrorSettingsOpen(false)}
          onApplied={handleMirrorApplied}
        />
      ) : null}
    </div>
  );
}

/** 从 ListSources 结果中取当前生效的源。 */
function activeSource(sources: readonly MirrorSource[] | null | undefined): MirrorSource | null {
  const list = api.asArray(sources);
  return list.find((item) => item.active) ?? list[0] ?? null;
}

/** 镜像源摘要：设置页只读展示当前生效的 SDK / JDK 镜像源；检测与更换统一在弹窗中完成。 */
function MirrorSummary({
  title,
  description,
  source,
}: {
  title: string;
  description: string;
  source: MirrorSource | null;
}) {
  return (
    <div className="mirror-summary__item">
      <div className="mirror-summary__head">
        <span className="mirror-summary__title">{title}</span>
        {source?.region ? <span className="chip chip--sm">{source.region}</span> : null}
        {source ? <span className="chip chip--sm chip--info">当前</span> : null}
      </div>
      <div className="mirror-summary__name">{source?.name ?? "正在读取…"}</div>
      <div className="mirror-summary__url mono truncate" title={source?.baseURL}>
        {source?.baseURL ?? ""}
      </div>
      <div className="muted" style={{ fontSize: 12 }}>
        {description}
        {source?.note ? ` · ${source.note}` : ""}
      </div>
    </div>
  );
}
