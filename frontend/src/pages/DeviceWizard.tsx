// 创建设备：只保留 设备名称 / System Image / 设备档案 三项。
//
// 镜像不存在时由后端在同一任务里通过官方 sdkmanager 自动安装，界面不再让用户配置
// CPU、GPU、RAM、分辨率等底层参数（见 目标.md §四.2）。
import { useCallback, useEffect, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { AvdSpec, DeviceProfile, SystemImage } from "../bridge/types";
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
  const [validation, setValidation] = useState<{ valid: boolean; reason?: string; suggest?: string }>({
    valid: true,
  });
  const [images, setImages] = useState<SystemImage[]>([]);
  const [allImagesLoaded, setAllImagesLoaded] = useState(false);
  const [imagesError, setImagesError] = useState("");
  const [imagePath, setImagePath] = useState("");
  const [loadingImages, setLoadingImages] = useState(true);
  const [profiles, setProfiles] = useState<DeviceProfile[]>([]);
  const [profileId, setProfileId] = useState(DEFAULT_PROFILE);
  const [submitting, setSubmitting] = useState(false);

  const loadImages = useCallback(
    async (installedOnly: boolean) => {
      setLoadingImages(true);
      try {
        const list = api.asArray((await api.Avd.ListImages(installedOnly)) as SystemImage[]);
        setImages(list);
        setImagesError("");
        // 只有全量列表真的加载成功才锁住按钮：失败后用户可以重试
        if (!installedOnly) setAllImagesLoaded(true);
        setImagePath((current) => (list.some((i) => i.path === current) ? current : (list[0]?.path ?? "")));
      } catch (err) {
        const text = errorText(err);
        setImagesError(text);
        onToast("danger", installedOnly ? "读取已安装镜像失败" : "读取可用镜像失败", text);
      } finally {
        setLoadingImages(false);
      }
    },
    [onToast],
  );

  // 打开时先读本地已安装镜像（不联网），设备档案来自官方 avdmanager
  useEffect(() => {
    void loadImages(true);
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
  }, [loadImages, onToast]);

  // 名称实时校验
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
          /* 后端未就绪时忽略 */
        }
      })();
    }, 250);
    return () => clearTimeout(timer);
  }, [name]);

  const selected = images.find((i) => i.path === imagePath);

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

  const canSubmit = validation.valid && !!imagePath && !submitting;

  return (
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
            onChange={(e) => setName(e.target.value)}
            placeholder="仅字母、数字、下划线、点、连字符"
            autoFocus
          />
          <div className={`field__hint${validation.valid ? "" : " field__hint--error"}`}>
            {validation.valid
              ? "用于设备目录名，创建后不建议修改"
              : `${validation.reason ?? "名称不合法"}${validation.suggest ? `（建议：${validation.suggest}）` : ""}`}
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
            disabled={loadingImages || images.length === 0}
          >
            {images.length === 0 ? (
              <option value="">{loadingImages ? "读取中…" : "没有可用的系统镜像"}</option>
            ) : null}
            {images.map((img) => (
              <option key={img.path} value={img.path}>
                {`Android ${img.api} · ${img.tag} · ${img.abi}${img.installed ? "（已安装）" : ""}`}
              </option>
            ))}
          </select>
          <div className={`field__hint${imagesError ? " field__hint--error" : ""}`}>
            {imagesError
              ? `读取镜像失败：${imagesError}（可重试「加载全部可用镜像」）`
              : selected && !selected.installed
                ? "该镜像尚未安装，创建时会自动通过 sdkmanager 下载（约 1-2 GB）"
                : "镜像由官方 sdkmanager 安装到软件自己的 SDK 目录"}
          </div>
        </div>
        <button
          className="btn btn--ghost"
          disabled={loadingImages || allImagesLoaded}
          onClick={() => void loadImages(false)}
        >
          {allImagesLoaded ? "已加载全部" : loadingImages ? "加载中…" : "加载全部可用镜像"}
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
  );
}
