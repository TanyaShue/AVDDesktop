// 原始 RGBA 帧的 WebGL2 画布。
//
// 网络帧只在内部 ref 中暂存，不进入 React 状态；每次 vsync 最多上传并绘制一帧，
// 没有新帧时不会重绘。WebGL2 不可用或上下文连续丢失两次时回落到 MJPEG。
import { useEffect, useRef } from "react";
import type { CSSProperties, PointerEventHandler } from "react";
import { FrameSocket } from "./frameSocket";
import type { RawFrame } from "./frameSocket";

const RAW_FORMAT_RGBA8888 = 0;
const SAMPLE_RING_SIZE = 256;
const STATS_INTERVAL_MS = 1_000;

const WEBGL_OPTIONS: WebGLContextAttributes = {
  alpha: false,
  antialias: false,
  depth: false,
  stencil: false,
  powerPreference: "high-performance",
};

const VERTEX_SHADER = `#version 300 es
in vec2 aPosition;
in vec2 aTexCoord;
out vec2 vTexCoord;
void main() {
  gl_Position = vec4(aPosition, 0.0, 1.0);
  vTexCoord = aTexCoord;
}
`;

const FRAGMENT_SHADER = `#version 300 es
precision mediump float;
uniform sampler2D uTexture;
in vec2 vTexCoord;
out vec4 outColor;
void main() {
  outColor = texture(uTexture, vTexCoord);
}
`;

export interface FrameCanvasStats {
  presentFps: number;
  frameIntervalP95: number;
  uploadMsP95: number;
  dropped: number;
}

interface FrameCanvasProps {
  url: string;
  token: string;
  rawBottomUp: boolean;
  initialWidth?: number;
  initialHeight?: number;
  className?: string;
  style?: CSSProperties;
  ariaLabel?: string;
  onFirstFrame: () => void;
  onFallback: () => void;
  onStats?: (stats: FrameCanvasStats) => void;
  onPointerDown?: PointerEventHandler<HTMLCanvasElement>;
  onPointerMove?: PointerEventHandler<HTMLCanvasElement>;
  onPointerUp?: PointerEventHandler<HTMLCanvasElement>;
  onPointerCancel?: PointerEventHandler<HTMLCanvasElement>;
}

let webgl2Support: boolean | null = null;

/** 结果在窗口生命周期内不会变化，缓存一次探测，避免重复创建探测上下文。 */
export function supportsWebGL2(): boolean {
  if (webgl2Support !== null) return webgl2Support;
  try {
    const canvas = document.createElement("canvas");
    webgl2Support = canvas.getContext("webgl2", WEBGL_OPTIONS) !== null;
  } catch {
    webgl2Support = false;
  }
  return webgl2Support;
}

function compileShader(
  gl: WebGL2RenderingContext,
  type: number,
  source: string,
): WebGLShader | null {
  const shader = gl.createShader(type);
  if (!shader) return null;
  gl.shaderSource(shader, source);
  gl.compileShader(shader);
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    console.error("[display] WebGL shader compile failed", gl.getShaderInfoLog(shader));
    gl.deleteShader(shader);
    return null;
  }
  return shader;
}

function createProgram(gl: WebGL2RenderingContext): WebGLProgram | null {
  const vertex = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER);
  if (!vertex) return null;
  const fragment = compileShader(gl, gl.FRAGMENT_SHADER, FRAGMENT_SHADER);
  if (!fragment) {
    gl.deleteShader(vertex);
    return null;
  }

  const program = gl.createProgram();
  if (!program) {
    gl.deleteShader(vertex);
    gl.deleteShader(fragment);
    return null;
  }

  gl.attachShader(program, vertex);
  gl.attachShader(program, fragment);
  gl.linkProgram(program);
  gl.deleteShader(vertex);
  gl.deleteShader(fragment);

  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    console.error("[display] WebGL program link failed", gl.getProgramInfoLog(program));
    gl.deleteProgram(program);
    return null;
  }
  return program;
}

/** 计算固定环形窗口中第 p 分位；只在 1Hz 采样点复制并排序。 */
function percentile(values: Float64Array, count: number, p: number): number {
  if (count === 0) return 0;
  const sorted = Array.from(values.subarray(0, count)).sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * p) - 1));
  return sorted[index];
}

function countWithin(values: Float64Array, count: number, cutoff: number): number {
  let total = 0;
  for (let i = 0; i < count; i += 1) {
    if (values[i] >= cutoff) total += 1;
  }
  return total;
}

export function FrameCanvas({
  url,
  token,
  rawBottomUp,
  initialWidth,
  initialHeight,
  className,
  style,
  ariaLabel,
  onFirstFrame,
  onFallback,
  onStats,
  onPointerDown,
  onPointerMove,
  onPointerUp,
  onPointerCancel,
}: FrameCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const callbacksRef = useRef({ onFirstFrame, onFallback, onStats });
  callbacksRef.current = { onFirstFrame, onFallback, onStats };

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const gl = canvas.getContext("webgl2", WEBGL_OPTIONS);
    if (!gl) {
      callbacksRef.current.onFallback();
      return;
    }

    let disposed = false;
    let fallbackCalled = false;
    let firstFrame = false;
    let contextLostCount = 0;
    let rafId: number | null = null;
    let latestFrame: RawFrame | null = null;
    let dropped = 0;

    let program: WebGLProgram | null = null;
    let vao: WebGLVertexArrayObject | null = null;
    let vertexBuffer: WebGLBuffer | null = null;
    let texture: WebGLTexture | null = null;

    // 固定长度窗口，避免每帧创建统计数组。
    const presentAt = new Float64Array(SAMPLE_RING_SIZE);
    const intervals = new Float64Array(SAMPLE_RING_SIZE);
    const uploads = new Float64Array(SAMPLE_RING_SIZE);
    let presentIndex = 0;
    let presentCount = 0;
    let intervalIndex = 0;
    let intervalCount = 0;
    let uploadIndex = 0;
    let uploadCount = 0;
    let lastPresentAt = 0;
    let statsWindowAt = performance.now();

    const onFallbackOnce = () => {
      if (disposed || fallbackCalled) return;
      fallbackCalled = true;
      callbacksRef.current.onFallback();
    };

    const disposeResources = () => {
      if (texture) gl.deleteTexture(texture);
      if (vertexBuffer) gl.deleteBuffer(vertexBuffer);
      if (vao) gl.deleteVertexArray(vao);
      if (program) gl.deleteProgram(program);
      texture = null;
      vertexBuffer = null;
      vao = null;
      program = null;
    };

    const createResources = (): boolean => {
      program = createProgram(gl);
      if (!program) return false;

      vao = gl.createVertexArray();
      vertexBuffer = gl.createBuffer();
      texture = gl.createTexture();
      if (!vao || !vertexBuffer || !texture) return false;

      // 两个三角形组成的全屏四边形；t=0 对应纹理首行。
      const vertices = new Float32Array([
        -1, -1, 0, 0,
        1, -1, 1, 0,
        -1, 1, 0, 1,
        1, 1, 1, 1,
      ]);
      gl.bindVertexArray(vao);
      gl.bindBuffer(gl.ARRAY_BUFFER, vertexBuffer);
      gl.bufferData(gl.ARRAY_BUFFER, vertices, gl.STATIC_DRAW);

      const position = gl.getAttribLocation(program, "aPosition");
      const texCoord = gl.getAttribLocation(program, "aTexCoord");
      gl.enableVertexAttribArray(position);
      gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 16, 0);
      gl.enableVertexAttribArray(texCoord);
      gl.vertexAttribPointer(texCoord, 2, gl.FLOAT, false, 16, 8);

      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, texture);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
      // 原始帧为自底向上时首行已经是 OpenGL 纹理底部，无需翻转；自顶向下才翻转。
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, rawBottomUp ? 0 : 1);

      gl.useProgram(program);
      const sampler = gl.getUniformLocation(program, "uTexture");
      if (sampler) gl.uniform1i(sampler, 0);
      gl.bindVertexArray(null);
      return true;
    };

    if (!createResources()) {
      disposeResources();
      onFallbackOnce();
      return;
    }

    const recordPresent = (now: number, uploadMs: number) => {
      presentAt[presentIndex] = now;
      presentIndex = (presentIndex + 1) % SAMPLE_RING_SIZE;
      if (presentCount < SAMPLE_RING_SIZE) presentCount += 1;

      if (lastPresentAt > 0) {
        intervals[intervalIndex] = now - lastPresentAt;
        intervalIndex = (intervalIndex + 1) % SAMPLE_RING_SIZE;
        if (intervalCount < SAMPLE_RING_SIZE) intervalCount += 1;
      }
      lastPresentAt = now;

      uploads[uploadIndex] = uploadMs;
      uploadIndex = (uploadIndex + 1) % SAMPLE_RING_SIZE;
      if (uploadCount < SAMPLE_RING_SIZE) uploadCount += 1;
    };

    const emitStats = (now: number) => {
      if (now - statsWindowAt < STATS_INTERVAL_MS) return;
      statsWindowAt = now;
      callbacksRef.current.onStats?.({
        presentFps: countWithin(presentAt, presentCount, now - STATS_INTERVAL_MS),
        frameIntervalP95: percentile(intervals, intervalCount, 0.95),
        uploadMsP95: percentile(uploads, uploadCount, 0.95),
        dropped,
      });
    };

    const presentLatest = () => {
      rafId = null;
      const frame = latestFrame;
      latestFrame = null;
      if (disposed || !frame || gl.isContextLost() || !program || !vao || !texture) return;

      if (frame.format !== RAW_FORMAT_RGBA8888) {
        onFallbackOnce();
        return;
      }

      const expectedBytes = frame.width * frame.height * 4;
      if (frame.payload.byteLength !== expectedBytes) return;

      if (canvas.width !== frame.width || canvas.height !== frame.height) {
        canvas.width = frame.width;
        canvas.height = frame.height;
      }

      // 画布比例正常时 viewBox 就是整张画布；这里仍按源比例计算一次，
      // 以便流尺寸变化时用深色清屏填满可能出现的信箱区。
      const sourceAspect = frame.width / frame.height;
      const bufferAspect = canvas.width / canvas.height;
      let viewX = 0;
      let viewY = 0;
      let viewWidth = canvas.width;
      let viewHeight = canvas.height;
      if (sourceAspect > bufferAspect) {
        viewHeight = canvas.width / sourceAspect;
        viewY = (canvas.height - viewHeight) / 2;
      } else if (sourceAspect < bufferAspect) {
        viewWidth = canvas.height * sourceAspect;
        viewX = (canvas.width - viewWidth) / 2;
      }

      gl.clearColor(0.024, 0.028, 0.038, 1);
      gl.clear(gl.COLOR_BUFFER_BIT);
      gl.viewport(viewX, viewY, viewWidth, viewHeight);
      gl.useProgram(program);
      gl.bindVertexArray(vao);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, texture);

      const uploadStarted = performance.now();
      gl.texImage2D(
        gl.TEXTURE_2D,
        0,
        gl.RGBA,
        frame.width,
        frame.height,
        0,
        gl.RGBA,
        gl.UNSIGNED_BYTE,
        frame.payload,
      );
      const uploadMs = performance.now() - uploadStarted;
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      gl.bindVertexArray(null);

      const now = performance.now();
      recordPresent(now, uploadMs);
      if (!firstFrame) {
        firstFrame = true;
        callbacksRef.current.onFirstFrame();
      }
      emitStats(now);
    };

    const onFrame = (frame: RawFrame) => {
      if (disposed) return;
      if (latestFrame) dropped += 1;
      latestFrame = frame;
      if (rafId === null) {
        rafId = window.requestAnimationFrame(presentLatest);
      }
    };

    const handleContextLost = (event: Event) => {
      event.preventDefault();
      contextLostCount += 1;
      if (contextLostCount >= 2) onFallbackOnce();
    };
    const handleContextRestored = () => {
      contextLostCount = 0;
      disposeResources();
      if (!createResources()) onFallbackOnce();
    };

    canvas.addEventListener("webglcontextlost", handleContextLost, false);
    canvas.addEventListener("webglcontextrestored", handleContextRestored, false);

    const socket = new FrameSocket(url, token, onFrame);
    socket.start();

    return () => {
      disposed = true;
      socket.close();
      canvas.removeEventListener("webglcontextlost", handleContextLost, false);
      canvas.removeEventListener("webglcontextrestored", handleContextRestored, false);
      if (rafId !== null) window.cancelAnimationFrame(rafId);
      latestFrame = null;
      disposeResources();
    };
  }, [url, token, rawBottomUp]);

  return (
    <canvas
      ref={canvasRef}
      className={className}
      style={style}
      width={initialWidth && initialWidth > 0 ? initialWidth : 2}
      height={initialHeight && initialHeight > 0 ? initialHeight : 2}
      aria-label={ariaLabel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      onContextMenu={(event) => event.preventDefault()}
    />
  );
}