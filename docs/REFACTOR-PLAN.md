# 重构方案（Phase 0 审计结论）

> 本文件是重构开始前的代码审计与执行方案（对应任务书 Phase 0）。
> 审计基线：`master @ 5dc7d31`，Go 69 文件 / 约 17.8k 行，前端 24 文件 / 约 4.5k 行。
>
> 后续变更（2026-09-22）：JDK 已从「依赖系统环境」改为软件自带。
> 环境检查只认 `<Root>/jdk`，缺失或版本低于 17 时自动下载 Eclipse Temurin 21，
> 子进程的 `JAVA_HOME` / `PATH` 也始终指向软件目录。
>
> 后续变更（2026-09-22）：重新加入最小化镜像能力。仍由官方 `sdkmanager` 负责安装，
> 仅通过 `SDK_TEST_BASE_URL` 切换仓库根地址；同时提供连接、延迟、吞吐和资源完整性检测。
> 未恢复自研仓库安装器、分片下载器或本地 package.xml 写入逻辑。
>
> 后续变更（2026-09-22）：JDK 增加独立镜像能力，内置南京大学 NJU、清华 TUNA、北外 BFSU 与
> GitHub 官方源，支持与 SDK 相同维度的延迟/采样速度检测；所有源继续使用内置 SHA-256 校验。

## 1. 当前架构问题

1. **SDK 来源是「系统 SDK」而不是软件自己的 SDK。**
   所有路径发现集中在 `internal/platform/paths.go`：`ResolveSdkRoot` 依赖
   `ANDROID_HOME`/`ANDROID_SDK_ROOT`（`paths.go:58,61`），`DiscoverSdkRoots` 扫描
   `%LOCALAPPDATA%\Android\Sdk`、`Android Studio` 安装目录、`PATH` 反推、甚至硬编码
   `C:\Android\Sdk`（`paths.go:104-138`）；`ResolveAvdHome` 依赖
   `ANDROID_AVD_HOME`/`ANDROID_USER_HOME`/`ANDROID_PREFS_ROOT`（`paths.go:31-49`）。
   用户机器上已存在的 SDK 会被直接当成软件运行环境。

2. **自研了一套 SDK 管理器（约 3.0k 行），与官方工具重复。**
   `internal/sdk/repo`（仓库 XML 索引解析）、`internal/sdk/localrepo`（自造 `package.xml`）、
   `internal/sdk/install`（自研安装器：下载 → 校验 → 解压 → 原子替换）、
   `internal/sdk/query`（本地包扫描）、`internal/download`（583 行分片并发下载框架）、
   `internal/mirror`（681 行镜像源 + 测速）。任务书第 3/18 节明确禁止这些实现。

3. **重复的环境检查与诊断入口。**
   `internal/sdk/detect`（948 行，含 3 种探测路径 + 跨组件归因）同时服务首页自检、
   设置页 `DiagnosticsService.RunSelfCheck`（`misc.go:93-162`）、启动前检查，多套入口。

4. **日志三重出口、任务日志重复显示。**
   任务日志既走 `job:log`（`job/manager.go:459`）又被 `LogHook` 转写进应用日志
   （`manager.go:26,244,468`），应用日志再经 `log:line` 推给设置页 `LogPanel`；
   同一个任务的行在「任务抽屉」和「设置页日志面板」各出现一次。
   另外 `emulator:log` 后端在发（`launcher.go:272`）但前端零订阅。

5. **页面与功能超出核心流程。**
   4 个标签页（首页 / 设备 / SDK / 设置）+ 4 个弹窗（创建向导、测速、logcat、任务抽屉）。
   首页 (`HomePage.tsx`) 与 SDK 页 (`SdkPage.tsx`) 承载了「SDK 包管理」这一类
   与「创建 AVD / 启动 Emulator」无关的功能。

6. **AVD 创建有两条后端。** `avdmanager` 后端 + 直写 `config.ini` 后端
   （`avd/backend/backend.go`），并为此维护 schema（`store/schema.go`）、
   镜像元数据自愈（`service/metadata.go`）等一整套兼容逻辑。

## 2. 准备删除的模块

| 模块 | 行数 | 原因 |
|---|---|---|
| `internal/sdk/repo` | 755 | XML 仓库索引解析器（任务书明确禁止） |
| `internal/sdk/localrepo` | 995 | 自造 `package.xml`，只为自研安装器存在 |
| `internal/sdk/install` | 554 | 自研安装器；改由 `sdkmanager` 安装 |
| `internal/sdk/query` | 341 | 本地包扫描；改由 `sdkmanager --list` 提供 |
| `internal/sdk/detect` | 1336 | 环境探测/归因；合并为「一套环境检查」 |
| `internal/mirror` | 739 | 镜像源表 + 测速引擎 |
| `internal/download` | 583 | 分片并发下载框架；只需单连接下载 1 个 zip |
| `internal/archive` | 401 | 仅保留「安全解压 zip」约 90 行 |
| `internal/avd/backend` | 342 | 删除「直写 config.ini」后端，只用 `avdmanager` |
| `internal/avd/store/schema.go` `export.go` | 588 | 硬件参数 schema 与导入导出，超出核心流程 |
| `internal/service/mirror.go` `metadata.go` | 365 | 依赖上面删除的包 |
| `internal/service/misc.go` 的诊断/自检部分 | ~150 | 与统一环境检查重复 |
| `internal/platform` 的路径发现部分 | ~280 | `ResolveSdkRoot`/`DiscoverSdkRoots`/`DefaultSdkRoot` |
| `internal/platform/open*.go` `proxy*.go` `virtualization*.go` | ~390 | 非核心流程（打开目录/终端代理/Windows 开关归因） |
| 前端 `HomePage` `SdkPage` `SpeedTestModal` `LogPanel` `LogcatModal` | 约 1.9k | 首页/SDK 页按任务书删除；日志面板合并到底部 |
| 测试 | — | 仅保留：AVD 创建、Emulator 启动、工具链路径；其余删除 |

## 3. 准备保留的模块（重写幅度不同）

| 模块 | 处置 |
|---|---|
| `internal/proc` | 保留（跨平台进程、超时、取消、进程树终止） |
| `internal/logging` | 保留（文件滚动 + 内存环 + sink） |
| `internal/job` | 保留骨架（任务列表/进度/日志/取消），去掉日志转写重复 |
| `internal/config` | 保留框架，字段大裁剪 |
| `internal/platform/fs.go` | 保留通用 IO 工具（删死函数） |
| `internal/adb` | 只保留：设备列表、等待 device、等待 boot_completed、emu kill |
| `internal/avd` | 保留 AVD 目录读写与模拟器启动，改为单文件小实现 |
| `internal/domain` | 保留被使用的类型，删掉镜像/包管理/诊断相关类型 |
| `internal/platform/singleinstance*.go` | 保留（防止两个实例同时写同一 SDK/AVD 目录） |
| 前端设计系统 | `styles/tokens.css`、`styles/app.css`、`ui.tsx`、`Chrome.tsx` 保留视觉风格 |

## 4. 目标结构

```
<软件根目录>                    exe 所在目录；不可写时回退 <用户数据目录>/AVDDesktop
├─ sdk/                        ANDROID_SDK_ROOT（软件自有）
│  ├─ cmdline-tools/latest/    sdkmanager / avdmanager（首启自动下载官方 zip）
│  ├─ platform-tools/          adb
│  ├─ emulator/                emulator
│  ├─ system-images/           sdkmanager 安装
│  └─ licenses/                sdkmanager 写入
├─ avd/                        ANDROID_AVD_HOME（软件自有）
├─ config/settings.json        软件设置
├─ logs/                       应用日志
└─ cache/                      下载临时文件
```

代码结构（目标 ~6k 行，删除约 2/3）：

```
main.go / app.go                        装配与生命周期
internal/platform/  approot.go paths.go fs.go disk_*.go singleinstance_*.go
internal/proc/      proc.go sysproc_*.go
internal/logging/   logger.go
internal/job/       manager.go
internal/config/    settings.go
internal/domain/    types.go errors.go
internal/sdk/       sdk.go（工具链/自举/列表/安装）download.go zip.go
internal/avd/       avd.go（AVD 读写 + avdmanager 创建）launch.go（emulator 启动）
internal/adb/       adb.go
internal/service/   runtime.go env.go avd.go emulator.go settings.go jobs.go log.go
internal/e2e/       e2e_test.go（3 个用例）
frontend/src/       pages/{DevicesPage,DeviceWizard,SettingsPage} + components/{Chrome,TaskDrawer,...}
```

关键数据流：

```
UI → service（参数校验 + 注册 Job）→ sdk/avd/adb（解析必要输出）→ 官方 CLI
```

## 5. 阶段计划与测试

| 阶段 | 内容 | 测试 | Commit |
|---|---|---|---|
| 0 | 审计（本文件） | 仅 `go build ./...` 基线 | `docs: add refactor audit and plan` |
| 1 | SDK/环境：自有 SDK 目录、自举 cmdline-tools、工具链路径、统一环境检查；删除自研 SDK 管理器/镜像/下载框架 | 路径与解析单测 + 真实自举 e2e（下载 cmdline-tools → `sdkmanager --list`） | `refactor: simplify android sdk management` |
| 2 | System Image + AVD：`sdkmanager` 装镜像、`avdmanager` 创建、AVD 列表/删除 | 单测（名称校验/ini 解析/镜像解析）+ e2e（真实创建 AVD 并确认存在） | `refactor: simplify avd management` |
| 3 | Emulator 启动：软件自带 emulator、端口分配、状态检测、超时、adb 检测 | e2e（启动 AVD → adb 进入 device 且 boot_completed=1） | `refactor: simplify emulator startup` |
| 4 | UI 精简：只留 设备/设置，删除首页与 SDK 页，环境检查单入口，向导只留必要参数 | `npm run build` + 页面能启动 + 创建 AVD / 启动 Emulator 可用 | `refactor: simplify application ui` |
| 5 | 日志/任务统一到底部区域，删除重复日志通道 | 任务进度/日志/错误/长任务显示 | `refactor: unify task logging` |
| 6 | 清理：死代码、死测试、无用配置、无用依赖 | `go test ./...` + 阶段 1-3 的 e2e 抽查 | `chore: remove obsolete code and tests` |

每个阶段结束：`git diff` 复查 → 只跑本阶段必要测试 → commit。
不跑无关的完整测试套件。

## 6. 风险与决策

| 风险 | 决策 |
|---|---|
| `sdkmanager` 需要 JDK | 已变更：JDK 由软件自带并自动下载到 `<Root>/jdk`；环境检查只认软件目录，系统 `JAVA_HOME` / `PATH` 不参与 |
| `sdkmanager` 许可确认会交互阻塞 | 通过 stdin 预置 `y` 行（`proc.StdinLines`），不维护许可哈希常量表 |
| `sdkmanager --list` 输出格式随版本变化 | 只解析「含 `|` 的行 → 第 1 列包路径」，单测固定样本；解析失败给明确错误 |
| 首次自举需要下载 JDK 约 200MB + cmdline-tools 约 150MB | 单连接 + 进度回调 + 超时；JDK 可选择国内 Adoptium 镜像或 GitHub 官方源（SHA-256），cmdline-tools 来自所选 SDK 镜像（SHA-1） |
| 软件根目录不可写（安装在 `Program Files`） | 回退用户数据目录，并在设置页显示实际根目录 |
| 删掉 `avdmanager` 直写后端后无 JDK 时无法创建 AVD | 已解决：`Prepare` 先补齐软件自带 JDK，后续 `avdmanager` 始终使用它 |
| 以前依赖系统 SDK 的用户会看不到旧设备 | 接受：任务书要求软件自有 SDK/AVD 目录，与系统 SDK 相互独立 |
