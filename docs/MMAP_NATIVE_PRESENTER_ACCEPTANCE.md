# MMAP 原生 Presenter 验收记录

## 验收环境

- 日期：2026-09-24
- 平台：Windows / amd64
- Android Emulator：37.1.11.0
- AVD：`MyDevice`，720×1280
- 图形后端：`-gpu host`（验证 renderer 输出时使用）

## 自动化门禁

以下命令全部通过：

```powershell
go test -count=1 ./...

cd frontend
npm run build
cd ..

$env:AVDDESKTOP_PRESENTER_SMOKE = "1"
go test -count=1 -run 'TestWindowsPresenter(Smoke|ToolbarClose)$' ./internal/presenter

wails build
```

`wails build` 成功生成：

```text
build/bin/AVDDesktop.exe
```

## 主程序控制面端到端

使用最终生产二进制验证 `EmulatorService(Start) → DisplayService.OpenNative → Close`：

```powershell
$env:AVDDESKTOP_E2E_HOME = (Resolve-Path "build/bin").Path
$env:AVDDESKTOP_NATIVE_BINARY = (Resolve-Path "build/bin/AVDDesktop.exe").Path
go test -tags e2e -count=1 -timeout 20m -run '^TestE2E_NativePresenter$' -v ./internal/e2e
```

结果：

```text
--- PASS: TestE2E_NativePresenter (39.10s)
PASS
ok  AVDDesktop/internal/e2e  41.263s
```

该测试覆盖：

1. 真实 AVD 以 `CustomUI` 启动。
2. `DisplayService.OpenNative` 拉起同一可执行文件的 `--display-host` 辅助进程。
3. 等待 `presenter.OnReady` 后才返回成功。
4. 8 秒稳定性观察期间 Presenter 未异常退出。
5. `Close` 后辅助进程和会话被回收。

## 真实交互验证

### MMAP 画面

- 使用 ADB `screencap` 获取设备原始画面。
- 使用 `PrintWindow` 获取 Windows Presenter 窗口内容。
- 两张图呈现同一锁屏壁纸、时间和布局，证明画面已通过 MMAP → 共享内存 → 原生 Presenter 到达窗口。

### 独立工具栏

- 设备进入 `Settings` 后，向工具栏 Back 按钮发送真实 `BM_CLICK`。
- 点击后 `com.android.settings` 不再出现在 top activity，验证 Back 按钮经 ADB keyevent 注入设备。

### 触摸映射

- 从 UI Automator 取得 `Connected devices` 条目的设备坐标。
- 将设备坐标映射到 Presenter client 坐标并发送 `WM_LBUTTONDOWN/WM_LBUTTONUP`。
- top resumed activity 从：
  `com.android.settings/.homepage.SettingsHomepageActivity`
  变为：
  `com.android.settings/.SubSettings`
- 证明 Presenter 鼠标事件确实经 gRPC `sendTouch` 到达 Android。

### 生命周期清理

- Close 后无 Presenter 子进程残留。
- 停止设备后无 emulator / qemu 残留。
- 共享内存文件由 `Region.Close` 清理。

## 已知限制

- 当前 Windows presenter 使用 CPU 帧复制、RGBA→BGRA 转换及 GDI 展示，未实现 DMA-BUF / GPU 零拷贝。
- MMAP 为单缓冲语义，官方文档提示可能 tearing；当前实现用帧消息后的复制降低风险，但未做大分辨率/持续动画压力基准。
- macOS / Linux 不提供原生 Presenter，自动回退到应用内 MJPEG 设备窗口。
- gRPC 默认未启用 token 认证。
