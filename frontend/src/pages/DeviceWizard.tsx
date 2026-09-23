// 创建设备：设备名称 / System Image / 设备档案，外加可选的自定义硬件参数。
//
// 镜像不存在时由后端在同一任务里通过官方 sdkmanager 自动安装。
// 硬件参数默认完全沿用设备档案；用户填写的项会在 avdmanager 创建完成后写进设备 config.ini
// （后端校验与写入，见 internal/avd/hardware.go）。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { AvdHardware, AvdSpec, DeviceProfile, SystemImage } from "../bridge/types";
import {
  androidVersionLabel,
  imageRootSupported,
  imageTagLabel,
  SystemImageModal,
} from "../components/SystemImageModal";
import { Modal } from "../components/ui";

interface Props {
  onClose: () => void;
  onCreated: () => void;
  onToast: (level: "info" | "success" | "warning" | "danger", title: string, text?: string) => void;
}

const DEFAULT_NAME = "MyDevice";
const DEFAULT_PROFILE = "medium_phone";

/** 可自定义的数值型硬件参数：表单键名与后端 domain.AvdHardware 的字段一一对应。 */
type HardwareNumberKey =
  | "ramMb"
  | "heapMb"
  | "cpuCores"
  | "lcdDensity"
  | "dataPartitionMb"
  | "sdcardMb";

/** 提交给后端的覆盖项：只有用户填写的项才会出现（未出现的字段沿用设备档案默认值）。 */
type HardwareDraft = Partial<Record<HardwareNumberKey | "lcdWidth" | "lcdHeight", number>>;

/** 表单状态：数字统一用字符串保存，空串表示沿用设备档案默认值。 */
type HardwareForm = Record<HardwareNumberKey, string> & {
  /** "" 表示跟随设备档案；"custom" 表示使用下面的宽高输入；其它为 "宽x高" 预设。 */
  resolution: string;
  lcdWidth: string;
  lcdHeight: string;
};

interface HardwareInputSpec {
  key: HardwareNumberKey;
  label: string;
  unit: string;
  min: number;
  max: number;
  step: number;
  /** 常见取值，仅作为输入建议（datalist），不限制自定义。 */
  presets: number[];
}

// 区间与 internal/avd/hardware.go 的 hardwareFields 保持一致；后端仍会再次校验。
const HARDWARE_INPUTS: HardwareInputSpec[] = [
  { key: "ramMb", label: "内存", unit: "MB", min: 512, max: 16384, step: 256, presets: [1024, 2048, 3072, 4096, 6144, 8192] },
  { key: "heapMb", label: "VM 堆", unit: "MB", min: 16, max: 2048, step: 32, presets: [128, 256, 512, 1024] },
  { key: "cpuCores", label: "CPU 核心", unit: "核", min: 1, max: 16, step: 1, presets: [2, 4, 6, 8] },
  { key: "lcdDensity", label: "屏幕密度", unit: "dpi", min: 72, max: 960, step: 10, presets: [240, 320, 420, 480, 560] },
  { key: "dataPartitionMb", label: "数据分区", unit: "MB", min: 512, max: 65536, step: 512, presets: [2048, 4096, 6144, 8192, 16384] },
  { key: "sdcardMb", label: "SD 卡", unit: "MB", min: 64, max: 65536, step: 256, presets: [512, 1024, 4096, 8192] },
];

const RESOLUTION_PRESETS = ["720x1280", "1080x1920", "1080x2400", "1440x2560", "1440x3120"];
const PIXEL_MIN = 240;
const PIXEL_MAX = 7680;

const EMPTY_HARDWARE_FORM: HardwareForm = {
  ramMb: "",
  heapMb: "",
  cpuCores: "",
  lcdDensity: "",
  dataPartitionMb: "",
  sdcardMb: "",
  resolution: "",
  lcdWidth: "",
  lcdHeight: "",
};

/** 解析正整数；空串与非法输入返回 0（0 表示该覆盖项未填写）。 */
function toPositiveInt(value: string): number {
  const n = Number(value.trim());
  return Number.isInteger(n) && n > 0 ? n : 0;
}

/** 解析表单当前选择的分辨率；未选择或填写不完整时返回 0。 */
function resolutionOf(form: HardwareForm): { width: number; height: number } {
  if (form.resolution === "custom") {
    return { width: toPositiveInt(form.lcdWidth), height: toPositiveInt(form.lcdHeight) };
  }
  const [width = "", height = ""] = form.resolution.split("x");
  return { width: toPositiveInt(width), height: toPositiveInt(height) };
}

/** 已填写的覆盖项数量（用于摘要文案与「恢复默认」按钮的显隐）。 */
function hardwareCount(form: HardwareForm): number {
  const numbers = HARDWARE_INPUTS.filter((spec) => toPositiveInt(form[spec.key]) > 0).length;
  const { width, height } = resolutionOf(form);
  return numbers + (width > 0 && height > 0 ? 1 : 0);
}

/** 本地校验：返回错误文案，空串表示通过。 */
function validateHardwareForm(form: HardwareForm): string {
  for (const spec of HARDWARE_INPUTS) {
    const raw = form[spec.key].trim();
    if (!raw) continue;
    const n = Number(raw);
    if (!Number.isInteger(n)) return `${spec.label}必须是整数（单位 ${spec.unit}）`;
    if (n < spec.min || n > spec.max) {
      return `${spec.label}应在 ${spec.min}–${spec.max} ${spec.unit} 之间，或留空沿用设备档案默认值`;
    }
  }
  if (form.resolution === "custom") {
    const { width, height } = resolutionOf(form);
    if (!width || !height) return "自定义分辨率需要同时填写宽度和高度";
    if (width < PIXEL_MIN || width > PIXEL_MAX) return `宽度应在 ${PIXEL_MIN}–${PIXEL_MAX} px 之间`;
    if (height < PIXEL_MIN || height > PIXEL_MAX) return `高度应在 ${PIXEL_MIN}–${PIXEL_MAX} px 之间`;
  }
  return "";
}

/** 组装后端需要的覆盖项；什么都没填时返回 undefined（完全沿用设备档案）。 */
function buildHardware(form: HardwareForm): HardwareDraft | undefined {
  const draft: HardwareDraft = {};
  for (const spec of HARDWARE_INPUTS) {
    const n = toPositiveInt(form[spec.key]);
    if (n > 0) draft[spec.key] = n;
  }
  const { width, height } = resolutionOf(form);
  if (width > 0 && height > 0) {
    draft.lcdWidth = width;
    draft.lcdHeight = height;
  }
  return Object.keys(draft).length > 0 ? draft : undefined;
}

export function DeviceWizard({ onClose, onCreated, onToast }: Props) {
  const [name, setName] = useState(DEFAULT_NAME);
  const [nameReady, setNameReady] = useState(false);
  const nameTouchedRef = useRef(false);
  const [validation, setValidation] = useState<{ valid: boolean; reason?: string; suggest?: string }>({
    valid: true,
  });
  const [installedImages, setInstalledImages] = useState<SystemImage[]>([]);
  const [allImages, setAllImages] = useState<SystemImage[]>([]);
  const [imagesError, setImagesError] = useState("");
  const [imagePath, setImagePath] = useState("");
  const [loadingInstalled, setLoadingInstalled] = useState(true);
  const [loadingAll, setLoadingAll] = useState(false);
  const [showAllImages, setShowAllImages] = useState(false);
  const [profiles, setProfiles] = useState<DeviceProfile[]>([]);
  const [profileId, setProfileId] = useState(DEFAULT_PROFILE);
  const [hardwareForm, setHardwareForm] = useState<HardwareForm>(EMPTY_HARDWARE_FORM);
  const [submitting, setSubmitting] = useState(false);

  const hardwareError = useMemo(() => validateHardwareForm(hardwareForm), [hardwareForm]);
  const hardwareFilled = useMemo(() => hardwareCount(hardwareForm), [hardwareForm]);

  /** 更新自定义硬件表单里的一个字段（空串表示沿用设备档案）。 */
  const setHardwareField = useCallback((key: keyof HardwareForm, value: string) => {
    setHardwareForm((prev) => ({ ...prev, [key]: value }));
  }, []);

  const loadInstalledImages = useCallback(async () => {
    setLoadingInstalled(true);
    setImagesError("");
    try {
      const list = api.asArray((await api.Avd.ListImages(true)) as SystemImage[]);
      setInstalledImages(list);
      setImagesError("");
      setImagePath((current) => current || list[0]?.path || "");
    } catch (err) {
      const text = errorText(err);
      setImagesError(text);
      onToast("danger", "读取已安装镜像失败", text);
    } finally {
      setLoadingInstalled(false);
    }
  }, [onToast]);

  const loadAllImages = useCallback(async () => {
    setLoadingAll(true);
    setImagesError("");
    try {
      const list = api.asArray((await api.Avd.ListImages(false)) as SystemImage[]);
      setAllImages(list);
      setImagesError("");
    } catch (err) {
      const text = errorText(err);
      setImagesError(text);
      onToast("danger", "读取可用镜像失败", text);
    } finally {
      setLoadingAll(false);
    }
  }, [onToast]);

  // 打开时先读本地已安装镜像（不联网），设备档案来自官方 avdmanager。
  useEffect(() => {
    void loadInstalledImages();
    void (async () => {
      try {
        const list = api.asArray((await api.Avd.ListProfiles(false)) as DeviceProfile[]);
        setProfiles(list);
        if (list.length > 0 && !list.some((p) => p.id === DEFAULT_PROFILE)) {
          setProfileId(list[0].id);
        }
      } catch (err) {
        onToast("warning", "读取设备档案失败", errorText(err));
      }
    })();
  }, [loadInstalledImages, onToast]);

  // 默认名被占用时直接替换为后端建议的 MyDevice_2，而不是把建议显示在输入框下方。
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      let nextName = DEFAULT_NAME;
      let initialValidation: typeof validation = { valid: true };
      try {
        const res = (await api.Avd.ValidateName(DEFAULT_NAME)) as {
          valid: boolean;
          reason?: string;
          suggest?: string;
        };
        if (!res.valid && res.suggest) {
          nextName = res.suggest;
        } else {
          initialValidation = res;
        }
      } catch {
        /* 后端未就绪时保留默认名，后续实时校验会再次检查 */
      }
      if (!cancelled && !nameTouchedRef.current) {
        setName(nextName);
        setValidation(initialValidation);
      }
      if (!cancelled) setNameReady(true);
    })();
    return () => {
      cancelled = true;
    };
    // 只在弹窗首次打开时解析默认名称。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 名称实时校验：输入框下方只展示错误原因，不再展示「建议：xxx」。
  useEffect(() => {
    if (!nameReady) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      void (async () => {
        try {
          const res = (await api.Avd.ValidateName(name)) as {
            valid: boolean;
            reason?: string;
            suggest?: string;
          };
          if (!cancelled) setValidation(res);
        } catch {
          /* 后端未就绪时忽略 */
        }
      })();
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [name, nameReady]);

  const selected = useMemo(
    () => allImages.find((item) => item.path === imagePath) ?? installedImages.find((item) => item.path === imagePath),
    [allImages, imagePath, installedImages],
  );
  const selectedInInstalled = installedImages.some((item) => item.path === imagePath);

  const submit = async () => {
    if (hardwareError) {
      onToast("warning", "自定义硬件参数不合法", hardwareError);
      return;
    }
    setSubmitting(true);
    try {
      const draft = buildHardware(hardwareForm);
      const spec = {
        name: name.trim(),
        systemImagePath: imagePath,
        profileId,
        // 未填写的覆盖项不会出现，后端按设备档案默认值处理
        hardware: draft as AvdHardware | undefined,
      } as AvdSpec;
      const id = await api.Avd.Create(spec);
      onToast(
        "success",
        "已开始创建设备",
        draft
          ? `任务 ${id}，自定义硬件参数将在 avdmanager 创建完成后写入设备配置`
          : `任务 ${id}，进度显示在底部任务区域`,
      );
      onCreated();
      onClose();
    } catch (err) {
      onToast("danger", "创建失败", errorText(err));
    } finally {
      setSubmitting(false);
    }
  };

  const canSubmit = nameReady && validation.valid && !!imagePath && !submitting && !hardwareError;

  const openAllImages = () => {
    setShowAllImages(true);
    if (allImages.length === 0 && !loadingAll) void loadAllImages();
  };

  return (
    <>
      <Modal
        title="新建设备"
        size="md"
        onClose={onClose}
        footer={
          <div className="modal__foot-right">
            <button className="btn btn--secondary" onClick={onClose}>
              取消
            </button>
            <button className="btn btn--primary" disabled={!canSubmit} onClick={() => void submit()}>
              {submitting ? "创建中…" : "创建"}
            </button>
          </div>
        }
      >
        <div className="field">
          <div className="field__label">设备名称</div>
          <div className="field__control">
            <input
              className={`input${validation.valid ? "" : " input--error"}`}
              value={name}
              onChange={(e) => {
                nameTouchedRef.current = true;
                setName(e.target.value);
              }}
              placeholder="仅字母、数字、下划线、点、连字符"
              autoFocus
            />
            <div className={`field__hint${validation.valid ? "" : " field__hint--error"}`}>
              {validation.valid
                ? "用于设备目录名，创建后不建议修改"
                : validation.reason ?? "名称不合法"}
            </div>
          </div>
        </div>

        <div className="field">
          <div className="field__label">System Image</div>
          <div className="field__control">
            <select
              className="select"
              value={imagePath}
              onChange={(e) => setImagePath(e.target.value)}
              disabled={(loadingInstalled && !selected) || (!installedImages.length && !selected)}
            >
              {installedImages.length === 0 && !selected ? (
                <option value="">{loadingInstalled ? "读取中…" : "没有已安装的系统镜像"}</option>
              ) : null}
              {installedImages.map((img) => (
                <option key={img.path} value={img.path}>
                  {`${androidVersionLabel(img)} · ${imageTagLabel(img.tag)} · ${img.abi}`}
                </option>
              ))}
              {selected && !selectedInInstalled ? (
                <option value={selected.path}>
                  {`${androidVersionLabel(selected)} · ${imageTagLabel(selected.tag)} · ${selected.abi}（未安装）`}
                </option>
              ) : null}
            </select>

            {selected ? (
              <div className="selected-image-tags">
                <span className="chip chip--info">{androidVersionLabel(selected)}（API {selected.api}）</span>
                <span className="chip">{selected.abi}</span>
                <span className={`chip ${imageRootSupported(selected) ? "chip--success" : "chip--warning"}`}>
                  {imageRootSupported(selected) ? "支持 Root" : "不支持 Root"}
                </span>
                <span className={`chip ${selected.installed ? "chip--success" : "chip--warning"}`}>
                  {selected.installed ? "已安装" : "创建时自动下载"}
                </span>
              </div>
            ) : null}

            <div className={`field__hint${imagesError ? " field__hint--error" : ""}`}>
              {imagesError
                ? `读取镜像失败：${imagesError}（可重试「加载全部镜像」）`
                : selected && !selected.installed
                  ? "该镜像尚未安装，创建时会自动通过 sdkmanager 下载（约 1-2 GB）"
                  : "下拉框只列出已安装镜像；需要其它版本时点击右侧按钮浏览全部镜像"}
            </div>
          </div>
          <button
            className="btn btn--secondary"
            disabled={loadingAll}
            onClick={openAllImages}
          >
            {loadingAll ? "读取中…" : "加载全部镜像"}
          </button>
        </div>

        <div className="field">
          <div className="field__label">设备档案</div>
          <div className="field__control">
            <select className="select" value={profileId} onChange={(e) => setProfileId(e.target.value)}>
              {profiles.length === 0 ? <option value="">（由 avdmanager 决定）</option> : null}
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <div className="field__hint">决定屏幕、内存等硬件默认值，来自官方 avdmanager</div>
          </div>
        </div>

        <div className="field">
          <div className="field__label">硬件参数</div>
          <div className="field__control">
            <details className="advanced">
              <summary className="advanced__summary">
                <span className="advanced__title">
                  {hardwareFilled > 0 ? `已自定义 ${hardwareFilled} 项参数` : "自定义硬件参数（可选）"}
                </span>
                <span className="advanced__meta">
                  {hardwareFilled > 0 ? "创建时写入设备配置" : "留空沿用设备档案"}
                </span>
              </summary>
              <div className="advanced__body">
                {HARDWARE_INPUTS.map((spec) => (
                  <div className="field" key={spec.key}>
                    <div className="field__label">{spec.label}</div>
                    <div className="field__control row">
                      <input
                        className="input input--num"
                        type="number"
                        inputMode="numeric"
                        list={`hw-preset-${spec.key}`}
                        min={spec.min}
                        max={spec.max}
                        step={spec.step}
                        placeholder="跟随档案"
                        value={hardwareForm[spec.key]}
                        onChange={(e) => setHardwareField(spec.key, e.target.value)}
                      />
                      <span className="field__unit">{spec.unit}</span>
                      <datalist id={`hw-preset-${spec.key}`}>
                        {spec.presets.map((v) => (
                          <option key={v} value={v} />
                        ))}
                      </datalist>
                    </div>
                  </div>
                ))}

                <div className="field">
                  <div className="field__label">分辨率</div>
                  <div className="field__control">
                    <select
                      className="select"
                      value={hardwareForm.resolution}
                      onChange={(e) => setHardwareField("resolution", e.target.value)}
                    >
                      <option value="">跟随设备档案</option>
                      {RESOLUTION_PRESETS.map((preset) => (
                        <option key={preset} value={preset}>
                          {preset.split("x").join(" × ")}
                        </option>
                      ))}
                      <option value="custom">自定义…</option>
                    </select>
                    {hardwareForm.resolution === "custom" ? (
                      <div className="row row--wrap" style={{ marginTop: 8 }}>
                        <input
                          className="input input--px"
                          type="number"
                          min={PIXEL_MIN}
                          max={PIXEL_MAX}
                          placeholder="宽 (px)"
                          value={hardwareForm.lcdWidth}
                          onChange={(e) => setHardwareField("lcdWidth", e.target.value)}
                        />
                        <span className="field__unit">×</span>
                        <input
                          className="input input--px"
                          type="number"
                          min={PIXEL_MIN}
                          max={PIXEL_MAX}
                          placeholder="高 (px)"
                          value={hardwareForm.lcdHeight}
                          onChange={(e) => setHardwareField("lcdHeight", e.target.value)}
                        />
                      </div>
                    ) : null}
                  </div>
                </div>

                <div className="advanced__foot">
                  <span className="field__hint">
                    填写的项在创建后写入设备 config.ini；分辨率会同步调整屏幕像素，避免被档案皮肤尺寸覆盖。
                  </span>
                  {hardwareFilled > 0 ? (
                    <button className="btn btn--ghost" onClick={() => setHardwareForm(EMPTY_HARDWARE_FORM)}>
                      恢复默认
                    </button>
                  ) : null}
                </div>
              </div>
            </details>
            {hardwareError ? <div className="advanced__error">{hardwareError}</div> : null}
          </div>
        </div>
      </Modal>

      {showAllImages ? (
        <SystemImageModal
          mode="select"
          images={allImages}
          loading={loadingAll}
          error={imagesError}
          selectedPath={imagePath}
          onClose={() => setShowAllImages(false)}
          onReload={() => void loadAllImages()}
          onSelect={(image) => {
            setImagePath(image.path);
            setShowAllImages(false);
          }}
        />
      ) : null}
    </>
  );
}
