# AVDDesktop 架构

AVDDesktop 是一个 Wails（Go + React）桌面应用：管理**软件自带**的 Android SDK、系统镜像、AVD，
并启动 emulator。所有 Android 能力都通过**官方命令行工具**完成，软件不重新实现 sdkmanager / avdmanager 的功能。

## 目录与数据流

```text
internal/platform   软件自有目录（Root 下的 sdk/avd/config/logs/cache）、工具链路径、单实例锁、通用文件操作
internal/domain     领域模型与错误码（不依赖任何其它内部包）
internal/config     settings.json 的读写与校验
internal/logging    应用日志（按天 + 按大小滚动，内存环形缓冲）
internal/proc       外部进程调用（隐藏窗口、行回调、超时与取消、进程树终止）
internal/sdk        SDK 自举（下载官方 cmdline-tools）、sdkmanager 封装、镜像解析、zip 解压
internal/avd        AVD 配置读写（.ini/config.ini）、创建（avdmanager）、启动与状态机（emulator + adb）
internal/adb        `adb devices -l` 解析与轮询
internal/job        统一长任务管理器（进度 + 日志 → 事件流）
internal/service    Wails 绑定层：参数校验 → 领域逻辑 → 注册 Job → 返回 jobID
internal/e2e        端到端测试（build tag `e2e`，需要真实 SDK/网络）
```

数据流：`React UI → wailsjs 绑定方法 → service → sdk/avd/adb → 官方 CLI`。
耗时操作不在绑定方法里同步执行：`service` 只做校验并登记 Job，绑定方法立即返回 `jobID`；
进度与日志通过 `job:created/progress/log/done/failed` 事件流推送到前端，统一显示在底部任务与日志区域。

## 三块核心能力与关键约束

### SDK（自举 + 组件安装）

- SDK 根目录固定为 `<Root>/sdk`（`AVDDESKTOP_HOME` 可覆盖），**不使用系统 Android SDK**。
- 自举：从 `https://dl.google.com/android/repository/` 下载官方 cmdline-tools zip（含 SHA-1 校验），
  解压到 `sdk/cmdline-tools/latest`（整体上限 30 分钟）。
- 组件安装/查询一律调用自带 `sdkmanager`：`--version` 60s、`--list` 3min、`--licenses` 3min、
  安装单个包 45min。
- 需要的组件：`platform-tools`（adb）、`emulator`，以及用户选择的 system image。
  许可通过向 `sdkmanager --licenses` 写入 `y` 行完成。

### AVD

- AVD 根目录为 `<Root>/avd`（注入子进程 `ANDROID_AVD_HOME`，指向存放 `<name>.avd` / `<name>.ini` 的目录本身）。
- 列表与配置来自 `.ini` / `config.ini` 的直接解析；创建走 `avdmanager create avd`（3 分钟超时，交互确认写入 `no`）。
- 名称校验在本地完成（非法字符、重名、同名建议名）。
- 删除前校验目标路径确实位于 AVD 根目录内，避免误删。

### Emulator

- 启动：`emulator -avd <name> -port <port> ...`，端口从 5554 起按步长 2 分配并回收；
  同一 AVD 不允许并发启动（错误码 `FILE_IN_USE`）。
- 状态机：`stopped → starting → booting → running → stopping`，由 adb 轮询推进：
  等待设备 3 分钟、`sys.boot_completed` 5 分钟、停止宽限 30 秒、强杀等待 15 秒。
- 退出码可诊断（如加速不可用），`emulator -accel-check` 结果进入环境检查。

## 跨平台注意

- 可执行文件后缀按平台区分：Windows 为 `sdkmanager.bat` / `avdmanager.bat` / `adb.exe` / `emulator.exe`。
- 子进程环境统一由 `platform.ChildEnv` 构造：注入 `ANDROID_HOME` / `ANDROID_SDK_ROOT` / `ANDROID_AVD_HOME`，
  并把自带工具链目录前置到 `PATH`，绝不修改当前进程环境。
- 进程启动隐藏控制台窗口（Windows）并放入独立进程组，终止时结束整棵进程树。
- 根目录解析顺序：`AVDDESKTOP_HOME` → 可执行文件目录（可写且非临时目录/非 `.app` 包内）→ 用户数据目录。
- 单实例：Windows 用命名互斥体；其它平台用 `<Root>/config/app.lock` + PID。

## 超时策略

所有外部命令都必须带超时（`proc.Options.Timeout`），并随 `context` 支持取消：
探测类 15–90s、列表/许可 60s–3min、创建 AVD 3min、SDK 自举 30min、安装组件 45min、
启动等待 3–5min、终止 10–30s。下载由 `context` 控制超时。

## 测试

- 单元测试全部 hermetic：临时目录自造样本，不依赖本机 SDK/JDK。
- `internal/e2e`（`-tags e2e`）才使用真实网络与 `AVDDESKTOP_E2E_HOME` 指定的真实目录，
  覆盖环境准备、创建 AVD、启动 emulator 三条链路。
