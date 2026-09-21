// SDK 管理页：已安装包 / 系统镜像 / 可用更新。
import { useCallback, useEffect, useMemo, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { SdkPackage, SystemImage } from "../bridge/types";
import { humanSize } from "../components/ui";

interface Props {
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
}

const SYSIMG_TAGS = [
  { id: "google_apis", label: "Google APIs" },
  { id: "google_apis_playstore", label: "Google Play" },
  { id: "android-desktop", label: "Desktop" },
  { id: "android-tv", label: "Android TV" },
];

export function SdkPage({ onToast }: Props) {
  const [tab, setTab] = useState<"installed" | "images" | "remote">("installed");
  const [installed, setInstalled] = useState<SdkPackage[]>([]);
  const [images, setImages] = useState<SystemImage[]>([]);
  const [remote, setRemote] = useState<SdkPackage[]>([]);
  const [loading, setLoading] = useState(false);
  const [tagFilter, setTagFilter] = useState<string[]>(["google_apis", "google_apis_playstore", "android-desktop"]);

  const loadInstalled = useCallback(async () => {
    try {
      setInstalled(((await api.Sdk.ListInstalled()) as SdkPackage[]) ?? []);
    } catch (err) {
      onToast("danger", "读取已安装包失败", errorText(err));
    }
  }, [onToast]);

  useEffect(() => {
    void loadInstalled();
  }, [loadInstalled]);

  const loadImages = useCallback(
    async (refresh: boolean) => {
      setLoading(true);
      try {
        const list = (await api.Sdk.ListSystemImages({
          tags: tagFilter,
          sourceId: "",
          refresh,
          onlyInstalled: false,
        })) as SystemImage[];
        setImages(list ?? []);
      } catch (err) {
        onToast("danger", "读取系统镜像失败", errorText(err));
      } finally {
        setLoading(false);
      }
    },
    [tagFilter, onToast],
  );

  const loadRemote = useCallback(async () => {
    setLoading(true);
    try {
      const list = (await api.Sdk.ListRemote({
        kinds: ["cmdline-tools", "platform-tools", "emulator", "platforms", "build-tools"],
        channel: "stable",
        sourceId: "",
        refresh: false,
        sysImgTag: "",
      })) as SdkPackage[];
      setRemote(list ?? []);
    } catch (err) {
      onToast("danger", "读取远程包列表失败", errorText(err));
    } finally {
      setLoading(false);
    }
  }, [onToast]);

  useEffect(() => {
    if (tab === "images" && images.length === 0) void loadImages(false);
    if (tab === "remote" && remote.length === 0) void loadRemote();
  }, [tab, images.length, remote.length, loadImages, loadRemote]);

  const install = async (packages: string[]) => {
    try {
      const id = await api.Sdk.Install({
        packages,
        sourceId: "",
        allowFallbackToOfficial: true,
        autoAcceptLicenses: true,
      });
      onToast("info", "已开始安装", `任务 ${id}`);
    } catch (err) {
      const ae = err as { code?: string; message?: string };
      if (ae?.code === "LICENSE_NOT_ACCEPTED") {
        onToast("warning", "需要接受许可协议", "正在打开许可确认…");
        try {
          const lics = (await api.Sdk.ListLicenses(packages)) as Array<{ id: string; text: string }>;
          if (lics.length > 0 && window.confirm(`${lics[0].text.slice(0, 1200)}\n\n（是否同意该许可协议？）`)) {
            await api.Sdk.AcceptLicenses(lics.map((l) => l.id));
            const id = await api.Sdk.Install({
              packages,
              sourceId: "",
              allowFallbackToOfficial: true,
              autoAcceptLicenses: true,
            });
            onToast("info", "已开始安装", `任务 ${id}`);
          }
        } catch (inner) {
          onToast("danger", "许可确认失败", errorText(inner));
        }
      } else {
        onToast("danger", "安装失败", errorText(err));
      }
    }
  };

  const uninstall = async (packages: string[]) => {
    if (!window.confirm(`确认卸载以下组件？\n\n${packages.join("\n")}`)) return;
    try {
      const id = await api.Sdk.Uninstall({ packages, dryRun: false });
      onToast("info", "正在卸载", `任务 ${id}`);
      setTimeout(() => void loadInstalled(), 2000);
    } catch (err) {
      onToast("danger", "卸载失败", errorText(err));
    }
  };

  const grouped = useMemo(() => {
    const map = new Map<string, SystemImage[]>();
    images.forEach((img) => {
      const list = map.get(img.tagId) ?? [];
      list.push(img);
      map.set(img.tagId, list);
    });
    return Array.from(map.entries());
  }, [images]);

  return (
    <div className="page">
      <div className="pageheader">
        <div className="pageheader__text">
          <div className="pageheader__title">SDK 组件</div>
          <div className="pageheader__subtitle">
            <span>安装、更新与卸载 Android SDK 组件（全部从当前镜像源下载）</span>
          </div>
        </div>
        <div className="pageheader__actions">
          <button
            className="btn btn--secondary"
            onClick={() => {
              if (tab === "images") void loadImages(true);
              else if (tab === "remote") void loadRemote();
              else void loadInstalled();
            }}
          >
            {loading ? "刷新中…" : "刷新"}
          </button>
        </div>
      </div>

      <div className="pagecontent">
        <div className="toolbar">
          <div className="segmented" style={{ width: "auto" }}>
            <button
              style={{ width: "auto", padding: "0 12px" }}
              aria-pressed={tab === "installed"}
              onClick={() => setTab("installed")}
            >
              已安装
            </button>
            <button
              style={{ width: "auto", padding: "0 12px" }}
              aria-pressed={tab === "images"}
              onClick={() => setTab("images")}
            >
              系统镜像
            </button>
            <button
              style={{ width: "auto", padding: "0 12px" }}
              aria-pressed={tab === "remote"}
              onClick={() => setTab("remote")}
            >
              工具与平台
            </button>
          </div>
        </div>

        {tab === "installed" ? (
          installed.length === 0 ? (
            <EmptyState
              title="还没有安装任何组件"
              desc="切换到「工具与平台」标签，从当前镜像源安装命令行工具、模拟器与 adb。"
            />
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>组件</th>
                  <th style={{ width: 100 }}>版本</th>
                  <th style={{ width: 110 }}>占用</th>
                  <th style={{ width: 200 }}>路径</th>
                  <th style={{ width: 160 }} />
                </tr>
              </thead>
              <tbody>
                {installed.map((pkg) => (
                  <tr key={pkg.path}>
                    <td>
                      <div style={{ fontWeight: 500 }}>{pkg.displayName}</div>
                      <div className="mono muted">{pkg.path}</div>
                    </td>
                    <td className="nums">{pkg.installedRevision || pkg.revision}</td>
                    <td className="nums">{humanSize(pkg.sizeBytes)}</td>
                    <td className="mono truncate" style={{ maxWidth: 200 }} title={pkg.url}>
                      {pkg.url}
                    </td>
                    <td>
                      <div className="row" style={{ gap: 6 }}>
                        <button
                          className="btn btn--ghost"
                          onClick={() =>
                            void (async () => {
                              try {
                                const res = (await api.Sdk.VerifyPackage(pkg.path)) as {
                                  ok: boolean;
                                  details: string[];
                                };
                                onToast(res.ok ? "success" : "warning", `${pkg.path} ${res.ok ? "正常" : "异常"}`, res.details.join("；"));
                              } catch (err) {
                                onToast("danger", "校验失败", errorText(err));
                              }
                            })()
                          }
                        >
                          校验
                        </button>
                        <button className="btn btn--danger-ghost" onClick={() => void uninstall([pkg.path])}>
                          卸载
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        ) : null}

        {tab === "images" ? (
          <>
            <div className="row" style={{ marginBottom: 12, flexWrap: "wrap", gap: 8 }}>
              <span className="muted">镜像类型：</span>
              {SYSIMG_TAGS.map((tag) => {
                const active = tagFilter.includes(tag.id);
                return (
                  <button
                    key={tag.id}
                    className={`chip${active ? " chip--info" : ""}`}
                    onClick={() => {
                      setTagFilter((prev) =>
                        prev.includes(tag.id) ? prev.filter((t) => t !== tag.id) : [...prev, tag.id],
                      );
                      setImages([]);
                    }}
                  >
                    {tag.label}
                  </button>
                );
              })}
            </div>

            {loading && images.length === 0 ? (
              <div className="card" style={{ padding: 24 }}>
                <span className="row">
                  <span className="spinner" /> 正在读取镜像索引…
                </span>
              </div>
            ) : images.length === 0 ? (
              <EmptyState title="没有取到系统镜像" desc="请先在首页测速并选择一个可用的镜像源。" />
            ) : (
              grouped.map(([tagId, list]) => (
                <div key={tagId} className="section">
                  <div className="section__title">
                    {list[0]?.tagDisplay || tagId}（{list.length}）
                  </div>
                  <table className="table">
                    <thead>
                      <tr>
                        <th>Android 版本</th>
                        <th style={{ width: 120 }}>ABI</th>
                        <th style={{ width: 110 }}>大小</th>
                        <th style={{ width: 140 }}>状态</th>
                        <th style={{ width: 120 }} />
                      </tr>
                    </thead>
                    <tbody>
                      {list
                        .slice()
                        .sort((a, b) => b.apiLevel.localeCompare(a.apiLevel, undefined, { numeric: true }))
                        .map((img) => (
                          <tr key={img.path}>
                            <td className="nums">
                              Android {img.apiLevel}
                              {img.isPlaystore ? <span className="chip chip--sm chip--warning" style={{ marginLeft: 8 }}>Play</span> : null}
                            </td>
                            <td className="mono">{img.abi}</td>
                            <td className="nums">{humanSize(img.sizeBytes)}</td>
                            <td>
                              {img.installed ? (
                                <span className="chip chip--success">已安装</span>
                              ) : (
                                <span className="chip">未安装</span>
                              )}
                            </td>
                            <td>
                              {img.installed ? (
                                <button className="btn btn--danger-ghost" onClick={() => void uninstall([img.path])}>
                                  卸载
                                </button>
                              ) : (
                                <button className="btn btn--primary" onClick={() => void install([img.path])}>
                                  下载安装
                                </button>
                              )}
                            </td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                </div>
              ))
            )}
          </>
        ) : null}

        {tab === "remote" ? (
          loading && remote.length === 0 ? (
            <div className="card" style={{ padding: 24 }}>
              <span className="row">
                <span className="spinner" /> 正在读取仓库索引…
              </span>
            </div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>组件</th>
                  <th style={{ width: 110 }}>最新版本</th>
                  <th style={{ width: 110 }}>大小</th>
                  <th style={{ width: 200 }}>本机状态</th>
                  <th style={{ width: 140 }} />
                </tr>
              </thead>
              <tbody>
                {remote.map((pkg) => (
                  <tr key={pkg.path}>
                    <td>
                      <div style={{ fontWeight: 500 }}>{pkg.displayName}</div>
                      <div className="mono muted">{pkg.path}</div>
                    </td>
                    <td className="nums">{pkg.revision}</td>
                    <td className="nums">{humanSize(pkg.sizeBytes)}</td>
                    <td>
                      {pkg.installed ? (
                        pkg.hasUpdate ? (
                          <span className="chip chip--warning">可更新（当前 {pkg.installedRevision}）</span>
                        ) : (
                          <span className="chip chip--success">已是最新</span>
                        )
                      ) : (
                        <span className="chip chip--danger">未安装</span>
                      )}
                    </td>
                    <td>
                      {pkg.installed && !pkg.hasUpdate ? (
                        <button className="btn btn--danger-ghost" onClick={() => void uninstall([pkg.path])}>
                          卸载
                        </button>
                      ) : (
                        <button className="btn btn--primary" onClick={() => void install([pkg.path])}>
                          {pkg.installed ? "更新" : "安装"}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        ) : null}
      </div>
    </div>
  );
}

function EmptyState({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="empty">
      <div className="empty__icon">🧩</div>
      <div className="empty__title">{title}</div>
      <div className="empty__desc">{desc}</div>
    </div>
  );
}
