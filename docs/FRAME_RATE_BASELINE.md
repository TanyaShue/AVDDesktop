# 自定义 UI 画面帧率基线（Phase 0）

> 采集日期：2026-09-28
> 机器：Intel i5-13500H（12C/16T）+ NVIDIA GeForce RTX 4050 Laptop，Windows 11
> AVD：MyDevice，1440×3120，density 420，6 vCPU，4GB RAM，`hw.lcd.vsync=60`
> 工作负载：Settings 列表连续上滑（`input swipe` 250ms + 30ms 间隔）

## 1. 模拟器采集侧（只接收 MMAP，不做编码/渲染）

测量方法：直接连接 `emulator -grpc` 的 `streamScreenshot(RGBA8888, MMAP)`，
只统计 `FrameMeta.Seq` 到达率，不复制、不编码、不渲染。工具为临时
`internal/measure`（不进入提交）。

| 模拟器图形后端 | 请求流尺寸 | 采样时长 | 平均接收帧率 | Seq gap |
|---|---:|---:|---:|---:|
| SwiftShader（当前 `hw.gpu.enabled=no`，无 `-gpu` 参数） | 540×1170 | 15s | **21.9fps**（329 帧） | 0 |
| `-gpu host`（NVIDIA RTX 4050 Laptop） | 540×1170 | 15s | **60.0fps**（900 帧） | 0 |
| `-gpu host` | 720×1560 | 12s | **60.0fps**（720 帧） | 0 |

结论：

- 当前 AVD 的 `hw.gpu.enabled=no` 会让模拟器落到 **SwiftShader 软件渲染**，采集侧只有约 22fps；
  这才是代码注释中“540 宽约 28fps”的主要原因，而不是 JPEG 编码本身。
- 显式加 `-gpu host` 后，模拟器在 540 和 720 宽下都能稳定给到 60fps，且无 seq gap；
  采集侧不是 60fps 目标的上限。
- 因此 **Phase 3（原生窗口嵌入 / scrcpy 式 H.264）不启动**；后续只需在 Phase 1 把
  自定义 UI 启动切到宿主 GPU，并给不支持 host GPU 的环境保留回落。

## 2. Go JPEG 编解码基准（设计阶段实测）

方法：Go 标准库 `image/jpeg`，连续编码/解码 20–30 次取平均；内容为真实 UI 截图
和“合成噪声”最坏情况。

| 内容 | 尺寸 | 质量 | 单帧编码 | Go 单帧解码 |
|---|---|---:|---:|---:|
| 真实 UI 截图 | 540×960 | 80 | 5.2–5.9ms | — |
| 真实 UI 截图 | 720×1280 | 80 | 8.9–10.5ms | — |
| 合成噪声（最坏） | 540×960 | 80 | 10.1ms | 5.0ms |
| 合成噪声（最坏） | 720×1280 | 80 | 29.4ms | 14.0ms |

结论：真实 UI 内容下，540p JPEG 编码本身还有 60fps 余量，但 720p 已接近
10ms/帧，叠加解码、MJPEG 提交与浏览器 compositor 后余量很小；高熵内容会明显
超过 16.7ms 预算。Phase 2 的原始帧 + WebGL2 路径仍然必要。

## 3. Phase 0 统计口径

已在代码中加入：

- 服务端：`recvFps`、`publishFps`、`seqGapFrames`、`copyMsP50/p95`、
  `encodeMsP50/p95`、`subscribers`、`dropForClient`、`recvOnly` 基准模式；
- 显示会话：订阅者数、发布总数/FPS、字节率、慢客户端丢帧计数；
- 前端：F8 / 标题栏 `FPS` 按钮打开诊断面板，1Hz 轮询服务端统计。

Phase 1 将以本文件为基线，逐项记录优化后的对比数据。

## 4. 环境备注

- 测量时主机上还有 MuMu 模拟器在运行，占用 adb 5555/16384；本 AVD 因此改用
  console 5560 / adb 5561 / gRPC 8654，避免端口冲突。
- `build/bin/logs/bench-emu-*.log` 保留了本次模拟器原始日志（未纳入 git）。

## 5. Phase 1 结果（2026-09-28）

### 5.1 启动侧

- `BuildArgs` 在 CustomUI 下默认追加 `-gpu host`；可用 `AVDDESKTOP_EMULATOR_GPU` 覆盖
  （`host` / `auto` / `swiftshader_indirect` / `angle_indirect` / `guest` / `off`）。
- 新建 AVD 默认写入 `hw.gpu.enabled=yes`、`hw.gpu.mode=host`；已有 AVD 不改配置，由启动参数覆盖。
- WebView2 辅助进程显式开启 GPU compositing（`--enable-gpu-rasterization --enable-zero-copy`，
  可用 `AVDDESKTOP_WEBVIEW_ARGS` 覆盖）。
- `TestE2E_DeviceWindow` 在真实 MyDevice 上通过（26.4s）：CustomUI 启动 → 窗口打开 → 8s 稳定
  → Close 后辅助进程回收。

### 5.2 端到端发布帧率（独立设备窗口 helper，`--stats`）

环境：MyDevice 1440×3120，`-gpu host`，stream 540×1170，Settings 列表连续滚动 15s。

| 指标 | 结果 |
|---|---:|
| recvFps | 59–61 |
| publishFps | 60 |
| subscribers | 1 |
| seqGapFrames | 0 |
| dropForClient | 0 |
| copyMs p50 / p95 | 0.51 / 1.18–1.38 |
| encodeMs p50 / p95 | 11.6–12.9 / 14.2–14.7 |
| dropBeforeEncode | 46（WebView 订阅建立前的预热丢帧，符合预期） |

结论：Phase 1 退出条件满足——采集侧 60fps、发布侧 60fps、无客户端丢帧、无 seq gap。
剩余风险是 JPEG 编码 p50 已到 ~12ms、p95 ~14.7ms，距离 16.7ms 预算很近，因此 Phase 2
的原始帧 + WebGL2 路径仍然必要（它会把编码/解码从主链路中移除）。
