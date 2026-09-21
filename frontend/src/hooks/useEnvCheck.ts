// 环境自检状态：整个程序生命周期只自动执行一次。
//
// 为什么由应用壳（App）持有而不是放在首页里：App 按 `page` 条件渲染页面，
// 切走再切回首页会卸载重建 HomePage。如果自检写在 HomePage 的 useEffect 中，
// 每切一次标签页就会重跑一次 Detect（内部会实跑 java / adb / emulator 校验），
// 界面还会闪一下加载态，用户体感就是"切个标签页就重新检查一遍"。
//
// 状态提升到 App（常驻）后：
//   - 启动时自检一次（首页就是首屏）；
//   - 切换标签页复用同一份结果，不再探测；
//   - 只有这三种情况会更新：
//       1. 用户点"重新检测"（reload(true)，忽略后端缓存）；
//       2. SDK 包安装/卸载等环境确实变了（sdk:changed，事件订阅必须常驻，
//          否则在 SDK 页装完包回到首页会看到过期报告）；
//       3. 后端主动推送新报告（env:changed，直接采用，不重复探测）。
import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../bridge/api";
import { EVENTS, errorText } from "../bridge/api";
import type { EnvReport, MirrorSource } from "../bridge/types";
import { useWailsEvent } from "./useApp";

type ToastLevel = "info" | "success" | "warning" | "danger";

export interface EnvCheck {
  report: EnvReport | null;
  source: MirrorSource | null;
  loading: boolean;
  /** 重新自检；force=true 忽略后端 3s 缓存，实跑一遍工具链。 */
  reload: (force?: boolean) => Promise<void>;
  /** 只刷新"当前镜像源"卡片（切源/测速后调用），不触发环境重探测。 */
  refreshSource: () => Promise<void>;
}

export function useEnvCheck(
  onToast: (level: ToastLevel, title: string, text?: string) => void,
): EnvCheck {
  const [report, setReport] = useState<EnvReport | null>(null);
  const [source, setSource] = useState<MirrorSource | null>(null);
  const [loading, setLoading] = useState(true);
  const started = useRef(false);

  const refreshSource = useCallback(async () => {
    const sources = api.asArray((await api.Mirror.ListSources()) as MirrorSource[]);
    const settings = (await api.Settings.Get()) as { activeSourceId: string };
    setSource(sources.find((s) => s.id === settings.activeSourceId) ?? sources[0] ?? null);
  }, []);

  const reload = useCallback(
    async (force = false) => {
      setLoading(true);
      try {
        const rep = api.normalizeEnvReport(
          (await api.Env.Detect({ force, sdkRootOverride: "" })) as EnvReport,
        );
        setReport(rep);
        await refreshSource();
      } catch (err) {
        onToast("danger", "自检失败", errorText(err));
      } finally {
        setLoading(false);
      }
    },
    [onToast, refreshSource],
  );

  // 启动时自检一次。StrictMode 下 effect 会执行两次，用 ref 保证只发一次请求。
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void reload();
  }, [reload]);

  // 后端推送的新报告：直接采用，不重复探测。
  useWailsEvent<EnvReport | null>(EVENTS.envChanged, (rep) => {
    if (rep) setReport(api.normalizeEnvReport(rep));
  });

  // 环境确实变了（安装/卸载 SDK 包）：重新检测。订阅在这里而不是首页，
  // 是因为变更多半发生在别的页面（SDK 页），首页当时可能并未挂载。
  useWailsEvent<unknown>(EVENTS.sdkChanged, () => void reload(true));

  return { report, source, loading, reload, refreshSource };
}
