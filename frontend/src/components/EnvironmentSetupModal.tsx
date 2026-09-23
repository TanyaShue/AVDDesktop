// 环境准备 / 修复弹窗：确认将要执行的动作，并显示本次实际使用的 SDK 与 JDK 镜像源。
//
// 镜像源的检测与更换已独立为设置页的「下载镜像源」一项：这里只读取当前生效的源，
// 不再内嵌整套联网检测与选择 UI（否则准备环境前必须先跑一次镜像检测，入口被拖慢）。
import { useEffect, useState } from "react";
import * as api from "../bridge/api";
import { errorText } from "../bridge/api";
import type { MirrorSource } from "../bridge/types";
import { Modal } from "./ui";

interface Props {
  /** JDK 已可用时，准备环境不会下载 JDK，此时 JDK 镜像只作展示。 */
  jdkReady?: boolean;
  onClose: () => void;
  onStart: () => Promise<void>;
  onRepair: () => Promise<void>;
  /** 关闭本弹窗并打开「下载镜像源」设置（由设置页负责页面级跳转）。 */
  onOpenMirrorSettings?: () => void;
}

export function EnvironmentSetupModal({
  jdkReady = false,
  onClose,
  onStart,
  onRepair,
  onOpenMirrorSettings,
}: Props) {
  const [sdkSource, setSdkSource] = useState<MirrorSource | null>(null);
  const [jdkSource, setJdkSource] = useState<MirrorSource | null>(null);
  const [sourceError, setSourceError] = useState("");
  const [starting, setStarting] = useState(false);
  const [repairing, setRepairing] = useState(false);
  const [error, setError] = useState("");

  // 只读取已保存的镜像源（本地读设置表，不联网），因此不会拖慢准备/修复入口。
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const [sdk, jdk] = await Promise.all([api.Mirror.Active(), api.Mirror.ActiveJDK()]);
        if (cancelled) return;
        setSdkSource(sdk as MirrorSource);
        setJdkSource(jdk as MirrorSource);
        setSourceError("");
      } catch (err) {
        if (!cancelled) setSourceError(errorText(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const busy = starting || repairing;

  const start = async () => {
    setStarting(true);
    setError("");
    try {
      await onStart();
      onClose();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setStarting(false);
    }
  };

  const repair = async () => {
    setRepairing(true);
    setError("");
    try {
      await onRepair();
      onClose();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setRepairing(false);
    }
  };

  return (
    <Modal
      title="准备 / 修复环境"
      size="lg"
      onClose={onClose}
      footer={
        <div className="modal__foot-bar">
          <span className="muted">准备/修复只下载缺失组件，不会删除已有系统镜像与设备。</span>
          <div className="row row--wrap">
            <button className="btn btn--secondary" onClick={onClose} disabled={busy}>
              取消
            </button>
            <button className="btn btn--secondary" onClick={() => void repair()} disabled={busy}>
              {repairing ? <span className="spinner" /> : "一键修复"}
            </button>
            <button className="btn btn--primary" onClick={() => void start()} disabled={busy}>
              {starting ? <span className="spinner" /> : "准备 / 补装"}
            </button>
          </div>
        </div>
      }
    >
      <div className="banner banner--info">
        <div className="banner__icon">⬇️</div>
        <div className="banner__body">
          <div className="banner__title">将使用下列镜像源补齐软件自带环境</div>
          <div className="banner__text">
            「准备 / 补装」只下载缺失部分：JDK（缺失或版本过低时，Eclipse Temurin 21，约 200 MB）、
            官方命令行工具（约 150 MB），并通过官方 sdkmanager 安装 platform-tools 与 emulator。
            「一键修复」会强制重装命令行工具、platform-tools 与 emulator，但保留许可、系统镜像和 AVD。
            所有下载进度显示在底部任务区域。
          </div>
        </div>
      </div>

      {error ? (
        <div className="banner banner--danger" style={{ marginTop: 12 }}>
          <div className="banner__icon">⛔</div>
          <div className="banner__body">
            <div className="banner__title">操作未开始</div>
            <div className="banner__text">{error}</div>
          </div>
        </div>
      ) : null}

      <div className="env-setup__sources">
        <div className="mirror-summary__item">
          <div className="mirror-summary__head">
            <span className="mirror-summary__title">Android SDK 镜像源</span>
            {sdkSource?.region ? <span className="chip chip--sm">{sdkSource.region}</span> : null}
          </div>
          <div className="mirror-summary__name">{sdkSource?.name ?? (sourceError ? "读取失败" : "读取中…")}</div>
          <div className="mirror-summary__url mono truncate" title={sdkSource?.baseURL}>
            {sdkSource?.baseURL ?? ""}
          </div>
        </div>
        <div className="mirror-summary__item">
          <div className="mirror-summary__head">
            <span className="mirror-summary__title">JDK 镜像源</span>
            {jdkSource?.region ? <span className="chip chip--sm">{jdkSource.region}</span> : null}
            {jdkReady ? <span className="chip chip--sm chip--info">JDK 已安装</span> : null}
          </div>
          <div className="mirror-summary__name">{jdkSource?.name ?? (sourceError ? "读取失败" : "读取中…")}</div>
          <div className="mirror-summary__url mono truncate" title={jdkSource?.baseURL}>
            {jdkSource?.baseURL ?? ""}
          </div>
        </div>
      </div>

      <div className="row row--wrap" style={{ marginTop: 12, justifyContent: "space-between" }}>
        <span className={sourceError ? "muted muted--danger" : "muted"}>
          {sourceError
            ? `读取镜像源失败：${sourceError}`
            : "镜像源不可达时下载会失败，可先检测并更换镜像源。"}
        </span>
        {onOpenMirrorSettings ? (
          <button className="btn btn--secondary btn--sm" onClick={onOpenMirrorSettings} disabled={busy}>
            检测 / 更换镜像源
          </button>
        ) : null}
      </div>
    </Modal>
  );
}
