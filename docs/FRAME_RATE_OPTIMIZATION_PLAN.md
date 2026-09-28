# 自定义 UI 画面帧率优化方案

> 状态：已实施（Phase 0–2、Phase 4；Phase 3 经实测不需要，见 FRAME_RATE_BASELINE.md）
> 基线：`master` @ `e9c51e5`，Windows 11 + WebView2，自定义 UI 独立设备窗口路径
> 关联：[ARCHITECTURE.md](ARCHITECTURE.md)、[DEVICE_WINDOW_WEBVIEW.md](DEVICE_WINDOW_WEBVIEW.md)

---

## 0. 结论摘要

当前自定义 UI 的画面链路是：

```text
Android Emulator (guest 60Hz 出帧)
  → gRPC streamScreenshot(RGBA8888, MMAP)
  → 共享内存整帧复制
  → Go image/jpeg 逐帧编码（质量 80）
  → MJPEG multipart HTTP 推送
  → WebView2 <img> 解码 / 重绘
```

它有三个结构性问题：

1. **每帧都要过一次 CPU JPEG 编解码**，且编码、发布、HTTP 写出全部串在同一条链路上；
2. **`<img>` + `multipart/x-mixed-replace` 没有帧节奏（frame pacing）和明确的丢帧点**，浏览器什么时候解码、什么时候提交完全不可控，静止时还必须靠 500ms 心跳重发；
3. **画面请求宽度固定 540**，与窗口在 125%–175% DPI 下的物理像素不匹配，画面被 CSS 放大，既损失清晰度，又让“清晰”和“流畅”无法按设备能力自适应。

本方案的推荐目标是：

- 先做 **Phase 0（1–2 天）可观测性**，测出模拟器 gRPC 侧真实出帧率 `seqRate`；
- 若 `seqRate ≥ 55fps`：走 **Phase 2 目标架构**——二进制帧通道（WebSocket）+ WebGL2 画布 + `requestAnimationFrame` 按刷新率呈现 + “只保留最新帧”的丢帧策略，彻底绕过 JPEG 与 `<img>` multipart；
- 若 `seqRate` 只有约 30fps：客户端优化只能降低延迟、消除卡顿，不能突破采集上限；此时进入 **Phase 3 决策**（原生窗口嵌入 / guest 侧 H.264 / 接受稳定 30fps），该决策需要产品确认，不放进默认改造范围。

整个路线分阶段推进，每阶段有独立验收门槛；任何阶段失败都可以回退到现有 MJPEG 路径。

---

## 1. 目标与验收口径

### 1.1 体验目标

- 连续动画（滚动、下拉通知栏、最近任务、视频）下无肉眼可见的周期性卡顿；
- 帧间隔稳定，不出现“一段时间 60fps、随后连续掉帧”的突发模式；
- 触摸、按键的响应延迟尽可能低；
- 静止画面不再做无意义的重绘和 JPEG 解码；
- 窗口缩放、DPI 切换、副屏移动后不出现长时间黑帧或错位。

### 1.2 量化 KPI

以 720×1280 AVD、主机 Windows 11、60Hz 显示为基准；测量点必须写清楚，避免“感觉流畅”。

| 指标 | 540p 流畅档目标 | 720p 清晰档目标 | 测量点 |
|---|---:|---:|---|
| 呈现帧率 p50 | ≥ 58fps | ≥ 58fps | 前端每次真实 draw 的 rAF 计数 |
| 帧间隔 p95 | ≤ 16.7ms | ≤ 16.7ms | 两次真实呈现的 rAF 时间差 |
| 帧间隔 p99 / 最大停顿 | ≤ 33ms / ≤ 100ms | ≤ 33ms / ≤ 100ms | 同上 |
| 触控→画面响应 p95 | ≤ 50ms | ≤ 60ms | 注入事件时间 → 含该结果的帧时间戳 |
| 端到端丢帧率 | < 1% | < 2% | seq gap 与前端 drop 计数 |
| 辅助进程 CPU | ≤ 10% 单核 | ≤ 20% 单核 | 任务管理器 / ETW / 进程计数器 |
| 静止画面重绘 | 0 次/秒（首帧提交后） | 0 次/秒 | 前端 draw 计数 |
| 冷启动首帧 | 不慢于现状 | 不慢于现状 | ready marker 时间戳 |

> 如果模拟器采集侧只能到 30fps，则“帧间隔稳定在 33.3ms、无抖动”优先于“平均值更高但不稳定”。KPI 的第一优先级是稳定，而不是峰值。

### 1.3 基准测试场景

每次改动都跑同一组场景，结果写入 `docs/FRAME_RATE_BASELINE.md`（Phase 0 创建）：

1. **连续滚动**：`adb shell input swipe` 循环滚动 Settings / Chrome 列表；
2. **通知栏动画**：从顶部下拉并收回，重复 10 次；
3. **最近任务动画**：进入 / 退出 Recents，重复 10 次；
4. **视频播放**：设备内播放 60fps 视频，观察 30 秒；
5. **静态桌面**：亮屏静止 30 秒，确认 0 重绘；
6. **窗口操作**：拖动、缩放、最大化、跨 100%/150%/175% DPI 显示器移动。

### 1.4 目标设备与边界

- 目标：Windows 10/11，WebView2 常青版，核显或独显均可；
- 低端兜底：WebGL2 不可用 / GPU 被驱动屏蔽时，自动回落到现有 MJPEG `<img>`；
- 明确不在本期范围：音频、多显示器、折叠屏、SharedArrayBuffer 零拷贝、macOS/Linux 的帧率专项验收。

---

## 2. 现状链路与代码位置

### 2.1 独立设备窗口（本期主目标）

```text
[主进程]
  DisplayService.OpenWindow
    └─ exec(自身, --display-host)  →  native_display.go

[辅助进程 internal/displayhost]
  main.RunHelper
    ├─ client.StreamScreenshotMMAP(streamW, streamH, region.Handle())   main.go
    ├─ frameSource.Next: 每帧 make([]byte) + copy 整帧                 source.go
    ├─ framePump.run: 逐帧 image/jpeg.Encode                            frames.go
    └─ display.Session.Publish(jpeg)
          └─ HTTP multipart/x-mixed-replace（500ms 心跳）              internal/display/server.go

[WebView 设备窗口]
  DeviceWindowApp.tsx
    └─ <img src="http://127.0.0.1:port/display/{id}">
          └─ CSS 把 540 宽的流放大到 stage 尺寸
```

关键代码：

| 文件 | 当前职责 | 与帧率的关系 |
|---|---|---|
| `internal/displayhost/main.go` | 启动辅助进程、打开 MMAP 流、组装 pump/window | 固定 `defaultStreamWidth = 540` |
| `internal/displayhost/source.go` | 从共享内存复制稳定帧 | 每帧一次堆分配 + 全帧 memcpy |
| `internal/displayhost/frames.go` | JPEG 编码并发布 | 单协程、串行编码，是最容易成为上限的一段 |
| `internal/display/server.go` | 最新帧覆盖 + MJPEG 广播 | 没有帧节奏；500ms 心跳强制静止重绘 |
| `frontend/src/components/DeviceWindowApp.tsx` | `<img>` 消费 MJPEG + 工具栏 | 无法控制解码/提交时机，无法显式丢帧 |
| `frontend/src/styles/device-window.css` | 画面等比放进 stage | 540 宽流在 175% DPI 下约放大到 709 物理像素 |

### 2.2 主窗口内浮层（兼容路径，本期不要求同等优化）

`internal/service/display.go` 的 `stream()` 走的是 **非 MMAP** 的 `StreamScreenshot`，RGBA 像素经 gRPC 消息序列化后在主进程内 JPEG 编码。它比独立窗口更慢，只作为兜底。Phase 4 可以评估是否把它也迁移到 MMAP/帧通道，本文不把它列为 60fps 的硬指标。

---

## 3. 设计阶段实测数据

以下数据在本次方案设计时于当前开发机测得，仅作为“假设的量级证据”，不是应用端到端数据：

- 机器：Intel i5-13500H（12 核 16 线程），Windows 11，Go 1.26.1；
- 方法：Go 标准库 `image/jpeg`，对同一张图连续编码/解码 20–30 次取平均；
- 内容：`合成噪声` 是高熵最坏情况；`真实 UI 截图` 是仓库内截图缩放到目标尺寸后的结果。

| 内容 | 尺寸 | 质量 | 单帧编码 | Go 单帧解码 | JPEG 大小 |
|---|---|---:|---:|---:|---:|
| 合成噪声（最坏） | 540×960 | 80 | 10.1ms | 5.0ms | 104KB |
| 合成噪声（最坏） | 720×1280 | 80 | 29.4ms | 14.0ms | 181KB |
| 合成噪声（最坏） | 900×1600 | 80 | 48.4ms | 23.7ms | 286KB |
| 真实 UI 截图 | 540×960 | 80 | 5.2–5.9ms | — | 28–33KB |
| 真实 UI 截图 | 720×1280 | 80 | 8.9–10.5ms | — | 44–49KB |

结论：

1. **真实 UI 内容下，540p JPEG 编码本身还不到 60fps 的硬上限**（约 5–6ms/帧），但 720p 已经接近 10ms/帧，再叠加解码、复制和浏览器提交，余量很小；
2. 对视频、游戏、复杂渐变等高熵内容，编码会明显恶化，纯 JPEG 路径无法保证 60fps；
3. 代码注释中的“540 宽实测约 28fps”需要通过 Phase 0 拆解，不能直接归因于 JPEG 编码。

---

## 4. 瓶颈假设与验证方法（按优先级）

| # | 假设 | 证据 / 现象 | Phase 0 验证方法 | 预期修复 |
|---|---|---|---|---|
| H1 | `<img>` + multipart MJPEG 没有帧节奏控制 | 浏览器自行决定解码/提交；无显式丢帧；500ms 心跳会强制静止重绘 | 用 `fetch` 读流计数 vs `<img>` 的 `onLoad` 计数，对比同一服务端的出帧数 | WebSocket 二进制帧 + canvas/WebGL |
| H2 | 540 固定宽度与 DPI 不匹配 | 175% 缩放下 stage 物理像素约 709 宽，流只有 540，浏览器放大 | 在 100%/150%/175% DPI 下记录 `devicePixelRatio`、stage 物理尺寸、实际 draw 尺寸 | 按物理像素选档（自适应/手动） |
| H3 | 每帧堆分配 + 单线程编码 | `make([]byte)`、`image/jpeg` 内部缓冲每帧分配；编码与 gRPC 消费串行 | pprof alloc + 编码耗时直方图；关掉编码只测接收 | 缓冲池 + 独立编码 worker + 最新帧合并；更进一步直接去掉 JPEG |
| H4 | 模拟器采集侧存在上限 | `streamScreenshot` 每帧要做 GPU readback/缩放；`-no-window` 下可能被节流 | 只接收不解码，记录 `FrameMeta.Seq` 到达率与 gap | 启动参数 A/B；若仍 < 55fps，进入 Phase 3 |
| H5 | WebView2 合成/GPU 路径未优化 | 默认可能软件栅格化；`<img>` 每帧触发 paint/layout | DevTools Performance / `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` 灰度；检查 GPU 状态 | 独立合成层、GPU 标志、canvas |
| H6 | gRPC/loopback 与窗口生命周期开销 | MMAP 元数据、HTTP 分块、窗口 resize | 分阶段时间戳；拖动窗口时对比静止时 | 保留 MMAP；避免 resize 期间重建流 |

### 4.1 Phase 0 必须先回答的问题

1. 连续动画时，模拟器实际推给我们的 `seq` 是多少 fps？gap 分布如何？
2. 只做 MMAP 复制、不做编码时，最多能接住多少 fps？
3. 编码 p50/p95 分别是多少？是否成为串行瓶颈？
4. 服务端发布了多少帧、浏览器真正提交了多少帧？差值是多少？
5. 把 stream width 从 540 调到 360 / 720 / 原生，`seqRate` 和编码耗时如何变化？

---

## 5. 方案总览与决策树

```text
Phase 0 建立可观测性，测 seqRate
   │
   ├─ seqRate ≥ 55fps
   │     → Phase 1 速赢（低风险）
   │     → Phase 2 目标架构：二进制帧 + WebGL2 + rAF 节奏
   │
   ├─ seqRate 35–55fps
   │     → Phase 1 + Phase 2，目标是“稳定到采集上限”
   │     → 同时做 Phase 3 spike 评估能否突破采集侧
   │
   └─ seqRate < 35fps
         → Phase 1 + Phase 2 只降低延迟与抖动
         → Phase 3 必须在以下路线中做产品决策：
             A. 模拟器原生窗口嵌入（最高帧率，平台相关）
             B. guest 侧 H.264（scrcpy 式）+ WebCodecs（保留 WebView 外观）
             C. 接受稳定 30fps，优化节奏与延迟
```

### 阶段一览

| 阶段 | 目标 | 主要交付 | 预估 | 门槛 |
|---|---|---|---|---|
| Phase 0 | 定位真实上限 | 统计接口 + 前端 FPS 叠加 + 基线报告 | 1–2 天 | 能回答 4.1 的 5 个问题 |
| Phase 1 | 低风险速赢 | 选档、编码 worker、缓冲池、CSS/GPU 优化 | 2–3 天 | 相对基线 ≥ +20% fps，或 p95 帧时间进入预算 |
| Phase 2 | 目标架构 | WebSocket 二进制帧 + WebGL2 + rAF 节奏 + 回落 | 3–5 天 | 540p p50 ≥ 58fps、p95 ≤ 16.7ms |
| Phase 3 | 条件分支（仅采集受限时） | 原生嵌入 / H.264 / 接受 30fps 的决策与 spike | 3–7 天 | 先做 1–2 天 spike，再决定是否立项 |
| Phase 4 | 硬化与验收 | 设置项、诊断面板、测试、文档、回退 | 2–3 天 | 全量验收清单通过 |

---

## 6. Phase 0：可观测性（先做，不改架构）

### 6.1 服务端统计

给辅助进程加 `--display-stats`（默认关闭，日志/诊断面板打开时启用），每秒输出一行 JSON 到 stdout，主进程转发到应用日志与诊断面板：

```json
{
  "ts": 1759000000123,
  "recvFps": 59.4,
  "seq": 12345,
  "seqGapFrames": 1,
  "copyMsP50": 0.18,
  "copyMsP95": 0.31,
  "encodeMsP50": 5.4,
  "encodeMsP95": 8.9,
  "publishFps": 59.4,
  "subscribers": 1,
  "droppedBeforeEncode": 0,
  "droppedBeforeSend": 0,
  "bytesPerSec": 1900000,
  "streamW": 540,
  "streamH": 960
}
```

### 6.2 前端统计

在设备窗口右下角加一个可开关的悬浮统计（默认隐藏，设置项/快捷键打开）：

- `recvFps`：WebSocket/HTTP 每秒收到的帧数；
- `presentFps`：rAF 中真实 draw 的次数；
- `frameIntervalP50/P95/max`；
- `uploadMsP50/P95`（texImage2D + draw 的耗时）；
- `droppedAtClient`：因为“只保留最新帧”丢弃的数量；
- `latencyMs`：用帧头里的同机 Unix 微秒时间戳估算；
- `gpuRenderer`：WebGL `UNMASKED_RENDERER_WEBGL`。

### 6.3 交叉验证手段

- 服务端 `seqRate` 对比前端 `recvFps`：判断瓶颈在 gRPC 采集还是本地链路；
- 前端 `recvFps` 对比 `presentFps`：判断瓶颈在传输还是浏览器提交；
- 编码耗时开关：`--display-benchmark=recv-only` 只接收、不编码，测采集上限；
- 分辨率扫描：360 / 540 / 720 / 原生，观察 `seqRate` 与编码耗时；
- Windows 侧用任务管理器/ETW 观察辅助进程、WebView2 GPU/Renderer 进程的 CPU/GPU 占用。

### 6.4 Phase 0 交付物

- `docs/FRAME_RATE_BASELINE.md`：包含上述 1.3 场景的原始数据表；
- `scripts/perf-device-window.ps1`：自动跑滚动/通知栏/最近任务场景并收集日志；
- 一份结论：明确瓶颈排序，并给出是否进入 Phase 2 / Phase 3 的判断。

---

## 7. Phase 1：现有链路速赢（低风险）

目标是在不改协议的前提下，把当前 MJPEG 链路推到它的上限，同时拿到可对比的改进数据。

### 7.1 画面选档与 DPI 对齐

- 增加三档：`流畅（540）`、`清晰（720）`、`自适应（按 DPR 在 360–720 间选）`；
- 默认先保持 540，避免默认行为回归；
- 自适应规则：`目标宽 = clamp(round(stageClientWidth × devicePixelRatio), 360, 720)`，并满足 `目标宽 ≤原生宽`；
- 记录窗口 resize / DPI 变化后的生效档位，避免频繁重建流（500ms 防抖，稳定后只重建一次）；
- 注意：改变 stream width 需要重建 MMAP region 与 gRPC 流，Phase 1 先只允许“打开窗口时选档”，动态切换放到 Phase 2/4。

### 7.2 编码与缓冲

- **只在有订阅者时编码**：`Session` 暴露 `HasSubscribers()`，没有 WebView 连接时直接丢帧，不做 JPEG；
- **缓冲池**：`frameSource` 的整帧复制 buffer 用 `sync.Pool` 复用，避免每帧 2MB 堆分配；
- **编码 worker + 最新帧覆盖**：接收协程只负责 copy 和入队；编码 worker 只处理最新帧，积压帧直接丢弃；编码结果按序发布；
- **质量调到 75**（Phase 0 有 A/B 数据支撑后再定），并记录质量/体积/耗时；
- 评估把 JPEG 编码固定到独立 OS 线程/更高优先级，避免与 gRPC 接收互相抢 CPU。

### 7.3 MJPEG 会话优化

- 心跳只保留“提交首帧所需的 2–3 次”，之后静止时停止重发；有新帧时立即恢复；
- 确认 `serve` 循环在慢客户端下真正丢帧（当前语义是先写旧帧、没有队列，行为正确，但要加 drop 计数）；
- 统一 `Content-Length`/`Flush` 的写出路径，避免每帧额外分配；
- 记录每个订阅者的写出耗时，慢订阅者超过阈值（例如 1s）直接断开，让前端重连而不是拖慢服务端。

### 7.4 WebView2 / CSS

- 确认 `WebviewGpuIsDisabled = false`（当前默认即为 GPU 可用），并在诊断面板显示 WebGL renderer；
- 对辅助进程设置 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`（只对 helper 的 `cmd.Env` 生效，不影响主窗口）：
  - `--enable-gpu-rasterization`
  - `--enable-zero-copy`
  - `--ignore-gpu-blocklist`（先灰度，记录是否有兼容问题）
  - 不使用 `--disable-frame-rate-limit`、`--disable-gpu-vsync` 这类会造成撕裂/空转的参数；
- CSS：
  - 给画面元素单独提升合成层（`will-change: transform` 或 `transform: translateZ(0)`，以实测为准）；
  - 避免在每帧变化的元素上放 `box-shadow` / `filter` / `backdrop-filter`；
  - 保持背景静态，stage 使用 `contain: strict` 类约束，减少无效重排。

### 7.5 模拟器启动参数 A/B

当前 `BuildArgs` 只有 `-no-window -grpc`。Phase 1 做开关化 A/B（不要直接改默认）：

| 参数 | 目的 | 备注 |
|---|---|---|
| `-gpu host` | 强制走宿主 GPU，避免软件渲染 | 与 `hw.gpu.mode` 交叉验证 |
| `-no-audio` | 关闭音频，减少模拟器侧占用 | 仅自定义 UI 场景 |
| `-feature -Vulkan` | 对比 Vulkan/GL 后端 | 不同驱动差异大，需要真机矩阵 |
| `-no-boot-anim` | 缩短启动期，不影响稳态 fps | 低风险 |

验收：至少一组参数让 `seqRate` 有可重复提升；若无提升，记录并保留默认参数。

### 7.6 Phase 1 退出条件

- 相对 Phase 0 基线，连续滚动场景 fps 提升 ≥ 20%，或 p95 帧间隔进入 ≤ 16.7ms；
- 编码/复制的 p95 处于预算内；
- 静止画面不再有周期性重绘；
- 所有优化可通过设置或编译开关回退。

---

## 8. Phase 2：目标架构（二进制帧 + WebGL2 + rAF 节奏）

这是解决 H1/H3 的结构性方案。核心原则：

1. **传输原始像素，不再传输 JPEG**（可保留 JPEG 作为回落/低带宽档）；
2. **生产端永不阻塞**：慢客户端只丢帧；
3. **消费端只在 vsync 呈现最新一帧**：多个新帧只上传/绘制最后一个；
4. **静止即零开销**：没有新帧就不 draw，不重发心跳。

### 8.1 目标链路

```text
emulator renderer
  → streamScreenshot(RGBA8888, MMAP)
  → frameSource：复制稳定帧 + 时间戳 + seq
  → frameHub：引用计数缓冲池、每订阅者 1 槽 mailbox
  → WebSocket 二进制帧（127.0.0.1:随机端口，带 token）
  → 前端 frameSocket：只保留最新 ArrayBuffer
  → WebGL2 纹理上传
  → requestAnimationFrame：每 vsync 最多 draw 一次
  → DWM/WebView2 合成
```

MJPEG `<img>` 端点继续保留，作为 WebGL2 不可用、GPU 被屏蔽或联调对比时的回落路径。

### 8.2 帧协议（WebSocket 二进制消息，一帧一条消息）

统一使用小端序，预留扩展位：

| 字段 | 类型 | 说明 |
|---|---|---|
| magic | uint32 | `AVDF`（0x46445641） |
| version | uint16 | 1 |
| flags | uint16 | bit0=行序自底向上，bit1=RGB888；其余保留 |
| width | uint16 | 像素宽 |
| height | uint16 | 像素高 |
| format | uint16 | 0=RGBA8888, 1=RGB888, 2=JPEG |
| reserved | uint16 | 对齐/扩展 |
| seq | uint32 | 来自模拟器 FrameMeta.Seq |
| timestampUnixUs | uint64 | 服务端发布时刻（同机延迟测量用） |
| payloadBytes | uint32 | 负载长度 |
| reserved2 | uint32 | 对齐/扩展 |
| payload | bytes | 像素或 JPEG 数据 |

实现要点：

- **v1 只启用 RGBA8888**，RGB888 作为 Phase 4 的带宽优化验证项（要确认 MMAP 的 stride / 行序）；
- WebSocket 关闭 permessage-deflate，避免无意义的 CPU 压缩；
- 前端 `binaryType = "arraybuffer"`，一帧一个消息，浏览器负责分片重组；
- 帧头行序 flag 由服务端根据实测决定，前端用 `UNPACK_FLIP_Y_WEBGL` 或纹理坐标翻转，不在 JS 里逐行拷贝。

### 8.3 服务端 frameHub

建议新增 `internal/framestream`（或扩展现有 `internal/display`）：

- `Hub`：保存最新帧，按订阅者分发；
- `Subscriber`：1 槽 mailbox + 独立写协程；
- `Publish(frame)`：
  - 有订阅者时，把同一个 `frameBuf` 引用投递到每个订阅者 mailbox；
  - mailbox 已有旧帧时，丢弃旧帧并释放引用；
  - 没有订阅者时直接释放，不做编码、不做网络写；
- `frameBuf`：固定容量缓冲 + `atomic.Int32` 引用计数，归零后回收到 `sync.Pool`；
- 写协程：`SetWriteDeadline(250ms)`；连续超时/慢消费直接关闭连接，由前端重连；
- 统计：发布 fps、每订阅者发送 fps、丢帧数、队列深度、发送耗时 p50/p95。

WebSocket 升级复用现有 `display.Server` 的 127.0.0.1 监听端口，新增：

- `GET /ws/display/{id}?token=...`：WebSocket 二进制帧；
- `GET /display/{id}`：现有 MJPEG 回落；
- `GET /stats`：诊断（仅本机，可关闭）。

安全：

- 继续只监听 `127.0.0.1`；
- `Session()` 返回一次性随机 token（不要放固定密钥）；WebSocket 握手校验 token + Origin；
- 不启用压缩、不做跨进程持久化。

### 8.4 前端渲染

新增两个模块，`DeviceWindowApp` 只负责会话/工具栏，不参与每帧 React 状态更新：

- `frontend/src/device/frameSocket.ts`：WebSocket 客户端，消息到达后只更新 `latestFrameRef`，并保证只保留最新一帧；
- `frontend/src/device/FrameCanvas.tsx`：WebGL2 画布，负责纹理上传、等比绘制、rAF 节奏、统计。

rAF 伪代码：

```ts
function onFrame(frame: FrameMessage) {
  latest = frame;            // 覆盖旧帧，不排队
  if (!rafPending) {
    rafPending = true;
    requestAnimationFrame(present);
  }
}

function present(now: number) {
  rafPending = false;
  const frame = latest;
  if (!frame) return;        // 没有新帧就不 draw，也不重绘
  latest = null;

  // 只在真正有新帧时上传纹理 + draw
  uploadTexture(frame);
  drawQuad();
  stats.record(now, frame.seq);
}
```

细节：

- `getContext("webgl2", { alpha:false, antialias:false, depth:false, stencil:false, powerPreference:"high-performance" })`；
- 不开启 `preserveDrawingBuffer`；
- 画布 drawingBuffer 尺寸 = 流尺寸，CSS 只负责等比缩放到 stage；避免在 JS 里做双三次缩放；
- `texImage2D` 直接用收到的 ArrayBuffer view，上传后不保留引用；
- 等比适配用 viewport 或顶点缩放完成，信箱区留给 CSS 背景；
- `visibilitychange` / 窗口最小化时停止请求新帧（通知服务端降低或暂停发布）；
- 每帧不触发 React setState；统计以 1Hz 批量刷新。

### 8.5 回落与灰度

- 启动时探测 `webgl2` 与 `UNMASKED_RENDERER_WEBGL`；
- WebGL2 不可用、context 连续丢失两次、或 GPU 被驱动屏蔽时，自动切回 `<img>` MJPEG；
- 提供设置项和 URL 参数（仅诊断用）强制 `mjpeg` / `webgl`，便于现场对比；
- 新路径默认灰度：先只有“流畅模式”使用，稳定后再设为默认。

### 8.6 自适应质量控制（Phase 4 或 Phase 2 后期）

- 档位：360 / 540 / 720（可选 900）；
- 输入：stage 物理宽度、DPR、最近 5 秒的 `uploadMs`/`frameInterval`、丢帧率；
- 规则：p95 超过预算连续 2 秒降一档；稳定 10 秒且余量充足升一档；
- 升/降档都重建 MMAP region + gRPC 流，需要防抖与“同一档位不重复重建”；
- 所有切换写入日志，诊断面板可见。

### 8.7 Phase 2 退出条件

- 540p：`presentFps p50 ≥ 58`、`frameInterval p95 ≤ 16.7ms`、辅助进程 CPU ≤ 10% 单核；
- 静态桌面：首帧提交后 0 次 draw；
- WebGL2 不可用时自动回落且功能不回归；
- `go test ./...`、`npm run build`、`wails build`、设备窗口 E2E 全部通过。

---

## 9. Phase 3：采集受限时的分支（仅在 Phase 0 证明 `seqRate < 55fps` 后启动）

### 9.1 选项对比

| 选项 | 预期 | 成本 | 主要风险 | 适用前提 |
|---|---|---|---|---|
| A. 模拟器原生窗口嵌入 | 60/120fps，最低延迟 | 高 | Win32 窗口父子化、焦点/DPI/多窗口合成；与 WebView 外观方案冲突 | 只要求 Windows；可接受平台特定实现 |
| B. guest 侧 H.264 + WebCodecs | 60fps，保留 WebView 外观 | 中高 | 需要 scrcpy 式 server、协议与授权；延迟高于原始帧 | 可接受 adb push + 设备端编码 |
| C. 宿主硬件编码（Media Foundation） | 降低编码 CPU，不提升采集 fps | 中 | 只解决 H3，不解决 H4 | Phase 0 证明瓶颈在编码而非采集 |
| D. 接受稳定 30fps | 无采集改造 | 低 | 无法满足“尽可能流畅”的 60fps 目标 | 采集上限无法突破且产品接受 30fps |

### 9.2 选项 A：模拟器原生窗口嵌入

- 不再 `-no-window`；启动模拟器 Qt 窗口；
- 用 Win32 去掉标题栏/边框，将模拟器 HWND 作为子窗口嵌入自定义外壳（或精确同步几何的兄弟顶层窗口）；
- 输入可直接转发到模拟器窗口，或继续用 gRPC/adb 注入；
- 优点：绕过 gRPC 截图链路，直接使用模拟器原生 GPU 呈现；
- 缺点：平台特定、与当前“WebView 承载一切”的方向冲突；需要单独处理窗口拖动、缩放、全屏、副屏、焦点、DPI 和模拟器内部快捷键；
- 建议先做 1–2 天 spike，验证嵌入后 60fps 与输入可用，再决定是否立项。

### 9.3 选项 B：guest 侧 H.264（scrcpy 式）+ WebCodecs

- 通过 `adb push` 运行设备端 server（scrcpy-server 思路，Apache-2.0，需要保留许可/NOTICE）；
- 设备端 `MediaCodec` 硬件编码 H.264，主机通过 adb socket 接收；
- WebView2 用 WebCodecs `VideoDecoder` 解码，`VideoFrame` → canvas/WebGL 呈现；
- 优点：保留现有 WebView 自定义外观，60fps 视频流常规可达；
- 缺点：编码/网络/解码引入 30–80ms 延迟；协议与设备端兼容性工作量大；
- 需要先验证：模拟器镜像的 MediaCodec 编码能力、旋转/分辨率变化、adb 通道吞吐、WebCodecs 在目标 WebView2 版本的可用性。

### 9.4 选项 C：宿主硬件编码

- 若 Phase 0 证明采集足够但 Go JPEG 编码是瓶颈，可评估 Media Foundation / WIC / libjpeg-turbo：
  - `image/jpeg` 是纯 Go，真实 UI 540p 约 5–6ms；硬件/优化编码可降到 1–2ms；
  - 但当前链路还有 `<img>` 解码与提交开销，单独换编码器收益有限；
- 适合作为 Phase 2 之后的性能补充，而不是替代 WebGL 路径的优先方案。

---

## 10. 文件级改动清单（预计）

| 文件 / 新增 | 改动 |
|---|---|
| `internal/displayhost/main.go` | 新配置项：`Transport`、`StreamTier`、`Stats`；接入 frameHub；保留 MJPEG 分支 |
| `internal/displayhost/source.go` | 缓冲池、时间戳、格式/行序参数、统计钩子 |
| `internal/displayhost/frames.go` | 从“JPEG pump”拆成“帧接收 + 发布”；JPEG 仅作为回落 |
| `internal/displayhost/window.go` | `Session()` 返回 `wsUrl`、`token`、档位；新增 `SetStreamTier` 绑定；helper 环境变量 |
| `internal/display/stream.go`（新） | `Hub`/`Subscriber`/帧协议/WS handler/统计 |
| `internal/display/server.go` | 挂载 `/ws/display/{id}`、`/stats`；保留 MJPEG |
| `internal/service/native_display.go` | 传递档位/传输模式；helper 的 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` |
| `frontend/src/device/frameSocket.ts`（新） | WS 客户端、最新帧覆盖、重连、统计 |
| `frontend/src/device/FrameCanvas.tsx`（新） | WebGL2 纹理/四边形/rAF 节奏/回落 |
| `frontend/src/components/DeviceWindowApp.tsx` | 用 FrameCanvas 替换 `<img>`；保留 MJPEG 回落 |
| `frontend/src/styles/device-window.css` | canvas 布局、合成层、去除逐帧阴影/滤镜 |
| `internal/config` + `frontend/src/pages/SettingsPage.tsx` | 画面模式设置（自适应/流畅/清晰/兼容） |
| `internal/displayhost/*_test.go` | 帧协议、引用计数、丢帧语义单测 |
| `internal/e2e/device_window_e2e_test.go` | 增加性能探针与统计读取 |
| `scripts/perf-device-window.ps1`（新） | 自动基准场景 |
| `docs/FRAME_RATE_BASELINE.md`（新） | 基线数据与每次优化对比 |

依赖：`github.com/gorilla/websocket` 已在 `go.mod` 的间接依赖中（Wails 引入），可以直接提升为直接依赖，无需新增下载；前端只使用原生 WebGL2/WebSocket API，不新增 npm 依赖。

---

## 11. 验收与测试

### 11.1 自动化门禁

```powershell
go test ./...
cd frontend; npm run build; cd ..
wails build -clean

$env:AVDDESKTOP_E2E_HOME = (Resolve-Path "build/bin").Path
$env:AVDDESKTOP_NATIVE_BINARY = (Resolve-Path "build/bin/AVDDesktop.exe").Path
go test -tags e2e -count=1 -timeout 20m -run '^TestE2E_DeviceWindow$' -v ./internal/e2e
```

新增：

- `TestFrameHub_DropsSlowSubscriberWithoutBlocking`：慢订阅者不反压生产者；
- `TestFrameBufferPool_RefCount`：引用归零才回收，无泄漏；
- `TestFrameProtocol_RoundTrip`：帧头编解码、行序/格式位；
- `TestE2E_DeviceWindowPerf`（e2e）：真实模拟器 + 脚本滚动，断言统计阈值；
- 性能结果归档到 `docs/FRAME_RATE_BASELINE.md`。

### 11.2 手工矩阵

| 项目 | 档位 | 结果要求 |
|---|---|---|
| 连续滚动 | 540 / 720 | 无周期性卡顿，统计达标 |
| 通知栏下拉 | 540 / 720 | 跟手，无黑帧 |
| 最近任务 | 540 / 720 | 动画完整 |
| 视频播放 | 540 / 720 | 不撕裂，音频不参与本方案 |
| 静态桌面 | 540 / 720 | 0 重绘，CPU 接近空闲 |
| 窗口拖动/缩放 | 540 | 不黑屏，resize 后自动恢复 |
| 100%/150%/175% DPI | 自适应 | 画面不模糊、不越界 |
| WebGL 禁用 | 兼容 | 自动回落 MJPEG，功能不缺失 |
| 模拟器停止 | 全部 | 窗口提示“画面已中断”，可重连 |

### 11.3 回退策略

- 新路径由设置项/特性开关控制，默认“自适应（优先 WebGL，失败回落 MJPEG）”；
- 任何阶段出现不可恢复的回归，强制切换到 `兼容（MJPEG）` 即可恢复现有行为；
- 保留现有 `display.Server` 与 `<img>` 组件，不删除、不重写。

---

## 12. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 模拟器采集本身 < 55fps | Phase 2 无法达到 60fps | Phase 0 先验证；进入 Phase 3 决策，不盲目投入 |
| WebView2 GPU 被驱动屏蔽 | WebGL 路径退化 | 运行时探测 + 自动回落 MJPEG；保留诊断信息 |
| WebSocket 大帧内存抖动 | GC 峰值、卡顿 | 引用计数缓冲池 + 每订阅者 1 槽 + 慢连接断开 |
| 高频 WS 消息导致浏览器主线程繁忙 | 掉帧 | 只在 rAF 消费最新帧；每帧不 setState；1Hz 统计 |
| DPI/resize 频繁重建流 | 黑帧、抖动 | 固定档位优先；动态重建加 500ms 防抖与同档去重 |
| 多显示器 144Hz/60Hz 混用 | 节奏抖动 | 以“新帧到达才 draw”为准，不做固定 60 次空转 |
| 新路径安全面扩大 | 本机端口暴露 | 127.0.0.1 + 一次性 token + Origin 校验 + 无压缩 |
| scrcpy/原生嵌入引入授权与维护成本 | 项目复杂度上升 | 先做 spike；明确只在采集受限时启用 |

---

## 13. 排期估算（1 人，含测试与文档）

| 阶段 | 预估 | 可并行项 |
|---|---:|---|
| Phase 0 可观测性 + 基线 | 1–2 天 | 前端统计叠加可与服务端统计并行 |
| Phase 1 速赢 | 2–3 天 | CSS/GPU 与编码 worker 可并行 |
| Phase 2 目标架构 | 3–5 天 | 服务端 frameHub 与前端 WebGL 可并行，接口先冻结 |
| Phase 3 spike（条件） | 1–2 天先验证，之后 3–7 天 | 原生嵌入与 H.264 二选一 |
| Phase 4 硬化验收 | 2–3 天 | 文档与测试并行 |

最短路径（采集足够）：**Phase 0 + Phase 1 + Phase 2 ≈ 6–10 个工作日**。
采集受限路径：在最短路径之外增加 Phase 3 的 1–2 天 spike 与一次产品决策。

---

## 14. 待确认的决策点

1. **目标档位**：默认以 540p 流畅优先，还是 720p 清晰优先？（建议：默认自适应，540p 保底）
2. **是否批准 Phase 2 新路径**：二进制 WebSocket + WebGL2 替换 `<img>` MJPEG（建议批准，作为灰度路径，保留 MJPEG 回落）。
3. **若 Phase 0 证明采集 < 55fps**：是否批准做 Phase 3 spike？优先验证“原生窗口嵌入”还是“scrcpy 式 H.264”？
4. **主窗口浮层**是否需要同等帧率目标？（建议本期只保证独立设备窗口，浮层继续作为兼容路径。）
5. **音频/多屏**是否确认不在本期范围？

在上述决策确认前，建议只执行 Phase 0（纯观测、低风险）；Phase 0 完成后用数据决定 Phase 1/2/3 的投入比例。


