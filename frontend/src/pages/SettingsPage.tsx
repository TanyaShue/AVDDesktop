// 设置页：环境路径、镜像源、下载、许可、外观、诊断。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { AppSettings, MirrorSource, ResolvedPaths } from "../bridge/types";
import { LogPanel } from "../components/LogPanel";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
  onSettingsChanged: (settings: AppSettings) => void;
}

export function SettingsPage({ onToast, onSettingsChanged }: Props) {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [paths, setPaths] = useState<ResolvedPaths | null>(null);
  const [sources, setSources] = useState<MirrorSource[]>([]);
  const [checks, setChecks] = useState<Array<{ name: string; ok: boolean; detail: string }>>([]);

  const load = useCallback(async () => {
    try {
      const [s, p, m] = await Promise.all([
        api.Settings.Get() as Promise<AppSettings>,
        api.Env.ResolvedPaths() as Promise<ResolvedPaths>,
        api.Mirror.ListSources() as Promise<MirrorSource[]>,
      ]);
      setSettings(s);
      setPaths(p);
      setSources(m ?? []);
    } catch (err) {
      onToast("danger", "读取设置失败", errorText(err));
    }
  }, [onToast]);

  useEffect(() => {
    void load();
  }, [load]);

  const patch = async (value: Record<string, unknown>) => {
    try {
      const next = (await api.Settings.Update(value)) as AppSettings;
      setSettings(next);
      onSettingsChanged(next);
      setPaths((await api.Env.ResolvedPaths()) as ResolvedPaths);
    } catch (err) {
      onToast("danger", "保存失败", errorText(err));
    }
  };

  const pickDir = async (field: keyof AppSettings, title: string) => {
    const dir = await api.Env.PickDirectory({ title });
    if (dir) await patch({ [field]: dir });
  };

  if (!settings) {
    return (
      <div className="page">
        <div className="pagecontent">
          <div className="card" style={{ padding: 24 }}>
            <span className="row">
              <span className="spinner" /> 正在读取设置…
            </span>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="page">
      <div className="pageheader">
        <div className="pageheader__text">
          <div className="pageheader__title">设置</div>
          <div className="pageheader__subtitle">
            <span className="truncate">
              配置文件 <span className="mono">{paths?.cacheDir ? "settings.json（应用数据目录）" : "settings.json"}</span>
            </span>
          </div>
        </div>
        <div className="pageheader__actions">
          <button className="btn btn--ghost" onClick={() => void api.Settings.Reset("")}>
            恢复默认
          </button>
        </div>
      </div>

      <div className="pagecontent">
        <div className="section">
          <div className="section__title">环境路径</div>
          <div className="card" style={{ padding: 20 }}>
            <PathRow
              label="SDK 根目录"
              value={settings.sdkRoot}
              resolved={paths?.sdkRoot}
              source={paths?.sdkRootSource}
              onPick={() => void pickDir("sdkRoot", "选择 Android SDK 根目录")}
              onClear={() => void patch({ sdkRoot: "" })}
            />
            <PathRow
              label="JDK 目录"
              value={settings.jdkPath}
              resolved={paths?.jdkPath}
              source="留空时自动探测 JAVA_HOME / PATH / Android Studio 自带 JBR"
              onPick={() => void pickDir("jdkPath", "选择 JDK 目录（包含 bin）")}
              onClear={() => void patch({ jdkPath: "" })}
            />
            <PathRow
              label="AVD 存放目录"
              value={settings.avdHome}
              resolved={paths?.avdHome}
              source={paths?.avdHomeSource}
              onPick={() => void pickDir("avdHome", "选择 AVD 目录")}
              onClear={() => void patch({ avdHome: "" })}
            />
            <div className="field">
              <div className="field__label">子进程环境变量</div>
              <div className="field__control">
                <label className="row" style={{ gap: 8 }}>
                  <input
                    type="checkbox"
                    checked={settings.injectEnvForChildren}
                    onChange={(e) => void patch({ injectEnvForChildren: e.target.checked })}
                  />
                  <span>启动 emulator / adb 时注入 ANDROID_HOME、ANDROID_AVD_HOME 与 PATH（不修改系统环境变量）</span>
                </label>
              </div>
            </div>
          </div>
        </div>

        <div className="section">
          <div className="section__title">镜像与下载</div>
          <div className="card" style={{ padding: 20 }}>
            <div className="field">
              <div className="field__label">当前镜像源</div>
              <div className="field__control">
                <select
                  className="input"
                  value={settings.activeSourceId || sources[0]?.id || ""}
                  onChange={(e) => void patch({ activeSourceId: e.target.value })}
                >
                  {sources.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}　{s.baseURL}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            <div className="field">
              <div className="field__label">自动回退官方源</div>
              <div className="field__control">
                <label className="row" style={{ gap: 8 }}>
                  <input
                    type="checkbox"
                    checked={settings.autoFallbackToOfficial}
                    onChange={(e) => void patch({ autoFallbackToOfficial: e.target.checked })}
                  />
                  <span>镜像缺少文件或校验失败时，自动改用 Google 官方源下载同一个包</span>
                </label>
              </div>
            </div>
            <div className="field">
              <div className="field__label">单文件并发连接</div>
              <div className="field__control">
                <input
                  className="input"
                  type="number"
                  min={1}
                  max={16}
                  value={settings.maxConnectionsPerFile}
                  onChange={(e) => void patch({ maxConnectionsPerFile: Number(e.target.value) })}
                />
                <div className="field__hint">支持断点续传的源会启用分片并发下载，建议 4</div>
              </div>
            </div>
            <div className="field">
              <div className="field__label">下载超时（秒）</div>
              <div className="field__control">
                <input
                  className="input"
                  type="number"
                  min={5}
                  max={3600}
                  value={settings.timeoutSeconds}
                  onChange={(e) => void patch({ timeoutSeconds: Number(e.target.value) })}
                />
              </div>
            </div>
            <div className="field">
              <div className="field__label">代理</div>
              <div className="field__control">
                <select
                  className="input"
                  value={settings.proxyMode}
                  onChange={(e) => void patch({ proxyMode: e.target.value })}
                >
                  <option value="off">不使用代理</option>
                  <option value="system">跟随系统代理</option>
                  <option value="custom">自定义</option>
                </select>
                {settings.proxyMode === "custom" ? (
                  <input
                    className="input"
                    style={{ marginTop: 8 }}
                    placeholder="http://127.0.0.1:7890"
                    defaultValue={settings.proxyURL}
                    onBlur={(e) => void patch({ proxyURL: e.target.value })}
                  />
                ) : null}
              </div>
            </div>
            <div className="field">
              <div className="field__label">限速（KB/s）</div>
              <div className="field__control">
                <input
                  className="input"
                  type="number"
                  min={0}
                  value={settings.speedLimitKBps}
                  onChange={(e) => void patch({ speedLimitKBps: Number(e.target.value) })}
                />
                <div className="field__hint">0 表示不限速</div>
              </div>
            </div>
          </div>
        </div>

        <div className="section">
          <div className="section__title">许可与默认值</div>
          <div className="card" style={{ padding: 20 }}>
            <div className="field">
              <div className="field__label">自动接受许可</div>
              <div className="field__control">
                <label className="row" style={{ gap: 8 }}>
                  <input
                    type="checkbox"
                    checked={settings.autoAcceptLicenses}
                    onChange={(e) => void patch({ autoAcceptLicenses: e.target.checked })}
                  />
                  <span>安装时自动写入 SDK 许可文件（等同于在 Android Studio 中点击「Accept」）</span>
                </label>
              </div>
            </div>
            <div className="field">
              <div className="field__label">默认设备档案</div>
              <div className="field__control">
                <input
                  className="input"
                  defaultValue={settings.defaultDeviceProfile}
                  onBlur={(e) => void patch({ defaultDeviceProfile: e.target.value })}
                />
              </div>
            </div>
            <div className="field">
              <div className="field__label">默认内存（MB）</div>
              <div className="field__control">
                <input
                  className="input"
                  type="number"
                  min={512}
                  max={16384}
                  defaultValue={settings.defaultRamMB}
                  onBlur={(e) => void patch({ defaultRamMB: Number(e.target.value) })}
                />
              </div>
            </div>
          </div>
        </div>

        <div className="section">
          <div className="section__title">外观</div>
          <div className="card" style={{ padding: 20 }}>
            <div className="field">
              <div className="field__label">主题</div>
              <div className="field__control">
                <select
                  className="input"
                  value={settings.theme}
                  onChange={(e) => void patch({ theme: e.target.value })}
                >
                  <option value="light">浅色</option>
                  <option value="dark">深色</option>
                  <option value="system">跟随系统</option>
                </select>
              </div>
            </div>
            <div className="field">
              <div className="field__label">设备视图</div>
              <div className="field__control">
                <select
                  className="input"
                  value={settings.deviceViewMode}
                  onChange={(e) => void patch({ deviceViewMode: e.target.value })}
                >
                  <option value="grid">网格</option>
                  <option value="list">列表</option>
                </select>
              </div>
            </div>
          </div>
        </div>

        <div className="section">
          <div className="section__title">运行日志</div>
          <LogPanel onToast={onToast} />
        </div>

        <div className="section">
          <div className="section__title">诊断</div>
          <div className="card" style={{ padding: 20 }}>
            <div className="row" style={{ flexWrap: "wrap", gap: 8, marginBottom: 12 }}>
              <button
                className="btn btn--primary"
                onClick={() =>
                  void (async () => {
                    try {
                      const id = await api.Diagnostics.RunSelfCheck();
                      onToast("info", "已开始自检", `任务 ${id}，结果会显示在任务面板`);
                    } catch (err) {
                      onToast("danger", "自检失败", errorText(err));
                    }
                  })()
                }
              >
                运行环境自检
              </button>
              <button
                className="btn btn--secondary"
                onClick={() =>
                  void (async () => {
                    try {
                      const path = await api.Diagnostics.ExportReport();
                      onToast("success", "诊断包已导出", path);
                    } catch (err) {
                      onToast("danger", "导出失败", errorText(err));
                    }
                  })()
                }
              >
                导出诊断包
              </button>
              <button
                className="btn btn--secondary"
                onClick={() =>
                  void (async () => {
                    try {
                      await api.Diagnostics.ClearCache();
                      onToast("success", "缓存已清理");
                    } catch (err) {
                      onToast("danger", "清理失败", errorText(err));
                    }
                  })()
                }
              >
                清理缓存与临时下载
              </button>
              <button className="btn btn--ghost" onClick={() => void api.Env.OpenInExplorer(paths?.logDir ?? "")}>
                打开日志目录
              </button>
            </div>
            {checks.length > 0 ? (
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {checks.map((c) => (
                  <li key={c.name} style={{ color: c.ok ? "var(--success)" : "var(--warning)" }}>
                    {c.name}：{c.ok ? "通过" : "未通过"}（{c.detail}）
                  </li>
                ))}
              </ul>
            ) : (
              <div className="muted">缓存目录：{paths?.cacheDir}</div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function PathRow({
  label,
  value,
  resolved,
  source,
  onPick,
  onClear,
}: {
  label: string;
  value: string;
  resolved?: string;
  source?: string;
  onPick: () => void;
  onClear: () => void;
}) {
  return (
    <div className="field">
      <div className="field__label">{label}</div>
      <div className="field__control">
        <div className="row">
          <input className="input mono" value={value} placeholder={resolved ?? "自动探测"} readOnly />
          <button className="btn btn--secondary" onClick={onPick}>
            选择
          </button>
          {value ? (
            <button className="btn btn--ghost" onClick={onClear}>
              清除
            </button>
          ) : null}
        </div>
        <div className="field__hint">
          当前生效：<span className="mono">{resolved || "未解析"}</span>
          {source ? `（${source}）` : ""}
        </div>
      </div>
    </div>
  );
}
