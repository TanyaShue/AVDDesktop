# AVDDesktop 架构设计

> 桌面端 Android 模拟器（AVD）管理器 / 启动器
> 技术栈：Wails v2 + Go 1.26 + React 19 + TypeScript + Vite 7
> 目标平台：Windows 优先（同时保留 macOS / Linux 的可移植结构）

---

## 1. 项目定位

一句话：**不装 Android Studio，也能在 Windows 上从零把 Android 模拟器跑起来的桌面工具。**

它解决三件事：

| 痛点 | 本项目能力 |
| --- | --- |
| SDK/AVD 工具链散落在命令行、下载慢、镜像难配 | **环境自检 → 镜像测速 → 一键从镜像安装** 的引导式安装流程 |
| AVD 创建依赖 `avdmanager`（要 JDK、参数难记） | 图形化创建向导 + 全量硬件配置项 + **无 JDK 回退路径** |
| 启动/多开/快照/调试分散在多个命令行工具 | 设备卡片式管理、多实例并行启动、内置 adb 面板与任务抽屉 |

### 功能范围（Scope）

**In scope**

- 工具链探测：JDK / `cmdline-tools`(sdkmanager, avdmanager) / `platform-tools`(adb) / `emulator` / 系统镜像 / 硬件加速 / 磁盘
- 镜像源管理 + 并发测速（延迟、吞吐、稳定性、可用性分级）
- 从镜像安装：cmdline-tools、platform-tools、emulator、system-images、platforms、build-tools
- AVD 生命周期：创建（向导）/ 克隆 / 重命名 / 编辑 / 删除 / 导出导入 / 清除数据
- 模拟器进程管理：启动（含启动参数）、多开（端口分配）、停止、冷启动、快照、实时状态
- adb 集成：设备列表、安装 APK、推送文件、shell、logcat、截图
- 设置、日志、诊断包导出

**Out of scope（明确不做）**

- 不做 Android 应用编译/构建（不替代 Gradle）
- 不内置 AVD 画面串流/远控（模拟器自身窗口足够；预留接口）
- 不下载 AOSP 源码、不管理 NDK 之外的大型构建生态（NDK 仅作为"可选安装包"出现在 SDK 页）

---

## 2. 总体架构

```
┌──────────────────────────────────────────────────────────────────────┐
│                        Wails v2 Application                          │
│                                                                      │
│  ┌────────────────────────┐          ┌────────────────────────────┐  │
│  │   React 19 + TS (UI)   │          │        Go 1.26 (Core)      │  │
│  │                        │          │                            │  │
│  │  pages/                │          │  service/  ← Wails 绑定层  │  │
│  │   Home  Sdk            │◄────────►│   EnvService  MirrorSvc    │  │
│  │   Devices  Settings    │  RPC     │   SdkService  AvdService   │  │
│  │                        │ (绑定方法)│   EmulatorSvc SettingsSvc  │  │
│  │  components/  (设计系统)│          │   DiagnosticsSvc           │  │
│  │  features/    (领域hooks)│        │            │               │  │
│  │  bridge/      (类型桥接)│◄────────►│  ┌─────────┴──────────┐    │  │
│  └────────────────────────┘  事件流   │  │   domain (模型)     │    │  │
│         ▲                             │  └─────────┬──────────┘    │  │
│         │  job:progress / job:log     │            │               │  │
│         │  emulator:state / speedtest │  ┌─────────▼──────────┐    │  │
│         └─────────────────────────────┤  │  job  任务管理器    │    │  │
│                                       │  └─────────┬──────────┘    │  │
│                                       │  镜像 / SDK / AVD / 宿主    │  │
│                                       │  ┌──────┬──────┬──────┐    │  │
│                                       │  │mirror│ sdk  │ avd  │    │  │
│                                       │  └──────┴──────┴──────┘    │  │
│                                       │  ┌──────────────────────┐  │  │
│                                       │  │ platform/proc/dl/zip │  │  │
│                                       │  └──────────┬───────────┘  │  │
│                                       └─────────────┼──────────────┘  │
└─────────────────────────────────────────────────────┼────────────────┘
                                                      │ os/exec / net/http
                          ┌───────────────────────────▼─────────────────────────┐
                          │ 文件系统：SDK 根 / AVD 主目录 / %LOCALAPPDATA% 配置  │
                          │ 外部进程：java(sdkmanager, avdmanager) emulator adb  │
                          │ 网络：镜像站 / dl.google.com  (HTTPS)                │
                          └─────────────────────────────────────────────────────┘
```

**分层规则（单向依赖，禁止反向）**

```
service  →  { mirror, sdk, avd, adb, settings }  →  { platform, proc, download, archive, job }  →  domain
```

- `domain` 只放数据结构，零外部依赖（可生成 TS 类型）
- `service` 只做「参数校验 → 调用领域服务 → 注册 Job → 返回 jobID」，不写业务逻辑
- 领域层不感知 Wails；事件通过 `job.Manager` 的广播接口解耦
- 所有外部进程调用统一走 `proc`（隐藏窗口、UTF-8 解码、超时、取消、行流式回传）
- 所有网络下载统一走 `download`（镜像 base-url、断点续传、进度、代理、限速、SHA-1 校验）

---

## 3. 关键设计决策（ADR 摘要）

### ADR-01 自研包下载器，而不是只调用 `sdkmanager --install`

| | |
| --- | --- |
| **决策** | 自行实现「解析 repository XML + 下载 + 校验 + 解压 + 写 licenses」的安装链路；`sdkmanager` 保留为兼容/兜底通道 |
| **理由** | ① 镜像支持：`sdkmanager` 只能通过 HTTP 代理换源，无法直接指向镜像的目录镜像；我们直接重写 XML 中的相对 URL 到镜像 base ② 不依赖 JDK：`emulator`（native）与包下载都不需要 Java，缺 JDK 也能装 ③ 体验：可做多连接分片、断点续传、精确进度/限速/ETA；`sdkmanager` 输出为进度条文本，解析脆弱 ④ 可控：可在解压前校验 SHA-1、可原子替换、可检测"文件被占用" |
| **代价** | 需要自己维护包依赖解析与许可写入逻辑 → 已通过实测固化（见 `RESEARCH-NOTES.md`） |
| **兜底** | 若某镜像 XML 可用但文件缺失/校验失败 → 自动回退到 Google 官方源下载同一包 |

### ADR-02 AVD 操作走可替换的 `AvdBackend` 抽象，而非直接绑定 `avdmanager`

| | |
| --- | --- |
| **决策** | 定义 `AvdBackend` 接口，提供两个实现：`AvdManagerBackend`（调用 `avdmanager.exe`，需要 JDK）和 `DirectBackend`（自己写 `.ini` / `config.ini`，无需 JDK）；未来可加 `AndroidCliBackend` |
| **理由** | ① `avdmanager` 官方已标注 deprecated（新方向是 Android CLI 的 `android emulator` 命令），抽象层让未来替换只写一个实现 ② JDK 缺失/损坏时仍能创建与启动 AVD（`emulator.exe` 是原生的，不需要 Java） ③ 直接写配置文件才能支持"高级 ini 直编"与未被 CLI 暴露的硬件项 |
| **策略** | 默认优先 `avdmanager`（保证 device profile hash、skin、权限位与官方一致），失败或不可用时自动降级 `DirectBackend` 并提示 |

### ADR-03 所有长任务统一为 Job + 事件流，不用同步阻塞 RPC

| | |
| --- | --- |
| **决策** | 测速 / 下载 / 安装 / 创建 AVD / 启动模拟器 全部登记为 `Job`，绑定方法立即返回 `jobID`；进度与日志通过 Wails 事件推送 |
| **理由** | ① 系统镜像单包 ~1.9 GB，必然需要进度与取消 ② 前端可关弹窗后台继续（任务抽屉）③ 统一节流（≤10 事件/秒）避免 Wails 序列化抖动 ④ 便于"一键准备环境"这种任务串联 |
| **约束** | 同一 SDK 根目录**同时只允许一个写任务**（`sdk` 包级互斥锁）；AVD 目录同理 |

### ADR-04 镜像兼容性分级，而不是"能连上就用"

| | |
| --- | --- |
| **决策** | 每个镜像源探测后给出 `Grade`：`full`（索引+包文件均可用）/ `index-only`（仅 XML）/ `invalid`（非目录镜像）/ `unreachable` |
| **理由** | 实测发现各镜像差异极大：目录镜像（腾讯云）可完整替代；部分镜像只有索引或返回 403/404（实测 TUNA/USTC/BFSU/东软均不可用于 HEAD 探测）；还有镜像不支持 Range 请求 |
| **影响** | 安装前校验目标源等级；`index-only` 源允许「索引用镜像、文件用官方」的混合模式 |

### ADR-05 无边框窗口 + 自绘标题栏

| | |
| --- | --- |
| **决策** | Wails `Frameless: true` + 前端自绘标题栏（`--wails-draggable: drag`），对齐参考图的顶部栏与左侧图标导航栏 |
| **理由** | 参考设计（MuMu 模拟器）是统一的自绘标题栏风格；Wails 原生标题栏无法做同色/同高/同圆角 |
| **代价** | 需自己实现最小化/最大化/关闭三个窗口按钮（Wails runtime：`WindowMinimise` / `WindowToggleMaximise` / `Quit`） |

---

## 4. 后端模块划分

| 包 | 职责 | 关键内容 |
| --- | --- | --- |
| `internal/domain` | 领域模型（纯数据） | `EnvReport` `ToolStatus` `MirrorSource` `SpeedSample` `SdkPackage` `SystemImage` `DeviceProfile` `AvdSummary` `AvdSpec` `HwConfigItem` `EmulatorInstance` `JobInfo` `AppSettings` |
| `internal/config` | 用户配置持久化 | `settings.json` 原子写、版本迁移、默认值、路径覆盖 |
| `internal/logging` | 日志 | 结构化、文件滚动（7 天）、内存环形缓冲（供 UI 实时看） |
| `internal/platform` | 宿主环境 | SDK/AVD 路径解析、候选路径枚举、环境变量注入、单实例锁、磁盘空间、Windows 功能检测（WHPX/AEHD/虚拟化）、打开目录/链接、文件选择对话框 |
| `internal/proc` | 进程执行 | 隐藏控制台窗口（Windows `HideWindow`）、UTF-8 输出解码、实时行回调、超时、`context` 取消、进程树终止 |
| `internal/download` | HTTP 下载 | 镜像 base-url 重写、Range 分片并发、断点续传（`.part` + ETag/Size 校验）、SHA-1 校验、进度回调、代理、限速、重试退避 |
| `internal/archive` | 解压 | 安全 zip（拒绝 `..`/绝对路径/符号链接越界）、保留可执行位、解压到 `*.tmp` 后原子 rename |
| `internal/job` | 任务编排 | Job 注册/取消/查询、进度聚合、日志转发、事件节流、并发闸门（信号量） |
| `internal/mirror` | 镜像源 | 内置源表、自定义源 CRUD、测速引擎（DNS/TCP/TTFB/吞吐/抖动）、评分、结果缓存（TTL） |
| `internal/sdk/repo` | 仓库客户端 | 解析 `repository2-3.xml` / `sys-img*/sys-img2-3.xml` / `addon2-3.xml`；相对 URL 重写；依赖递归；channel（stable/beta/canary）；host-os/arch 选择 |
| `internal/sdk/licenses` | 许可 | license id → hash 常量表（实测值）、写 `licenses/<id>`、许可文本读取 |
| `internal/sdk/detect` | 工具链探测 | 组件扫描、`source.properties` 版本读取、工具可用性实跑校验（`--version` / `-list-avds` / `-accel-check`） |
| `internal/sdk/query` | 本地包扫描 | 遍历 SDK 目录读 `source.properties` → 已安装包列表与可更新判定 |
| `internal/sdk/install` | 安装编排 | 解析依赖树 → 生成安装计划 → 下载 → 校验 → 解压 → 落盘 → 写许可 → 记录（含 bootstrap 特例） |
| `internal/avd/profile` | 设备档案 | 解析 `avdmanager list device` 输出（含 `-c` 紧凑模式）与备用内置档案 |
| `internal/avd/store` | AVD 存储 | AVD 主目录解析、`<name>.ini` / `<name>.avd/config.ini` 读写、硬件项 schema（类型/默认值/范围/中文说明）、命名校验、克隆（含 `hw.device.hash2` 处理） |
| `internal/avd/backend` | AVD 操作后端 | `AvdBackend` 接口 + `AvdManagerBackend` + `DirectBackend` |
| `internal/avd/launch` | 模拟器进程 | 端口分配（5554+2n 空闲探测）、启动参数组装、进程句柄、状态机、日志、退出码归因、`adb emu kill` 优雅停止 |
| `internal/adb` | adb 客户端 | `devices -l` 解析、`-s <serial> install/push/shell/logcat/exec-out screencap`、`emu avd name` 关联 AVD、按键/旋转/网络等 emu 命令 |
| `internal/service` | Wails 绑定层 | `EnvService` `MirrorService` `SdkService` `AvdService` `EmulatorService` `SettingsService` `DiagnosticsService`（全部只做薄封装） |

---

## 5. 目录结构（目标态）

```
AVDDesktop/
├── main.go                      # Wails 应用装配、窗口选项、绑定
├── app.go                       # App 结构体：构造并持有各 Service
├── docs/                        # 设计文档（本目录）
│   ├── ARCHITECTURE.md          # ← 本文
│   ├── API-CONTRACT.md          # 前后端接口契约
│   ├── UI-SPEC.md               # UI 设计规范与页面规格
│   └── RESEARCH-NOTES.md        # 实测调研结论（镜像/包/许可/CLI）
├── internal/
│   ├── domain/                  # 领域模型
│   ├── config/                  # settings.json
│   ├── logging/                 # 日志
│   ├── platform/                # 宿主环境、路径解析、对话框、单实例
│   ├── proc/                    # 进程执行
│   ├── download/                # 下载器
│   ├── archive/                 # 安全解压
│   ├── job/                     # 任务管理
│   ├── mirror/                  # 镜像源 + 测速
│   ├── sdk/
│   │   ├── repo/                # 仓库 XML 客户端
│   │   ├── licenses/            # 许可哈希
│   │   ├── detect/              # 工具链探测
│   │   ├── query/               # 本地包扫描
│   │   └── install/             # 安装编排
│   ├── avd/
│   │   ├── profile/             # 设备档案
│   │   ├── store/               # AVD 配置读写 + schema
│   │   ├── backend/             # avdmanager / direct 后端
│   │   └── launch/              # 模拟器进程管理
│   ├── adb/                     # adb 客户端
│   └── service/                 # Wails 绑定服务
└── frontend/
    ├── src/
    │   ├── app/                 # 应用壳、路由、Provider、标题栏
    │   ├── bridge/              # wailsjs 包装 + 领域类型 TS 映射
    │   ├── components/ui/       # 设计系统原子组件
    │   ├── components/          # 业务组件
    │   ├── features/            # 领域 hooks / 状态
    │   ├── pages/               # Home / Sdk / Devices / Settings
    │   ├── styles/tokens.css    # 设计令牌
    │   └── i18n/
    └── wailsjs/                 # 自动生成（勿手改）
```

---

## 6. 核心流程

### 6.1 环境自检（首页）

**目标**：一屏看清"缺什么、能不能跑、下一步点哪里"。

```
启动
 └─ EnvService.Detect()
     ├─ 1) 定位 SDK 根目录（多来源优先级）
     │     settings.sdkRoot
     │     → 环境变量 ANDROID_HOME / ANDROID_SDK_ROOT
     │     → 常见路径 %LOCALAPPDATA%\Android\Sdk
     │                 %USERPROFILE%\AppData\Local\Android\Sdk
     │                 C:\Android\Sdk / C:\Android\sdk
     │                 %ProgramFiles%\Android\Sdk
     │                 Android Studio 安装目录下的 sdk
     │     → PATH 中 adb/emulator 所在目录的上溯
     │     → Chocolatey / Scoop / 便携目录
     │   每个候选做"有效性打分"（存在 cmdline-tools / platform-tools / emulator 目录）
     ├─ 2) 定位 AVD 主目录（见 §9.4）
     ├─ 3) JDK 探测
     │     Settings.jdkPath → JAVA_HOME → PATH 中 java → Android Studio 自带 jbr
     │     → 运行 `java -version` 取版本；要求 ≥ 17（cmdline-tools 12+ 要求）
     ├─ 4) 组件探测（并行）
     │     cmdline-tools : 存在 sdkmanager.bat / avdmanager.bat + 读 source.properties 版本
     │     platform-tools: 存在 adb.exe + `adb --version`
     │     emulator      : 存在 emulator.exe + `emulator -version`（读取 Pkg.Revision）
     │     系统镜像      : 扫描 system-images/**/source.properties（API/tag/ABI/是否 Google Play）
     │     加速          : `emulator -accel-check`（退出码 0 = 可用；输出含 WHPX/AEHD/HAXM 详情）
     │     磁盘          : SDK 分区剩余空间（阈值提示：≥ 30 GB 舒适 / ≥ 12 GB 最低）
     │     配置可写      : SDK 根、AVD 主目录是否可写
     └─ 4) 汇总为 EnvReport：每个组件 {状态, 版本, 路径, 问题, 修复建议, 修复动作}
```

**组件状态机**

```
unknown → missing          未安装 → [安装]
        → present          正常   → 显示版本/路径
        → outdated         有更新 → [更新]
        → broken           文件在但跑不起来（缺 JDK / 被杀软拦截 / 权限） → [修复]
        → incompatible     版本过低（如 JDK < 17、emulator < 33） → [升级]
```

**首启引导（Onboarding）**：当 `cmdline-tools` 缺失时，首页顶部显示「一键准备开发环境」主按钮，按序执行：

```
① 选择镜像源（弹测速窗口，已选则跳过）
② cmdline-tools（bootstrap：直接下载 zip 解压到 cmdline-tools/latest）
③ 写 licenses（许可确认后）
④ platform-tools（adb）
⑤ emulator
⑥ （可选）下载一个系统镜像
⑦ 硬件加速检查（WHPX 已开 / AEHD 驱动安装引导）
```

每一步是一个 Job，UI 显示步骤条 + 进度 + 可取消；失败给出可操作错误（如"需要开启 Hyper-V 的 Windows 虚拟机监控程序平台"）。

### 6.2 镜像测速弹窗

见 `UI-SPEC.md` §5.3 与 `RESEARCH-NOTES.md` §1。算法：

```
对每个源（并发，每源 ≤2 连接，全局 ≤6）：
  t0 = now
  ① DNS 解析（net.Resolver，超时 3s）               → dnsMs, ips[]
  ② TCP/TLS 建连（到 repository2-3.xml）            → connectMs
  ③ Range GET <base>/repository2-3.xml 前 64 KiB    → ttfbMs, httpStatus, 是否 206
  ④ XML 解析 + 关键包存在性（cmdline-tools;latest / emulator）→ grade
  ⑤ 吞吐采样：GET <base>/<某真实包> Range 0-4MiB
        - 若 206 → 读满 4 MiB（或 8s 上限）
        - 若 200（不支持 Range）→ 读满 4 MiB 后主动 abort，标记 noRange
        - 超时/失败 → 退化：只对 XML 文件测吞吐
     → throughputMBps, jitterMs（3 次采样取中位数 + 极差）
  ⑥ 综合评分 score = 100 * (0.25*latencyScore + 0.5*throughputScore + 0.25*stabilityScore)
     latencyScore    = clamp(1 - ttfbMs/1500)
     throughputScore = clamp(throughputMBps / 20)
     stabilityScore  = clamp(1 - jitterMs/500)
结果逐条事件推送（speedtest:result），弹窗内实时刷新
```

**输出**：每个源显示 状态徽标（推荐 / 可用 / 仅索引 / 不可用）、延迟、下载速度、评分；选中一个作为 `activeSourceId`，持久化到 settings。

### 6.3 从镜像安装（安装编排）

```
Install(pkgs []path, sourceID)
 ① 取仓库索引：GET <base>/repository2-3.xml (+ 需要的 sys-img*/sys-img2-3.xml)
    本地缓存 10 分钟 + ETag/Last-Modified 条件请求
 ② 解析目标包与依赖（<dependency path=...>）→ 拓扑排序 → InstallPlan
 ③ 许可：收集 plan 内所有 uses-license ref
    → 若 settings.autoAcceptLicenses 或用户点击"同意" → 写 licenses/<id>
    → 否则中断并返回许可文本（UI 展示）
 ④ 逐个包：下载（分片/续传/进度）→ SHA-1 校验 → 安全解压到 <sdk>/<path>.tmp → 原子替换
     特例 bootstrap：commandlinetools-win-*_latest.zip 内层为 cmdline-tools/
        → 必须落到 <sdk>/cmdline-tools/latest/（并补 source.properties）
     特例 占用：emulator/ 下 exe 正被运行中的模拟器占用 → 先提示停止实例或延迟到退出后替换
 ⑤ 写 .knownPackages 等 SDK 书签文件（尽力，失败不影响）
 ⑥ 结束：刷新本地包索引 → 事件 job:done
```

包路径 → 目录映射（实测）：`system-images;android-36.1;google_apis;x86_64` → `system-images/android-36.1/google_apis/x86_64/`；`cmdline-tools;latest` → `cmdline-tools/latest/`；`platform-tools` → `platform-tools/`；`emulator` → `emulator/`。

### 6.4 创建 AVD

```
Create(spec AvdSpec)
 ① 校验：名称（^[A-Za-z0-9._-]{1,64}$、不与已有 AVD/目录冲突）、system image 已安装、目标目录可写
 ② 选后端：AvdManagerBackend（默认） / DirectBackend（无 JDK 或高级项）
 ③ AvdManagerBackend：
      avdmanager create avd -n <name> -k <sysimg> -d <profileId>
                 [-c <sdcardSize>] [--path <dir>] [-f]
      stdout 交互（"Do you wish to create a custom hardware profile? [no]"）→ 自动回 "no"
 ④ 后处理（两种后端都要做）：
      写 config.ini 增量项（RAM/核数/存储/GPU/网络/传感器/相机…）
      写 avd.ini.displayname（中文/带空格显示名）
      记录本项目的元数据：<avd>/avddesktop.json（创建时间、来源镜像源、模板名、启动预设）
 ⑤ 校验：读回 config.ini 解析成功；必要时 emulator -list-avds 验证可见
 ⑥ 事件 avd:changed + 返回 AvdSummary
```

### 6.5 启动 / 管理模拟器实例

```
Start(name, launchOpts)
 ① 端口分配：从 5554 起，步长 2，探测 console port 与 adb port(+1) 均空闲
 ② 组装参数：emulator -avd <name> -port <p> [-no-snapshot-load] [-wipe-data] [-no-window]
                      [-gpu <mode>] [-netdelay/-netspeed] [-timezone] [-no-boot-anim]
                      [-writable-system] [-http-proxy ...] + 用户自定义参数
 ③ 启动进程（隐藏窗口、捕获日志），登记 EmulatorInstance{serial: emulator-<port>}
 ④ 状态机：starting → booting（adb wait-for-device + getprop sys.boot_completed）→ running → stopping → stopped/failed
 ⑤ 心跳：每 2s adb devices -l + getprop 校验，异常退出 → 归因（accel / 缺镜像 / 端口占用 / 显卡驱动）
 ⑥ 停止：优先 `adb -s <serial> emu kill`，超时后 terminate 进程树
```

**多开**：每个实例独立端口、独立 datadir（AVD 目录内的 `*.img`/`snapshots`），共享同一 system image（只读）。同一 AVD 默认禁止并发启动（可用「克隆后多开」）。

---

## 7. 任务与并发模型

```go
type Job struct {
    ID        string
    Kind      string   // "speedtest" | "download" | "install" | "sdk-bootstrap" | "avd-create" | "emulator-start"
    Title     string
    Percent   float64
    Phase     string
    BytesDone int64
    BytesTotal int64
    SpeedBps  int64
    ETASeconds int
    Logs      []LogLine     // 环形缓冲，UI 可回看
    Status    JobStatus     // queued/running/succeeded/failed/canceled
    Err       *AppError
}

type Manager interface {
    Start(ctx context.Context, spec JobSpec) (*Job, context.Context)
    Get(id string) (*Job, bool)
    List() []*Job
    Cancel(id string) error
    CancelAll()
    Subscribe(fn func(Event))   // 由 service 层桥接到 Wails 事件
}
```

- **闸门**：`globalDownloadSem = 4`（分片连接）、`sdkWriteLock`（同一 SDK 根只允许一个写任务）、`avdWriteLock`
- **事件节流**：`progress` 合并到 10 Hz；`log` 批量 50 行/200 ms
- **取消**：所有下游（http.Request、解压循环、exec.Cmd）都接受 `context.Context`
- **崩溃恢复**：`.part` 文件保留，下次安装可续传；启动时清理超期的 `*.tmp` 目录

---

## 8. 数据模型与持久化

### 8.1 应用数据目录

```
%LOCALAPPDATA%\AVDDesktop\
├── settings.json          # 用户设置（原子写：写 tmp → rename）
├── cache\
│   ├── repository2-3.xml  # 仓库索引缓存 + meta.json(ETag/时间)
│   ├── sys-img-*.xml
│   └── speedtest.json     # 测速结果（TTL 1h）
├── logs\app-YYYYMMDD.log
└── downloads\             # 大包临时目录（可配置到其他盘）
```

### 8.2 设置项（`AppSettings`）

| 分组 | 字段 | 说明 |
| --- | --- | --- |
| 环境 | `sdkRoot` / `jdkPath` / `avdHome` | 可覆盖自动探测结果 |
| 环境 | `injectEnvForChildren` | 启动子进程时注入 `ANDROID_HOME` / `ANDROID_AVD_HOME` / `PATH` |
| 镜像 | `activeSourceId` / `customSources[]` / `autoFallbackToOfficial` | 主动源与自定义源 |
| 下载 | `maxConnectionsPerFile` / `maxParallelPackages` / `timeoutSeconds` / `speedLimitKBps` / `proxyMode(off/system/custom)` / `proxyURL` / `downloadDir` | 下载行为 |
| 许可 | `acceptedLicenseIds[]` / `autoAcceptLicenses` | 许可自动确认 |
| AVD | `defaultDeviceProfile` / `defaultRamMB` / `defaultCores` / `defaultDataPartitionGB` / `defaultGpuMode` / `launchPresets[]` | 创建默认值 |
| UI | `theme(light/dark/system)` / `language(zh-CN/en-US)` / `deviceViewMode(grid/list)` / `confirmBeforeDelete` / `showTaskDrawer` | 外观与交互 |
| 高级 | `logLevel` / `keepLogDays` / `askBeforeDriverInstall` / `singleInstanceLock` | 诊断 |

### 8.3 领域模型要点

- `ToolStatus`：`{id, name, state, version, path, detail, fixKind, fixLabel, sizeBytes?}`，`fixKind ∈ {install, update, repair, setJdk, enableWhpx, installAehd, downloadImage, choosePath}`
- `MirrorSource`：`{id, name, baseURL, kind(official|mirror|custom), grade, enabled, lastResult}`
- `SpeedResult`：`{sourceId, ok, dnsMs, connectMs, ttfbMs, throughputMBps, jitterMs, httpStatus, rangeSupported, xmlOK, hasCmdlineTools, hasEmulator, score, grade, error, at}`
- `SdkPackage`：`{path, displayName, revision, channel, sizeBytes, checksumSHA1, installed, installedRevision, licenseId, dependencies[], kind}`
- `SystemImage`：`{path, apiLevel, apiString, tagId, tagDisplay, abi, vendor, isPlaystore, installed, sizeBytes, revision}`
- `DeviceProfile`：`{id, index, name, oem, tag, category, diagonalInches?, resolution?, density?}`
- `AvdSpec`：`{name, displayName, profileId, systemImagePath, path?, sdcardSize?, hw HwOverrides, launchDefaults LaunchOptions, createWithAvdManager bool}`
- `HwOverrides`：`map[key]value`，key 为 config.ini 的键（见 `avd/store/schema.go` 的完整表）
- `EmulatorInstance`：`{id, avdName, serial, port, pid, state, startedAt, exitCode?, lastError?, localScreenshotAt?}`
- `AppError`：`{code, message, detail, hint, actions[]}`（`code` 稳定，供前端做差异化 UI）

---

## 9. Android SDK 领域知识（实现依据，均已实测）

### 9.1 工具链与依赖关系

| 组件 | 可执行文件 | 是否需 JDK | 作用 | 包路径 |
| --- | --- | --- | --- | --- |
| Command-line Tools | `sdkmanager.bat` `avdmanager.bat` | **是**（≥ 17） | 包管理 / AVD 管理 | `cmdline-tools;latest` |
| Platform-Tools | `adb.exe` `fastboot.exe` | 否 | 设备/实例通信 | `platform-tools` |
| Emulator | `emulator.exe` | **否** | 模拟器引擎 | `emulator` |
| System Image | — | 否 | 系统镜像（AVD 必装） | `system-images;<api>;<tag>;<abi>` |
| Platform | — | 否 | 编译目标（非模拟器必需） | `platforms;android-<api>` |
| Build-Tools | `aapt2.exe` 等 | 否 | 构建工具（非必需） | `build-tools;<ver>` |
| AEHD 驱动 | — | 否 | Windows 加速（HAXM 后继） | `extras;google;Android_Emulator_Hypervisor_Driver` |

> 关键结论：**只有 `cmdline-tools` 需要 Java**。这支撑了 ADR-02 的"无 JDK 回退路径"。

### 9.2 仓库索引与镜像重写（实测）

| 索引文件 | 覆盖内容 | 相对 URL 基准 |
| --- | --- | --- |
| `repository2-3.xml` | cmdline-tools / platform-tools / emulator / platforms / build-tools / extras / add-ons | **仓库根** |
| `sys-img/<tag>/sys-img2-3.xml` | 系统镜像（`google_apis`、`google_apis_playstore`、`android-desktop`、`android-tv`、`android-automotive` 等 tag 各自一个文件） | **该 XML 所在目录** |
| `addon2-3.xml` | add-on（Google Maps 等） | 仓库根 |

要点：

1. 归档 `url` 是**相对路径**（如 `emulator-windows_x64-16349944.zip`、`x86_64-36.1_r04.zip`）→ 把 base 换成镜像根即可整站换源。
2. `emulator` / `cmdline-tools` 的归档带 `<host-os>`（windows/macosx/linux）与可选的 `<host-arch>`（x64/aarch64）→ 必须按平台筛选；**系统镜像的归档不带 host-os**。
3. 每个归档有 `<size>` 与 `<checksum type="sha1">` → 下载后强制校验。
4. `<dependency path="...">` 需递归解析（如系统镜像依赖 `emulator#<ver>`）。
5. `<uses-license ref="android-sdk-license">` 决定需要写哪个许可文件。

### 9.3 许可（licenses）

`<sdk>/licenses/<licenseId>` 文件内容 = 该许可的 SHA-1 哈希（每行一个，sdkmanager 写入）。实测常量：

| license id | hash |
| --- | --- |
| `android-sdk-license` | `24333f8a63b6825ea9c5514f83c2829b004d1fee` |
| `android-sdk-preview-license` | `84831b9409646a918e30573bab4c9c91346d8abd` |
| `android-sdk-arm-dbt-license` | `859f317696f67ef3d7f30a50a5560e7834b43903` |
| `android-googletv-license` | `601085b94cd77f0b54ff86406957099ebe79c4d6` |
| `android-googlexr-license` | `ceff83576aac4f7f37cb98fe189e9fb3c49d3b81` |
| `google-gdk-license` | `33b6a2b64607f11b759f320ef9dff4ae5c47d97a` |
| `mips-android-sysimage-license` | `e9acab5b5fbb560a72cfaecce8946896ff6aab9d` |

设计：许可文本从仓库 XML 的 `<license>` 节点取，UI 展示并要求用户确认；确认后写哈希。这样既不自动替用户同意，也不需要解析 `sdkmanager --licenses` 的交互式输出。

### 9.4 AVD 主目录解析（实测规则）

优先级（与官方工具一致）：

```
1. ANDROID_AVD_HOME                    → $ANDROID_AVD_HOME        （直接就是 avd 目录）
2. ANDROID_USER_HOME                   → $ANDROID_USER_HOME/avd
3. ANDROID_SDK_HOME  （已废弃但仍在用）  → $ANDROID_SDK_HOME/.android/avd
4. ANDROID_PREFS_ROOT                  → $ANDROID_PREFS_ROOT/.android/avd
5. 默认                                → %USERPROFILE%\.android\avd
6. 兜底                                → <SDK>/avd（若存在）
```

> 实测本机：`ANDROID_SDK_HOME=C:\Android` → AVD 落在 `C:\Android\.android\avd\`，而不是 `%USERPROFILE%\.android\avd`。**必须先按此规则解析，否则会"看不到"用户的设备。**

AVD 文件结构：

```
<avdHome>/
├── <Name>.ini                # 指向 .avd 目录：path / path.rel / target
└── <Name>.avd/
    ├── config.ini            # 硬件与运行配置（见 §9.5）
    ├── hardware-qemu.ini     # 由 emulator 首次启动生成（运行时写，勿手改）
    ├── userdata-qemu.img / cache.img / sdcard.img / snapshots/
    └── avddesktop.json       # 本项目元数据（自动生成）
```

`.ini` 示例（实测）：

```ini
avd.ini.encoding=UTF-8
path=C:\Android\.android\avd\Medium_Phone.avd
path.rel=avd\Medium_Phone.avd
target=android-36.1
```

### 9.5 `config.ini` 配置项（AVD 的"具体有哪些配置"）

分类整理（完整 schema 见 `internal/avd/store/schema.go`，UI 见 `UI-SPEC.md` §5.4）：

**基础 / 身份**
`AvdId`、`avd.ini.displayname`、`avd.ini.encoding`、`target`、`abi.type`、`image.sysdir.1`、`tag.id` / `tag.ids` / `tag.display` / `tag.displaynames`、`PlayStore.enabled`、`hw.device.name`、`hw.device.manufacturer`、`hw.device.hash2`

**计算资源**
`hw.ramSize`(MB)、`vm.heapSize`(MB)、`hw.cpu.arch`、`hw.cpu.ncore`、`hw.cpu.model`、`hw.gpu.enabled`、`hw.gpu.mode`(`auto|host|swiftshader_indirect|angle_indirect|guest`)、`hw.gpu.option`

**存储**
`disk.dataPartition.size`(如 `6G`)、`disk.cachePartition`、`disk.cachePartition.size`、`hw.sdCard`、`sdcard.size`、`hw.sdCard.path`、`disk.systemPartition.size`、`disk.vendorPartition.size`

**显示 / 外观**
`hw.lcd.width`、`hw.lcd.height`、`hw.lcd.density`、`hw.initialOrientation`(`portrait|landscape`)、`skin.name`、`skin.path`、`skin.dynamic`、`showDeviceFrame`、`hw.keyboard`、`hw.dPad`、`hw.mainKeys`、`hw.trackBall`、`hw.arc`

**输入 / 传感器**
`hw.audioInput`、`hw.audioOutput`、`hw.accelerometer`、`hw.gyroscope`、`hw.battery`、`hw.gps`、`hw.sensors.proximity`、`hw.sensors.light`、`hw.sensors.magnetic_field`、`hw.sensors.orientation`、`hw.sensors.pressure`、`hw.sensors.temperature`、`hw.sensors.heart_rate`、`hw.multitouch`

**相机**
`hw.camera.back` / `hw.camera.front`（`none|emulated|virtualscene|webcam0`）、`hw.camera.back.orientation`

**网络 / 位置**
`runtime.network.latency`(`none|gprs|edge|umts|hsdpa|full`)、`runtime.network.speed`、`hw.dns1`/`hw.dns2`、`hw.httpProxy`、`hw.cpu.ncore`、`hw.timezone`、`hw.initialLocale`（部分由启动参数覆盖）

**启动行为**
`fastboot.forceColdBoot`、`fastboot.forceFastBoot`、`fastboot.forceChosenSnapshotBoot`、`fastboot.chosenSnapshotFile`、`hw.arc`、`firstboot.bootFromDownloadableSnapshot`、`snapshot.present`、`vm.heapSize`

**本项目扩展**（写入 `avddesktop.json`，不污染 config.ini）
启动预设（默认参数组合）、标签/分组、备注、上次启动时间、关联镜像源、创建模板快照

### 9.6 硬件加速判定（实测）

- 权威判定：`emulator -accel-check` → 退出码 0 且输出 `WHPX(10.0.26200) is installed and usable.`
- 输出取值：`WHPX` / `AEHD` / `HAXM` / `gvm` / 无（并给可读原因）
- 建议映射：
  - 有 WHPX → 直接用，无需额外安装
  - 无 WHPX 但有 VT-x/AMD-V → 引导开启 Windows 功能「虚拟机监控程序平台（HypervisorPlatform）」+「虚拟机平台（VirtualMachinePlatform）」，或安装 AEHD（`extras;google;Android_Emulator_Hypervisor_Driver`，需管理员 + 重启）
  - 同时启用 Hyper-V 与 AEHD 会冲突 → 给出明确原因与二选一建议
  - 注意：AEHD 官方将于 2026-12-31 停止支持，长期方案是 WHPX → UI 文案优先推荐 WHPX
- 无加速也可启动，但提示「启动会很慢」（可用 `-no-accel`、ARM 镜像另说）

---

## 10. 风险与兜底

| 风险 | 触发场景 | 兜底策略 |
| --- | --- | --- |
| 镜像不可用 / 只镜像部分文件 | 第三方镜像同步滞后 | 兼容性分级 + 自动回退官方源 + 手动"混合模式"（索引用镜像、文件用官方） |
| 镜像不支持 Range | 无法分片下载 | 单连接顺序下载 + 续传（按已写字节 `Range: bytes=N-`），若也不支持续传则重下并提示 |
| 无 JDK 或 JDK 版本过低 | 用户只有 JRE/旧 JDK | 复用 Android Studio 自带 `jbr`；否则引导设置 JDK 路径；**AVD 全流程走 `DirectBackend` + 原生 emulator** |
| `emulator` 目录被运行中进程占用 | 更新 emulator 时 | 安装前检测运行实例 → 提示"停止 N 个实例后继续"或"稍后自动重试" |
| 系统镜像 ~2 GB 下载失败 | 网络抖动 | 断点续传 + 分片重试 + 校验失败自动重下该分片 |
| 端口占用 / 多开冲突 | 多实例 | 5554+2n 逐个探测；显示已占用端口 |
| WHPX/AEHD 不可用 | 未开虚拟化 | `-accel-check` 归因 + 分步引导（含 bcdedit/Win 功能开关说明与需要重启的提示） |
| Windows 长路径 (>260) | 深层 SDK 路径 | 提示使用短路径；解压使用 `\\?\` 前缀（Windows） |
| 杀软/Defender 拦截 | 解压 exe/dll | 明确错误提示与日志，不静默重试 |
| SDK 根目录权限不足 | Program Files | 检测可写性，推荐 `%LOCALAPPDATA%` 或用户自选 |
| 多实例并发写 SDK | 用户狂点安装 | `sdkWriteLock` 串行化 + UI 按钮禁用 |
| `avdmanager` 未来被移除 | 官方弃用 | `AvdBackend` 抽象 + 计划中的 `AndroidCliBackend` |

---

## 11. 里程碑与验收标准

| 里程碑 | 交付 | 验收标准（可执行的检查） |
| --- | --- | --- |
| **M0 骨架** | Wails 接线、无边框窗口、设计令牌、左导航 + 标题栏、任务抽屉、日志/配置、Job 管理器 | `wails dev` 启动；窗口无边框可拖拽/最小化/关闭；任务抽屉能显示一个假任务的进度与日志 |
| **M1 环境自检** | `sdk/detect` + 首页状态卡片 | 本机（SDK 在 `%LOCALAPPDATA%\Android\Sdk`、AVD 在 `C:\Android\.android\avd`）能正确识别 cmdline-tools/platform-tools/emulator/系统镜像/加速，版本号与 `emulator -version` 一致 |
| **M2 镜像测速** | `mirror` + 测速弹窗 | 弹窗中同时测「Google 官方 + 腾讯云 + 自定义源」；显示延迟/速度/评分；选中的源持久化 |
| **M3 工具链安装** | `download`/`archive`/`licenses`/`sdk/install` + bootstrap | 在**空目录**上从腾讯云安装 cmdline-tools + platform-tools + emulator 成功；`sdkmanager --version`、`adb --version`、`emulator -version` 全部可运行；断网重连能续传；SHA-1 校验失败能被识别 |
| **M4 系统镜像** | sys-img 索引 + 大文件下载 + 依赖解析 | 能列出各 API/tag/ABI 镜像并显示大小；安装一个 ~2 GB 镜像成功且 `source.properties` 版本正确 |
| **M5 设备管理** | `avd/*` + 设备页 + 创建向导 + 启动 | 用向导创建 AVD（选 medium_phone + android-36.1 google_apis_playstore）并成功启动到开机完成；`adb devices` 显示 `emulator-5554`；多开第二个实例不冲突；`avdmanager list avd` 能看到我们创建的设备 |
| **M6 高级能力** | 快照、克隆/导出、adb 面板（安装 APK/日志/截图）、设置页、诊断包 | 快照保存/恢复可用；APK 能在运行实例上安装；日志可导出 |
| **M7 打磨** | 深色主题、i18n、安装包（NSIS）、图标 | 生成 MSI/EXE 安装包并在干净机器上完成 M1–M5 全流程 |

---

## 12. 非功能需求

- **启动性能**：冷启动到首页可交互 < 1.5 s；自检在后台并行执行，先渲染骨架屏
- **安全**：仅 HTTPS；zip 解压防目录穿越；下载内容 SHA-1 校验；执行外部程序只允许白名单（emulator/adb/java/sdkmanager/avdmanager），不拼接 shell 字符串，一律 `exec.CommandContext(name, args...)`
- **隐私**：不采集遥测；仅访问用户配置的镜像/官方源
- **可观测**：结构化日志（级别、模块、耗时）；诊断包 = 设置 + 环境报告 + 最近 3 天日志（脱敏用户目录）
- **国际化**：文案走 i18n（默认 zh-CN），错误信息代码化（`AppError.code`）而非硬编码中文
- **可测试性**：领域层纯函数优先（路径解析、XML 解析、ini 读写、评分、参数组装）→ 单元测试；外部进程通过接口注入 → 可 mock；`proc`/`download` 提供 fake 实现供集成测试
- **可移植性**：平台差异集中在 `platform` 与 `sdk/repo`（host-os 选择）、`archive`（权限位）
