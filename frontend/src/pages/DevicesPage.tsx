// 设备页：卡片列表 + 启动/停止 + 多开（对齐参考截图的工具栏与卡片布局）。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { AvdState, AvdSummary, EmulatorInstance } from "../bridge/types";
import type { EnvCheck } from "../hooks/useEnvCheck";
import { useWailsEvent } from "../hooks/useApp";
import { DeviceWizard } from "./DeviceWizard";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
  env: EnvCheck;
  /** 来自设置页：关闭后删除设备不再弹确认框。 */
  confirmBeforeDelete: boolean;
}

export function DevicesPage({ onToast, env, confirmBeforeDelete }: Props) {
  const [devices, setDevices] = useState<AvdSummary[]>([]);
  const [instances, setInstances] = useState<EmulatorInstance[]>([]);
  const [query, setQuery] = useState("");
  const [view, setView] = useState<"grid" | "list">("grid");
  const [sortBy, setSortBy] = useState<"name" | "api">("name");
  const [showWizard, setShowWizard] = useState(false);
  const [busyNames, setBusyNames] = useState<ReadonlySet<string>>(() => new Set());

  /** 标记 / 解除某台设备的操作中状态（按设备名分别管理，避免并发操作互相清除）。 */
  const setBusy = useCallback((name: string, busy: boolean) => {
    setBusyNames((prev) => {
      const next = new Set(prev);
      if (busy) next.add(name);
      else next.delete(name);
      return next;
    });
  }, []);

  const loadSeq = useRef(0);

  const load = useCallback(async () => {
    const seq = ++loadSeq.current;
    try {
      const list = api.asArray((await api.Avd.List()) as AvdSummary[]);
      const running = api.asArray((await api.Emulator.ListRunning()) as EmulatorInstance[]);
      // 只应用最新一次加载的结果：emulator:state 在启动过程中会密集触发 load，
      // 乱序返回的旧快照会把新状态覆盖回去（界面显示"已停止"但进程还在跑）。
      if (seq !== loadSeq.current) return;
      setDevices(list);
      setInstances(running);
    } catch (err) {
      if (seq !== loadSeq.current) return;
      onToast("danger", "无法读取设备列表", errorText(err));
    }
  }, [onToast]);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<EmulatorInstance>(EVENTS.emulatorState, () => void load());
  useWailsEvent<unknown>(EVENTS.avdChanged, () => void load());

  // active 的实例（进程仍活着，含 starting/booting/stopping/error）才提供停止按钮：
  // 后端对同一 AVD 的重复启动会直接返回 FILE_IN_USE，误显示「启动」会让用户白撞。
  const active = useMemo(() => {
    const map = new Map<string, EmulatorInstance>();
    instances.forEach((inst) => {
      if (instanceAlive(inst)) map.set(inst.avdName, inst);
    });
    return map;
  }, [instances]);

  const runningCount = useMemo(
    () => instances.filter((inst) => inst.state === "running").length,
    [instances],
  );

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
    setBusy(device.name, true);
    try {
      await api.Emulator.Start({
        avdName: device.name,
        coldBoot: opts?.coldBoot ?? false,
        noWindow: opts?.noWindow ?? false,
      });
      onToast("info", "正在启动 " + device.name, "首次启动可能需要几分钟，进度显示在底部任务区域");
      await load();
    } catch (err) {
      onToast("danger", "启动失败", errorText(err));
    } finally {
      setBusy(device.name, false);
    }
  };

  const stopDevice = async (device: AvdSummary) => {
    setBusy(device.name, true);
    try {
      await api.Emulator.StopByAvd(device.name, false);
      onToast("success", "已停止 " + device.name);
      await load();
    } catch (err) {
      onToast("danger", "停止失败", errorText(err));
    } finally {
      setBusy(device.name, false);
    }
  };

  const deleteDevice = async (device: AvdSummary) => {
    if (confirmBeforeDelete) {
      const ok = window.confirm(
        `确认删除设备「${device.name}」？

将删除该设备的全部数据，此操作不可撤销。`,
      );
      if (!ok) return;
    }
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
              共 {devices.length} 个设备，{runningCount} 个运行中
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
                  ? "软件自带 JDK / SDK 尚未就绪，需要先补齐 JDK、官方命令行工具并安装 platform-tools 与 emulator。"
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
              className="select select--compact"
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
              const inst = active.get(device.name);
              // Wails 把 Go 的 AvdState 生成为 string，这里收敛回联合类型后再交给状态展示函数
              const state = inst?.state as AvdState | undefined;
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
                        <span className="dot" style={{ background: stateColor(state) }} />
                        {stateLabel(state)}
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
                      {inst ? (
                        <button
                          className="btn btn--circle btn--circle-stop"
                          title="停止"
                          disabled={busyNames.has(device.name)}
                          onClick={() => void stopDevice(device)}
                        >
                          ■
                        </button>
                      ) : (
                        <button
                          className="btn btn--circle"
                          title="启动"
                          disabled={busyNames.has(device.name)}
                          onClick={() => void startDevice(device)}
                        >
                          {busyNames.has(device.name) ? <span className="spinner" /> : "▶"}
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

/** 实例进程是否仍活着：stopped 表示已退出；error 状态只有在进程退出（有退出码）后才算结束。 */
function instanceAlive(inst: EmulatorInstance): boolean {
  if (inst.state === "stopped") return false;
  if (inst.state === "error") return inst.exitCode == null;
  return true;
}

/** 状态点颜色：与后端状态机一致，异常用红色、过渡状态用橙色。 */
function stateColor(state: AvdState | undefined): string {
  switch (state) {
    case "running":
      return "var(--success)";
    case "error":
      return "var(--danger)";
    case "starting":
    case "booting":
    case "stopping":
      return "var(--warning)";
    default:
      return "var(--text-disabled)";
  }
}

function stateLabel(state: AvdState | undefined): string {
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
