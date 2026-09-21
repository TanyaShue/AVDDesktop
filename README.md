# AVDDesktop

不装 Android Studio，也能在 Windows 上把 Android 模拟器跑起来的桌面工具。

- **环境自检**：一屏看清 JDK / sdkmanager / avdmanager / adb / emulator / 系统镜像 / 硬件加速 / 磁盘的状态与版本
- **镜像测速**：内置常见镜像站与官方源，实测延迟、下载速度、可用性并给出评分，一键切换
- **从镜像安装**：直接解析 `repository2-3.xml` 与 `sys-img2-3.xml`，下载 → SHA-1 校验 → 原子解压 → 写许可，支持断点续传
- **AVD 管理**：图形化创建向导（设备档案 + 系统镜像 + 全量硬件配置）、启动/停止/多开、克隆、清除数据、快照、adb 集成
- **无 JDK 也能用**：`emulator.exe` 是原生的，AVD 配置可由本工具直接写入（`AvdBackend` 双实现）

技术栈：Wails v2 + Go 1.26 + React 19 + TypeScript + Vite 7（零第三方前端依赖）

---

## 快速开始

```powershell
# 开发模式（热重载；设置 WAILS_DEV 会把日志同时输出到控制台）
wails dev

# 构建可执行文件 → build/bin/AVDDesktop.exe
wails build

# 单元测试（含连接真实镜像与外部工具的集成测试）
go test ./...
go test -short ./...          # 跳过需要网络/外部进程的集成测试

# 端到端回归（走真实的下载、解压、建机、开机、adb）
.\scripts\e2e.ps1             # 轻量：自检 + 索引 + 测速 + AVD 生命周期 + 导出导入
.\scripts\e2e.ps1 -Heavy      # 追加：从镜像真实下载安装 cmdline-tools / platform-tools
.\scripts\e2e.ps1 -Boot       # 追加：无窗口启动模拟器并验证 adb / 截图 / logcat
.\scripts\e2e.ps1 -All        # 全部（耗时最长）
```

要求：Go 1.25+、Node 20+、Wails CLI v2.16+。

环境变量：

| 变量 | 作用 |
| --- | --- |
| `AVDDESKTOP_LOG_LEVEL` | 启动阶段日志级别（debug/info/warn/error），设置页可随时覆盖 |
| `AVDDESKTOP_LOG_STDOUT=0` | 禁用开发模式下的控制台日志输出 |
| `AVDDESKTOP_NO_UI_DIALOG=1` | 已运行实例的提示改为写标准错误（自动化场景） |

---

## 设计文档

| 文档 | 内容 |
| --- | --- |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 总体架构、模块划分、关键决策（ADR）、核心流程、风险与兜底、里程碑 |
| [docs/API-CONTRACT.md](docs/API-CONTRACT.md) | 前后端接口契约：服务方法、事件协议、数据结构、错误码 |
| [docs/UI-SPEC.md](docs/UI-SPEC.md) | UI 设计规范：设计令牌、组件规格、页面布局（对齐 `assets/` 参考图） |
| [docs/RESEARCH-NOTES.md](docs/RESEARCH-NOTES.md) | 实测调研：镜像可用性、仓库索引结构、许可哈希、CLI 输出格式、AVD 配置 |

---

## 代码结构

```
main.go / app.go             # Wails 装配：无边框窗口、生命周期、服务绑定
internal/
  domain/                    # 领域模型（纯数据，零依赖）
  config/                    # settings.json（原子写 + 校验 + 迁移）
  logging/                   # 按天滚动日志 + 内存环形缓冲
  platform/                  # 路径解析（SDK/AVD/JDK）、磁盘、代理、应用目录
  proc/                      # 外部进程：隐藏窗口、行流式输出、超时与取消
  download/                  # 下载器：Range 分片 / 顺序续传 / 降级三模式
  archive/                   # 安全 zip 解压、SHA-1 校验、原子替换
  job/                       # 任务管理器 + 事件节流 + 按资源键互斥
  mirror/                    # 内置源表 + 并发测速引擎 + 评分
  sdk/
    repo/                    # repository2-3.xml / sys-img2-3.xml 解析与镜像重写
    licenses/                # 许可哈希常量表（实测值）
    detect/                  # 环境自检（组件、版本、加速）
    query/                   # 本地已安装包扫描（source.properties）
    install/                 # 安装编排：依赖 → 下载 → 校验 → 解压 → 许可
  avd/
    profile/                 # avdmanager list device 解析（含内置兜底档案）
    store/                   # config.ini 读写、硬件配置 schema、克隆、元数据
    backend/                 # AvdBackend：avdmanager / 直写配置文件
    launch/                  # 端口分配、启动参数、状态机、停止
  adb/                       # adb 客户端（设备/安装/推送/shell/截图）
  service/                   # Wails 绑定层（薄）：Env/Mirror/Sdk/Avd/Emulator/Adb/Settings/Diagnostics/Jobs/Window
frontend/
  src/app/                   # 应用壳（标题栏 + 导航 + 页面路由）
  src/bridge/                # 绑定包装与类型别名（页面只从这里调后端）
  src/components/            # UI 组件（弹窗、任务抽屉、测速表、设备卡片）
  src/pages/                 # 首页自检 / 设备 / SDK / 设置 / 创建向导
  src/styles/                # 设计令牌 + 组件样式（纯 CSS）
  wailsjs/                   # 自动生成，勿手改
```

---

## 实现状态

| 能力 | 状态 |
| --- | --- |
| 环境自检（路径、JDK、工具链、加速、磁盘、许可、设备健康） | ✅ 已在真机验证（`ready=true`，含单飞与缓存） |
| Windows 开关与虚拟化归因（WHPX/AEHD/BIOS 三种情况区分） | ✅ 已在真机验证（含中文系统名称编码修正） |
| 问题清单（每条带可复制的修复命令） | ✅ 已实现 |
| 镜像源管理 + 并发测速 + 评分 | ✅ 已连真实镜像验证（8 个源，可用性分级正确） |
| 仓库索引解析（含系统镜像的目录相对 URL 规则） | ✅ 有真实索引样本测试 |
| 下载（分片/续传/限速/代理）+ 安全解压 + SHA-1 校验 | ✅ 已实现（含官方归档包装目录剥离） |
| 从镜像安装组件（含 cmdline-tools bootstrap） | ✅ E2E 验证：空目录安装并成功运行 sdkmanager/avdmanager |
| 许可写入（常量哈希表） | ✅ 已实现 |
| AVD 存储层（config.ini 读写、schema、克隆、元数据） | ✅ E2E 验证 |
| 设备档案解析（`avdmanager list device`） | ✅ 已实现（88-96 个档案与分类） |
| 创建 AVD（avdmanager 后端 + 无 JDK 直写后端） | ✅ E2E 验证两种后端选择语义正确 |
| 启动 / 停止 / 多开（端口分配、状态机、日志） | ✅ E2E 验证：无窗口开机完成 + 优雅停止 |
| 设备导出 / 导入（zip，含重名自动改名） | ✅ E2E 验证 |
| adb 集成：设备列表 / shell / 安装 APK / 推送 / 截图 | ✅ E2E 验证（截图 1.7MB base64） |
| logcat 实时流（长驻进程 + 事件推送） | ✅ E2E 验证（3 秒收到 500 行） |
| 日志：分级、按天+按大小滚动、实时面板、任务日志转写 | ✅ 已实现（7 个单测） |
| 单实例保护 | ✅ 已验证（第二实例提示后退出） |
| 界面：首页自检 / 设备卡片 / 创建向导 / SDK 管理 / 设置 / 任务抽屉 / 测速弹窗 / 日志面板 / logcat 面板 | ✅ 已实现（前端可构建） |
| 快照保存/恢复 | 🚧 列表与删除已实现，保存/恢复走模拟器控制台 |
| 批量操作 / 排列窗口 | 🚧 未实现（界面已占位并标注） |
| i18n（中英切换） | 🚧 文案已集中，尚未接入切换 |
| 安装包（NSIS / MSI）与签名 | 🚧 M7 |

---

## 重要设计取舍（详见 ADR）

1. **自研下载器而非只调用 `sdkmanager`**：镜像只能通过替换 base URL 生效（sdkmanager 仅支持 HTTP 代理换源）；自研还能不依赖 JDK、支持分片续传与精确进度。
2. **`AvdBackend` 抽象**：`avdmanager` 官方已标注废弃，且需要 JDK。抽象层让“无 JDK 也能创建并启动模拟器”成为可能（`emulator.exe` 是原生程序）。
3. **所有长任务走 Job + 事件流**：系统镜像单包约 2 GB，必须能后台继续、能取消、能看进度。
4. **镜像兼容性分级**：实测只有 Google 官方与腾讯云能完整镜像 SDK 仓库，其它常见镜像返回 403/404。因此内置源表区分“启用/停用 + 实测备注”，并支持自定义源与自动回退官方源。
5. **日志依赖注入而非全局单例**：领域包接受 `logging.Interface`，未注入时用 `Nop()`，保证可测试；同时通过 `Sink` 向前端实时推送。
6. **端到端测试优先于手测**：E2E 发现并修复了 4 个真实缺陷（镜像目录拼接、归档包装目录、`ANDROID_AVD_HOME`、后端选择语义），另有 1 个（并发重复扫描）由运行时日志发现 —— 详见 `docs/RESEARCH-NOTES.md` §9。

---

## 授权与致谢

- Android SDK 相关内容版权归 Google；本工具只做下载与本地编排，**不会**自动替用户同意许可协议（除用户在设置中显式开启）。
- AEHD 驱动将于 2026-12-31 停止支持，界面引导优先推荐 Windows 自带的 WHPX。
