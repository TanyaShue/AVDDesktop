# 设备画面原始帧通道协议（Phase 2）

> 状态：Phase 2 实施契约
> 目标：把自定义 UI 的主画面链路从 “RGBA → JPEG → MJPEG → <img>” 换成
> “RGBA → WebSocket 二进制帧 → WebGL2 纹理 → rAF”，MJPEG 只保留为回落。

## 1. 传输与语义

- 服务端：`internal/framestream` 包，监听地址复用 `internal/display.Server` 的 127.0.0.1 随机端口。
- 端点：`GET /ws/display/{id}?token=<token>`（WebSocket Upgrade）。
- 每条 WebSocket 二进制消息 = 32 字节帧头 + 1 帧负载。
- 关闭 permessage-deflate；不做文本消息。
- 服务端遵循“只保留最新帧、慢客户端丢帧、Publish 永不阻塞生产者”。
- 前端遵循“只保留最新帧、每 vsync 最多上传/绘制一次、没有新帧不重绘”。

## 2. 帧头（固定 32 字节，小端序）

| 偏移 | 长度 | 字段 | 说明 |
|---:|---:|---|---|
| 0 | 4 | magic | `0x46445641`（ASCII `AVDF` 的小端 uint32） |
| 4 | 2 | version | 当前 1 |
| 6 | 2 | flags | bit0=行序自底向上；其余保留 |
| 8 | 2 | width | 像素宽 |
| 10 | 2 | height | 像素高 |
| 12 | 2 | format | 0=RGBA8888，1=RGB888，2=JPEG（回落专用） |
| 14 | 2 | reserved | 0 |
| 16 | 4 | seq | 模拟器 FrameMeta.Seq |
| 20 | 8 | timestampUnixUs | 服务端发布时刻（Unix 微秒） |
| 28 | 4 | payloadBytes | 负载长度，必须等于字节数 |

v1 只发送 `RGBA8888`；`RGB888`/`JPEG` 为后续优化与回落预留。

## 3. Go API（冻结）

```go
package framestream

type Format uint16

const (
    FormatRGBA8888 Format = 0
    FormatRGB888   Format = 1
    FormatJPEG     Format = 2
)

const (
    HeaderSize = 32
    FlagBottomUp uint16 = 1 << 0
)

type Frame struct {
    Pix       []byte
    Width     int
    Height    int
    Format    Format
    Seq       uint32
    Timestamp time.Time
    BottomUp  bool

    // Release 由生产者在缓冲不再被任何订阅者使用时调用；Hub 取得 Pix 的所有权。
    // 允许为 nil。
    Release func()
}

type Options struct {
    Token        string
    WriteTimeout time.Duration // 默认 250ms
    Logf         func(format string, args ...any)
}

func NewHub(opts Options) *Hub

// Publish 取得 frame 的所有权，立即返回；旧的最新帧和慢订阅者未消费的帧会走 Release。
func (h *Hub) Publish(frame Frame)

// ServeWS 处理一次 WebSocket 订阅。token 从 query 读取并校验。
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request)

func (h *Hub) Stats() Stats
func (h *Hub) Close()
```

`Stats` 至少包含：`Subscribers`、`PublishTotal`、`PublishFPS`、`DropForClient`、
`BytesPerSec`、`LastPublishUnixMs`。

实现要点：

- Hub 内部对同一 `Frame` 使用引用计数：latest 持 1 个引用，每个订阅者的 1 槽 mailbox 各持 1 个引用；
- 发布新帧时，若订阅者 mailbox 已有旧帧，释放旧帧并记为一次 `DropForClient`；
- 订阅者写协程用 `conn.NextWriter(BinaryMessage)` 先写帧头再写负载，关闭 writer 后释放帧；
- 写超时/连接断开：注销订阅者并释放其持有的帧；
- `Close` 释放 latest、关闭所有订阅者、唤醒等待中的写协程。

## 4. 前端 API（冻结）

`DeviceSession` 增加：

```ts
rawUrl: string;       // ws://127.0.0.1:随机端口/ws/display/{id}
rawToken: string;     // 一次性随机 token
rawFormat: string;    // "rgba8888"
rawBottomUp: boolean; // 行序标志
```

新增 `frontend/src/device/frameSocket.ts`：

```ts
export interface RawFrame {
  width: number;
  height: number;
  format: number;
  flags: number;
  seq: number;
  timestampUnixUs: number;
  payload: Uint8Array; // 直接看向 WebSocket 消息的 ArrayBuffer
}

export class FrameSocket {
  constructor(url: string, token: string, onFrame: (frame: RawFrame) => void);
  start(): void;
  close(): void;
  stats(): { connected: boolean; recvFps: number; dropped: number };
}
```

新增 `frontend/src/device/FrameCanvas.tsx`：

- `webgl2` context，`alpha:false`、`antialias:false`、`depth:false`、`stencil:false`；
- `UNPACK_FLIP_Y_WEBGL` 按 `rawBottomUp` 设置；
- 只在收到新帧且 rAF 未挂起时安排一次 `requestAnimationFrame`；每帧最多一次 `texImage2D` + draw；
- 画布 drawingBuffer 尺寸 = 流尺寸，CSS 等比 fit；
- 统计：`presentFps`、`frameIntervalP95`、`uploadMsP95`、`dropped`；
- WebGL2 不可用或 context 连续丢失两次 → `onFallback()`，DeviceWindowApp 切回 `<img>` MJPEG。

## 5. 集成点

- `internal/display.Session` 增加 hub 挂载与 `PublishRaw`；`Server.ServeHTTP` 增加
  `/ws/display/{id}` 路由；现有 `/display/{id}`（MJPEG）保持不变。
- `internal/displayhost`：
  - 生成随机 token，创建 Hub 并挂到 session；
  - `DeviceSession` 返回 `rawUrl/rawToken/rawFormat/rawBottomUp`；
  - framePump：有 MJPEG 订阅者才编码 JPEG；随后把同一帧的所有权交给 Hub；
    两者都没有订阅者时直接 Release。
- 前端 `DeviceWindowApp`：优先 FrameCanvas；`<img>` MJPEG 只作为回落。

## 6. 非目标（本期不做）

- SharedArrayBuffer 零拷贝（Wails 绑定/跨进程映射限制，保持 WebSocket 路径）；
- 音频、多显示器、折叠屏；
- RGB888 / JPEG-over-WebSocket 作为默认路径（仅预留格式字段）。
