// 设备设置：每台设备自己的配置面板（性能 / 显示 / 启动与快照 / 权限与输入）。
//
// 设计约束：
//   - 可编辑项对应 AVD 自己的 config.ini（root 由后端记录在设备目录里）；模拟器只在
//     启动时读配置，因此界面明确提示「重启后生效」；
//   - 设备运行中只允许通过「保存并重启」写入配置：先停止、保存，再冷启动一次；
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
import type { IconName } from "../components/icons";

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

/**
 * 选项与后端区间保持一致（internal/avd/settings.go）；后端仍会再次校验。
 *
 * 内存以 MB 为准（与 config.ini 的 hw.ramSize 一致），下拉框只把标签显示成 GB：
 * 表单里存 GB 会在非整 GB 的取值（例如 2500 MB）上引入「显示 2.44 GB、保存变 2499 MB」
 * 的舍入漂移，所以这里保留精确的 MB。
 */
const RAM_PRESETS_MB = ["1024", "1536", "2048", "3072", "4096", "6144", "8192", "12288", "16384"];
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

type SettingsSectionID = "performance" | "display" | "startup" | "access";

interface SettingsSection {
  id: SettingsSectionID;
  label: string;
  meta: string;
  icon: IconName;
  title: string;
  description: string;
}

const SETTINGS_SECTIONS: SettingsSection[] = [
  {
    id: "performance",
    label: "性能",
    meta: "内存与计算",
    icon: "cpu",
    title: "性能配置",
    description: "调整模拟器的运行内存、CPU 核心和虚拟机堆上限。",
  },
  {
    id: "display",
    label: "显示",
    meta: "分辨率与密度",
    icon: "display",
    title: "显示配置",
    description: "设置屏幕分辨率和像素密度；保存后按新尺寸重新启动。",
  },
  {
    id: "startup",
    label: "启动与快照",
    meta: "冷启动与快速启动",
    icon: "snowflake",
    title: "启动与快照",
    description: "新设备默认不使用快照；需要恢复上次运行状态时再开启快速启动。",
  },
  {
    id: "access",
    label: "权限与输入",
    meta: "Root 与键盘",
    icon: "settings",
    title: "权限与输入",
    description: "配置系统分区权限和宿主键盘输入，改动在设备重启后生效。",
  },
];

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
  quickBoot: boolean;
  root: boolean;
}

/** 表单里实际生效的内存（MB）；0 表示未设置。 */
function ramMbOf(form: Form): number {
  return form.ram.mode === "auto" ? 0 : toPositiveInt(form.ram.value);
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
function recommendDensity(width: number, height: number): number {
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
    ram: presetChoiceOf(s.ramMb ?? 0),
    cpu: presetChoiceOf(s.cpuCores ?? 0),
    // 有分辨率时按「当前值」展示；用户主动选自定义才会出现宽高输入框
    resolution: width > 0 && height > 0 ? { mode: "preset", value: preset } : AUTO,
    lcdWidth: width > 0 ? String(width) : "",
    lcdHeight: height > 0 ? String(height) : "",
    density: presetChoiceOf(s.lcdDensity ?? 0),
    heap: presetChoiceOf(s.vmHeapMb ?? 0),
    keyboard: !!s.keyboard,
    quickBoot: !!s.quickBoot,
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
  update.quickBoot = form.quickBoot;
  update.root = form.root;
  return update;
}

/** 某一分类的保存签名，用于在左侧导航标出未保存的分类。 */
function sectionSignature(name: string, form: Form, section: SettingsSectionID): string {
  const update = buildUpdate(name, form);
  switch (section) {
    case "performance":
      return [update.ramMb ?? 0, update.cpuCores ?? 0, update.vmHeapMb ?? 0].join("|");
    case "display":
      return [update.lcdWidth ?? 0, update.lcdHeight ?? 0, update.lcdDensity ?? 0].join("|");
    case "startup":
      return update.quickBoot ? "1" : "0";
    case "access":
      return [update.keyboard ? 1 : 0, update.root ? 1 : 0].join("|");
  }
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
  parts.push(settings.quickBoot ? "快速启动" : "不使用快照");
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
  const [activeSection, setActiveSection] = useState<SettingsSectionID>("performance");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [savingAction, setSavingAction] = useState<"save" | "restart" | null>(null);
  const [saveStep, setSaveStep] = useState("");
  const [saveError, setSaveError] = useState("");

  const running = !!instance;
  const saving = savingAction !== null;

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
    setActiveSection("performance");
    void load();
  }, [load]);

  /** 修改表单里的一个字段。 */
  const patch = useCallback((next: Partial<Form>) => {
    setForm((prev) => (prev ? { ...prev, ...next } : prev));
  }, []);

  const dirtySections = useMemo(() => {
    if (!form || !initial) return [] as SettingsSectionID[];
    return SETTINGS_SECTIONS.filter(
      (section) =>
        sectionSignature(device.name, form, section.id) !==
        sectionSignature(device.name, initial, section.id),
    ).map((section) => section.id);
  }, [form, initial, device.name]);

  const dirty = dirtySections.length > 0;

  // 外部停止设备后重新读取：运行状态和可操作性会变化。保存并重启流程自己负责
  // 停止 / 写入 / 启动，不能在这里重载表单；用户先点「先停止」时也保留未保存改动。
  const wasRunning = useMemo(() => !!detail && detail.state !== "stopped", [detail]);
  useEffect(() => {
    if (running || !wasRunning || saving) return;
    if (dirty) {
      setDetail((prev) =>
        prev ? ({ ...prev, state: "stopped", instanceId: undefined } as AvdDetail) : prev,
      );
      return;
    }
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running, saving, dirty]);

  const error = useMemo(() => (form ? validate(form) : ""), [form]);
  const rootSupported = detail?.rootSupported ?? true;
  const { width: formWidth, height: formHeight } = form ? resolutionOf(form) : { width: 0, height: 0 };
  const recommended = recommendDensity(formWidth, formHeight);
  const effectiveDensity = form ? densityOf(form) : 0;
  const activeMeta = SETTINGS_SECTIONS.find((section) => section.id === activeSection) ?? SETTINGS_SECTIONS[0];

  /** 把当前表单写入 config.ini，并同步本地详情与初值。 */
  const persist = async (): Promise<AvdDetail> => {
    if (!form || !detail) throw new Error("设置尚未加载完成");
    const next = (await api.Avd.UpdateSettings(buildUpdate(device.name, form))) as AvdDetail;
    const nextForm = formFromDetail(next);
    setDetail(next);
    setForm(nextForm);
    setInitial(nextForm);
    onSaved?.(next);
    return next;
  };

  const validateBeforeSave = (): boolean => {
    if (!form || !detail) return false;
    if (error) {
      onToast("warning", "设置有误", error);
      return false;
    }
    return true;
  };

  const saveOnly = async () => {
    if (!validateBeforeSave()) return;
    if (running) {
      onToast("warning", "设备正在运行", "请使用「保存并重启」，或先停止设备");
      return;
    }
    setSavingAction("save");
    setSaveStep("正在保存配置");
    setSaveError("");
    try {
      const next = await persist();
      onToast("success", `${device.name} 的设置已保存`, `${describeSettings(next.settings)}（下次启动时生效）`);
    } catch (err) {
      const text = errorText(err);
      setSaveError(text);
      onToast("danger", "保存设置失败", text);
    } finally {
      setSavingAction(null);
      setSaveStep("");
    }
  };

  /** 保存并直接重启：停止运行中的实例 → 写入配置 → 冷启动一次应用配置。 */
  const saveAndRestart = async () => {
    if (!validateBeforeSave()) return;
    const wasRunningNow = running;
    let stopped = false;
    let saved = false;
    setSavingAction("restart");
    setSaveError("");
    try {
      if (running) {
        setSaveStep("正在停止模拟器");
        await api.Emulator.StopByAvd(device.name, false);
        stopped = true;
        setDetail((prev) =>
          prev ? ({ ...prev, state: "stopped", instanceId: undefined } as AvdDetail) : prev,
        );
      }
      setSaveStep("正在写入设备配置");
      const next = await persist();
      saved = true;
      setSaveStep("正在冷启动模拟器");
      // 必须冷启动一次：快照可能保存了旧的硬件状态，直接恢复会让刚保存的配置不生效。
      await api.Emulator.Start({ avdName: device.name, coldBoot: true });
      setSavingAction(null);
      setSaveStep("");
      onToast(
        "success",
        wasRunningNow ? `${device.name} 已保存并重启` : `${device.name} 已保存并启动`,
        "设备正在冷启动，新的设置本次已经生效",
      );
      onClose();
    } catch (err) {
      const text = errorText(err);
      const message = saved
        ? `设置已保存，但设备启动失败：${text}`
        : stopped
          ? `设备已停止，但设置保存失败：${text}`
          : `无法保存并重启：${text}`;
      setSaveError(message);
      onToast("danger", saved ? "启动失败" : "保存并重启失败", message);
      setSavingAction(null);
      setSaveStep("");
    }
  };

  /** 关闭前确认未保存的改动（避免填了一半被 Esc 关掉）。 */
  const requestClose = () => {
    if (saving) return;
    if (dirty && !window.confirm("有未保存的设置改动，确定放弃吗？")) return;
    onClose();
  };

  const canPersist = !!detail && dirty && !error && !saving;

  const footerHint = saving
    ? saveStep
    : running && dirty
      ? "保存会停止设备、写入配置并自动冷启动"
      : dirty
        ? `${dirtySections.length} 个分类有未保存改动，保存后启动设备即可生效`
        : "配置来自设备自己的 config.ini，模拟器启动时读取";

  const renderActiveSection = () => {
    if (!detail || !form) return null;
    switch (activeSection) {
      case "performance":
        return (
          <>
            <div className="devset__group">
              <div className="devset__group-head">
                <div>
                  <div className="devset__group-title">计算资源</div>
                  <div className="devset__group-desc">较大的游戏或长时间下载可适当提高内存和堆上限。</div>
                </div>
                <span className="devset__count">3 项</span>
              </div>
              <div className="devset__grid">
                <ChoiceField
                  label="运行内存"
                  choice={form.ram}
                  onChange={(next) => patch({ ram: next })}
                  options={RAM_PRESETS_MB.map((mb) => ({ value: mb, label: formatRam(Number(mb)) }))}
                  autoLabel={detail.settings.ramMb ? undefined : "跟随设备档案"}
                  placeholder="MB"
                  min={RAM_MIN_MB}
                  max={RAM_MAX_MB}
                  step={256}
                  describe={(value) => formatRam(Number(value))}
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
            </div>
            <div className="devset__note devset__note--info">
              <Icon name="info" size={14} />
              内存与核心数会直接影响宿主资源占用。设备重启后可在 Android 设置中查看新的运行状态。
            </div>
          </>
        );

      case "display":
        return (
          <>
            <div className="devset__group">
              <div className="devset__group-head">
                <div>
                  <div className="devset__group-title">屏幕参数</div>
                  <div className="devset__group-desc">分辨率会同步更新皮肤配置，避免旧皮肤覆盖新尺寸。</div>
                </div>
                <span className="devset__count">2 项</span>
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
                  ) : null}
                  <span className="devset__hint">
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
            </div>
            <div className="devset__note devset__note--info">
              <Icon name="display" size={14} />
              修改显示参数需要重新启动模拟器；保存并重启会自动处理。
            </div>
          </>
        );

      case "startup":
        return (
          <>
            <div className="devset__group">
              <div className="devset__group-head">
                <div>
                  <div className="devset__group-title">启动策略</div>
                  <div className="devset__group-desc">控制是否读取和保存模拟器 Quick Boot 快照。</div>
                </div>
                <span className="devset__count">1 项</span>
              </div>
              <div className="devset__rows devset__rows--single">
                <Switch
                  label="快速启动（快照）"
                  description={
                    form.quickBoot
                      ? "允许模拟器保存并在下次启动恢复快照，启动更快，但可能恢复旧的系统状态。"
                      : "每次冷启动，且不读取、不保存快照。新建设备默认使用此模式。"
                  }
                  checked={form.quickBoot}
                  onChange={(next) => patch({ quickBoot: next })}
                />
              </div>
            </div>
            <div className="devset__strategy">
              <div className="devset__strategy-row">
                <span>当前策略</span>
                <strong>{detail.settings.quickBoot ? "快速启动，使用快照" : "冷启动，不使用快照"}</strong>
              </div>
              <div className="devset__strategy-row">
                <span>本次保存并重启</span>
                <strong>始终冷启动一次</strong>
              </div>
              <div className="devset__strategy-row">
                <span>保存后的默认启动</span>
                <strong>{form.quickBoot ? "恢复快照" : "冷启动，不写快照"}</strong>
              </div>
            </div>
            <div className="devset__note devset__note--info">
              <Icon name="snowflake" size={14} />
              关闭快速启动不会删除已有快照，只是后续启动不再读取或写入；快速启动快照可能包含旧的硬件配置，因此保存设置后的重启会冷启动一次。
            </div>
            {form.quickBoot && !detail.settings.quickBoot ? (
              <div className="devset__note devset__note--warn">
                <Icon name="alert" size={14} />
                从关闭切换为开启后，第一次重启仍会冷启动，以便生成与当前配置一致的新快照。
              </div>
            ) : null}
          </>
        );

      case "access":
        return (
          <>
            <div className="devset__group">
              <div className="devset__group-head">
                <div>
                  <div className="devset__group-title">系统与输入</div>
                  <div className="devset__group-desc">Root 会影响系统分区挂载；键盘开关影响原生窗口输入。</div>
                </div>
                <span className="devset__count">2 项</span>
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
                  description="让设备接受宿主键盘输入（在模拟器窗口中直接打字时建议开启）"
                  checked={form.keyboard}
                  onChange={(next) => patch({ keyboard: next })}
                />
              </div>
            </div>
            {!rootSupported ? (
              <div className="devset__note devset__note--info">
                <Icon name="info" size={14} />
                Google Play 系统镜像是正式版（user）构建，无法取得 root；需要 root 请改用 Google APIs 镜像。
              </div>
            ) : null}
            {form.root && rootSupported ? (
              <div className="devset__note devset__note--warn">
                <Icon name="alert" size={14} />
                开启后需要停止并重新启动设备才会生效；保存并重启会自动使用冷启动。
              </div>
            ) : null}
          </>
        );
    }
  };

  return (
    <Modal
      title={`设备设置 · ${device.name}`}
      size="xl"
      className="devset-modal"
      bodyClassName="devset__body"
      onClose={requestClose}
      footer={
        <div className="modal__foot-bar">
          <div className="devset__foot-hint">
            {saving ? <span className="spinner" /> : <Icon name={running && dirty ? "alert" : "info"} size={14} />}
            {footerHint}
          </div>
          <div className="modal__foot-right devset__actions">
            {dirty ? (
              <button
                className="btn btn--ghost"
                disabled={saving}
                onClick={() => initial && setForm(initial)}
              >
                <Icon name="refresh" size={15} />
                放弃改动
              </button>
            ) : null}
            <button className="btn btn--secondary" disabled={saving} onClick={requestClose}>
              <Icon name="close" size={16} />
              关闭
            </button>
            {!running && dirty ? (
              <button className="btn btn--secondary" disabled={!canPersist} onClick={() => void saveOnly()}>
                {savingAction === "save" ? <span className="spinner" /> : <Icon name="check" size={16} />}
                {savingAction === "save" ? "保存中…" : "仅保存"}
              </button>
            ) : null}
            {dirty ? (
              <button className="btn btn--primary" disabled={!canPersist} onClick={() => void saveAndRestart()}>
                {savingAction === "restart" ? (
                  <span className="spinner" />
                ) : (
                  <Icon name={running ? "refresh" : "play"} size={16} />
                )}
                {savingAction === "restart" ? "处理中…" : running ? "保存并重启" : "保存并启动"}
              </button>
            ) : null}
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
          <aside className="devset__rail">
            <div className="devset__device">
              <div className="devset__device-title">Android {detail.api || "?"}</div>
              <div className="devset__chips">
                {detail.tag ? <span className="chip">{imageTagLabel(detail.tag)}</span> : null}
                {detail.abi ? <span className="chip">{detail.abi}</span> : null}
                <span className={`chip${running ? " chip--success" : ""}`}>{running ? "运行中" : "已停止"}</span>
              </div>
            </div>

            <nav className="devset__nav" aria-label="设备设置分类">
              {SETTINGS_SECTIONS.map((section) => {
                const active = section.id === activeSection;
                const changed = dirtySections.includes(section.id);
                return (
                  <button
                    key={section.id}
                    type="button"
                    className={`devset__nav-item${active ? " is-active" : ""}`}
                    aria-current={active ? "page" : undefined}
                    onClick={() => setActiveSection(section.id)}
                  >
                    <span className="devset__nav-icon">
                      <Icon name={section.icon} size={17} />
                    </span>
                    <span className="devset__nav-text">
                      <span className="devset__nav-label">{section.label}</span>
                      <span className="devset__nav-meta">{section.meta}</span>
                    </span>
                    {changed ? <span className="devset__nav-dot" title="有未保存改动" /> : null}
                  </button>
                );
              })}
            </nav>

            <div className="devset__rail-foot">
              <div className="devset__rail-label">配置文件</div>
              <div className="devset__path nums" title={detail.path}>
                {detail.path}
              </div>
            </div>
          </aside>

          <section className="devset__panel" aria-labelledby="devset-panel-title">
            <header className="devset__panel-head">
              <div className="devset__panel-kicker">
                <Icon name={activeMeta.icon} size={14} />
                设备设置 / {activeMeta.meta}
              </div>
              <div className="devset__panel-title-row">
                <h2 className="devset__panel-title" id="devset-panel-title">
                  {activeMeta.title}
                </h2>
                <span className="devset__restart-badge">
                  <Icon name="refresh" size={12} />
                  重启生效
                </span>
              </div>
              <p className="devset__panel-desc">{activeMeta.description}</p>
            </header>

            <div className="devset__panel-scroll">
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
                      配置只在模拟器启动时读取。可以直接使用底部「保存并重启」，软件会先停止设备再冷启动一次。
                    </div>
                  </div>
                  {onStop ? (
                    <button className="btn btn--secondary" disabled={saving} onClick={onStop}>
                      <Icon name="stop" size={15} />
                      先停止
                    </button>
                  ) : null}
                </div>
              ) : null}

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

              {renderActiveSection()}

              <div className="devset__restart-note">
                <Icon name="clock" size={14} />
                <span>
                  <strong>重启生效：</strong>性能、显示、启动策略和权限输入都在模拟器启动时读取。保存配置不会丢失设备内已安装的应用或数据。
                </span>
              </div>
            </div>
          </section>
        </div>
      ) : null}
    </Modal>
  );
}
