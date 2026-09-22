// 创建设备：只保留 设备名称 / System Image / 设备档案 三项。
//
// 镜像不存在时由后端在同一任务里通过官方 sdkmanager 自动安装，界面不再让用户配置
// CPU、GPU、RAM、分辨率等底层参数（见 目标.md §四.2）。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { AvdSpec, DeviceProfile, SystemImage } from "../bridge/types";
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
  const [submitting, setSubmitting] = useState(false);

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
    setSubmitting(true);
    try {
      const spec = {
        name: name.trim(),
        systemImagePath: imagePath,
        profileId,
      } as AvdSpec;
      const id = await api.Avd.Create(spec);
      onToast("success", "已开始创建设备", `任务 ${id}，进度显示在底部任务区域`);
      onCreated();
      onClose();
    } catch (err) {
      onToast("danger", "创建失败", errorText(err));
    } finally {
      setSubmitting(false);
    }
  };

  const canSubmit = nameReady && validation.valid && !!imagePath && !submitting;

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
