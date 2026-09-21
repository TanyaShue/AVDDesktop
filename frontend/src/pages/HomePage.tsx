// 首页：环境自检（对齐 docs/UI-SPEC.md §5.1）。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { EnvReport, MirrorSource, ToolState, ToolStatus } from "../bridge/types";
import { useWailsEvent } from "../hooks/useApp";
import { SpeedTestModal } from "../components/SpeedTestModal";
import { Progress, stateChip, stateTone } from "../components/ui";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
  onGotoDevices: () => void;
}

export function HomePage({ onToast, onGotoDevices }: Props) {
  const [report, setReport] = useState<EnvReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [source, setSource] = useState<MirrorSource | null>(null);
  const [showSpeedTest, setShowSpeedTest] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(async (force = false) => {
    setLoading(true);
    try {
      const rep = (await api.Env.Detect({ force, sdkRootOverride: "" })) as EnvReport;
      setReport(rep);
      const sources = (await api.Mirror.ListSources()) as MirrorSource[];
      const settings = (await api.Settings.Get()) as { activeSourceId: string };
      setSource(sources.find((s) => s.id === settings.activeSourceId) ?? sources[0] ?? null);
    } catch (err) {
      onToast("danger", "自检失败", errorText(err));
    } finally {
      setLoading(false);
    }
  }, [onToast]);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<EnvReport>(EVENTS.envChanged, (rep) => setReport(rep));
  useWailsEvent<unknown>(EVENTS.sdkChanged, () => void load(true));

  const applyFix = async (tool: ToolStatus) => {
    const fix = tool.fix;
    if (!fix) return;
    setBusy(tool.id);
    try {
      switch (fix.kind) {
        case "install":
        case "repair": {
          if (!source) {
            setShowSpeedTest(true);
            return;
          }
          const id = await api.Sdk.Install({
            packages: fix.payload ? [fix.payload] : [],
            sourceId: source.id,
            allowFallbackToOfficial: true,
            autoAcceptLicenses: false,
          });
          onToast("info", "已开始安装", `${fix.label}（任务 ${id}）`);
          break;
        }
        case "downloadImage": {
          onToast("info", "请选择系统镜像", "打开 SDK 页面选择需要的 Android 版本");
          break;
        }
        case "setJdk": {
          const dir = await api.Env.PickDirectory({ title: "选择 JDK 目录（包含 bin/java）" });
          if (dir) {
            await api.Settings.Update({ jdkPath: dir });
            onToast("success", "已设置 JDK 路径", dir);
            await load(true);
          }
          break;
        }
        case "enableWhpx": {
          await api.Env.OpenExternalURL(
            "https://learn.microsoft.com/zh-cn/windows/apps/develop/dev-drive#enable-hypervisor-platform",
          );
          onToast("info", "已打开官方文档", "开启「Windows 虚拟机监控程序平台」后需要重启电脑");
          break;
        }
        case "installAehd":
        case "choosePath": {
          const dir = await api.Env.PickDirectory({ title: "选择目录" });
          if (dir) {
            await api.Settings.Update({ sdkRoot: dir });
            onToast("success", "已切换目录", dir);
            await load(true);
          }
          break;
        }
        case "openSpeedTest": {
          setShowSpeedTest(true);
          break;
        }
        default:
          onToast("info", "暂不支持的操作", fix.kind);
      }
    } catch (err) {
      onToast("danger", "操作失败", errorText(err));
    } finally {
      setBusy(null);
    }
  };

  /** 一键准备：安装命令行工具 + adb + 模拟器（含许可确认）。 */
  const prepareAll = async () => {
    if (!source) {
      setShowSpeedTest(true);
      return;
    }
    const needsCmdline = report?.components.find((c) => c.id === "cmdline-tools")?.state !== "present";
    try {
      if (needsCmdline) {
        const id = await api.Sdk.BootstrapCmdlineTools({
          sourceId: source.id,
          withPlatformTools: true,
          withEmulator: true,
          acceptLicenses: false,
        });
        onToast("info", "开始安装开发环境", `任务 ${id}，可在底部任务面板查看进度`);
      } else {
        const id = await api.Sdk.Install({
          packages: ["platform-tools", "emulator"],
          sourceId: source.id,
          allowFallbackToOfficial: true,
          autoAcceptLicenses: false,
        });
        onToast("info", "开始补齐平台工具与模拟器", `任务 ${id}`);
      }
    } catch (err) {
      const ae = err as { code?: string; message?: string };
      if (ae?.code === "LICENSE_NOT_ACCEPTED") {
        onToast("warning", "需要先接受许可协议", "请在 SDK 页面确认许可后可继续安装");
      } else {
        onToast("danger", "无法启动安装", errorText(err));
      }
    }
  };

  return (
    <div className="page">
      <div className="pageheader">
        <div className="pageheader__text">
          <div className="pageheader__title">环境自检</div>
          <div className="pageheader__subtitle">
            <span className="truncate" style={{ maxWidth: 420 }} title={report?.sdkRoot}>
              SDK：{report?.sdkRoot || "未解析"}
            </span>
            <span className="truncate" style={{ maxWidth: 320 }} title={report?.avdHome?.path}>
              AVD：{report?.avdHome?.path || "未解析"}
            </span>
          </div>
        </div>
        <div className="pageheader__actions">
          <button className="btn btn--secondary" onClick={() => void load(true)} disabled={loading}>
            {loading ? "检测中…" : "重新检测"}
          </button>
          <button className="btn btn--primary btn--lg" onClick={() => void prepareAll()}>
            {report?.ready ? "补齐组件" : "一键准备开发环境"}
          </button>
        </div>
      </div>

      <div className="pagecontent">
        {loading && !report ? (
          <div className="grid grid--tools">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="card" style={{ padding: 16 }}>
                <div className="skeleton" style={{ height: 18, width: "40%" }} />
                <div className="skeleton" style={{ height: 12, marginTop: 12 }} />
                <div className="skeleton" style={{ height: 12, width: "70%", marginTop: 8 }} />
              </div>
            ))}
          </div>
        ) : (
          <>
            {report && report.blockers.length > 0 ? (
              <div className="banner">
                <span className="banner__icon" aria-hidden>
                  ⚠
                </span>
                <div className="banner__body">
                  <div className="banner__title">
                    还有 {report.blockers.length} 项需要处理，完成后即可创建并启动模拟器
                  </div>
                  <div className="banner__text">
                    {report.blockers
                      .map((b) => b.name)
                      .slice(0, 3)
                      .join("、")}
                    {report.blockers.length > 3 ? " 等" : ""}
                  </div>
                </div>
                <button className="btn btn--primary" onClick={() => void prepareAll()}>
                  立即处理
                </button>
              </div>
            ) : null}

            {report?.ready ? (
              <div className="banner banner--info">
                <span className="banner__icon" aria-hidden>
                  ✓
                </span>
                <div className="banner__body">
                  <div className="banner__title">环境已就绪</div>
                  <div className="banner__text">
                    已安装 {report.components.find((c) => c.id === "system-images")?.version ?? "0 个"}系统镜像，
                    硬件加速 {report.accel.available ? `可用（${report.accel.kind.toUpperCase()}）` : "不可用（启动会较慢）"}
                  </div>
                </div>
                <button className="btn btn--secondary" onClick={onGotoDevices}>
                  去创建设备
                </button>
              </div>
            ) : null}

            <div className="grid grid--tools">
              {(report?.components ?? []).map((tool) => (
                <ToolCard key={tool.id} tool={tool} busy={busy === tool.id} onFix={() => void applyFix(tool)} />
              ))}
            </div>

            <div className="section" style={{ marginTop: 24 }}>
              <div className="section__title">当前镜像源</div>
              <div className="card" style={{ padding: 16, display: "flex", alignItems: "center", gap: 16 }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontWeight: 600 }}>{source?.name ?? "未选择"}</div>
                  <div className="mono muted truncate">{source?.baseURL}</div>
                  {source?.lastResult ? (
                    <div className="muted" style={{ marginTop: 4 }}>
                      延迟 {source.lastResult.ttfbMs} ms · 速度 {source.lastResult.throughputMBps.toFixed(2)} MB/s · 评分{" "}
                      {source.lastResult.score}
                    </div>
                  ) : (
                    <div className="muted" style={{ marginTop: 4 }}>
                      尚未测速，建议先测速以选择最快的源
                    </div>
                  )}
                </div>
                <button className="btn btn--secondary" onClick={() => setShowSpeedTest(true)}>
                  测速并更换
                </button>
              </div>
            </div>

            {report && report.accel.hints && report.accel.hints.length > 0 ? (
              <div className="section">
                <div className="section__title">硬件加速建议</div>
                <div className="card" style={{ padding: 16 }}>
                  <ul style={{ margin: 0, paddingLeft: 18, color: "var(--text-secondary)", lineHeight: "22px" }}>
                    {report.accel.hints.map((h, i) => (
                      <li key={i}>{h}</li>
                    ))}
                  </ul>
                </div>
              </div>
            ) : null}
          </>
        )}
      </div>

      {showSpeedTest ? (
        <SpeedTestModal
          activeSourceId={source?.id ?? ""}
          onClose={() => setShowSpeedTest(false)}
          onToast={onToast}
          onUseSource={async (id) => {
            await api.Mirror.SetActiveSource(id);
            onToast("success", "已切换镜像源");
            await load(true);
          }}
        />
      ) : null}
    </div>
  );
}

function ToolCard({ tool, busy, onFix }: { tool: ToolStatus; busy: boolean; onFix: () => void }) {
  const chip = stateChip(tool.state as ToolState | undefined);
  const tone = stateTone(tool.state as ToolState | undefined);
  return (
    <div className="card card--hover tool">
      <div className={tone.cls} aria-hidden>
        {tone.char}
      </div>
      <div className="tool__main">
        <div className="tool__head">
          <div className="tool__name truncate">{tool.name}</div>
          <span className={chip.cls}>{chip.text}</span>
        </div>
        {tool.version ? (
          <div className="muted" style={{ marginTop: 4 }}>
            版本 {tool.version}
          </div>
        ) : null}
        {tool.path ? (
          <div
            className="tool__path truncate"
            title={tool.path}
            onClick={() => void api.Env.CopyToClipboard(tool.path!)}
          >
            {tool.path}
          </div>
        ) : null}
        {tool.detail ? <div className="tool__detail">{tool.detail}</div> : null}
        <div className="tool__actions">
          {tool.fix ? (
            <button className="btn btn--primary" disabled={busy} onClick={onFix}>
              {busy ? <span className="spinner" /> : null}
              {tool.fix.label}
            </button>
          ) : null}
          {tool.path ? (
            <button
              className="btn btn--ghost"
              onClick={() => void api.Env.OpenInExplorer(tool.path!)}
              title="在文件管理器中打开"
            >
              打开位置
            </button>
          ) : null}
          {busy ? (
            <div style={{ flex: 1, alignSelf: "center" }}>
              <Progress percent={0} indeterminate />
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
