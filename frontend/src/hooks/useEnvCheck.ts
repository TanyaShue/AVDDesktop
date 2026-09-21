// 环境检查状态：唯一入口，供设置页与创建向导复用。
//
// 检查本身会实跑 java / sdkmanager / adb / emulator，因此状态提升到应用壳（常驻），
// 切页面不会重复探测。以下三种情况会更新：
//  1. 用户点「重新检查」；
//  2. 后端自动准备完成（env:changed 事件）；
//  3. 安装 SDK 组件后主动重新检查。
import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { EnvReport } from "../bridge/types";
import { useWailsEvent } from "./useApp";

type ToastLevel = "info" | "success" | "warning" | "danger";

export interface EnvCheck {
  report: EnvReport | null;
  loading: boolean;
  /** 重新检查环境（实跑工具链）。 */
  reload: () => Promise<void>;
  /** 自动准备 SDK 环境（下载命令行工具 / 安装 platform-tools 与 emulator）。 */
  prepare: () => Promise<void>;
}

export function useEnvCheck(
  onToast: (level: ToastLevel, title: string, text?: string) => void,
): EnvCheck {
  const [report, setReport] = useState<EnvReport | null>(null);
  const [loading, setLoading] = useState(true);
  const busy = useRef(false);

  const reload = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    setLoading(true);
    try {
      setReport(api.normalizeEnvReport((await api.Env.Check()) as EnvReport));
    } catch (err) {
      onToast("danger", "环境检查失败", errorText(err));
    } finally {
      busy.current = false;
      setLoading(false);
    }
  }, [onToast]);

  const prepare = useCallback(async () => {
    try {
      await api.Env.Prepare();
    } catch (err) {
      onToast("danger", "准备环境失败", errorText(err));
    }
  }, [onToast]);

  // 启动时检查一次（StrictMode 下 effect 会执行两次，用 busy 保证不会并发重复请求）。
  useEffect(() => {
    void reload();
  }, [reload]);

  // 后端推送的新报告：直接采用，不重复探测。
  useWailsEvent<EnvReport | null>(EVENTS.envChanged, (rep) => {
    if (rep) {
      setReport(api.normalizeEnvReport(rep));
      return;
    }
    void reload();
  });

  return { report, loading, reload, prepare };
}
