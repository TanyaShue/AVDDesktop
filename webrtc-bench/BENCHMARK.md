# Benchmark Report

## 最终环境

- Host: Apple M4, 10 CPU cores, 16 GB RAM, macOS 27.2.1 arm64
- Android Emulator Preview: 37.1.8 (build 16833367)
- System image: Android 37.0 Google APIs arm64-v8a
- AVD: 从现有 `MyDevice` 配置派生，独立 benchmark datadir
- Guest: 4 vCPU / 4 GB RAM
- GPU: Apple M4 host GPU，`virtio-gpu-gfxstream`
- Display: 720×1280、1080×1920、1440×2560、2160×3840；60/120 Hz
- Browser: Playwright Chromium 145 headless
- Frontend: `android-emulator-webrtc` 2.0.1
- RTC: `android.emulation.v2.RtcService`
- 启动模式: **有 emulator 窗口**，`SHOW_WINDOW=1`
- Workload: 全屏 Canvas 动画 APK，内部约 120 FPS
- 每项采样：5 s warm-up + 20 s measurement

## Apple M4 Host GPU 结果

### H.264

| 分辨率 | Hz | 渲染 FPS | P10 | 解码 FPS | 码率 | 丢帧 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 720×1280 | 60 | 60.0 | 59.0 | 60 | 2.29 Mbps | 0 |
| 720×1280 | 120 | **119.9** | 118.0 | **120** | 2.38 Mbps | 0 |
| 1080×1920 | 60 | 60.0 | 59.1 | 60 | 2.39 Mbps | 0 |
| 1080×1920 | 120 | **119.1** | 109.9 | **120** | 2.53 Mbps | 0 |
| 1440×2560 | 60 | 60.0 | 59.8 | 60 | 2.38 Mbps | 0 |
| 1440×2560 | 120 | **78.2** | 76.1 | 78 | 2.51 Mbps | 0 |
| 2160×3840 | 60 | 47.7 | 45.0 | 48 | 2.52 Mbps | 0 |
| 2160×3840 | 120 | **43.0** | 41.9 | 43 | 2.51 Mbps | 0 |

### VP8 / 120 Hz

| 分辨率 | 渲染 FPS | P10 | 解码 FPS |
| --- | ---: | ---: | ---: |
| 720×1280 | 92.0 | 90.0 | 95 |
| 1080×1920 | 43.0 | 40.0 | 43 |
| 1440×2560 | 33.1 | 32.8 | 33 |
| 2160×3840 | 18.0 | 17.0 | 18 |

H.264 在 1080p/120Hz 是 VP8 的约 2.8 倍，在 1440p 是约 2.4 倍。

## 本机正式版 37.2.13 + API 36 Host GPU（内部渲染）

本机 SDK 正式版 emulator 37.2.13 使用：

```text
Graphics Adapter: Apple M4
Graphics API: OpenGL ES 3.0 (4.1 Metal - 91.7)
```

该构建没有 RTC 服务，不能测网页 WebRTC，但可以准确测同一动画 APK 的 GPU 内部渲染 FPS：

| 分辨率 | 60 Hz | 120 Hz |
| --- | ---: | ---: |
| 720×1280 | 59.992 | 119.988 |
| 1080×1920 | 59.974 | 119.989 |
| 1440×2560 | 59.956 | 119.981 |
| 2160×3840 | 59.978 | 120.002 |

因此，macOS stable host GPU 渲染本身在 4K/120Hz 都不是瓶颈。它与 WebRTC 结果的区别是：
stable 缺少 RTC 服务，不能把内部渲染帧传给浏览器。

## API 36 软件 GPU 对照

这里的 API 36 对照是 **Preview 37.1.8 的新引擎 + API 36**，该组合会明确提示 host GPU
不可用并回退 lavapipe。macOS stable emulator 本身并不缺少 host GPU 渲染能力；
stable 缺少的是本实验所需的 RTC v2 服务。120 Hz H.264：

| 分辨率 | 渲染 FPS | 解码 FPS |
| --- | ---: | ---: |
| 720×1280 | 119.0 | 120 |
| 1080×1920 | 67.0 | 67 |
| 1440×2560 | 39.9 | 40 |
| 2160×3840 | 17.0 | 17 |

## 分析

### 1080p 已能跑满 120 Hz

Host GPU 将 1080p/120Hz 从 67 提升到 119 FPS，浏览器呈现与 WebRTC 解码基本重合。
`requestVideoFrameCallback` 略低于 120 是 Chrome compositor 采样抖动，不是丢帧。

### 1440p 约 78 FPS

GPU 合成瓶颈已经解除，但 1440p×120Hz 需要约 442 MP/s。当前视频采集、NV12 转换、
H.264 编码/传输链路只能稳定提供约 78 FPS。

### 4K 约 43 FPS

4K 的 60 Hz 与 120 Hz 结果分别是 47.7 和 43.0 FPS，说明已经进入高分辨率编码/搬运瓶颈。
继续提高显示刷新率不会显著提高帧率。

### VP8 不再是可选方案

Host GPU 下 VP8 1080p 只有 43 FPS，而 H.264 为 119 FPS。H.264 是必须启用的路径。

## 关键实现

### 新 RTC 服务

API 37 Preview 使用：

```text
android.emulation.v2.RtcService
```

而不是旧的：

```text
android.emulation.control.v2.Rtc
```

新 `RtcStreamRequest` 需要：

```proto
repeated TrackType requested_tracks = 1;
IceServerConfig ice_server_config = 3;
```

`TrackType.TRACK_TYPE_SCREEN_PRIMARY` 必须显式请求。

### Host GPU 不是纯 headless

使用 `-no-window` 时：

```text
InProcessVideoSource: Display ... is unavailable; WebRTC video track will remain idle.
```

因此 API 37 host-GPU 路径必须使用窗口模式。窗口可以放在后台，但不能用 `-no-window`。

## 原始数据

- `results/hostgpu-summary.json`
- `results/fps-<resolution>-<codec>-<hz>hz-api37-hostgpu.json`
- `results/fps-1080x1920-h264-120hz-api37-hostgpu.png`
