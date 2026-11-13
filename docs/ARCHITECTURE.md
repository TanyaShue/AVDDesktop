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
internal/job        统一长任务管理器（进度 + 日志 → 事件流）
internal/service    Wails 绑定层：参数校验 → 领域逻辑 → 注册 Job → 返回 jobID
internal/e2e        端到端测试（build tag `e2e`，需要真实网络/SDK/AVD）
```

数据流：`React UI → wailsjs 绑定方法 → service → jdk/sdk/avd/adb → 官方 CLI`。
模拟器画面与输入完全由 Android Emulator 自带窗口负责，应用只管理进程与 AVD 生命周期，不接管或转发像素。
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
  其余硬件参数完全沿用设备档案，不在创建流程里堆叠低频选项（创建后再到「设备设置」里调整即可）。
  `avdmanager` 仍是 config.ini 的生成者，后端只在创建成功后对指定键做定点修正
  （`internal/avd/hardware.go`，原子写入，保留注释 / 顺序 / 未知键）。写入失败时回滚整个设备，
  避免留下“参数与请求不符”的设备。区间校验在启动任务前完成。
- 自定义分辨率会同时更新 `hw.lcd.width/height`，并让皮肤尺寸不再覆盖（`skin.path` 指向真实皮肤
  或另一组数值尺寸时改为 `_no_skin`；`skin.name` 仅在已有该键时同步）。依据：模拟器实际使用
  `skin.path`，`skin.name` 只供工具展示。
- 删除前校验目标路径确实位于 AVD 根目录内，避免误删。
- **设备设置（每台设备独立）**：设备卡片的齿轮按钮 / 「更多操作 → 设备设置」打开设置面板，
  数据源是 `AvdService.Detail`，保存走 `AvdService.UpdateSettings`（同步方法，不注册任务：
  只写一份 `config.ini` 与一个状态文件，界面需要立刻拿到结果与错误）。
  设备运行中直接拒绝（`FILE_IN_USE`）：`config.ini` 只在模拟器启动时读取，改完不会生效，
  还可能与其退出时的配置回写互相覆盖。取键口径与创建向导一致，写入沿用同一套定点修正。

| 设置项 | 存储 | 允许区间（防呆） |
|---|---|---|
| 运行内存 | `hw.ramSize` | 512–16384 MB（界面按 GB 选择） |
| CPU 核心 | `hw.cpu.ncore` | 1–16 |
| 虚拟机堆上限 | `vm.heapSize` | 16–4096 MB |
| 分辨率 | `hw.lcd.width` / `hw.lcd.height` | 240–7680 px（需成对给出） |
| 像素密度 | `hw.lcd.density` | 72–720 dpi（未指定时按最短边推荐 160/240/320/420/480/560） |
| 物理键盘 | `hw.keyboard` | yes / no |
| Root 权限 | `<avd 目录>/.avddesktop.json` | 开关（软件自有标记，不写进 config.ini） |

- root 的落地方式是「启动时附加 `-writable-system` + 开机后 `adb root` / `adb remount`」
  （`internal/avd/launch.go` 的 `applyRootPermissions`，有界且尽力而为：adb root 会重启 adbd，
  remount 按间隔重试到上限；user 构建只记录日志，不影响设备继续运行）。
  Google Play 镜像是正式版（user）构建，镜像本身不开放 root：判据同时看 `PlayStore.enabled`、
  `tag.id` 与 `image.sysdir.1`，不支持时界面上禁用开关并给出原因。
- 自定义分辨率同样走 `resolutionSkinChanges`（`skin.path` 指向真实皮肤时改为 `_no_skin`），
  与创建时的行为一致。
- **容量类键的两套写法**：新版 `avdmanager` 写出 `hw.ramSize=2G` / `vm.heapSize=228M`
  （字节单位后缀），旧格式与本应用写入的是纯数字 MB（`hw.ramSize=2048`）——实测同一台机器上
  两种并存。读取统一走 `internal/avd/size.go` 的 `parseSizeMB`（带后缀按字节单位换算、
  纯数字按 MB），解析不了的值按「未设置」处理而不是猜；保存一律写回纯数字 MB，
  因此保存一次就会把 `2G` 归一化成 `2048`。不要用「只取前导数字」的解析（会把 `2G` 读成 2，
  界面显示成 `2 MB`，与真实内存差 1024 倍）。
- 其余低频硬件键（`disk.dataPartition.size`、`sdcard.size`、摄像头等）仍完全由设备档案决定，
  应用不覆盖：改动数据分区会牵动 userdata 镜像，风险高于收益。

### Emulator

- 启动：`emulator -avd <name> -port <port> ...`，端口从 5554 起按步长 2 分配并回收；
  同一 AVD 不允许并发启动（错误码 `FILE_IN_USE`）。
- 状态机：`stopped → starting → booting → running → stopping`，由 adb 轮询推进：
  等待设备 3 分钟、`sys.boot_completed` 5 分钟、停止宽限 30 秒、强杀等待 15 秒。
- 退出码可诊断（如加速不可用），`emulator -accel-check` 结果进入环境检查。

#### 原生模拟器窗口

- 启动命令保持 `emulator -avd <name> -port <port>`，由 Android Emulator 创建并管理设备窗口。
- 应用不附加 `-no-window`，不开启 gRPC 画面通道，也不提供独立的设备画面、触摸或工具栏窗口。
- 冷启动仅追加 `-no-snapshot-load`；开启 root 的设备按需追加 `-writable-system`。

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




