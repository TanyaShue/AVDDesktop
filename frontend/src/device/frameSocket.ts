// 原始帧 WebSocket 客户端。
//
// 服务端每条二进制消息由 32 字节小端帧头和像素负载组成。这里只负责校验、解析和
// 订阅生命周期；连接断开后自动重连，画面消费端只会在有订阅者时收到最新帧。
export interface RawFrame {
  width: number;
  height: number;
  format: number;
  flags: number;
  seq: number;
  timestampUnixUs: number;
  /** 直接看向当前 WebSocket 消息的 ArrayBuffer，不在解析阶段复制像素。 */
  payload: Uint8Array;
}

export interface FrameSocketStats {
  connected: boolean;
  recvFps: number;
  dropped: number;
}

const HEADER_SIZE = 32;
const MAGIC = 0x46445641;
const VERSION = 1;
const MIN_RETRY_MS = 250;
const MAX_RETRY_MS = 5_000;
const RECV_WINDOW_MS = 1_000;
const RECV_RING_SIZE = 256;

/** 解析失败时返回 null，不把异常扩散到 WebSocket 事件回调。 */
function parseFrame(data: unknown): RawFrame | null {
  if (!(data instanceof ArrayBuffer) || data.byteLength < HEADER_SIZE) return null;

  const view = new DataView(data);
  const magic = view.getUint32(0, true);
  const version = view.getUint16(4, true);
  const width = view.getUint16(8, true);
  const height = view.getUint16(10, true);
  const payloadBytes = view.getUint32(28, true);

  if (magic !== MAGIC || version !== VERSION || width === 0 || height === 0) return null;
  if (payloadBytes !== data.byteLength - HEADER_SIZE) return null;

  return {
    width,
    height,
    format: view.getUint16(12, true),
    flags: view.getUint16(6, true),
    seq: view.getUint32(16, true),
    timestampUnixUs: Number(view.getBigUint64(20, true)),
    payload: new Uint8Array(data, HEADER_SIZE, payloadBytes),
  };
}

export class FrameSocket {
  private readonly url: string;
  private readonly token: string;
  private readonly onFrame: (frame: RawFrame) => void;

  private socket: WebSocket | null = null;
  private started = false;
  private closed = false;
  private retryDelay = MIN_RETRY_MS;
  private reconnectTimer: number | null = null;
  private flushTimer: number | null = null;
  private pendingFrame: RawFrame | null = null;
  private dropped = 0;

  // 用固定环形窗口统计最近 1 秒的接收帧数，避免每帧分配数组。
  private readonly recvAt = new Float64Array(RECV_RING_SIZE);
  private recvIndex = 0;
  private recvCount = 0;

  constructor(url: string, token: string, onFrame: (frame: RawFrame) => void) {
    this.url = url;
    this.token = token;
    this.onFrame = onFrame;
  }

  /** 幂等启动；主动 close 后不会被后续 start 重新拉起。 */
  start(): void {
    if (this.started || this.closed) return;
    this.started = true;
    this.connect();
  }

  /** 主动关闭时清除重连和待交付帧，不再重新连接。 */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.started = false;
    this.clearReconnect();
    this.clearFlush();
    this.pendingFrame = null;

    const socket = this.socket;
    this.socket = null;
    if (socket && socket.readyState < WebSocket.CLOSING) {
      socket.close();
    }
  }

  stats(): FrameSocketStats {
    const cutoff = performance.now() - RECV_WINDOW_MS;
    let received = 0;
    for (let i = 0; i < this.recvCount; i += 1) {
      if (this.recvAt[i] >= cutoff) received += 1;
    }

    return {
      connected: this.socket?.readyState === WebSocket.OPEN,
      recvFps: received,
      dropped: this.dropped,
    };
  }

  private connect(): void {
    if (this.closed) return;

    let socket: WebSocket;
    try {
      socket = new WebSocket(`${this.url}?token=${encodeURIComponent(this.token)}`);
    } catch {
      this.scheduleReconnect();
      return;
    }

    socket.binaryType = "arraybuffer";
    this.socket = socket;

    socket.onopen = () => {
      if (this.closed || this.socket !== socket) return;
      this.retryDelay = MIN_RETRY_MS;
    };

    socket.onmessage = (event: MessageEvent<ArrayBuffer>) => {
      if (this.closed || this.socket !== socket) return;
      const frame = parseFrame(event.data);
      if (!frame) return;
      this.recordReceive();
      this.enqueue(frame);
    };

    socket.onerror = () => {
      if (this.socket !== socket) return;
      // error 之后浏览器不保证立刻触发 close，主动关闭以统一进入退避重连。
      if (socket.readyState < WebSocket.CLOSING) socket.close();
    };

    socket.onclose = () => {
      if (this.socket === socket) this.socket = null;
      if (this.closed) return;
      this.scheduleReconnect();
    };
  }

  private scheduleReconnect(): void {
    if (this.closed || this.reconnectTimer !== null) return;

    const delay = this.retryDelay;
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
    this.retryDelay = Math.min(this.retryDelay * 2, MAX_RETRY_MS);
  }

  private clearReconnect(): void {
    if (this.reconnectTimer === null) return;
    window.clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
  }

  private clearFlush(): void {
    if (this.flushTimer === null) return;
    window.clearTimeout(this.flushTimer);
    this.flushTimer = null;
  }

  /**
   * 单槽 pending 队列：上一帧尚未交给画布时直接替换，并在下一轮任务交付最新帧。
   * 这样网络突发不会把旧帧排进画布，也不会阻塞 WebSocket 事件线程。
   */
  private enqueue(frame: RawFrame): void {
    if (this.pendingFrame) this.dropped += 1;
    this.pendingFrame = frame;
    if (this.flushTimer !== null) return;

    this.flushTimer = window.setTimeout(() => {
      this.flushTimer = null;
      const pending = this.pendingFrame;
      this.pendingFrame = null;
      if (!pending || this.closed) return;
      try {
        this.onFrame(pending);
      } catch (err: unknown) {
        console.error("[display] raw frame callback failed", err);
      }
    }, 0);
  }

  private recordReceive(): void {
    this.recvAt[this.recvIndex] = performance.now();
    this.recvIndex = (this.recvIndex + 1) % RECV_RING_SIZE;
    if (this.recvCount < RECV_RING_SIZE) this.recvCount += 1;
  }
}