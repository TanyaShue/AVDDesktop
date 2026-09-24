# MMAP 共享内存 + 独立原生 Presenter + 独立工具栏

## 目标

自定义 UI 启动设备后，不再由主窗口 WebView 承载设备画面：

```text
emulator renderer
  -> streamScreenshot(ImageTransport.MMAP)
  -> 共享内存中的 RGBA 帧
  -> 独立原生 Presenter 窗口
  -> 独立原生工具栏窗口
```

主窗口只负责设备管理、启动停止、任务、日志和设置。设备画面与工具栏不再作为主窗口内的 React 浮层。

严格意义上的“emulator renderer 直接把 surface 挂到 AVDDesktop 创建的 HWND”无法通过 stock SDK 实现，本方案采用官方允许的 MMAP 帧传输。它是原生显示链路，不是 renderer 零拷贝直绘。

## 阶段与提交边界

### 阶段 1：协议和共享内存基础

交付：

- 补齐 `ImageFormat.transport` 与 `ImageTransport.MMAP` proto 字段并重新生成 Go 代码。
- `emulatorgrpc` 增加 MMAP 画面流 API，保留现有截图流作为兼容路径。
- 新增跨平台共享内存封装：
  - Windows 使用 file-backed memory mapping。
  - 非 Windows 使用 mmap fallback。
- 单元测试覆盖字段编码、请求参数、共享内存生命周期和尺寸校验。

提交：`feat(display): add mmap screenshot transport`

### 阶段 2：原生 Presenter 与工具栏

交付：

- 新增辅助进程模式，可由 AVDDesktop 可执行文件直接启动。
- Windows 原生 Presenter：
  - 独立顶层窗口；
  - 消费 MMAP 帧；
  - RGBA 到显示缓冲转换；
  - 保持设备宽高比；
  - 鼠标事件转换为设备触摸事件；
  - DPI、缩放、拖动和窗口生命周期处理。
- 独立工具栏窗口：
  - Back / Home / AppSwitch；
  - 关闭 Presenter；
  - 停止设备所需的命令入口。
- 非 Windows 返回 unsupported，由现有 WebView/MJPEG 路径兜底。

提交：`feat(display): add native presenter and toolbar`

### 阶段 3：Wails 集成与生命周期

交付：

- `DisplayService` 增加 native presenter 会话。
- 自定义 UI 启动时优先打开原生 Presenter。
- 原生窗口关闭、设备退出、应用退出时正确回收子进程和共享内存。
- 重复打开保持幂等；同一实例只允许一个 Presenter。
- 前端不再为 native 会话挂载主窗口设备浮层。
- 工具栏按钮保持现有返回、主页、多任务语义。

提交：`feat(display): integrate native custom UI lifecycle`

### 阶段 4：统一验收与文档

交付：

- `go test ./...`
- 前端 `npm run build`
- Wails 构建。
- Windows 真机 smoke：
  - 启动 AVD；
  - 确认没有主窗口设备浮层；
  - Presenter 独立出现；
  - 工具栏独立出现；
  - 画面更新；
  - 触摸和导航键可用；
  - 关闭 Presenter 不停止设备；
  - 停止设备后 Presenter 自动退出；
  - 应用退出不残留进程和映射文件。
- README / ARCHITECTURE 更新。
- 验收结果写入 `docs/MMAP_NATIVE_PRESENTER_ACCEPTANCE.md`。

提交：`docs(display): document native presenter acceptance`

## 并行工作流

- Agent A：proto、gRPC MMAP 客户端、相关单测。
- Agent B：共享内存封装、平台实现、生命周期单测。
- Agent C：原生 Presenter、独立工具栏、窗口坐标与输入映射单测。
- 主流程：阶段契约、Wails 集成、冲突合并、统一验收、分阶段提交。

所有 agent 使用互斥写集。跨包接口先由主流程冻结，agent 不修改对方拥有的文件。

## 验收门禁

每个阶段必须同时满足：

1. 代码在 Windows 上可编译。
2. 新增测试通过。
3. 现有 `go test ./...` 不回归。
4. 涉及前端时通过 `npm run build`。
5. 不把 MMAP 映射文件、子进程或 gRPC 连接遗留到应用退出之后。

## 明确不做的短期项

- 不 fork Android Emulator，不注入 DLL，不复制 `aemu-gl-init` 内部实现。
- 不实现 DMA-BUF / 原生 GPU handle 零拷贝。
- 不在首版同时交付 macOS/Linux 原生显示后端；这些平台继续使用现有 MJPEG 兜底。
