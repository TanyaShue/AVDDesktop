# AVDDesktop

管理**软件自带**的 JDK、Android SDK、系统镜像与 AVD，并启动 Android 模拟器；不需要安装 Android Studio，
也不使用系统里的 JDK / sdkmanager / avdmanager / emulator / adb。

- **自带 JDK + SDK**：所有内容放在软件自己的目录里，与系统 JDK / Android SDK 完全隔离
- **首次运行自动准备**：发现自带 JDK 或 SDK 不存在时，自动下载 Eclipse Temurin 21 JDK、命令行工具，接受许可并安装 platform-tools 与 emulator
- **镜像站选择与检测**：Android SDK 内置 Google 官方、Google 中国下载、腾讯云等镜像；JDK 默认使用南京大学 NJU，并内置清华 TUNA、北外 BFSU 与 GitHub 官方源。设置页的「下载镜像源」是唯一入口：并发检测各源的连接、延迟、采样速度与资源完整性，分别保存 SDK 与 JDK 镜像源，环境准备与系统镜像下载都按已保存的源执行
- **环境修复**：兼容新版 Android CLI 输出；检测到旧版本残留、安装中断或元数据缺失时，可一键重装 cmdline-tools、platform-tools 与 emulator
- **系统镜像**：列出所选 SDK 仓库提供的镜像，按需通过 `sdkmanager` 安装；设置页可直接删除本机已安装的镜像释放磁盘空间，「查看全部镜像」弹窗可按版本、架构与 Root 支持筛选下载，并显示当前使用的 SDK 镜像源
- **AVD 管理**：列出 / 创建 / 删除设备，启动与停止模拟器并显示运行状态；创建时按系统镜像与设备档案生成，
  也可以自行覆盖内存、VM 堆、CPU 核心、分辨率与屏幕密度、数据分区、SD 卡等硬件参数（未填写的项沿用档案默认值，
  设备卡片上直接显示当前生效的内存 / 核心 / 分辨率）
- **统一日志与任务**：底部区域显示长任务进度与日志

技术栈：Wails v2 + Go + React 19 + TypeScript + Vite。

---

## 目录结构

软件的数据目录（下称 `<Root>`）解析顺序：环境变量 `AVDDESKTOP_HOME` → 可执行文件所在目录（可写且不是
临时目录或 macOS `.app` 包内）→ 用户数据目录。目录内容：

```text
<Root>/jdk       自带 JDK（Eclipse Temurin 21，JAVA_HOME）
<Root>/sdk       自带 Android SDK（cmdline-tools/latest、platform-tools、emulator、system-images）
<Root>/avd       自带 AVD（<name>.ini 与 <name>.avd/config.ini）
<Root>/config    设置文件 settings.json
<Root>/logs      应用日志（按天 + 按大小滚动）
<Root>/cache     下载临时文件
```

## 首次运行行为

1. 创建上述目录并写入默认设置；
2. 自检：只探测软件目录下的 JDK 与工具链，不使用系统 `JAVA_HOME` / `PATH` 中的 JDK；同时检查硬件加速与磁盘空间；
3. 若软件自带 JDK 或 SDK 尚未就绪，自动准备：从当前所选 JDK 镜像下载 Eclipse Temurin 21 →
   从当前所选 SDK 镜像下载 cmdline-tools → 解压 → 接受许可 → 安装 `platform-tools` 与 `emulator`（需要网络，进度显示在底部任务区域）；
4. 在「设备」页选择系统镜像创建 AVD，然后启动模拟器。镜像尚未安装时，界面会引导用 `sdkmanager` 安装。

## 环境要求

- **无需系统 JDK**：软件首次运行会从所选 JDK 镜像下载 Eclipse Temurin 21（JDK 17+）到 `<Root>/jdk`，
  并使用内置 SHA-256 校验；`sdkmanager` / `avdmanager` 只使用该 JDK，`emulator` / `adb` 是原生程序，不需要 JDK。
- Windows 10/11 或 macOS / Linux（Windows 使用 WebView2，系统自带）。
- 构建与测试：Go 1.25+、Node 20+、Wails CLI v2.16+。

## 构建与测试

```powershell
# 前端构建
cd frontend; npm install; npm run build; cd ..

# 构建可执行文件 → build/bin/AVDDesktop.exe
wails build

# 单元测试（全部 hermetic，不需要网络与本机 SDK/JDK）
go test ./...

# 端到端测试（真实网络 + 真实 JDK/SDK/AVD 目录，耗时较长）
$env:AVDDESKTOP_E2E_HOME = "E:\avddesktop-e2e"
go test -tags e2e -count=1 -timeout 300m ./internal/e2e/ -v
```

## 常见问题

- **下载失败 / 卡住**：软件自身不代理，下载走系统代理环境变量（`HTTPS_PROXY` / `HTTP_PROXY`）。
  可在「设置 → 下载镜像源」中检测并切换到延迟较低且资源完整的镜像；若公司网络拦截下载，
  请先配置代理后重试。失败的任务可以在底部任务区域取消后重跑。
- **提示 JDK 缺失 / 自动下载失败**：软件只认 `<Root>/jdk`，不会回退系统 JDK。请在「设置 → 下载镜像源」中检测并切换 JDK 下载源；国内可使用南京大学 NJU、清华 TUNA 或北外 BFSU，异常时切回 GitHub 官方源。所有来源都使用程序内置 SHA-256 校验。
- **没有硬件加速**：自检会给出原因。Windows 可启用「Windows 虚拟机监控程序平台」（WHPX）或在 BIOS/UEFI
  中开启 VT-x / AMD-V；没有加速时模拟器仍能启动，但明显变慢。
- **磁盘空间不足**：JDK 与系统镜像合计需要数 GB，建议软件目录所在分区至少保留 12 GB。
- **环境显示已安装但工具仍异常，或系统镜像列表读取失败**：新版 `sdkmanager` 已委托给 Android CLI，
  输出格式和退出码可能变化。应用会优先扫描本地包元数据并以实际落盘结果判定安装是否成功；
  若怀疑是旧版本残留，请在设置页点击「准备 / 修复环境」，再执行「一键修复」（使用「下载镜像源」中已保存的镜像）。
  该操作会重装核心工具链，但保留 SDK 许可、已有系统镜像和 AVD。

## 文档

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)：模块划分、数据流、JDK / SDK / AVD / emulator 的关键约束与超时策略。

## 许可证

本项目基于 [MIT License](LICENSE) 开源。
