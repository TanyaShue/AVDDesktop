// 设备页：卡片列表 + 启动/停止 + 多开（对齐参考截图的工具栏与卡片布局）。
import { useCallback, useEffect, useMemo, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { AvdSummary, EmulatorInstance } from "../bridge/types";
import type { EnvCheck } from "../hooks/useEnvCheck";
import { useWailsEvent } from "../hooks/useApp";
import { DeviceWizard } from "./DeviceWizard";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
  env: EnvCheck;
}

export function DevicesPage({ onToast, env }: Props) {
  const [devices, setDevices] = useState<AvdSummary[]>([]);
  const [instances, setInstances] = useState<EmulatorInstance[]>([]);
  const [query, setQuery] = useState("");
  const [view, setView] = useState<"grid" | "list">("grid");
  const [sortBy, setSortBy] = useState<"name" | "api">("name");
  const [showWizard, setShowWizard] = useState(false);
  const [busyName, setBusyName] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const list = api.asArray((await api.Avd.List()) as AvdSummary[]);
      setDevices(list);
      setInstances(api.asArray((await api.Emulator.ListRunning()) as EmulatorInstance[]));
    } catch (err) {
      onToast("danger", "无法读取设备列表", errorText(err));
    }
  }, [onToast]);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<EmulatorInstance>(EVENTS.emulatorState, () => void load());
  useWailsEvent<unknown>(EVENTS.avdChanged, () => void load());

  const running = useMemo(() => {
    const map = new Map<string, EmulatorInstance>();
    instances.forEach((inst) => {
      if (inst.state === "running" || inst.state === "booting" || inst.state === "starting") {
        map.set(inst.avdName, inst);
      }
    });
    return map;
  }, [instances]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = devices.filter(
      (d) => !q || `${d.name} ${d.api} ${d.tag} ${d.abi}`.toLowerCase().includes(q),
    );
    return [...list].sort((a, b) => {
      switch (sortBy) {
        case "api":
          return (b.api || "").localeCompare(a.api || "", undefined, { numeric: true });
        default:
          return a.name.localeCompare(b.name);
      }
    });
  }, [devices, query, sortBy]);

  const startDevice = async (device: AvdSummary, opts?: { coldBoot?: boolean; noWindow?: boolean }) => {
    setBusyName(device.name);
    try {
      await api.Emulator.Start({
        avdName: device.name,
        coldBoot: opts?.coldBoot ?? false,
        noWindow: opts?.noWindow ?? false,
      });
      onToast("info", "正在启动 " + device.name, "首次启动可能需要几分钟");
      await load();
    } catch (err) {
      onToast("danger", "启动失败", errorText(err));
    } finally {
      setBusyName(null);
    }
  };

  const stopDevice = async (device: AvdSummary) => {
    setBusyName(device.name);
    try {
      await api.Emulator.StopByAvd(device.name, false);
      onToast("success", "已停止 " + device.name);
      await load();
    } catch (err) {
      onToast("danger", "停止失败", errorText(err));
    } finally {
      setBusyName(null);
    }
  };

  const deleteDevice = async (device: AvdSummary) => {
    const ok = window.confirm(
      `确认删除设备「${device.name}」？

将删除该设备的全部数据，此操作不可撤销。`,
    );
    if (!ok) return;
    try {
      const id = await api.Avd.Delete(device.name);
      onToast("info", "正在删除设备", `任务 ${id}`);
    } catch (err) {
      onToast("danger", "删除失败", errorText(err));
    }
  };

  return (
    <div className="page">
      <div className="pageheader">
        <div className="pageheader__text">
          <div className="pageheader__title">设备</div>
          <div className="pageheader__subtitle">
            <span>
              共 {devices.length} 个设备，{running.size} 个运行中
            </span>
          </div>
        </div>
      </div>

      <div className="pagecontent">
        {env.report && !env.report.ready ? (
          <div className="banner banner--info" style={{ marginBottom: 12 }}>
            <div className="banner__icon">⚠️</div>
            <div className="banner__body">
              <div className="banner__title">环境尚未就绪</div>
              <div className="banner__text">
                {env.report.needInit
                  ? "软件自带 SDK 尚未初始化，需要先下载官方命令行工具并安装 platform-tools 与 emulator。"
                  : "部分组件不可用，请到设置页查看环境检查结果。"}
              </div>
            </div>
            {env.report.needInit ? (
              <button className="btn btn--primary" onClick={() => void env.prepare()}>
                立即准备
              </button>
            ) : null}
          </div>
        ) : null}

        <div className="toolbar">
          <button className="btn btn--primary btn--lg" onClick={() => setShowWizard(true)}>
            ＋ 新建设备
          </button>
          <div className="toolbar__right">
            <input
              className="search"
              placeholder="搜索设备…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <select
              className="input"
              style={{ width: 120, height: 36 }}
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value as typeof sortBy)}
              aria-label="排序方式"
            >
              <option value="name">按名称</option>
              <option value="api">按 Android 版本</option>
            </select>
            <div className="segmented">
              <button aria-pressed={view === "grid"} onClick={() => setView("grid")} title="网格视图">
                ▦
              </button>
              <button aria-pressed={view === "list"} onClick={() => setView("list")} title="列表视图">
                ☰
              </button>
            </div>
          </div>
        </div>

        {filtered.length === 0 ? (
          <div className="empty">
            <div className="empty__icon">📱</div>
            <div className="empty__title">{devices.length === 0 ? "还没有模拟器设备" : "没有匹配的设备"}</div>
            <div className="empty__desc">
              {devices.length === 0
                ? "创建第一个设备后即可启动 Android 模拟器。需要先安装命令行工具、模拟器与至少一个系统镜像。"
                : "换个关键词试试，或清空搜索条件。"}
            </div>
            {devices.length === 0 ? (
              <button className="btn btn--primary btn--lg" onClick={() => setShowWizard(true)}>
                ＋ 新建设备
              </button>
            ) : null}
          </div>
        ) : (
          <div className={view === "grid" ? "grid grid--devices" : "grid"} style={{ gridTemplateColumns: view === "list" ? "1fr" : undefined }}>
            {filtered.map((device) => {
              const inst = running.get(device.name);
              const isRunning = !!inst;
              return (
                <div key={device.name} className="card card--hover device">
                  <div className="device__thumb" aria-hidden>
                    {inst?.state === "running" ? inst.avdName.slice(0, 2).toUpperCase() : "AVD"}
                  </div>

                  <div className="device__body">
                    <div className="device__name truncate" title={device.name}>
                      {device.name}
                    </div>
                    <div className="device__chips">
                      <span className="chip">Android {device.api || "?"}</span>
                      {device.tag ? <span className="chip">{device.tag}</span> : null}
                      {device.abi ? <span className="chip">{device.abi}</span> : null}
                      {device.broken ? <span className="chip chip--danger">配置异常</span> : null}
                    </div>
                    <div className="device__meta nums">
                      <span className="device__state">
                        <span
                          className="dot"
                          style={{
                            background: isRunning ? "var(--success)" : "var(--text-disabled)",
                          }}
                        />
                        {stateLabel(inst?.state, isRunning)}
                      </span>
                      {inst ? (
                        <>
                          <span className="device__meta-sep" />
                          <span>{inst.serial}</span>
                        </>
                      ) : null}
                    </div>
                    {device.broken ? (
                      <div className="muted" style={{ color: "var(--danger)", marginTop: 6 }}>
                        {device.broken}
                      </div>
                    ) : null}
                  </div>

                  <div className="device__actions">
                    <div className="device__row-actions">
                      {isRunning ? (
                        <button
                          className="btn btn--circle btn--circle-stop"
                          title="停止"
                          disabled={busyName === device.name}
                          onClick={() => void stopDevice(device)}
                        >
                          ■
                        </button>
                      ) : (
                        <button
                          className="btn btn--circle"
                          title="启动"
                          disabled={busyName === device.name}
                          onClick={() => void startDevice(device)}
                        >
                          {busyName === device.name ? <span className="spinner" /> : "▶"}
                        </button>
                      )}
                      <details style={{ position: "relative" }}>
                        <summary
                          className="btn btn--round-soft"
                          style={{ listStyle: "none", display: "grid", placeItems: "center" }}
                          title="更多操作"
                        >
                          ⋯
                        </summary>
                        <div
                          className="card"
                          style={{
                            position: "absolute",
                            right: 0,
                            top: 44,
                            zIndex: 20,
                            padding: 6,
                            minWidth: 180,
                            boxShadow: "var(--shadow-pop)",
                          }}
                        >
                          <MenuItem
                            label="冷启动（不使用快照）"
                            onClick={() => void startDevice(device, { coldBoot: true })}
                          />
                          <MenuItem
                            label="无窗口启动"
                            onClick={() => void startDevice(device, { noWindow: true })}
                          />
                          <MenuItem
                            label="复制启动命令"
                            onClick={() =>
                              void api.Env.CopyToClipboard(
                                `emulator -avd ${device.name} -port ${inst?.port ?? 5554}`,
                              )
                            }
                          />
                          <MenuItem label="删除设备" danger onClick={() => void deleteDevice(device)} />
                        </div>
                      </details>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {showWizard ? (
        <DeviceWizard
          onClose={() => setShowWizard(false)}
          onCreated={() => void load()}
          onToast={onToast}
        />
      ) : null}
    </div>
  );
}

function MenuItem({ label, onClick, danger }: { label: string; onClick: () => void; danger?: boolean }) {
  return (
    <button
      className={`btn ${danger ? "btn--danger-ghost" : "btn--ghost"}`}
      style={{ width: "100%", justifyContent: "flex-start" }}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function stateLabel(state: string | undefined, isRunning: boolean): string {
  if (!isRunning) return "已停止";
  switch (state) {
    case "starting":
      return "启动中";
    case "booting":
      return "开机中";
    case "running":
      return "运行中";
    case "stopping":
      return "停止中";
    case "error":
      return "异常";
    default:
      return "已停止";
  }
}
