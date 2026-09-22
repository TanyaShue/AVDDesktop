// 设置页：环境检查（唯一入口）+ 基础偏好。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { AppSettings, ResolvedPaths, SystemImage } from "../bridge/types";
import type { EnvCheck } from "../hooks/useEnvCheck";
import { formatMs } from "../components/ui";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
  onSettingsChanged: (next: AppSettings) => void;
  env: EnvCheck;
}

export function SettingsPage({ onToast, onSettingsChanged, env }: Props) {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [paths, setPaths] = useState<ResolvedPaths | null>(null);
  const [images, setImages] = useState<SystemImage[]>([]);
  const [loadingImages, setLoadingImages] = useState(false);

  const load = useCallback(async () => {
    try {
      setSettings((await api.Settings.Get()) as AppSettings);
      setPaths((await api.Env.Resolved()) as ResolvedPaths);
    } catch (err) {
      onToast("danger", "读取设置失败", errorText(err));
    }
  }, [onToast]);

  useEffect(() => {
    void load();
  }, [load]);

  const patch = async (next: Partial<AppSettings>) => {
    try {
      const updated = (await api.Settings.Update(next)) as AppSettings;
      setSettings(updated);
      onSettingsChanged(updated);
    } catch (err) {
      onToast("danger", "保存设置失败", errorText(err));
    }
  };

  const loadImages = async () => {
    setLoadingImages(true);
    try {
      setImages(api.asArray((await api.Avd.ListImages(false)) as SystemImage[]));
    } catch (err) {
      onToast("danger", "读取系统镜像列表失败", errorText(err));
    } finally {
      setLoadingImages(false);
    }
  };

  const report = env.report;
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
            <span>软件自带 SDK 与模拟器环境</span>
          </div>
        </div>
        <div className="pageheader__actions">
          <button className="btn btn--secondary" disabled={env.loading} onClick={() => void env.reload()}>
            {env.loading ? "检查中…" : "重新检查"}
          </button>
        </div>
      </div>

      <div className="pagecontent">
        <div className="settings-content">
        {/* ---------------------------------------------------------- 环境检查 */}
        <div className="section">
          <div className="section__title">环境检查</div>
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
                        <td>{c.name}</td>
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
                      <div className="banner__title">需要初始化软件自带 SDK</div>
                      <div className="banner__text">
                        将下载官方命令行工具（约 150 MB），安装 platform-tools 与 emulator。进度显示在底部任务区域。
                      </div>
                    </div>
                    <button className="btn btn--primary" onClick={() => void env.prepare()}>
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
                        {issue.fixLabel && issue.fixKind === "prepare" ? (
                          <button className="btn btn--secondary" onClick={() => void env.prepare()}>
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

        {/* ---------------------------------------------------------- 系统镜像 */}
        <div className="section">
          <div className="section__title">系统镜像</div>
          <div className="card">
            <div className="row row--wrap" style={{ justifyContent: "space-between", alignItems: "center" }}>
              <span className="muted">
                镜像由官方 sdkmanager 安装到软件自己的 SDK 目录。创建 AVD 时若缺少镜像会自动安装。
              </span>
              <button className="btn btn--secondary" disabled={loadingImages} onClick={() => void loadImages()}>
                {loadingImages ? "读取中…" : "查看可用镜像"}
              </button>
            </div>
            {images.length > 0 ? (
              <div className="table-scroll">
                <table className="table table--images">
                <thead>
                  <tr>
                    <th>Android</th>
                    <th>类型</th>
                    <th>ABI</th>
                    <th>状态</th>
                  </tr>
                </thead>
                <tbody>
                  {images
                    .filter((img) => img.installed)
                    .concat(images.filter((img) => !img.installed).slice(0, 40))
                    .map((img) => (
                      <tr key={img.path}>
                        <td className="nums">API {img.api}</td>
                        <td>{img.tag}</td>
                        <td className="nums">{img.abi}</td>
                        <td>
                          <span className={`chip ${img.installed ? "chip--success" : "chip--sm"}`}>
                            {img.installed ? "已安装" : "未安装"}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="muted" style={{ marginTop: 8 }}>
                {report ? `已安装 ${report.images} 个镜像` : ""}
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
              { label: "JDK", value: paths?.javaPath },
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
                  className="input"
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
                  className="input"
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
            <div className="field">
              <div className="field__label">自动展开任务区域</div>
              <div className="field__control">
                <input
                  type="checkbox"
                  checked={settings?.showTaskDrawer ?? true}
                  onChange={(e) => void patch({ showTaskDrawer: e.target.checked })}
                />
              </div>
            </div>
          </div>
        </div>
        </div>
      </div>
    </div>
  );
}
