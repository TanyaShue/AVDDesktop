// 主窗口预览入口（开发期视觉走查用）。
//
// 用法：`npx vite` 后打开 `preview.html?page=settings&dialog=mirror&theme=dark&drawer=1`
// 支持参数：
//   page   devices | settings
//   dialog wizard | env | mirror | images
//   drawer 1 展开底部任务面板
//   view   compact（切换为紧凑列表视图）
//   sort   1 展开排序浮层
//   menu   1 展开第一台设备的「更多操作」菜单
//   toast  1 手动派发成功 / 失败事件，核对 toast 样式
//   theme  light | dark
//   env    missing（渲染"环境未就绪"分支）
import React from "react";
import { createRoot } from "react-dom/client";
import App from "../app/App";
import { ErrorBoundary } from "../components/ErrorBoundary";
import { installPreviewBridge, previewParams, emitPreviewEvent } from "./mock";
import "../styles/tokens.css";
import "../styles/app.css";

installPreviewBridge();

const params = previewParams();

/** 按可见文本点击元素（用于把预览切到指定页面 / 打开指定弹窗）。 */
function clickByText(text: string, selector = "button"): boolean {
  const nodes = Array.from(document.querySelectorAll<HTMLElement>(selector));
  const hit = nodes.find((node) => (node.textContent ?? "").includes(text));
  if (!hit) return false;
  hit.click();
  return true;
}

/** 分帧重试直到目标出现，最多等 maxFrames 帧。 */
function clickWhenReady(text: string, selector = "button", maxFrames = 240): void {
  let frames = 0;
  const tick = () => {
    if (clickByText(text, selector) || frames++ > maxFrames) return;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

/** 点击第 index 个匹配元素（用于没有稳定文案的图标按钮）。 */
function clickNthWhenReady(selector: string, index: number, maxFrames = 240): void {
  let frames = 0;
  const tick = () => {
    const node = document.querySelectorAll<HTMLElement>(selector)[index];
    if (node) {
      node.click();
      return;
    }
    if (frames++ > maxFrames) return;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

function applyPreviewState(): void {
  const page = params.get("page");
  if (page === "settings") clickWhenReady("设置", ".navrail__item");

  const dialog = params.get("dialog");
  if (dialog === "wizard") clickWhenReady("新建设备");
  if (dialog === "env") clickWhenReady("准备 / 修复环境");
  if (dialog === "mirror") clickWhenReady("检测 / 更换镜像源");
  if (dialog === "images") clickWhenReady("查看全部镜像");

  if (params.get("drawer") === "1") clickWhenReady("", ".drawer__head", 240);
  // 紧凑（列表）视图：分段控件的第二个按钮
  if (params.get("view") === "compact") clickNthWhenReady(".segmented button", 1);
  // 排序浮层：工具栏的排序图标按钮
  if (params.get("sort") === "1") clickNthWhenReady(".toolbar__right .icon-btn", 0);
  // 设备「更多操作」浮层：第一台设备的更多按钮
  if (params.get("menu") === "1") clickNthWhenReady(".device__actions .btn--round-soft", 0);

  // 触发两条 toast（成功 + 失败）与一条任务完成事件，用于核对提示样式。
  // 延迟到截图前一刻派发：toast 4.2 秒后会自动消失，早发会在 --virtual-time-budget
  // 走完之前就被回收掉。
  if (params.get("toast") === "1") {
    window.setTimeout(() => {
      emitPreviewEvent("job:done", { status: "succeeded", title: "准备自带 JDK" });
      emitPreviewEvent("job:failed", {
        error: { code: "NETWORK", message: "下载中断", hint: "请检查网络或更换镜像源" },
      });
    }, 7800);
  }
}

const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <ErrorBoundary scope="应用">
      <App />
    </ErrorBoundary>
  </React.StrictMode>,
);

applyPreviewState();
