// 预览用假数据：只服务于 `preview.html`（开发期视觉走查），不参与打包入口。
import type {
  AppSettings,
  AvdSummary,
  EmulatorInstance,
  EnvReport,
  JobInfo,
  LogLine,
  MirrorCheck,
  MirrorSource,
  DeviceProfile,
  SystemImage,
} from "../bridge/types";

const now = Date.now();

/** Wails 生成的 class 带 convertValues（仅用于发送请求）；前端消费的是纯数据。 */
type Plain<T> = Omit<T, "convertValues">;

export const DEVICES: Plain<AvdSummary>[] = [
  {
    name: "Pixel_8_API_34",
    path: "D:\\AVDDesktop\\avd\\Pixel_8_API_34.avd",
    api: "34",
    tag: "google_apis_playstore",
    abi: "x86_64",
    deviceProfileId: "pixel_8",
    state: "running",
    instanceId: "inst-1",
    serial: "emulator-5554",
    port: 5554,
    ramMb: 4096,
    cpuCores: 4,
    lcdWidth: 1080,
    lcdHeight: 2400,
  },
  {
    name: "Tablet_API_33",
    path: "D:\\AVDDesktop\\avd\\Tablet_API_33.avd",
    api: "33",
    tag: "google_apis",
    abi: "x86_64",
    deviceProfileId: "pixel_tablet",
    state: "stopped",
    ramMb: 2048,
    cpuCores: 2,
    lcdWidth: 2560,
    lcdHeight: 1600,
  },
  {
    name: "Nexus_5X_API_30",
    path: "D:\\AVDDesktop\\avd\\Nexus_5X_API_30.avd",
    api: "30",
    tag: "google_apis_playstore",
    abi: "arm64-v8a",
    deviceProfileId: "nexus_5x",
    state: "stopped",
    ramMb: 1536,
    cpuCores: 4,
    lcdWidth: 1080,
    lcdHeight: 1920,
  },
  {
    name: "Broken_AVD",
    path: "D:\\AVDDesktop\\avd\\Broken_AVD.avd",
    api: "29",
    tag: "default",
    abi: "x86",
    deviceProfileId: "nexus_4",
    state: "stopped",
    broken: "config.ini 缺少 hw.lcd.width，设备配置已损坏",
  },
];

export const INSTANCES: Plain<EmulatorInstance>[] = [
  {
    id: "inst-1",
    avdName: "Pixel_8_API_34",
    serial: "emulator-5554",
    port: 5554,
    pid: 21344,
    state: "running",
    startedAt: now - 128_000,
    args: ["-avd", "Pixel_8_API_34", "-no-window"],
    customUI: true,
    grpcPort: 8554,
  },
];

export const JOBS: Plain<JobInfo>[] = [
  {
    id: "job-1",
    kind: "download",
    title: "下载系统镜像 computer-control",
    subtitle: "system-images;android-34;google_apis_playstore;x86_64",
    status: "running",
    phase: "解压中",
    percent: 62,
    bytesDone: 1_290_000_000,
    bytesTotal: 2_080_000_000,
    speedBps: 8_400_000,
    etaSeconds: 94,
    startedAt: now - 150_000,
  },
  {
    id: "job-2",
    kind: "prepare",
    title: "准备自带 JDK",
    status: "succeeded",
    percent: 100,
    bytesDone: 0,
    bytesTotal: 0,
    speedBps: 0,
    etaSeconds: 0,
    startedAt: now - 900_000,
    endedAt: now - 600_000,
  },
  {
    id: "job-3",
    kind: "install",
    title: "安装 platform-tools",
    status: "failed",
    percent: 12,
    bytesDone: 0,
    bytesTotal: 0,
    speedBps: 0,
    etaSeconds: 0,
    startedAt: now - 1_800_000,
    endedAt: now - 1_700_000,
    error: { code: "NETWORK", message: "下载中断", hint: "请检查网络或更换镜像源" } as JobInfo["error"],
  },
];

export const LOG_LINES: Plain<LogLine>[] = [
  { seq: 1, at: now - 9_000, level: "info", source: "env", message: "检测自带 JDK：D:\\AVDDesktop\\jdk (Temurin 21.0.4)" },
  { seq: 2, at: now - 8_000, level: "info", source: "env", message: "sdkmanager --list 返回 42 条系统镜像" },
  { seq: 3, at: now - 6_000, level: "warn", source: "mirror", message: "Google 中国下载 延迟 812ms，已自动降权" },
  { seq: 4, at: now - 4_000, level: "info", source: "emulator", message: "emulator -avd Pixel_8_API_34 -port 5554 已启动 (pid=21344)" },
  { seq: 5, at: now - 2_000, level: "error", source: "job-3", message: "download failed: unexpected EOF" },
];

export const SYSTEM_IMAGES: Plain<SystemImage>[] = [
  {
    path: "system-images;android-34;google_apis_playstore;x86_64",
    api: "34",
    tag: "google_apis_playstore",
    abi: "x86_64",
    version: "14.0",
    description: "Android 14 with Play Store",
    androidVersion: "Android 14",
    rootSupported: false,
    installed: true,
  },
  {
    path: "system-images;android-34;google_apis;x86_64",
    api: "34",
    tag: "google_apis",
    abi: "x86_64",
    version: "14.0",
    description: "Android 14",
    androidVersion: "Android 14",
    rootSupported: true,
    installed: false,
  },
  {
    path: "system-images;android-33;google_apis;x86_64",
    api: "33",
    tag: "google_apis",
    abi: "x86_64",
    version: "13.0",
    description: "Android 13",
    androidVersion: "Android 13",
    rootSupported: true,
    installed: false,
  },
  {
    path: "system-images;android-30;default;arm64-v8a",
    api: "30",
    tag: "default",
    abi: "arm64-v8a",
    version: "11.0",
    description: "Android 11 (AOSP)",
    androidVersion: "Android 11",
    rootSupported: true,
    installed: true,
  },
];

export const PROFILES: Plain<DeviceProfile>[] = [
  { id: "pixel_8", index: 0, name: "Pixel 8", oem: "Google", tag: "phone" },
  { id: "pixel_tablet", index: 1, name: "Pixel Tablet", oem: "Google", tag: "tablet" },
  { id: "nexus_5x", index: 2, name: "Nexus 5X", oem: "Google", tag: "phone" },
  { id: "nexus_4", index: 3, name: "Nexus 4", oem: "Google", tag: "phone" },
];

export const MIRROR_SOURCES: Plain<MirrorSource>[] = [
  { id: "google", name: "Google 官方", baseURL: "https://dl.google.com/android/repository/", region: "全球", official: true, active: true },
  { id: "google-cn", name: "Google 中国下载", baseURL: "https://dl.google.com/android/repository/", region: "中国大陆" },
  { id: "tencent", name: "腾讯云", baseURL: "https://mirrors.cloud.tencent.com/AndroidSDK/", region: "中国大陆" },
  { id: "nju", name: "南京大学 NJU", baseURL: "https://mirror.nju.edu.cn/adoptium/", region: "中国大陆" },
];

export const MIRROR_CHECKS: Plain<MirrorCheck>[] = [
  {
    sourceId: "google",
    sourceName: "Google 官方",
    baseURL: "https://dl.google.com/android/repository/",
    region: "全球",
    reachable: true,
    compatible: true,
    recommended: true,
    latencyMs: 218,
    throughputBps: 12_400_000,
    resources: [
      { id: "cmdline", name: "命令行工具", required: true, available: true, statusCode: 200, sizeBytes: 148_000_000, latencyMs: 210 },
      { id: "platform-tools", name: "platform-tools", required: true, available: true, statusCode: 200, sizeBytes: 12_400_000, latencyMs: 190 },
      { id: "emulator", name: "emulator", required: true, available: true, statusCode: 200, sizeBytes: 420_000_000, latencyMs: 260 },
    ],
    checkedAt: now,
    elapsedMs: 1_240,
  },
  {
    sourceId: "tencent",
    sourceName: "腾讯云",
    baseURL: "https://mirrors.cloud.tencent.com/AndroidSDK/",
    region: "中国大陆",
    reachable: true,
    compatible: false,
    recommended: false,
    latencyMs: 34,
    throughputBps: 26_800_000,
    resources: [
      { id: "cmdline", name: "命令行工具", required: true, available: true, statusCode: 200, sizeBytes: 148_000_000, latencyMs: 30 },
      { id: "emulator", name: "emulator", required: true, available: false, error: "404 Not Found" },
    ],
    checkedAt: now,
    elapsedMs: 640,
  },
];

export const ENV_REPORT: EnvReport = {
  appRoot: "D:\\AVDDesktop",
  jdkRoot: "D:\\AVDDesktop\\jdk",
  sdkRoot: "D:\\AVDDesktop\\sdk",
  avdHome: "D:\\AVDDesktop\\avd",
  javaPath: "D:\\AVDDesktop\\jdk\\bin\\java.exe",
  mirrorSourceId: "google",
  mirrorSourceName: "Google 官方",
  jdkMirrorSourceId: "nju",
  jdkMirrorSourceName: "南京大学 NJU",
  ready: true,
  needInit: false,
  components: [
    { id: "jdk", name: "自带 JDK", state: "present", version: "Temurin 21.0.4", path: "D:\\AVDDesktop\\jdk" },
    { id: "cmdline-tools", name: "命令行工具", state: "present", version: "19.0", path: "D:\\AVDDesktop\\sdk\\cmdline-tools\\latest" },
    { id: "platform-tools", name: "platform-tools", state: "present", version: "35.0.2", path: "D:\\AVDDesktop\\sdk\\platform-tools" },
    { id: "emulator", name: "emulator", state: "present", version: "35.2.10", path: "D:\\AVDDesktop\\sdk\\emulator" },
  ] as EnvReport["components"],
  accel: { available: true, kind: "whpx", raw: "WHPX (10.0.22631)", hints: [] },
  disk: { path: "D:\\", totalGB: 953, freeGB: 128, sufficient: true },
  host: { os: "windows", arch: "amd64", cpuCores: 16, memoryGB: 32 },
  images: 3,
  avds: 4,
  issues: [],
  checkedAt: now,
  elapsedMs: 1_860,
};

export const SETTINGS: AppSettings = {
  version: 1,
  theme: "light",
  confirmBeforeDelete: true,
  showTaskDrawer: false,
  logLevel: "info",
  keepLogDays: 14,
  displayQuality: "smooth",
  mirrorSourceId: "google",
  jdkMirrorSourceId: "nju",
};
