// 系统镜像 / AVD 的展示文案与格式化。
//
// 这些映射同时被设备页、创建向导与系统镜像弹窗使用：放在一个模块里，
// 避免同一套 tag 文案在多处各写一份（曾经 "google_apis_playstore" 只在一处被翻译）。
import type { SystemImage } from "../bridge/types";

export function androidVersionLabel(image: SystemImage): string {
  return image.androidVersion?.trim() || `Android API ${image.api || "?"}`;
}

/** SDK 仓库的 tag 名 → 界面上显示的短文案。 */
export function imageTagLabel(tag: string): string {
  switch (tag.toLowerCase()) {
    case "default":
      return "默认";
    case "google_apis":
      return "Google APIs";
    case "google_apis_playstore":
      return "Google Play";
    case "aosp_atd":
      return "AOSP ATD";
    case "google_atd":
      return "Google APIs ATD";
    case "android-desktop":
      return "Android Desktop";
    case "google_apis_ps16k":
      return "Google APIs (16 KB)";
    case "google_apis_playstore_ps16k":
      return "Google Play (16 KB)";
    default:
      return tag || "未知";
  }
}

/** 镜像是否支持 Root：允许后端显式给出，否则按 playstore 约定推断。 */
export function imageRootSupported(image: SystemImage): boolean {
  if (typeof image.rootSupported === "boolean") return image.rootSupported;
  return !image.tag.toLowerCase().includes("playstore");
}

/** 内存按 MB 存储，界面上优先用 GB 显示（1024 MB = 1 GB，半 GB 保留一位小数）。 */
export function formatRam(mb: number): string {
  if (mb >= 1024 && mb % 512 === 0) return `${(mb / 1024).toFixed(1).replace(/\.0$/, "")} GB`;
  return `${mb} MB`;
}
