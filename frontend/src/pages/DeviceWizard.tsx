// 创建 / 编辑设备向导（对齐 docs/UI-SPEC.md §5.4）。
import { useEffect, useMemo, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { AvdSpec, DeviceProfile, HwConfigItem, SystemImage } from "../bridge/types";
import { Modal } from "../components/ui";

const STEPS = ["基本信息", "设备型号", "系统镜像", "硬件配置"] as const;

interface Props {
  onClose: () => void;
  onCreated: () => void;
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
}

export function DeviceWizard({ onClose, onCreated, onToast }: Props) {
  const [step, setStep] = useState(0);
  const [profiles, setProfiles] = useState<DeviceProfile[]>([]);
  const [images, setImages] = useState<SystemImage[]>([]);
  const [schema, setSchema] = useState<HwConfigItem[]>([]);
  const [meta, setMeta] = useState<{ groupOrder: string[]; groupLabels: Record<string, string> }>({
    groupOrder: [],
    groupLabels: {},
  });
  const [profileQuery, setProfileQuery] = useState("");
  const [profileCategory, setProfileCategory] = useState("all");
  const [submitting, setSubmitting] = useState(false);

  const [name, setName] = useState("MyDevice");
  const [displayName, setDisplayName] = useState("");
  const [validation, setValidation] = useState<{ valid: boolean; reason?: string; suggest?: string }>({
    valid: true,
  });
  const [profileId, setProfileId] = useState("medium_phone");
  const [imagePath, setImagePath] = useState("");
  const [sdcard, setSdcard] = useState("512M");
  const [hw, setHw] = useState<Record<string, string>>({});
  const [showIni, setShowIni] = useState(false);

  useEffect(() => {
    void (async () => {
      try {
        const [p, m] = await Promise.all([
          api.Avd.ListProfiles(false) as Promise<DeviceProfile[]>,
          api.Avd.ConfigSchemaMetadata() as Promise<{
            groupOrder: string[];
            groupLabels: Record<string, string>;
            presets: unknown;
          }>,
        ]);
        setProfiles(p ?? []);
        setMeta({ groupOrder: m?.groupOrder ?? [], groupLabels: m?.groupLabels ?? {} });
        const s = (await api.Avd.ListConfigSchema()) as HwConfigItem[];
        setSchema(s ?? []);
      } catch (err) {
        onToast("warning", "设备档案加载失败", errorText(err));
      }
    })();
  }, [onToast]);

  // 只在打开"系统镜像"步骤时拉取镜像列表（可能触发网络请求）
  useEffect(() => {
    if (step !== 2 || images.length > 0) return;
    void (async () => {
      try {
        const list = (await api.Sdk.ListSystemImages({
          tags: [],
          sourceId: "",
          refresh: false,
          onlyInstalled: true,
        })) as SystemImage[];
        setImages(list ?? []);
        if (!imagePath && list && list.length > 0) setImagePath(list[0].path);
      } catch (err) {
        onToast("warning", "无法读取已安装镜像", errorText(err));
      }
    })();
  }, [step, images.length, imagePath, onToast]);

  useEffect(() => {
    const timer = setTimeout(() => {
      void (async () => {
        try {
          const res = (await api.Avd.ValidateName(name)) as {
            valid: boolean;
            reason?: string;
            suggest?: string;
          };
          setValidation(res);
        } catch {
          /* 忽略 */
        }
      })();
    }, 250);
    return () => clearTimeout(timer);
  }, [name]);

  const filteredProfiles = useMemo(() => {
    const q = profileQuery.trim().toLowerCase();
    return profiles.filter((p) => {
      if (profileCategory !== "all" && p.category !== profileCategory) return false;
      if (!q) return true;
      return `${p.name} ${p.id} ${p.oem}`.toLowerCase().includes(q);
    });
  }, [profiles, profileQuery, profileCategory]);

  const categories = useMemo(() => {
    const set = new Map<string, number>();
    profiles.forEach((p) => set.set(p.category, (set.get(p.category) ?? 0) + 1));
    return Array.from(set.entries());
  }, [profiles]);

  const groupedSchema = useMemo(() => {
    const map = new Map<string, HwConfigItem[]>();
    schema.forEach((item) => {
      const list = map.get(item.group) ?? [];
      list.push(item);
      map.set(item.group, list);
    });
    return map;
  }, [schema]);

  const buildSpec = (): AvdSpec =>
    ({
      name,
      displayName: displayName || name,
      profileId,
      systemImagePath: imagePath,
      sdcardSize: sdcard,
      hw,
      createWithAvdManager: true,
    }) as AvdSpec;

  const canNext = () => {
    if (step === 0) return validation.valid && name.trim().length > 0;
    if (step === 1) return !!profileId;
    if (step === 2) return !!imagePath;
    return true;
  };

  const submit = async () => {
    setSubmitting(true);
    try {
      const id = await api.Avd.Create(buildSpec());
      onToast("success", "已开始创建设备", `任务 ${id}，可在底部任务面板查看进度`);
      onCreated();
      onClose();
    } catch (err) {
      onToast("danger", "创建失败", errorText(err));
    } finally {
      setSubmitting(false);
    }
  };

  const [cmdPreview, setCmdPreview] = useState<Record<string, string> | null>(null);

  return (
    <Modal
      title="新建设备"
      size="xl"
      onClose={onClose}
      footer={
        <>
          <button
            className="btn btn--ghost"
            onClick={() =>
              void (async () => {
                try {
                  const res = (await api.Avd.ComputeCommand(buildSpec())) as Record<string, string>;
                  setCmdPreview(res);
                } catch (err) {
                  onToast("warning", "无法生成命令", errorText(err));
                }
              })()
            }
          >
            查看等效命令
          </button>
          <div className="modal__foot-right">
            <button className="btn btn--secondary" disabled={step === 0} onClick={() => setStep((s) => s - 1)}>
              上一步
            </button>
            {step < STEPS.length - 1 ? (
              <button className="btn btn--primary" disabled={!canNext()} onClick={() => setStep((s) => s + 1)}>
                下一步
              </button>
            ) : (
              <button className="btn btn--primary" disabled={submitting || !validation.valid} onClick={() => void submit()}>
                {submitting ? "创建中…" : "创建"}
              </button>
            )}
          </div>
        </>
      }
    >
      <div className="row" style={{ marginBottom: 20, gap: 8 }}>
        {STEPS.map((label, i) => (
          <div key={label} className="row" style={{ gap: 8 }}>
            <span
              className={`chip${i === step ? " chip--info" : i < step ? " chip--success" : ""}`}
              style={{ height: 28 }}
            >
              {i < step ? "✓" : i + 1} {label}
            </span>
            {i < STEPS.length - 1 ? <span style={{ color: "var(--text-disabled)" }}>›</span> : null}
          </div>
        ))}
      </div>

      {step === 0 ? (
        <>
          <div className="field">
            <div className="field__label">设备名称</div>
            <div className="field__control">
              <input
                className={`input${validation.valid ? "" : " input--error"}`}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="仅字母、数字、下划线、点、连字符"
              />
              <div className={`field__hint${validation.valid ? "" : " field__hint--error"}`}>
                {validation.valid
                  ? "用于目录名与命令行，创建后不建议修改"
                  : `${validation.reason ?? "名称不合法"}${validation.suggest ? `（建议：${validation.suggest}）` : ""}`}
              </div>
            </div>
          </div>
          <div className="field">
            <div className="field__label">显示名称</div>
            <div className="field__control">
              <input
                className="input"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="支持中文与空格，留空则与设备名称相同"
              />
            </div>
          </div>
        </>
      ) : null}

      {step === 1 ? (
        <>
          <div className="row" style={{ marginBottom: 12 }}>
            <input
              className="input"
              style={{ maxWidth: 260 }}
              placeholder="搜索设备型号…"
              value={profileQuery}
              onChange={(e) => setProfileQuery(e.target.value)}
            />
            <div className="row" style={{ flexWrap: "wrap", gap: 6 }}>
              <button
                className={`chip${profileCategory === "all" ? " chip--info" : ""}`}
                onClick={() => setProfileCategory("all")}
              >
                全部 {profiles.length}
              </button>
              {categories.map(([cat, count]) => (
                <button
                  key={cat}
                  className={`chip${profileCategory === cat ? " chip--info" : ""}`}
                  onClick={() => setProfileCategory(cat)}
                >
                  {categoryLabel(cat)} {count}
                </button>
              ))}
            </div>
          </div>
          <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(240px, 1fr))", maxHeight: 380, overflow: "auto" }}>
            {filteredProfiles.map((p) => (
              <button
                key={p.id}
                className={`card card--hover${p.id === profileId ? " " : ""}`}
                style={{
                  padding: 12,
                  textAlign: "left",
                  borderColor: p.id === profileId ? "var(--primary)" : undefined,
                  background: p.id === profileId ? "var(--primary-soft)" : undefined,
                }}
                onClick={() => setProfileId(p.id)}
              >
                <div style={{ fontWeight: 600, fontSize: 13 }} className="truncate">
                  {p.name}
                </div>
                <div className="muted" style={{ fontSize: 12 }}>
                  {p.oem} · {categoryLabel(p.category)}
                </div>
                {p.width ? (
                  <div className="muted nums" style={{ fontSize: 12, marginTop: 4 }}>
                    {p.width} × {p.height} @ {p.density}dpi
                  </div>
                ) : (
                  <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>
                    {p.id}
                  </div>
                )}
              </button>
            ))}
          </div>
        </>
      ) : null}

      {step === 2 ? (
        images.length === 0 ? (
          <div className="empty">
            <div className="empty__icon">🧩</div>
            <div className="empty__title">还没有已安装的系统镜像</div>
            <div className="empty__desc">
              创建 AVD 需要至少一个系统镜像。请到「SDK」页面从当前镜像源下载一个（推荐 Google APIs，约 1.5-2 GB）。
            </div>
          </div>
        ) : (
          <div className="grid" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(260px, 1fr))" }}>
            {images.map((img) => (
              <button
                key={img.path}
                className="card card--hover"
                style={{
                  padding: 12,
                  textAlign: "left",
                  borderColor: img.path === imagePath ? "var(--primary)" : undefined,
                  background: img.path === imagePath ? "var(--primary-soft)" : undefined,
                }}
                onClick={() => setImagePath(img.path)}
              >
                <div style={{ fontWeight: 600 }}>Android {img.apiLevel}</div>
                <div className="muted" style={{ fontSize: 12, marginTop: 2 }}>
                  {img.tagDisplay || img.tagId} · {img.abi}
                </div>
                <div className="row" style={{ marginTop: 6, gap: 6 }}>
                  {img.isPlaystore ? <span className="chip chip--sm chip--warning">Play</span> : null}
                  <span className="chip chip--sm">rev {img.revision}</span>
                </div>
              </button>
            ))}
          </div>
        )
      ) : null}

      {step === 3 ? (
        <>
          <div className="row" style={{ marginBottom: 12 }}>
            <button className="btn btn--secondary" onClick={() => setShowIni((v) => !v)}>
              {showIni ? "返回可视化配置" : "直接编辑 config.ini 键值"}
            </button>
            <span className="muted">留空的项使用设备档案默认值</span>
          </div>

          {showIni ? (
            <textarea
              className="input mono"
              style={{ height: 320, padding: 12, resize: "vertical" }}
              value={Object.entries(hw)
                .map(([k, v]) => `${k}=${v}`)
                .join("\n")}
              onChange={(e) => {
                const next: Record<string, string> = {};
                e.target.value.split("\n").forEach((line) => {
                  const idx = line.indexOf("=");
                  if (idx > 0) next[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
                });
                setHw(next);
              }}
              placeholder={"hw.ramSize=4096\nhw.cpu.ncore=8"}
            />
          ) : (
            <div style={{ maxHeight: 380, overflow: "auto" }}>
              {meta.groupOrder.map((group) => {
                const items = groupedSchema.get(group);
                if (!items) return null;
                return (
                  <div key={group} className="section">
                    <div className="section__title">{meta.groupLabels[group] ?? group}</div>
                    {items
                      .filter((item) => !item.advanced)
                      .map((item) => (
                        <HwField
                          key={item.key}
                          item={item}
                          value={hw[item.key] ?? ""}
                          onChange={(v) => setHw((prev) => ({ ...prev, [item.key]: v }))}
                        />
                      ))}
                  </div>
                );
              })}
              <div className="section">
                <div className="section__title">存储</div>
                <div className="field">
                  <div className="field__label">SD 卡容量</div>
                  <div className="field__control">
                    <select className="input" value={sdcard} onChange={(e) => setSdcard(e.target.value)}>
                      {["256M", "512M", "1G", "2G", "4G"].map((v) => (
                        <option key={v} value={v}>
                          {v}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
              </div>
            </div>
          )}
        </>
      ) : null}

      {cmdPreview ? (
        <div className="card" style={{ padding: 12, marginTop: 16 }}>
          <div className="section__title">等效命令行</div>
          <div className="mono" style={{ whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
            {Object.entries(cmdPreview)
              .map(([k, v]) => `# ${k}\n${v}`)
              .join("\n\n")}
          </div>
          <button className="btn btn--ghost" style={{ marginTop: 8 }} onClick={() => setCmdPreview(null)}>
            收起
          </button>
        </div>
      ) : null}
    </Modal>
  );
}

function HwField({
  item,
  value,
  onChange,
}: {
  item: HwConfigItem;
  value: string;
  onChange: (v: string) => void;
}) {
  const placeholder = item.default ? `默认 ${item.default}` : "未设置";
  return (
    <div className="field">
      <div className="field__label" title={item.description}>
        {item.label}
        {item.unit ? <span className="muted"> ({item.unit})</span> : null}
      </div>
      <div className="field__control">
        {item.type === "bool" ? (
          <select className="input" value={value || ""} onChange={(e) => onChange(e.target.value)}>
            <option value="">{placeholder}</option>
            <option value="yes">开启</option>
            <option value="no">关闭</option>
          </select>
        ) : item.type === "enum" ? (
          <select className="input" value={value || ""} onChange={(e) => onChange(e.target.value)}>
            <option value="">{placeholder}</option>
            {(item.enumValues ?? []).map((v) => (
              <option key={v} value={v}>
                {v || "默认"}
              </option>
            ))}
          </select>
        ) : (
          <input
            className="input"
            value={value}
            placeholder={placeholder}
            onChange={(e) => onChange(e.target.value)}
          />
        )}
        <div className="field__hint">{item.description}</div>
      </div>
    </div>
  );
}

function categoryLabel(cat: string): string {
  switch (cat) {
    case "phone":
      return "手机";
    case "tablet":
      return "平板";
    case "desktop":
      return "桌面";
    case "tv":
      return "电视";
    case "automotive":
      return "车机";
    case "wear":
      return "手表";
    case "xr":
      return "XR";
    default:
      return "其他";
  }
}
