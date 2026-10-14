// 主窗口预览入口（开发期视觉走查用）。
//
// 用法：`npx vite` 后打开 `preview.html?page=settings&dialog=mirror&theme=dark&drawer=1`
// 支持参数：
//   page   devices | settings
//   dialog wizard | env | mirror | images | devset（设备设置；配 dev=<序号> 选设备）
//   dev    设备设置的设备序号（0 = 第一台，默认 1）
//   drawer 1 展开底部任务面板
//   view   compact（切换为紧凑列表视图）
//   sort   1 展开排序浮层
//   menu   1 展开第一台设备的「更多操作」菜单
//   q      搜索词（播种到设备页搜索框，核对清空按钮 / 匹配计数 / 无结果空状态）
//   probe  CSS 选择器：把命中元素的计算样式写入 DOM（配合 --dump-dom 读值排查样式问题）
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

// 与生产一致：主题属性必须在首屏绘制之前就落在 <html> 上。
// 真实应用由 index.html 的内联脚本按 localStorage 预置；预览只能从 URL 参数拿，
// 但同样要在 React 挂载前设置 —— 否则会先渲染一帧浅色再切深色，
// headless 截图可能抓到「背景已重绘、文字层还是上一次主题颜色」的中间帧，
// 看起来像样式 bug（排查时曾被误判过）。
const previewTheme = params.get("theme");
if (previewTheme === "dark" || previewTheme === "light") {
  document.documentElement.dataset.theme = previewTheme;
}

// `?noanim=1`：关掉全部动画与过渡。
// headless + --virtual-time-budget 下 CSS 动画时钟可能停在起始帧，
// 导致截图抓到「半途」的状态（排查样式问题时曾被误判成颜色 bug）。
if (params.get("noanim") === "1") {
  const style = document.createElement("style");
  style.textContent = "*,*::before,*::after{animation:none !important;transition:none !important}";
  document.head.appendChild(style);
}

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

/** 向受控输入框写入值并触发 React 的 onChange（原生 setter + input 事件）。 */
function seedInputWhenReady(selector: string, value: string, maxFrames = 240): void {
  let frames = 0;
  const tick = () => {
    const input = document.querySelector<HTMLInputElement>(selector);
    if (input) {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(input, value);
      input.dispatchEvent(new Event("input", { bubbles: true }));
      return;
    }
    if (frames++ > maxFrames) return;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

/** 把设备设置里的每个下拉框都切到「自定义…」，用于核对自定义输入框的布局。 */
function selectDevsetCustomWhenReady(maxFrames = 240): void {
  let frames = 0;
  const tick = () => {
    const selects = Array.from(document.querySelectorAll<HTMLSelectElement>(".devset select"));
    if (selects.length > 0) {
      const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")?.set;
      selects.forEach((select) => {
        if (select.value === "custom") return;
        setter?.call(select, "custom");
        select.dispatchEvent(new Event("change", { bubbles: true }));
      });
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
  // 设备设置：默认打开第二台设备（已停止 + 支持 root），`dev=0` 打开第一台（运行中 + Play 镜像）
  if (dialog === "devset") {
    const index = Number(params.get("dev") ?? "1");
    clickNthWhenReady('[aria-label^="设备设置"]', Number.isFinite(index) ? index : 1);
    // custom=1：把设置面板里的下拉框全部切到「自定义…」，核对自定义输入框布局
    if (params.get("custom") === "1") selectDevsetCustomWhenReady();
  }

  if (params.get("drawer") === "1") clickWhenReady("", ".drawer__head", 240);
  // 紧凑（列表）视图：分段控件的第二个按钮
  if (params.get("view") === "compact") clickNthWhenReady(".segmented button", 1);
  // 排序浮层：工具栏的排序图标按钮（用 aria-label 定位，避免搜索清空按钮改变索引）
  if (params.get("sort") === "1") clickWhenReady("", '[aria-label="排序方式"]');
  // 设备「更多操作」浮层：第一台设备的更多按钮
  if (params.get("menu") === "1") clickNthWhenReady(".device__actions .btn--round-soft", 0);
  // 搜索词：播种到受控输入框，用于核对清空按钮 / 匹配计数 / 无结果空状态
  const q = params.get("q");
  if (q) seedInputWhenReady(".search", q);

  // 样式探针：headless 截图看不出「为什么不可见」时，用它把计算样式落到 DOM 里，
  // 再用 `--dump-dom` 抓出来看（不依赖 DevTools 协议）。
  const probe = params.get("probe");
  if (probe) {
    window.setTimeout(() => {
      const lines = Array.from(document.querySelectorAll<HTMLElement>(probe)).map((el, i) => {
        const cs = getComputedStyle(el);
        return [
          `[${i}] class="${el.className}"`,
          `text="${(el.textContent ?? "").trim().slice(0, 24)}"`,
          `color=${cs.color}`,
          `borderColor=${cs.borderTopColor}`,
          `bg=${cs.backgroundColor}`,
          `opacity=${cs.opacity}`,
          `visibility=${cs.visibility}`,
          `fill=${cs.webkitTextFillColor}`,
          `size=${cs.width}x${cs.height}`,
          `--text-primary=${cs.getPropertyValue("--text-primary").trim()}`,
          `--border=${cs.getPropertyValue("--border").trim()}`,
          `--bg-surface=${cs.getPropertyValue("--bg-surface").trim()}`,
        ].join(" ");
      });
      const root = getComputedStyle(document.documentElement);
      lines.unshift(
        `html data-theme="${document.documentElement.dataset.theme ?? ""}" ` +
          `--text-primary=${root.getPropertyValue("--text-primary").trim()} ` +
          `--border=${root.getPropertyValue("--border").trim()} ` +
          `--bg-surface=${root.getPropertyValue("--bg-surface").trim()}`,
      );
      const pre = document.createElement("pre");
      pre.id = "preview-probe";
      // 固定在最上层：这样截图里能同时看到「探针读数」和「实际绘制结果」
      pre.setAttribute(
        "style",
        "position:fixed;top:0;left:0;right:0;z-index:99999;margin:0;padding:6px 8px;" +
          "background:#ffe600;color:#111;font:12px/16px Consolas,monospace;white-space:pre-wrap",
      );
      pre.textContent = `probe ${probe}\n${lines.join("\n")}`;
      document.body.appendChild(pre);
    }, 600);
  }

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
