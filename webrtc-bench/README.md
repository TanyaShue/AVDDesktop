# Android Emulator WebRTC FPS Lab

使用官方 [`android-emulator-webrtc`](https://github.com/google/android-emulator-webrtc)
React 组件，把 Android 模拟器画面通过 WebRTC 显示到网页，并测量 60 Hz / 120 Hz
以及不同原生分辨率下的帧率。

本实验已经跑通 **API 37 + Apple M4 host GPU + 新 RTC v2 协议**，同时保留 API 36
Preview 软件 GPU 路径作为兼容基线。

> 重要澄清：macOS 的 stable Android Emulator 本身一直支持 `-gpu host` / Metal
> host GPU。这里使用 Preview 37.1.8 + API 37 的原因是 stable 构建没有可用的 WebRTC
> RTC 服务；不是因为 macOS 只有 Preview 才能使用 host GPU。

所有数据基于 `~/Library/Application Support/AVDDesktop` 中的 SDK、系统镜像和现有
`MyDevice` 设备配置，不修改原 AVD 用户数据。

## 最终结果

API 37.0 / Emulator Preview 37.1.8 / Apple M4 host GPU / H.264：

| 原生分辨率 | 60 Hz | 120 Hz | 120 Hz P10 |
| --- | ---: | ---: | ---: |
| 720×1280 | 60.0 | **119.9** | 118.0 |
| 1080×1920 | 60.0 | **119.1** | 109.9 |
| 1440×2560 | 60.0 | **78.2** | 76.1 |
| 2160×3840 | 47.7 | **43.0** | 41.9 |

VP8 在 120 Hz 下明显更慢：720p 92 FPS、1080p 43 FPS、1440p 33 FPS、4K 18 FPS。
因此默认使用 H.264。

所有测试均为 0 丢帧。完整对比见 [`BENCHMARK.md`](BENCHMARK.md)，汇总数据见
[`hostgpu-summary.json`](results/hostgpu-summary.json)。

### Host GPU 带来的提升

| 分辨率 / 120 Hz / H.264 | API 36 软件 GPU | API 37 Apple M4 | 提升 |
| --- | ---: | ---: | ---: |
| 720×1280 | 119.0 | 119.9 | 已到 120 Hz 上限 |
| 1080×1920 | 67.0 | **119.1** | +78% |
| 1440×2560 | 39.9 | **78.2** | +96% |
| 2160×3840 | 17.0 | **43.0** | +153% |

## 本机正式版 37.2.13 + API 36 Host GPU

为了排除标题中的混淆，我单独使用本机 SDK 自带的正式版 emulator：

```text
~/Library/Application Support/AVDDesktop/sdk/emulator
```

版本和 GPU 日志：

```text
Android Emulator 37.2.13
Graphics Adapter: Apple M4
Graphics API: OpenGL ES 3.0 (4.1 Metal - 91.7)
```

使用同一动画 APK、API 36.1、4 vCPU / 4 GB，读取 APK 内部每秒 FPS：

| 分辨率 | 60 Hz 内部 FPS | 120 Hz 内部 FPS |
| --- | ---: | ---: |
| 720×1280 | 59.992 | 119.988 |
| 1080×1920 | 59.974 | 119.989 |
| 1440×2560 | 59.956 | 119.981 |
| 2160×3840 | 59.978 | 120.002 |

结论：

- macOS stable emulator 的 Metal host GPU 在 4K/120Hz 也能跑满 120 FPS；
- 不能把这组数据与 WebRTC 端到端 FPS 混为一谈；
- 本地 stable 37.2.13 的 gRPC service 列表里不存在任何 RTC 服务，旧/新 RTC RPC 都返回
  `UNIMPLEMENTED`；因此这个正式版不是配置问题，而是安装包未包含 WebRTC/video bridge；
- 之前 API 36 Preview 的 67/40/17 FPS 是 Preview 37.1.8 新引擎回退 lavapipe 后的 WebRTC 路径结果，不代表 stable Metal 渲染能力。

复现：

```bash
./scripts/measure-local-stable-gpu.sh
```

原始数据：[`local-stable-gpu.json`](results/local-stable-gpu.json)。
RTC 服务检查：[`local-stable-rtc-check.txt`](results/local-stable-rtc-check.txt)。

## 为什么 WebRTC 实验最终使用 Preview 37.1.8

Host GPU 渲染本身在 macOS stable emulator 上就能使用。这里的 Preview 是为了把
host GPU 渲染和 WebRTC 视频捕获同时跑通，完整条件是：

1. **API 37 镜像 + Preview 37.1.8**：该 Preview 新引擎在 API 36 下会回退到 Mesa
   lavapipe；stable emulator 的 API 36 host GPU 路径正常，但没有本实验需要的 RTC v2 服务。
2. **Emulator Preview 37.1.8**：stable 37.1.11/37.3.9 没有可用的新 RTC v2 服务。
3. **使用新 RTC 协议**：`android.emulation.v2.RtcService`，而不是旧的
   `android.emulation.control.v2.Rtc`。
4. **必须带 emulator 窗口**：新 host-GPU 的 `InProcessVideoSource` 在
   `-no-window` 下只产生首帧；OpenGL/WebRTC 会保持 idle。
5. **H.264 优先**：Chrome 默认 VP8，1080p 以上会损失大量帧率。

本实验新增了新版 protobuf 和 gateway 适配：

- `RtcStreamRequest.requested_tracks` 必须包含 `TRACK_TYPE_SCREEN_PRIMARY`；
- 使用 `RtcSession.session_id` 管理会话；
- `JsepMessage` 包含 `sdp` / `ice_candidate` oneof；
- 自动根据 `TARGET_API=37.x` 选择新 gateway 路径。

## 快速开始

```bash
cd webrtc-bench

# 首次安装 API 37 镜像、preview emulator、gateway、前端和动画 APK
./scripts/setup-hostgpu.sh

# Host GPU 完整矩阵
./scripts/run-hostgpu.sh

# 单分辨率交互查看
./scripts/dev-hostgpu.sh 1080x1920
```

`dev-hostgpu.sh` 会显示 emulator 窗口；网页仍然通过官方 React 组件观看和测速。

## API 36 软件 GPU 基线

保留原基线脚本用于对比：

```bash
VSYNC=120 \
RESOLUTIONS="720x1280 1080x1920 1440x2560 2160x3840" \
DURATION=20 WARMUP=5 CODEC=h264 \
./scripts/run-benchmark.sh
```

API 36 推荐只使用 720p @ 120Hz；1080p 约 67 FPS，1440p 约 40 FPS，4K 约 17 FPS。

## 测量方法

1. 从现有 `MyDevice.avd/config.ini` 派生临时 AVD；
2. 写入目标 `hw.lcd.width/height/vsync`；
3. 使用实验目录内独立 datadir 启动；
4. 执行 `wm size reset` / `wm density reset`，避免历史覆盖；
5. 120 Hz 时设置 `peak_refresh_rate=120` / `min_refresh_rate=120`；
6. 安装全屏 120 Hz Canvas 动画 APK，持续产生新帧；
7. 官方 React 组件通过 Python gateway 建立 WebRTC；
8. 每秒记录 `requestVideoFrameCallback`、`inbound-rtp.framesPerSecond`、码率、丢帧和 codec；
9. 丢弃 5 秒 warm-up，对随后 20 秒采样计算 median / P10。

页面预览为 360×640 CSS px；WebRTC 解码帧保持模拟器原生分辨率。

## 目录

```text
src/                 React 测速页与 WebRTC stats hook
gateway/             官方 gateway + 新旧 RTC protobuf + 双协议实现
motion-app/          60/120 Hz 全屏动画测试 APK 源码
scripts/             安装、启动、host-GPU 测速与清理脚本
results/             JSON、summary 和截图（本地生成）
BENCHMARK.md         完整结果
OPTIMIZATIONS.md     优化实验、采纳项与拒绝项
```

## 兼容性修正

1. `android-emulator-webrtc@2.0.1` 缺少 `main/types`，且 `dist/index.js` 未导出
   `logger`；页面直接引用 dist 组件。
2. 官方 Python gateway 只支持旧 `android.emulation.control.v2.Rtc`；本实验新增
   `android.emulation.v2.RtcService` gateway。
3. 新 RTC 服务的 `requested_tracks` 为必需字段，旧 gateway 无法正确请求视频轨道。
4. emulator allowlist 增加新旧 RTC、reflection 服务。
5. 新 host-GPU 视频源在 headless 模式下不出连续帧，本实验通过有窗口启动解决。
6. Chrome 默认 VP8；测速页在 SDP offer 中把 H.264 payload 提前。

## 注意

- API 37 Preview host GPU 模式需要保留 emulator 窗口；这不是纯 `-no-window` 模式。
- API 36 Preview fallback 可以无窗口，但这套 Preview 新引擎会使用 lavapipe；stable emulator
  的 host GPU 能力不适用于该 WebRTC 路径，因为 stable 缺少 RTC v2 服务。
- 4K/120Hz 约 43 FPS，当前瓶颈已经转到高分辨率视频编码/帧传输，而不是 GPU 合成。
