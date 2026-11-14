export interface BenchSample {
  t: number;
  renderFps: number | null;
  inboundFps: number | null;
  decodedFps: number | null;
  bitrateKbps: number | null;
  framesDecoded: number | null;
  framesDropped: number | null;
  droppedDelta: number;
  width: number | null;
  height: number | null;
  codec: string | null;
  jitterMs: number | null;
  packetsLost: number | null;
}

export interface BenchSnapshot {
  connection: "idle" | "connecting" | "connected" | "disconnected";
  connectedAt: number | null;
  firstFrameAt: number | null;
  videoWidth: number;
  videoHeight: number;
  renderFps: number | null;
  inboundFps: number | null;
  bitrateKbps: number | null;
  framesDecoded: number | null;
  framesDropped: number | null;
  codec: string | null;
  errors: string[];
  samples: BenchSample[];
}

type Listener = (snapshot: BenchSnapshot) => void;

const listeners = new Set<Listener>();
const peerConnections: RTCPeerConnection[] = [];
let video: HTMLVideoElement | null = null;
let frameCallbacksStarted = false;
let sampleTimer: number | null = null;
let frameCounter = 0;
let previousFrameCounter = 0;
let previousSampleAt = 0;
let previousStats: any = null;
const requestedCodec = (new URLSearchParams(window.location.search).get("codec") || "").toLowerCase();
const requestedPayload = Number(new URLSearchParams(window.location.search).get("pt") || 0);
const requestedBitrateKbps = Number(new URLSearchParams(window.location.search).get("bitrate") || 0);
const benchParams = new URLSearchParams(window.location.search);
const requestedTurnUrl = benchParams.get("turn") || "";
const requestedTurnUser = benchParams.get("turnUser") || "";
const requestedTurnPass = benchParams.get("turnPass") || "";
const requestedIceDelayMs = Number(benchParams.get("iceDelay") || 0);
const lowLatencyMode = benchParams.get("lowLatency") === "1";
const benchIceServers: RTCIceServer[] = requestedTurnUrl
  ? [{ urls: requestedTurnUrl, username: requestedTurnUser, credential: requestedTurnPass }]
  : [];

function withBenchIceServers(configuration?: RTCConfiguration): RTCConfiguration | undefined {
  if (benchIceServers.length === 0) return configuration;
  return { ...(configuration || {}), iceServers: benchIceServers, iceTransportPolicy: "relay" };
}


function preferVideoCodec(sdp: string, codec: string): string {
  const debug = ((window as any).__benchCodecDebug ||= {
    requestedCodec: codec,
    calls: 0,
  });
  debug.calls += 1;
  debug.sdpLength = sdp.length;
  if (!sdp || !codec) return sdp;
  const target = codec.toUpperCase();
  const newline = sdp.includes("\r\n") ? "\r\n" : "\n";
  const lines = sdp.split(/\r?\n/);
  const payloads = new Map<number, string>();
  const rtx = new Map<number, number>();
  for (const line of lines) {
    let match = line.match(/^a=rtpmap:(\d+) (VP8|VP9|H264)\/90000$/i);
    if (match) payloads.set(Number(match[1]), match[2].toUpperCase());
    match = line.match(/^a=fmtp:(\d+) apt=(\d+)/i);
    if (match) rtx.set(Number(match[1]), Number(match[2]));
  }
  const allWanted = [...payloads.entries()].filter(([, name]) => name === target).map(([pt]) => pt);
  const wanted = requestedPayload > 0 && allWanted.includes(requestedPayload)
    ? [requestedPayload]
    : allWanted;
  debug.payloads = Object.fromEntries(payloads);
  debug.requestedPayload = requestedPayload;
  debug.wanted = wanted;
  if (wanted.length === 0) return sdp;

  const mediaIndex = lines.findIndex((line) => line.startsWith("m=video "));
  debug.mediaIndex = mediaIndex;
  if (mediaIndex < 0) return sdp;
  const beforeLine = lines[mediaIndex];
  const parts = lines[mediaIndex].split(" ");
  const offered = parts.slice(3).map(Number);
  const preferredRtx = [...rtx.entries()]
    .filter(([, apt]) => wanted.includes(apt))
    .map(([pt]) => pt);
  const priority = [...wanted, ...preferredRtx];
  const reordered = [...priority, ...offered.filter((pt) => !priority.includes(pt))];
  lines[mediaIndex] = `${parts.slice(0, 3).join(" ")} ${[...new Set(reordered)].join(" ")}`;

  if (requestedBitrateKbps > 0) {
    for (let i = lines.length - 1; i > mediaIndex; i -= 1) {
      if (!lines[i].startsWith("b=") && !lines[i].startsWith("a=fmtp:")) break;
      if (lines[i].startsWith("b=AS:") || lines[i].startsWith("b=TIAS:")) lines.splice(i, 1);
    }
    const bitrateDirectives = [
      `b=AS:${Math.round(requestedBitrateKbps)}`,
      `b=TIAS:${Math.round(requestedBitrateKbps * 1000)}`,
    ];
    lines.splice(mediaIndex + 1, 0, ...bitrateDirectives);
    const start = Math.round(requestedBitrateKbps * 0.7);
    for (let i = mediaIndex + 1; i < lines.length; i += 1) {
      const match = lines[i].match(/^a=fmtp:(\d+) /);
      if (match && payloads.get(Number(match[1])) === "H264") {
        lines[i] += `;x-google-max-bitrate=${Math.round(requestedBitrateKbps)};x-google-min-bitrate=${start};x-google-start-bitrate=${start}`;
      }
    }
  }

  debug.before = beforeLine;
  debug.requestedBitrateKbps = requestedBitrateKbps;
  debug.after = lines[mediaIndex];
  return lines.join(newline);
}

const snapshot: BenchSnapshot = {
  connection: "idle",
  connectedAt: null,
  firstFrameAt: null,
  videoWidth: 0,
  videoHeight: 0,
  renderFps: null,
  inboundFps: null,
  bitrateKbps: null,
  framesDecoded: null,
  framesDropped: null,
  codec: null,
  errors: [],
  samples: [],
};

function emit() {
  const value: BenchSnapshot = {
    ...snapshot,
    samples: snapshot.samples.slice(),
    errors: snapshot.errors.slice(),
  };
  listeners.forEach((listener) => listener(value));
}

function recordError(error: unknown) {
  const text = error instanceof Error ? error.message : String(error);
  snapshot.errors.push(text);
  if (snapshot.errors.length > 20) snapshot.errors.shift();
  emit();
}

function findVideoStats(report: RTCStatsReport): any | null {
  for (const value of report.values()) {
    const item = value as any;
    if (item.type === "inbound-rtp" && (item.kind === "video" || item.mediaType === "video")) {
      return item;
    }
  }
  return null;
}

async function collectStats() {
  for (let i = peerConnections.length - 1; i >= 0; i -= 1) {
    const pc = peerConnections[i];
    if (lowLatencyMode && pc.connectionState === "connected") {
      for (const receiver of pc.getReceivers()) {
        try { (receiver as any).jitterBufferTarget = 0; } catch {}
        try { (receiver as any).playoutDelayHint = 0; } catch {}
        if (receiver.track?.kind === "video") receiver.track.contentHint = "motion";
      }
    }
    if (pc.connectionState === "closed") {
      peerConnections.splice(i, 1);
      continue;
    }

    try {
      const report = await pc.getStats();
      const inbound = findVideoStats(report);
      if (!inbound) continue;

      const now = performance.now();
      const elapsed = previousSampleAt ? now - previousSampleAt : 0;
      const decodedDelta =
        previousStats?.framesDecoded != null && inbound.framesDecoded != null
          ? inbound.framesDecoded - previousStats.framesDecoded
          : null;
      const droppedDelta =
        previousStats?.framesDropped != null && inbound.framesDropped != null
          ? inbound.framesDropped - previousStats.framesDropped
          : null;
      const byteDelta =
        previousStats?.bytesReceived != null && inbound.bytesReceived != null
          ? inbound.bytesReceived - previousStats.bytesReceived
          : null;

      snapshot.framesDecoded = inbound.framesDecoded ?? null;
      snapshot.framesDropped = inbound.framesDropped ?? null;
      snapshot.inboundFps =
        inbound.framesPerSecond ?? (decodedDelta != null && elapsed > 0 ? decodedDelta * 1000 / elapsed : null);
      snapshot.bitrateKbps =
        byteDelta != null && elapsed > 0 ? (byteDelta * 8) / elapsed : null;
      if (inbound.frameWidth) snapshot.videoWidth = inbound.frameWidth;
      if (inbound.frameHeight) snapshot.videoHeight = inbound.frameHeight;

      const codec = inbound.codecId ? report.get(inbound.codecId) : null;
      if (codec && "mimeType" in codec) snapshot.codec = String(codec.mimeType);

      const frameDelta = frameCounter - previousFrameCounter;
      const wallDelta = previousSampleAt ? now - previousSampleAt : 0;
      snapshot.renderFps = wallDelta > 0 ? (frameDelta * 1000) / wallDelta : null;

      const sample: BenchSample = {
        t: now,
        renderFps: snapshot.renderFps,
        inboundFps: snapshot.inboundFps,
        decodedFps: decodedDelta != null && elapsed > 0 ? decodedDelta * 1000 / elapsed : null,
        bitrateKbps: snapshot.bitrateKbps,
        framesDecoded: inbound.framesDecoded ?? null,
        framesDropped: inbound.framesDropped ?? null,
        droppedDelta: droppedDelta ?? 0,
        width: inbound.frameWidth ?? null,
        height: inbound.frameHeight ?? null,
        codec: snapshot.codec,
        jitterMs: inbound.jitter != null ? inbound.jitter * 1000 : null,
        packetsLost: inbound.packetsLost ?? null,
      };
      snapshot.samples.push(sample);
      if (snapshot.samples.length > 600) snapshot.samples.shift();

      previousFrameCounter = frameCounter;
      previousSampleAt = now;
      previousStats = inbound;
      emit();
    } catch (error) {
      recordError(error);
    }
  }
}

function startFrameCounter() {
  if (frameCallbacksStarted || !video) return;
  frameCallbacksStarted = true;

  const onFrame = (now: number) => {
    if (!video) return;
    frameCounter += 1;
    if (snapshot.firstFrameAt == null) snapshot.firstFrameAt = now;
    if (video.videoWidth > 0) snapshot.videoWidth = video.videoWidth;
    if (video.videoHeight > 0) snapshot.videoHeight = video.videoHeight;
    video.requestVideoFrameCallback(onFrame);
  };

  video.requestVideoFrameCallback(onFrame);
}

function attachVideo(element: HTMLVideoElement) {
  if (video === element) return;
  video = element;
  frameCallbacksStarted = false;
  snapshot.videoWidth = element.videoWidth || snapshot.videoWidth;
  snapshot.videoHeight = element.videoHeight || snapshot.videoHeight;
  startFrameCounter();

  if (sampleTimer == null) {
    previousSampleAt = performance.now();
    previousFrameCounter = frameCounter;
    sampleTimer = window.setInterval(collectStats, 1000);
  }
  emit();
}

export function setConnectionState(state: BenchSnapshot["connection"]) {
  snapshot.connection = state;
  if (state === "connected" && snapshot.connectedAt == null) {
    snapshot.connectedAt = performance.now();
  }
  if (state === "disconnected") {
    snapshot.renderFps = null;
    snapshot.inboundFps = null;
  }
  emit();
}

export function recordBenchError(error: unknown) {
  recordError(error);
}

export function scanForVideo() {
  const candidate = document.querySelector("video") as HTMLVideoElement | null;
  if (candidate) attachVideo(candidate);
}

export function subscribeBench(listener: Listener) {
  listeners.add(listener);
  listener({
    ...snapshot,
    samples: snapshot.samples.slice(),
    errors: snapshot.errors.slice(),
  });
  return () => {
    listeners.delete(listener);
  };
}

export function getBenchSnapshot(): BenchSnapshot {
  return {
    ...snapshot,
    samples: snapshot.samples.slice(),
    errors: snapshot.errors.slice(),
  };
}

export function resetBenchSamples() {
  snapshot.samples = [];
  previousStats = null;
  previousSampleAt = performance.now();
  previousFrameCounter = frameCounter;
  snapshot.renderFps = null;
  snapshot.inboundFps = null;
  snapshot.bitrateKbps = null;
  emit();
}

export function installBenchHooks() {
  const benchWindow = window as typeof window & {
    __webrtcBench?: {
      snapshot: typeof getBenchSnapshot;
      reset: typeof resetBenchSamples;
      scan: typeof scanForVideo;
    };
    __benchPeerConnections: RTCPeerConnection[];
  };

  if (!benchWindow.__benchPeerConnections) {
    benchWindow.__benchPeerConnections = peerConnections;
  }
  benchWindow.__webrtcBench = {
    snapshot: getBenchSnapshot,
    reset: resetBenchSamples,
    scan: scanForVideo,
  };

  const nativePeerConnection = window.RTCPeerConnection;
  if (nativePeerConnection && !(window as any).__benchPcPatched) {
    if (requestedCodec) {
      const prototype = nativePeerConnection.prototype as any;
      const nativeCreateOffer = prototype.createOffer;
      prototype.createOffer = async function (...args: any[]) {
        const offer = await nativeCreateOffer.apply(this, args);
        return {
          type: offer.type,
          sdp: preferVideoCodec(offer.sdp || "", requestedCodec),
        };
      };
      const nativeCreateAnswer = prototype.createAnswer;
      prototype.createAnswer = async function (...args: any[]) {
        const answer = await nativeCreateAnswer.apply(this, args);
        return {
          type: answer.type,
          sdp: preferVideoCodec(answer.sdp || "", requestedCodec),
        };
      };
    }

    class CapturedPeerConnection extends nativePeerConnection {
      constructor(configuration?: RTCConfiguration) {
        super(withBenchIceServers(configuration));
        peerConnections.push(this);
        if (lowLatencyMode) {
          this.addEventListener("track", (event) => {
            const track = event.track;
            if (track.kind === "video") track.contentHint = "motion";
            for (const receiver of this.getReceivers()) {
              try { (receiver as any).jitterBufferTarget = 0; } catch {}
              try { (receiver as any).playoutDelayHint = 0; } catch {}
            }
          });
        }
      }
    }

    if (requestedIceDelayMs > 0) {
      const nativeIceDescriptor = Object.getOwnPropertyDescriptor(
        nativePeerConnection.prototype,
        "onicecandidate",
      );
      if (nativeIceDescriptor?.set && nativeIceDescriptor?.get) {
        Object.defineProperty(CapturedPeerConnection.prototype, "onicecandidate", {
          configurable: true,
          enumerable: true,
          get() {
            return nativeIceDescriptor.get!.call(this);
          },
          set(handler: ((event: RTCPeerConnectionIceEvent) => void) | null) {
            if (typeof handler !== "function") {
              nativeIceDescriptor.set!.call(this, handler);
              return;
            }
            nativeIceDescriptor.set!.call(this, (event: RTCPeerConnectionIceEvent) => {
              window.setTimeout(() => handler.call(this, event), requestedIceDelayMs);
            });
          },
        });
      }
    }
    window.RTCPeerConnection = CapturedPeerConnection as typeof RTCPeerConnection;
    (window as any).__benchPcPatched = true;
  }

  (window as any).__benchHookDebug = {
    requestedCodec,
    requestedPayload,
    requestedBitrateKbps,
    requestedTurnUrl,
    requestedIceDelayMs,
    lowLatencyMode,
    nativeName: nativePeerConnection?.name,
    assignedName: window.RTCPeerConnection?.name,
    patched: (window as any).__benchPcPatched,
    createOfferPatched: Boolean(requestedCodec),
    createAnswerPatched: Boolean(requestedCodec),
  };
}
