# Optimization Report

本文件记录实际测试结果。最终方案已经从 **Preview 37.1.8 + API 36 + lavapipe**
升级为 **Preview 37.1.8 + API 37 + Apple M4 host GPU + 新 RTC v2 协议**。

注意：macOS stable emulator 本身支持 Metal host GPU；使用 Preview 的原因是需要新版
`android.emulation.v2.RtcService`，不是为了让 Mac 获得 host GPU。

## 最终最优配置

```bash
./scripts/run-hostgpu.sh
```

等价环境：

```bash
TARGET_API=37.0 \
VSYNC=120 \
SHOW_WINDOW=1 \
CODEC=h264 \
RESOLUTIONS="720x1280 1080x1920 1440x2560 2160x3840" \
DURATION=20 WARMUP=5 \
./scripts/run-benchmark.sh
```

交互查看：

```bash
./scripts/dev-hostgpu.sh 1080x1920
```

## 本机正式版 Metal Host GPU 独立测试

本机 SDK 自带正式版 emulator 37.2.13 + API 36.1 使用 Apple M4 Metal host GPU。
只测 GPU/APK 内部渲染，不经过 WebRTC：

| 分辨率 | 60 Hz | 120 Hz |
| --- | ---: | ---: |
| 720×1280 | 59.992 | 119.988 |
| 1080×1920 | 59.974 | 119.989 |
| 1440×2560 | 59.956 | 119.981 |
| 2160×3840 | 59.978 | 120.002 |

结论：正式版 Metal host GPU 在所有测试分辨率都能跑满刷新率。它不能用于本 WebRTC
项目的原因不是 GPU，而是正式版 37.2.13 没有 RTC 服务。对正式版进行 gRPC reflection
后，服务列表中没有 `Rtc` / `RtcService`，旧/新 RTC RPC 均返回 `UNIMPLEMENTED`。
安装包检查也显示 stable 不包含 `lib/qemu/libgoldfish_aarch64.dylib`，而 Preview 包含
`goldfish::videobridge::RtcService`。

## Host GPU 带来的收益

120 Hz / H.264：

| 分辨率 | API 36 lavapipe | API 37 Apple M4 | 提升 |
| --- | ---: | ---: | ---: |
| 720×1280 | 119.0 | 119.9 | 已到刷新率上限 |
| 1080×1920 | 67.0 | 119.1 | +78% |
| 1440×2560 | 39.9 | 78.2 | +96% |
| 2160×3840 | 17.0 | 43.0 | +153% |

## 已采纳的优化

| 优化 | 作用 |
| --- | --- |
| API 37.0 镜像 + Preview | 让新引擎的 host GPU 与 RTC v2 视频捕获共存 |
| 新 `android.emulation.v2.RtcService` | API 37 实际 RTC 服务 |
| `requested_tracks=TRACK_TYPE_SCREEN_PRIMARY` | 请求主屏视频轨道 |
| `SHOW_WINDOW=1` | 让 `InProcessVideoSource` 持续收到 host-GPU 帧 |
| H.264 SDP 优先 | 1080p/120Hz：VP8 43 → H.264 119 FPS |
| 4 vCPU / 4 GB | 8 vCPU 对瓶颈无收益 |
| 关闭摄像头、显式 `-vsync-rate` | 减少无关负载 |
| `wm size/density reset` | 防止历史 override 污染 |
| 独立预热 datadir | 避免跨 API/分辨率污染 |
| 动态 60/120 FPS 图表目标 | 结果展示一致 |

## 已拒绝的优化

### 提高 bitrate

| bitrate 声明 | 实际码率 | API36 1080p/120 FPS |
| --- | ---: | ---: |
| 默认 | 2.3 Mbps | 67 |
| 10 Mbps | 3.5 Mbps | 65 |
| 20 Mbps | 6.2 Mbps | 62 |

更高码率没有增加帧率，反而增加编码负载。Host GPU 下也保持默认码率。

### H.264 Main / High profile

当前 emulator 只稳定协商 Constrained Baseline `42e01f`。强制 Main/High payload 会回退 VP8。

### 8 vCPU / 6 GB

1440p/120Hz 与 4 vCPU / 4 GB 基本相同，不是 CPU 核心数瓶颈。

### Chrome GPU flags / compositor layer

`--enable-zero-copy`、`--enable-accelerated-video-decode`、`--ignore-gpu-blocklist`、
`translateZ(0)` 等组合在 headless 下从基线 119 降到约 110 FPS，已回退。

### 纯无头 host-GPU

API 37 Preview host GPU + `-no-window` 时 MediaTrack 只产生首帧：

```text
InProcessVideoSource: Display ... is unavailable; WebRTC video track will remain idle.
```

因此 host-GPU 模式必须带窗口。

## 当前瓶颈

Host GPU 解决后：

- 1080p/120Hz 已跑满，浏览器 119 FPS；
- 1440p/120Hz 约 78 FPS，瓶颈变为视频采集/NV12 转换/H.264 编码；
- 4K 约 43–48 FPS，编码与高分辨率帧搬运是主要限制。

## 推荐策略

| 目标 | 推荐配置 |
| --- | --- |
| 120 FPS | 1080×1920，120 Hz，H.264，host GPU |
| 60 FPS | 1440×2560，60 Hz，H.264，host GPU |
| 4K | 约 43–48 FPS；不能稳定 60 |
| 无窗口/兼容模式 | API 36，720p @ 120Hz |
