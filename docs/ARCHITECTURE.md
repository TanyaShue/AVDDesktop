# AVDDesktop 架构

AVDDesktop 是一个 Wails（Go + React）桌面应用：管理**软件自带**的 JDK、Android SDK、系统镜像、AVD，
并启动 emulator。所有 Android 能力都通过**官方命令行工具**完成，软件不重新实现 sdkmanager / avdmanager 的功能。

## 目录与数据流

```text
internal/platform   软件自带目录（Root 下的 jdk/sdk/avd/config/logs/cache）、工具链路径、单实例锁、通用文件操作
internal/domain     领域模型与错误码（不依赖任何其它内部包）
internal/config     settings.json 的读写与校验
internal/logging    应用日志（按天 + 按大小滚动，内存环形缓冲）
internal/proc       外部进程调用（隐藏窗口、行回调、超时与取消、进程树终止）
internal/archive    通用下载（SHA-1 / SHA-256 校验）、zip / tar.gz 安全解压
internal/jdk        JDK 镜像表、连接/延迟/吞吐检测、Eclipse Temurin 下载与原子安装
internal/mirror     Android SDK 镜像表、连接/延迟/吞吐检测、资源完整性校验
internal/sdk        SDK 自举（从所选镜像解析 cmdline-tools）、sdkmanager 封装、镜像解析
internal/avd        AVD 配置读写（.ini/config.ini）、创建（avdmanager）、启动与状态机（emulator + adb）
internal/adb        `adb devices -l` 解析与轮询、`adb shell` 调用
internal/emulatorgrpc 模拟器 gRPC 控制通道（截图 / MMAP 画面流 / 触摸注入）
internal/sharedmem   file-backed 共享内存（emulator MMAP 帧缓冲）
internal/displayhost 独立 Presenter 辅助进程（MMAP → Presenter → 工具栏）
internal/presenter   Windows 原生 Presenter 窗口与独立工具栏（无 cgo）
internal/display     本机 MJPEG 画面服务（非 Windows 兼容路径）
internal/job        统一长任务管理器（进度 + 日志 → 事件流）
internal/service    Wails 绑定层：参数校验 → 领域逻辑 → 注册 Job → 返回 jobID
internal/e2e        端到端测试（build tag `e2e`，需要真实网络/SDK/AVD）
```

数据流：`React UI → wailsjs 绑定方法 → service → jdk/sdk/avd/adb → 官方 CLI`。
自定义 UI 在 Windows 上走原生路径：`emulator renderer → streamScreenshot(MMAP) → internal/sharedmem → internal/displayhost → internal/presenter（独立画面窗口 + 独立工具栏）`。
其它平台保留兼容路径：`React UI（设备窗口 <img>）→ 本机 MJPEG（internal/display）← gRPC 画面流（internal/emulatorgrpc）← emulator`。
耗时操作不在绑定方法里同步执行：`service` 只做校验并登记 Job，绑定方法立即返回 `jobID`；
进度与日志通过 `job:created/progress/log/done/failed` 事件流推送到前端，统一显示在底部任务与日志区域。

## 四块核心能力与关键约束

### JDK（环境检查 + 自举）

- JDK 根目录固定为 `<Root>/jdk`（`AVDDESKTOP_HOME` 可覆盖），**只使用软件自带 JDK**。
  环境检查不会读取系统 `JAVA_HOME` / `PATH`；缺失或版本低于 17 时由 `Prepare` 自动下载。
- 下载源与 Android SDK 镜像相互独立：默认南京大学 NJU，并内置清华 TUNA、北外 BFSU 与 GitHub 官方发布地址。
- 镜像源的检测与设置统一在设置页的「下载镜像源」一页完成：并发检测各源的连接、延迟、采样速度与资源完整性，
  分别保存 SDK 与 JDK 镜像源；「准备 / 修复环境」弹窗只读取已保存的源，不再内嵌检测与选择。
- 各源下载的是同一批 Eclipse Temurin 21 LTS 归档（Windows / macOS / Linux，x64 / arm64），
  始终使用程序内置 SHA-256 校验；解压到 `<Root>/jdk.staging` 后整体替换 `<Root>/jdk`，失败时保留旧 JDK。
- macOS 的 `JAVA_HOME` 指向 `<Root>/jdk/Contents/Home`，Windows / Linux 指向 `<Root>/jdk`。
- 子进程环境由 `platform.ChildEnv` 注入 `JAVA_HOME` 并把自带 JDK 的 `bin` 前置到 `PATH`，
  因此 `sdkmanager` / `avdmanager` 不会使用系统 JDK。

### SDK（自举 + 组件安装）

- SDK 根目录固定为 `<Root>/sdk`（`AVDDESKTOP_HOME` 可覆盖），**不使用系统 Android SDK**。
- 镜像：设置页「下载镜像源」是检测与设置的唯一入口，并发检测内置镜像的连接、延迟、采样速度和资源完整性。
  资源校验覆盖 `repository2-3.xml`、cmdline-tools、platform-tools、emulator 与 Google APIs 系统镜像索引；
  环境准备与系统镜像下载都只读取已保存的镜像源（系统镜像弹窗里仅只读显示当前源）。
- 自举：读取所选镜像的 `repository2-3.xml`，按当前平台的归档 URL 与 SHA-1 下载 cmdline-tools，
  解压到 `sdk/cmdline-tools/latest`（整体上限 30 分钟）；官方源索引不可用时回退内置官方归档。
- 组件安装/查询一律调用自带 `sdkmanager`，并通过 `SDK_TEST_BASE_URL` 将仓库根地址切换到所选镜像：
  `--version` 60s、`--list` 3min、`--licenses` 3min、安装单个包 45min。
- 新版 Windows `sdkmanager.bat` 会把包路径分号拆成参数；安装前统一转换为 Android CLI 的
  `system-images/android-X/tag/abi` 斜杠形式，避免系统镜像被拆成多个不存在的包。
- 新版 `cmdline-tools` 会把 `sdkmanager` 委托给 Android CLI，并可能在遥测失败后返回非零退出码。
  安装结果因此同时检查退出码、`source.properties`、`package.xml`、关键可执行文件与实际目录；
  只要目标组件完整落盘并通过校验，就按成功处理。
- 已安装包列表直接扫描软件自带 SDK 目录，兼容新旧 Android CLI 表格格式，避免把中断安装残留
  误判为可用组件。设置页「准备 / 修复环境 → 一键修复」会强制重装 cmdline-tools、platform-tools 与
  emulator，并保留 licenses、system-images 与 AVD（与「下载镜像源」互不影响：前者只重装工具链，后者只改下载地址）。
- JDK 不依赖 Android SDK 镜像，使用独立的 JDK 镜像选择；下载内容仍强制通过内置 SHA-256 校验。
- 删除本机系统镜像同样交给 `sdkmanager --uninstall`（包路径同样转成斜杠形式，超时 10min），
  并以目录是否真的消失为最终判据；仍被 AVD 引用的镜像（按 `config.ini` 的 `image.sysdir.1` 判定）会被拒绝删除。
- 需要的组件：`platform-tools`（adb）、`emulator`，以及用户选择的 system image。
  许可通过向 `sdkmanager --licenses` 写入 `y` 行完成。

### AVD

- AVD 根目录为 `<Root>/avd`（注入子进程 `ANDROID_AVD_HOME`，指向存放 `<name>.avd` / `<name>.ini` 的目录本身）。
- 列表与配置来自 `.ini` / `config.ini` 的直接解析；创建走 `avdmanager create avd`（3 分钟超时，交互确认写入 `no`）。
- 名称校验在本地完成（非法字符、重名、同名建议名）。
- 硬件覆盖（可选）：创建向导只暴露三项常用参数（内存 / CPU 核心 / 分辨率，单位分别为 GB / 核 / px）；
  其余硬件参数（VM 堆、屏幕密度、数据分区、SD 卡等）完全沿用设备档案，不在界面上堆叠低频选项。
  `avdmanager` 仍是 config.ini 的生成者，后端只在创建成功后对指定键做定点修正
  （`internal/avd/hardware.go`，原子写入，保留注释 / 顺序 / 未知键）。写入失败时回滚整个设备，
  避免留下“参数与请求不符”的设备。区间校验在启动任务前完成，参考取值见上表。
- 自定义分辨率会同时更新 `hw.lcd.width/height`，并让皮肤尺寸不再覆盖（`skin.path` 指向真实皮肤
  或另一组数值尺寸时改为 `_no_skin`；`skin.name` 仅在已有该键时同步）。依据：模拟器实际使用
  `skin.path`，`skin.name` 只供工具展示。
- 删除前校验目标路径确实位于 AVD 根目录内，避免误删。

| 参数 | config.ini 键 | 允许区间（防呆） |
|---|---|---|
| 内存 | `hw.ramSize` | 512–16384 MB（界面按 GB 选择） |
| CPU 核心 | `hw.cpu.ncore` | 1–16 |
| 分辨率 | `hw.lcd.width` / `hw.lcd.height` | 240–7680 px（需成对给出） |

其余硬件键（`vm.heapSize`、`hw.lcd.density`、`disk.dataPartition.size`、`sdcard.size` 等）
由 `avdmanager` 按设备档案生成，应用不覆盖。

### Emulator

- 启动：`emulator -avd <name> -port <port> ...`，端口从 5554 起按步长 2 分配并回收；
  同一 AVD 不允许并发启动（错误码 `FILE_IN_USE`）。
- 状态机：`stopped → starting → booting → running → stopping`，由 adb 轮询推进：
  等待设备 3 分钟、`sys.boot_completed` 5 分钟、停止宽限 30 秒、强杀等待 15 秒。
- 退出码可诊断（如加速不可用），`emulator -accel-check` 结果进入环境检查。

#### 自定义 UI：MMAP + 独立原生 Presenter（Windows）

- 启动仍为 `-no-window -grpc <gRPC 端口>`；Qt 主窗口不加载。
- 辅助进程与主程序使用同一个可执行文件，以 `--display-host` 模式启动，不初始化 Wails、不抢单实例锁。
- `getScreenshot(PNG)` 读 IHDR 探测原生分辨率，随后请求
  `streamScreenshot(RGBA8888, ImageTransport.MMAP)`；像素写入客户端创建的 file-backed 共享内存，
  gRPC 只传尺寸、序号、时间戳和旋转元数据。
- `internal/displayhost` 读取共享内存并复制稳定帧，`internal/presenter` 在独立顶层窗口用
  GDI top-down DIB 显示；工具栏是另一个 `WS_EX_TOOLWINDOW` 顶层窗口，不占用主程序布局。
- 输入：Presenter 鼠标事件映射为设备坐标并调用 gRPC `sendTouch`；工具栏 Back/Home/Apps 调用
  `adb shell input keyevent`（4/3/187）。
- 帧是脏帧驱动的，窗口必须保留最后一帧。MMAP 传输可能 tearing，因此 data plane 在消息到达后复制帧再转换。
- 非 Windows 平台返回 `UNSUPPORTED`，前端自动回退到原有 MJPEG + `<img>` 浮层。
- 已知缺口：gRPC 默认无认证；当前原生 Presenter 仍使用 CPU 帧复制与 GDI 展示，未实现 DMA-BUF / GPU 零拷贝。
- 非目标：音频、多显示器、折叠屏、旋转、剪贴板与快照。

## 跨平台注意

- 可执行文件后缀按平台区分：Windows 为 `sdkmanager.bat` / `avdmanager.bat` / `adb.exe` / `emulator.exe` / `java.exe`。
- 子进程环境统一由 `platform.ChildEnv` 构造：注入 `JAVA_HOME` / `ANDROID_HOME` / `ANDROID_SDK_ROOT` /
  `ANDROID_AVD_HOME`，并把自带 JDK 与工具链目录前置到 `PATH`，绝不修改当前进程环境。
- 进程启动隐藏控制台窗口（Windows）并放入独立进程组，终止时结束整棵进程树。
- 根目录解析顺序：`AVDDESKTOP_HOME` → 可执行文件目录（可写且非临时目录/非 `.app` 包内）→ 用户数据目录。
- 单实例：Windows 用命名互斥体；其它平台对 `<Root>/config/app.lock` 加 flock 排他锁
  （进程退出或崩溃时由内核自动释放，不依赖 PID 存活探测）。

## 超时策略

所有外部命令都必须带超时（`proc.Options.Timeout`），并随 `context` 支持取消：
探测类 15–90s、列表/许可 60s–3min、创建 AVD 3min、JDK 安装 30min、SDK 自举 30min、
安装组件 45min、卸载组件 10min、启动等待 3–5min、终止 10–30s。下载由 `context` 控制超时。

## 测试

- 单元测试全部 hermetic：临时目录自造样本，不依赖本机 SDK/JDK。
- `internal/e2e`（`-tags e2e`）才使用真实网络与 `AVDDESKTOP_E2E_HOME` 指定的真实目录，
  覆盖 JDK/SDK 环境准备、创建 AVD、启动 emulator 三条链路。JDK/SDK 镜像检测均有 hermetic 单测。
