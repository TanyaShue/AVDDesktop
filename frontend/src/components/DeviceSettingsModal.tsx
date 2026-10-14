// 设备设置：每台设备自己的配置面板（运行内存 / CPU 核心 / 虚拟机堆 / 分辨率 /
// 像素密度 / root / 物理键盘）。
//
// 设计约束：
//   - 可编辑项对应 AVD 自己的 config.ini（root 由后端记录在设备目录里）；模拟器只在
//     启动时读配置，因此界面明确提示「重启后生效」；
//   - 设备运行中不允许保存（后端同样拒绝），界面就地提供「停止设备」入口；
//   - 每个数值项都是「跟随档案 / 预设 / 自定义」三态，未改动时保存按钮保持禁用；
//     提交时只带上有变化的项，未动的配置不会被覆盖。
import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type {
  AvdDetail,
  AvdSettings,
  AvdSettingsUpdate,
  AvdSummary,
  EmulatorInstance,
} from "../bridge/types";
import { formatRam, imageTagLabel } from "../lib/imageLabels";
import { Modal, Switch } from "../components/ui";
import { Icon } from "../components/icons";

interface Props {
  device: AvdSummary;
  /** 该设备当前运行的实例（没有则为 undefined）：运行中不允许改配置。 */
  instance?: EmulatorInstance;
  onClose: () => void;
  /** 保存成功后的回调（父级用它刷新列表）。 */
  onSaved?: (detail: AvdDetail) => void;
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
  /** 运行中时提供的「停止设备」动作。 */
  onStop?: () => void;
}

/** 选项与后端区间保持一致（internal/avd/settings.go）；后端仍会再次校验。 */
const RAM_PRESETS_GB = ["1", "1.5", "2", "3", "4", "6", "8", "12", "16"];
const CPU_PRESETS = ["1", "2", "4", "6", "8", "12", "16"];
const RESOLUTION_PRESETS = ["720x1280", "1080x1920", "1080x2340", "1080x2400", "1440x2560", "1440x3120"];
const DENSITY_PRESETS = ["160", "240", "320", "420", "480", "560"];
const HEAP_PRESETS_MB = ["128", "256", "512", "1024", "2048"];

const RAM_MIN_MB = 512;
const RAM_MAX_MB = 16384;
const CPU_MIN = 1;
const CPU_MAX = 16;
const PIXEL_MIN = 240;
const PIXEL_MAX = 7680;
const DENSITY_MIN = 72;
const DENSITY_MAX = 720;
const HEAP_MIN_MB = 16;
const HEAP_MAX_MB = 4096;

/** 自定义项的哨兵值（下拉框里「自定义…」的选项值）。 */
const CUSTOM = "custom";

/**
 * 数值项的三态：auto（跟随设备档案或按分辨率推荐）、preset（命中预设）、custom（自由填写）。
 *
 * 单独记录 mode 是必要的：只靠「值是否命中预设」无法区分「用户主动选了自定义但还没改数字」
 * 与「就是选了这个预设」，会让自定义输入框时隐时现。
 */
interface Choice {
  mode: "auto" | "preset" | "custom";
  value: string;
}

const AUTO: Choice = { mode: "auto", value: "" };

/** 数值 → 三态：0 = auto（跟随设备档案 / 自动），其余按「当前值」选项展示。 */
function presetChoiceOf(value: number): Choice {
  return value ? { mode: "preset", value: String(value) } : AUTO;
}

/**
 * 内存（后端是 MB）→ 三态。
 *
 * 表单里的内存单位是 GB（与创建向导一致），因此必须换算：直接把 1536 当成 GB 会让
 * 输入框显示成「1536 GB」。非预设的取值（例如 2.5 GB）会作为「当前值」选项出现在
 * 下拉框里，只有用户主动选「自定义…」才切到自由输入。
 */
function ramChoiceOf(mb: number): Choice {
  return mb ? { mode: "preset", value: String(mb / 1024) } : AUTO;
}

/** 解析正整数；空串与非法输入返回 0（0 表示该项未设置）。 */
function toPositiveInt(value: string): number {
  const n = Number(value.trim());
  return Number.isInteger(n) && n > 0 ? n : 0;
}

/** 表单状态：数值项用 Choice，字符串项都是受控输入的原始值。 */
interface Form {
  ram: Choice;
  cpu: Choice;
  /** auto 表示跟随设备档案；custom 时使用 lcdWidth / lcdHeight。 */
  resolution: Choice;
  lcdWidth: string;
  lcdHeight: string;
  /** auto 表示「按分辨率推荐」。 */
  density: Choice;
  heap: Choice;
  keyboard: boolean;
  root: boolean;
}

/** 表单里实际生效的内存（MB）；0 表示未设置。 */
function ramMbOf(form: Form): number {
  if (form.ram.mode === "auto") return 0;
  const gb = Number(form.ram.value.trim());
  return Number.isFinite(gb) && gb > 0 ? Math.round(gb * 1024) : 0;
}

/** 表单里实际生效的 CPU 核心；0 表示未设置。 */
function cpuOf(form: Form): number {
  return form.cpu.mode === "auto" ? 0 : toPositiveInt(form.cpu.value);
}

/** 表单里实际生效的堆上限（MB）；0 表示未设置。 */
function heapOf(form: Form): number {
  return form.heap.mode === "auto" ? 0 : toPositiveInt(form.heap.value);
}

/** 表单里实际生效的分辨率；跟随档案时返回 0/0。 */
function resolutionOf(form: Form): { width: number; height: number } {
  if (form.resolution.mode === "auto") return { width: 0, height: 0 };
  if (form.resolution.mode === "custom") {
    return { width: toPositiveInt(form.lcdWidth), height: toPositiveInt(form.lcdHeight) };
  }
  const [width = "", height = ""] = form.resolution.value.split("x");
  return { width: toPositiveInt(width), height: toPositiveInt(height) };
}

/**
 * 按屏幕最短边推荐像素密度。
 *
 * 与 Android 常见的密度分桶一致：480→240（hdpi）、720→320（xhdpi）、
 * 1080→420（Pixel 常见的 420）、1440→560（2K 机型）。
 */
export function recommendDensity(width: number, height: number): number {
  const shortest = Math.min(width, height);
  if (shortest <= 0) return 0;
  if (shortest < 400) return 160;
  if (shortest < 600) return 240;
  if (shortest < 800) return 320;
  if (shortest < 1200) return 420;
  if (shortest < 1600) return 480;
  return 560;
}

/** 表单里实际会写入的像素密度；自动时按分辨率推荐，无法推荐时返回 0（不写入）。 */
function densityOf(form: Form): number {
  if (form.density.mode === "custom") return toPositiveInt(form.density.value);
  if (form.density.mode === "preset") return toPositiveInt(form.density.value);
  const { width, height } = resolutionOf(form);
  return recommendDensity(width, height);
}

/** 本地校验：返回错误文案，空串表示通过。 */
function validate(form: Form): string {
  const ramMb = ramMbOf(form);
  if (form.ram.mode !== "auto" && (!ramMb || ramMb < RAM_MIN_MB || ramMb > RAM_MAX_MB)) {
    return `内存应在 ${RAM_MIN_MB / 1024}–${RAM_MAX_MB / 1024} GB 之间`;
  }
  const cores = cpuOf(form);
  if (form.cpu.mode !== "auto" && (!cores || cores < CPU_MIN || cores > CPU_MAX)) {
    return `CPU 核心应在 ${CPU_MIN}–${CPU_MAX} 核之间`;
  }
  if (form.resolution.mode === "custom") {
    const { width, height } = resolutionOf(form);
    if (!width || !height) return "自定义分辨率需要同时填写宽度和高度";
    if (width < PIXEL_MIN || width > PIXEL_MAX) return `宽度应在 ${PIXEL_MIN}–${PIXEL_MAX} px 之间`;
    if (height < PIXEL_MIN || height > PIXEL_MAX) return `高度应在 ${PIXEL_MIN}–${PIXEL_MAX} px 之间`;
  }
  if (form.density.mode === "custom") {
    const dpi = toPositiveInt(form.density.value);
    if (!dpi) return "请填写自定义像素密度";
    if (dpi < DENSITY_MIN || dpi > DENSITY_MAX) {
      return `像素密度应在 ${DENSITY_MIN}–${DENSITY_MAX} dpi 之间`;
    }
  }
  const heap = heapOf(form);
  if (form.heap.mode !== "auto" && (!heap || heap < HEAP_MIN_MB || heap > HEAP_MAX_MB)) {
    return `虚拟机堆上限应在 ${HEAP_MIN_MB}–${HEAP_MAX_MB} MB 之间`;
  }
  return "";
}

/** 后端详情 → 表单初值。 */
function formFromDetail(detail: AvdDetail): Form {
  const s = detail.settings;
  const width = s.lcdWidth ?? 0;
  const height = s.lcdHeight ?? 0;
  const preset = `${width}x${height}`;
  return {
    ram: ramChoiceOf(s.ramMb ?? 0),
    cpu: presetChoiceOf(s.cpuCores ?? 0),
    // 有分辨率时按「当前值」展示；用户主动选自定义才会出现宽高输入框
    resolution: width > 0 && height > 0 ? { mode: "preset", value: preset } : AUTO,
    lcdWidth: width > 0 ? String(width) : "",
    lcdHeight: height > 0 ? String(height) : "",
    density: presetChoiceOf(s.lcdDensity ?? 0),
    heap: presetChoiceOf(s.vmHeapMb ?? 0),
    keyboard: !!s.keyboard,
    root: !!s.root,
  };
}

/** 表单 → 保存入参（0 表示不修改该项）。 */
function buildUpdate(name: string, form: Form): AvdSettingsUpdate {
  const update: AvdSettingsUpdate = { name };
  const ramMb = ramMbOf(form);
  if (ramMb > 0) update.ramMb = ramMb;
  const cores = cpuOf(form);
  if (cores > 0) update.cpuCores = cores;
  const { width, height } = resolutionOf(form);
  if (width > 0 && height > 0) {
    update.lcdWidth = width;
    update.lcdHeight = height;
  }
  const dpi = densityOf(form);
  if (dpi > 0) update.lcdDensity = dpi;
  const heap = heapOf(form);
  if (heap > 0) update.vmHeapMb = heap;
  update.keyboard = form.keyboard;
  update.root = form.root;
  return update;
}

/** 只比较会影响保存结果的字段（用于判断是否有改动）。 */
function updateSignature(update: AvdSettingsUpdate): string {
  return [
    update.ramMb ?? 0,
    update.cpuCores ?? 0,
    update.lcdWidth ?? 0,
    update.lcdHeight ?? 0,
    update.lcdDensity ?? 0,
    update.vmHeapMb ?? 0,
    update.keyboard ? 1 : 0,
    update.root ? 1 : 0,
  ].join("|");
}

/** 设置的可读摘要（保存成功提示用）。 */
function describeSettings(settings: AvdSettings): string {
  const parts: string[] = [];
  if (settings.ramMb) parts.push(`内存 ${formatRam(settings.ramMb)}`);
  if (settings.cpuCores) parts.push(`${settings.cpuCores} 核`);
  if (settings.lcdWidth && settings.lcdHeight) {
    parts.push(
      `${settings.lcdWidth}×${settings.lcdHeight}${settings.lcdDensity ? ` @ ${settings.lcdDensity} dpi` : ""}`,
    );
  }
  if (settings.vmHeapMb) parts.push(`堆 ${settings.vmHeapMb} MB`);
  if (settings.keyboard) parts.push("物理键盘");
  if (settings.root) parts.push("root");
  return parts.join(" · ");
}

/** 三态数值项：下拉框（跟随/预设/自定义）+ 自定义时的输入框。 */
function ChoiceField({
  label,
  choice,
  onChange,
  options,
  autoLabel,
  placeholder,
  hint,
  min,
  max,
  step,
  inputMode = "numeric",
  describe,
}: {
  label: string;
  choice: Choice;
  onChange: (next: Choice) => void;
  /** 预设选项：value 同时用于命中判断。 */
  options: { value: string; label: string }[];
  /**
   * 非空时在下拉框顶部提供「跟随设备档案」选项。
   *
   * 只在设备当前没有该取值时传入：配置项一旦有值，就没有「清空」这个语义
   * （后端只写显式给出的键），给出该选项会让用户以为能撤销。
   */
  autoLabel?: string;
  placeholder: string;
  hint: ReactNode;
  min?: number;
  max?: number;
  step?: number;
  inputMode?: "numeric" | "decimal";
  /** 当前值不在预设里时，额外选项的显示文案（例如「2.5 GB」）。 */
  describe?: (value: string) => string;
}) {
  const presetValues = options.map((o) => o.value);
  const extra =
    choice.mode === "preset" && !presetValues.includes(choice.value)
      ? { value: choice.value, label: describe ? describe(choice.value) : choice.value }
      : null;
  return (
    <label className="devset__item">
      <span className="devset__label">{label}</span>
      <select
        className="select"
        value={choice.mode === "custom" ? CUSTOM : choice.value}
        onChange={(e) => {
          const selected = e.target.value;
          if (selected === CUSTOM) onChange({ mode: "custom", value: choice.value });
          else if (selected === "") onChange(AUTO);
          else onChange({ mode: "preset", value: selected });
        }}
      >
        {autoLabel ? <option value="">{autoLabel}</option> : null}
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
        {extra ? <option value={extra.value}>{extra.label}</option> : null}
        <option value={CUSTOM}>自定义…</option>
      </select>
      {choice.mode === "custom" ? (
        <input
          className="input"
          type="number"
          inputMode={inputMode}
          min={min}
          max={max}
          step={step}
          placeholder={placeholder}
          value={choice.value}
          onChange={(e) => onChange({ mode: "custom", value: e.target.value })}
        />
      ) : null}
      <span className="devset__hint">{hint}</span>
    </label>
  );
}

export function DeviceSettingsModal({ device, instance, onClose, onSaved, onToast, onStop }: Props) {
  const [detail, setDetail] = useState<AvdDetail | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [initial, setInitial] = useState<Form | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  const running = !!instance;

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError("");
    try {
      const next = (await api.Avd.Detail(device.name)) as AvdDetail;
      const nextForm = formFromDetail(next);
      setDetail(next);
      setForm(nextForm);
      setInitial(nextForm);
    } catch (err) {
      setLoadError(errorText(err));
    } finally {
      setLoading(false);
    }
  }, [device.name]);

  useEffect(() => {
    void load();
  }, [load]);

  // 设备停止后重新读取：可编辑性与状态文案都跟着变（运行中不允许保存）。
  const wasRunning = useMemo(() => !!detail && detail.state !== "stopped", [detail]);
  useEffect(() => {
    if (!running && wasRunning) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running]);

  /** 修改表单里的一个字段。 */
  const patch = useCallback((next: Partial<Form>) => {
    setForm((prev) => (prev ? { ...prev, ...next } : prev));
  }, []);

  const dirty = useMemo(() => {
    if (!form || !initial) return false;
    return updateSignature(buildUpdate(device.name, form)) !== updateSignature(buildUpdate(device.name, initial));
  }, [form, initial, device.name]);

  const error = useMemo(() => (form ? validate(form) : ""), [form]);
  const rootSupported = detail?.rootSupported ?? true;
  const { width: formWidth, height: formHeight } = form ? resolutionOf(form) : { width: 0, height: 0 };
  const recommended = recommendDensity(formWidth, formHeight);
  const effectiveDensity = form ? densityOf(form) : 0;

  const save = async () => {
    if (!form || !detail) return;
    if (error) {
      onToast("warning", "设置有误", error);
      return;
    }
    if (running) {
      onToast("warning", "设备正在运行", "请先停止设备：配置只在启动时读取");
      return;
    }
    setSaving(true);
    setSaveError("");
    try {
      const next = (await api.Avd.UpdateSettings(buildUpdate(device.name, form))) as AvdDetail;
      const nextForm = formFromDetail(next);
      setDetail(next);
      setForm(nextForm);
      setInitial(nextForm);
      onSaved?.(next);
      onToast("success", `${device.name} 的设置已保存`, `${describeSettings(next.settings)}（重启设备后生效）`);
    } catch (err) {
      const text = errorText(err);
      setSaveError(text);
      onToast("danger", "保存设置失败", text);
    } finally {
      setSaving(false);
    }
  };

  /** 关闭前确认未保存的改动（避免填了一半被 Esc 关掉）。 */
  const requestClose = () => {
    if (dirty && !window.confirm("有未保存的设置改动，确定放弃吗？")) return;
    onClose();
  };

  const canSave = !!detail && dirty && !error && !running && !saving;

  return (
    <Modal
      title={`设备设置 · ${device.name}`}
      size="lg"
      className="devset-modal"
      onClose={requestClose}
      footer={
        <div className="modal__foot-bar">
          <div className="devset__foot-hint">
            {running ? (
              <>
                <Icon name="info" size={14} />
                设备运行中：请先停止，配置只在启动时读取
              </>
            ) : dirty ? (
              <>
                <Icon name="alert" size={14} />
                有未保存的改动，保存后在下次启动时生效
              </>
            ) : (
              <>配置来自设备自己的 config.ini，重启设备后生效</>
            )}
          </div>
          <div className="modal__foot-right">
            {dirty ? (
              <button className="btn btn--ghost" disabled={saving} onClick={() => initial && setForm(initial)}>
                <Icon name="refresh" size={15} />
                放弃改动
              </button>
            ) : null}
            <button className="btn btn--secondary" onClick={requestClose}>
              <Icon name="close" size={16} />
              关闭
            </button>
            <button className="btn btn--primary" disabled={!canSave} onClick={() => void save()}>
              {saving ? <span className="spinner" /> : <Icon name="check" size={17} />}
              {saving ? "保存中…" : "保存设置"}
            </button>
          </div>
        </div>
      }
    >
      {loading ? (
        <div className="devset__state">
          <span className="spinner" />
          正在读取设备配置…
        </div>
      ) : loadError ? (
        <div className="devset__state devset__state--error">
          <Icon name="alert" size={18} />
          <div>
            <div>读取设备配置失败</div>
            <div className="muted break-all">{loadError}</div>
          </div>
          <button className="btn btn--secondary" onClick={() => void load()}>
            <Icon name="refresh" size={15} />
            重试
          </button>
        </div>
      ) : detail && form ? (
        <div className="devset">
          <div className="devset__head">
            <span className="chip chip--info">Android {detail.api || "?"}</span>
            {detail.tag ? <span className="chip">{imageTagLabel(detail.tag)}</span> : null}
            {detail.abi ? <span className="chip">{detail.abi}</span> : null}
            {detail.settings.ramMb ? <span className="chip">{formatRam(detail.settings.ramMb)}</span> : null}
            {detail.settings.root ? <span className="chip chip--warning">Root</span> : null}
            <span className={`chip${running ? " chip--success" : ""}`}>{running ? "运行中" : "已停止"}</span>
          </div>
          <div className="devset__path nums" title={detail.path}>
            {detail.systemImagePath || detail.path}
          </div>

          {detail.broken ? (
            <div className="banner banner--danger devset__banner">
              <div className="banner__icon">
                <Icon name="alert" size={18} />
              </div>
              <div className="banner__body">
                <div className="banner__title">设备配置有问题</div>
                <div className="banner__text break-all">{detail.broken}</div>
              </div>
            </div>
          ) : null}

          {running ? (
            <div className="banner banner--info devset__banner">
              <div className="banner__icon">
                <Icon name="info" size={18} />
              </div>
              <div className="banner__body">
                <div className="banner__title">设备正在运行</div>
                <div className="banner__text">
                  内存、分辨率等参数只在模拟器启动时读取。停止设备后即可修改，改动在下次启动时生效。
                </div>
              </div>
              {onStop ? (
                <button className="btn btn--secondary" onClick={onStop}>
                  <Icon name="stop" size={15} />
                  停止设备
                </button>
              ) : null}
            </div>
          ) : null}

          <section className="devset__section">
            <div className="devset__section-title">
              <Icon name="memory" size={15} />
              性能
            </div>
            <div className="devset__grid">
              <ChoiceField
                label="运行内存"
                choice={form.ram}
                onChange={(next) => patch({ ram: next })}
                options={RAM_PRESETS_GB.map((gb) => ({ value: gb, label: `${gb} GB` }))}
                autoLabel={detail.settings.ramMb ? undefined : "跟随设备档案"}
                placeholder="GB"
                min={RAM_MIN_MB / 1024}
                max={RAM_MAX_MB / 1024}
                step={0.5}
                inputMode="decimal"
                describe={(value) => `${value} GB`}
                hint={
                  <>
                    当前 {detail.settings.ramMb ? formatRam(detail.settings.ramMb) : "跟随设备档案"}
                    {ramMbOf(form) > 0 && ramMbOf(form) !== (detail.settings.ramMb ?? 0)
                      ? `　→ ${formatRam(ramMbOf(form))}`
                      : ""}
                  </>
                }
              />
              <ChoiceField
                label="CPU 核心"
                choice={form.cpu}
                onChange={(next) => patch({ cpu: next })}
                options={CPU_PRESETS.map((n) => ({ value: n, label: `${n} 核` }))}
                autoLabel={detail.settings.cpuCores ? undefined : "跟随设备档案"}
                placeholder="核"
                min={CPU_MIN}
                max={CPU_MAX}
                describe={(value) => `${value} 核`}
                hint={
                  <>
                    当前 {detail.settings.cpuCores ? `${detail.settings.cpuCores} 核` : "跟随设备档案"}
                    {cpuOf(form) > 0 && cpuOf(form) !== (detail.settings.cpuCores ?? 0)
                      ? `　→ ${cpuOf(form)} 核`
                      : ""}
                  </>
                }
              />
              <ChoiceField
                label="虚拟机堆上限"
                choice={form.heap}
                onChange={(next) => patch({ heap: next })}
                options={HEAP_PRESETS_MB.map((mb) => ({ value: mb, label: `${mb} MB` }))}
                autoLabel={detail.settings.vmHeapMb ? undefined : "跟随设备档案"}
                placeholder="MB"
                min={HEAP_MIN_MB}
                max={HEAP_MAX_MB}
                describe={(value) => `${value} MB`}
                hint={
                  <>
                    单个应用可用的 Java 堆上限，大型应用可调高
                    {heapOf(form) > 0 && heapOf(form) !== (detail.settings.vmHeapMb ?? 0)
                      ? `　→ ${heapOf(form)} MB`
                      : ""}
                  </>
                }
              />
            </div>
          </section>

          <section className="devset__section">
            <div className="devset__section-title">
              <Icon name="display" size={15} />
              显示
            </div>
            <div className="devset__grid">
              <label className="devset__item">
                <span className="devset__label">分辨率</span>
                <select
                  className="select"
                  value={form.resolution.mode === "custom" ? CUSTOM : form.resolution.value}
                  onChange={(e) => {
                    const selected = e.target.value;
                    if (selected === CUSTOM) {
                      // 进入自定义时带上当前值（或一份常见默认值），避免空白输入框
                      const width = form.lcdWidth || String(detail.settings.lcdWidth || 1080);
                      const height = form.lcdHeight || String(detail.settings.lcdHeight || 2400);
                      patch({
                        resolution: { mode: "custom", value: `${width}x${height}` },
                        lcdWidth: width,
                        lcdHeight: height,
                      });
                    } else if (selected === "") {
                      patch({ resolution: AUTO });
                    } else {
                      patch({ resolution: { mode: "preset", value: selected } });
                    }
                  }}
                >
                  {/* 「跟随设备档案」只在当前没有分辨率时出现：已有取值时后端只写显式键，无法清空 */}
                  {detail.settings.lcdWidth && detail.settings.lcdHeight ? null : (
                    <option value="">跟随设备档案</option>
                  )}
                  {RESOLUTION_PRESETS.map((preset) => (
                    <option key={preset} value={preset}>
                      {preset.split("x").join(" × ")}
                    </option>
                  ))}
                  {form.resolution.mode === "preset" && !RESOLUTION_PRESETS.includes(form.resolution.value) ? (
                    <option value={form.resolution.value}>{form.resolution.value.split("x").join(" × ")}</option>
                  ) : null}
                  <option value={CUSTOM}>自定义…</option>
                </select>
                {form.resolution.mode === "custom" ? (
                  <div className="devset__custom">
                    <input
                      className="input"
                      type="number"
                      inputMode="numeric"
                      min={PIXEL_MIN}
                      max={PIXEL_MAX}
                      placeholder="宽"
                      aria-label="分辨率宽度"
                      value={form.lcdWidth}
                      onChange={(e) => patch({ lcdWidth: e.target.value })}
                    />
                    <span className="devset__times">×</span>
                    <input
                      className="input"
                      type="number"
                      inputMode="numeric"
                      min={PIXEL_MIN}
                      max={PIXEL_MAX}
                      placeholder="高"
                      aria-label="分辨率高度"
                      value={form.lcdHeight}
                      onChange={(e) => patch({ lcdHeight: e.target.value })}
                    />
                    <span className="devset__unit">px</span>
                  </div>
                ) : null}
                <span className="devset__hint">
                  当前{" "}
                  {detail.settings.lcdWidth && detail.settings.lcdHeight
                    ? `${detail.settings.lcdWidth} × ${detail.settings.lcdHeight}`
                    : "跟随设备档案"}
                  {detail.skinPath && detail.skinPath !== "_no_skin"
                    ? "（配置里指定了皮肤，保存后按新分辨率显示）"
                    : ""}
                </span>
              </label>

              <label className="devset__item">
                <span className="devset__label">像素密度</span>
                <select
                  className="select"
                  value={form.density.mode === "custom" ? CUSTOM : form.density.value}
                  onChange={(e) => {
                    const selected = e.target.value;
                    if (selected === CUSTOM) patch({ density: { mode: "custom", value: form.density.value } });
                    else if (selected === "") patch({ density: AUTO });
                    else patch({ density: { mode: "preset", value: selected } });
                  }}
                >
                  <option value="">自动（按分辨率）</option>
                  {DENSITY_PRESETS.map((dpi) => (
                    <option key={dpi} value={dpi}>
                      {dpi} dpi
                    </option>
                  ))}
                  {form.density.mode === "preset" && !DENSITY_PRESETS.includes(form.density.value) ? (
                    <option value={form.density.value}>{form.density.value} dpi</option>
                  ) : null}
                  <option value={CUSTOM}>自定义…</option>
                </select>
                {form.density.mode === "custom" ? (
                  <input
                    className="input"
                    type="number"
                    inputMode="numeric"
                    min={DENSITY_MIN}
                    max={DENSITY_MAX}
                    placeholder="dpi"
                    aria-label="像素密度"
                    value={form.density.value}
                    onChange={(e) => patch({ density: { mode: "custom", value: e.target.value } })}
                  />
                ) : null}                <span className="devset__hint">
                  当前 {detail.settings.lcdDensity ? `${detail.settings.lcdDensity} dpi` : "跟随设备档案"}
                  {form.density.mode === "auto" && recommended > 0 ? (
                    <>　→ 保存为推荐 {recommended} dpi</>
                  ) : recommended > 0 && effectiveDensity !== recommended ? (
                    <button
                      type="button"
                      className="devset__inline-btn"
                      onClick={() =>
                        patch({
                          density: DENSITY_PRESETS.includes(String(recommended))
                            ? { mode: "preset", value: String(recommended) }
                            : { mode: "custom", value: String(recommended) },
                        })
                      }
                    >
                      改为推荐 {recommended} dpi
                    </button>
                  ) : null}
                </span>
              </label>
            </div>
          </section>

          <section className="devset__section">
            <div className="devset__section-title">
              <Icon name="settings" size={15} />
              权限与输入
            </div>
            <div className="devset__rows">
              <Switch
                label="Root 权限"
                description={
                  rootSupported
                    ? "启动时附加 -writable-system，并在开机后自动 adb root / remount，可写系统分区"
                    : detail.rootReason || "该镜像不支持 root"
                }
                checked={form.root}
                disabled={!rootSupported && !form.root}
                onChange={(next) => patch({ root: next })}
              />
              <Switch
                label="物理键盘输入"
                description="让设备接受宿主键盘输入（在设备窗口里直接打字时建议开启）"
                checked={form.keyboard}
                onChange={(next) => patch({ keyboard: next })}
              />
            </div>
            {!rootSupported ? (
              <div className="devset__note">
                <Icon name="info" size={14} />
                Google Play 系统镜像是正式版（user）构建，无法取得 root；需要 root 请改用 Google APIs 镜像。
              </div>
            ) : null}
            {form.root && rootSupported ? (
              <div className="devset__note devset__note--warn">
                <Icon name="alert" size={14} />
                开启后需要停止并重新启动设备才会生效（建议冷启动，避免沿用旧的系统分区快照）。
              </div>
            ) : null}
          </section>

          {saveError ? (
            <div className="hw__error">
              <Icon name="cross" size={14} /> {saveError}
            </div>
          ) : null}
          {error ? (
            <div className="hw__error">
              <Icon name="alert" size={14} /> {error}
            </div>
          ) : null}
        </div>
      ) : null}
    </Modal>
  );
}
