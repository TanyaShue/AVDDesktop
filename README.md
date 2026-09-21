# AVDDesktop

管理**软件自带**的 Android SDK、系统镜像与 AVD，并启动 Android 模拟器；不需要安装 Android Studio，
也不使用系统里的 sdkmanager / avdmanager / emulator / adb。

- **自带 SDK**：所有内容放在软件自己的目录里，与系统 Android SDK 完全隔离
- **首次运行自动准备**：发现自带 SDK 不存在时，自动下载官方命令行工具、接受许可、安装 platform-tools 与 emulator
- **系统镜像**：列出官方仓库提供的镜像，按需通过 `sdkmanager` 安装
- **AVD 管理**：列出 / 创建 / 删除设备，启动与停止模拟器并显示运行状态
- **统一日志与任务**：底部区域显示长任务进度与日志

技术栈：Wails v2 + Go + React 19 + TypeScript + Vite。

---

## 目录结构

软件的数据目录（下称 `<Root>`）解析顺序：环境变量 `AVDDESKTOP_HOME` → 可执行文件所在目录（可写且不是
临时目录或 macOS `.app` 包内）→ 用户数据目录。目录内容：

```text
<Root>/sdk       自带 Android SDK（cmdline-tools/latest、platform-tools、emulator、system-images）
<Root>/avd       自带 AVD（<name>.ini 与 <name>.avd/config.ini）
<Root>/config    设置文件 settings.json
<Root>/logs      应用日志（按天 + 按大小滚动）
<Root>/cache     下载临时文件
```

## 首次运行行为

1. 创建上述目录并写入默认设置；
2. 自检：探测自带工具链与 JDK（java），检查硬件加速与磁盘空间；
3. 若自带 SDK 还没有 `sdkmanager`，自动准备：下载官方 cmdline-tools → 解压 → 接受许可 →
   安装 `platform-tools` 与 `emulator`（需要网络，进度显示在底部任务区域）；
4. 在「设备」页选择系统镜像创建 AVD，然后启动模拟器。镜像尚未安装时，界面会引导用 `sdkmanager` 安装。

## 环境要求

- **JDK 17 或更高版本**：`sdkmanager` / `avdmanager` 依赖它，需要设置 `JAVA_HOME`（或把 `java` 加入 `PATH`）。
  `emulator` / `adb` 是原生程序，不需要 JDK。
- Windows 10/11 或 macOS / Linux（Windows 使用 WebView2，系统自带）。
- 构建与测试：Go 1.25+、Node 20+、Wails CLI v2.16+。

## 构建与测试

```powershell
# 前端构建
cd frontend; npm install; npm run build; cd ..

# 构建可执行文件 → build/bin/AVDDesktop.exe
wails build

# 单元测试（全部 hermetic，不需要网络与本机 SDK）
go test ./...

# 端到端测试（真实网络 + 真实 SDK/AVD 目录，耗时较长）
$env:AVDDESKTOP_E2E_HOME = "E:\avddesktop-e2e"
go test -tags e2e -count=1 -timeout 60m ./internal/e2e/ -v
```

## 常见问题

- **下载失败 / 卡住**：软件自身不代理，下载走系统代理环境变量（`HTTPS_PROXY` / `HTTP_PROXY`）。
  若公司网络拦截 `dl.google.com`，请先配置代理后重试；失败的任务可以在底部任务区域取消后重跑。
- **提示找不到 JDK / java**：安装 JDK 17+ 并设置 `JAVA_HOME`，重启应用后重新自检。
- **没有硬件加速**：自检会给出原因。Windows 可启用「Windows 虚拟机监控程序平台」（WHPX）或在 BIOS/UEFI
  中开启 VT-x / AMD-V；没有加速时模拟器仍能启动，但明显变慢。
- **磁盘空间不足**：系统镜像单个约 1–2 GB，建议 SDK 所在分区至少保留 12 GB。

## 文档

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)：模块划分、数据流、SDK / AVD / emulator 三块的关键约束与超时策略。
