#!/usr/bin/env node
import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

const options = Object.fromEntries(
  process.argv.slice(2).map((arg) => {
    const [key, ...value] = arg.replace(/^--/, "").split("=");
    return [key, value.join("=")];
  }),
);

const resolution = options.resolution || "720x1280";
const requestedCodec = (options.codec || "vp8").toLowerCase();
const refreshHz = Number(options.hz || 60);
const requestedPayload = Number(options.pt || 0);
const requestedBitrateKbps = Number(options.bitrate || 0);
const turnUrl = options.turn || process.env.TURN_URL || "";
const turnUser = options.turnUser || process.env.TURN_USERNAME || "";
const turnPass = options.turnPass || process.env.TURN_CREDENTIAL || "";
const iceDelayMs = Number(options.iceDelay || process.env.ICE_DELAY_MS || 0);
const lowLatency = options.lowLatency === "1" || process.env.LOW_LATENCY === "1";
const resultTag = options.tag ? `-${options.tag.replace(/[^a-z0-9_-]/gi, "-")}` : "";
const durationSeconds = Number(options.duration || 20);
const warmupSeconds = Number(options.warmup || 5);
const webUrl = options.url || "http://127.0.0.1:5173";
const gateway = options.gateway || "http://127.0.0.1:8080";
const outputDir = path.resolve(options.outputDir || "results");
const outputFile = path.join(outputDir, `fps-${resolution}-${requestedCodec}-${refreshHz}hz${resultTag}.json`);
const screenshotFile = path.join(outputDir, `fps-${resolution}-${requestedCodec}-${refreshHz}hz${resultTag}.png`);

function percentile(values, fraction) {
  if (values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * fraction) - 1));
  return sorted[index];
}

function summarize(values) {
  const clean = values.filter((value) => value != null && Number.isFinite(value));
  if (clean.length === 0) return { count: 0, mean: null, median: null, min: null, max: null, p10: null };
  return {
    count: clean.length,
    mean: clean.reduce((sum, value) => sum + value, 0) / clean.length,
    median: percentile(clean, 0.5),
    min: Math.min(...clean),
    max: Math.max(...clean),
    p10: percentile(clean, 0.1),
  };
}

await mkdir(outputDir, { recursive: true });

const browser = await chromium.launch({
  headless: true,
  args: [
    "--autoplay-policy=no-user-gesture-required",
    "--disable-background-timer-throttling",
    "--disable-backgrounding-occluded-windows",
    "--disable-renderer-backgrounding",
    "--disable-features=CalculateNativeWinOcclusion,WebRtcHideLocalIpsWithMdns",
    "--force-device-scale-factor=1",
  ],
});

const context = await browser.newContext({
  viewport: { width: 1400, height: 1000 },
  deviceScaleFactor: 1,
});

const page = await context.newPage();
const consoleErrors = [];
page.on("console", (message) => {
  if (message.type() === "error") consoleErrors.push(message.text());
});
page.on("pageerror", (error) => consoleErrors.push(error.message));

const url = new URL(webUrl);
url.searchParams.set("url", gateway);
url.searchParams.set("autoconnect", "1");
url.searchParams.set("codec", requestedCodec);
url.searchParams.set("hz", String(refreshHz));
if (requestedPayload > 0) url.searchParams.set("pt", String(requestedPayload));
if (requestedBitrateKbps > 0) url.searchParams.set("bitrate", String(requestedBitrateKbps));
if (iceDelayMs > 0) url.searchParams.set("iceDelay", String(iceDelayMs));
if (lowLatency) url.searchParams.set("lowLatency", "1");
if (turnUrl) {
  url.searchParams.set("turn", turnUrl);
  url.searchParams.set("turnUser", turnUser);
  url.searchParams.set("turnPass", turnPass);
}

const startedAt = Date.now();
await page.goto(url.toString(), { waitUntil: "networkidle", timeout: 30_000 });
try {
  await page.waitForFunction(
    () => {
      const bench = window.__webrtcBench;
      if (!bench) return false;
      const state = bench.snapshot();
      return state.connection === "connected" && state.videoWidth > 0 && state.videoHeight > 0;
    },
    { timeout: 45_000 },
  );
} catch (error) {
  const diagnostic = await page.evaluate(async () => {
    const pcs = window.__benchPeerConnections || [];
    return Promise.all(pcs.map(async (pc) => {
      const stats = await pc.getStats();
      const candidates = [];
      let selected = null;
      for (const report of stats.values()) {
        if (report.type === "local-candidate" || report.type === "remote-candidate") {
          candidates.push({ type: report.type, candidateType: report.candidateType, address: report.address, port: report.port, protocol: report.protocol });
        }
        if (report.type === "candidate-pair" && report.selected) selected = report;
      }
      return {
        connectionState: pc.connectionState,
        iceConnectionState: pc.iceConnectionState,
        iceGatheringState: pc.iceGatheringState,
        signalingState: pc.signalingState,
        localType: pc.localDescription?.type,
        remoteType: pc.remoteDescription?.type,
        candidates,
        selected,
        bench: window.__webrtcBench?.snapshot(),
        hook: window.__benchHookDebug,
      };
    }));
  });
  console.error("BENCHMARK CONNECTION DIAGNOSTIC");
  console.error(JSON.stringify(diagnostic, null, 2));
  console.error("CONSOLE ERRORS", consoleErrors);
  throw error;
}
try {
  await page.waitForFunction(
    () => (window.__webrtcBench?.snapshot().framesDecoded ?? 0) > 10,
    { timeout: 30_000 },
  );
} catch (error) {
  const diagnostic = await page.evaluate(async () => {
    const pcs = window.__benchPeerConnections || [];
    return Promise.all(pcs.map(async (pc) => {
      const stats = await pc.getStats();
      const candidates = [];
      let selected = null;
      for (const report of stats.values()) {
        if (report.type === "local-candidate" || report.type === "remote-candidate") {
          candidates.push({ type: report.type, candidateType: report.candidateType, address: report.address, port: report.port, protocol: report.protocol });
        }
        if (report.type === "candidate-pair" && report.selected) selected = report;
      }
      return {
        connectionState: pc.connectionState,
        iceConnectionState: pc.iceConnectionState,
        iceGatheringState: pc.iceGatheringState,
        signalingState: pc.signalingState,
        candidates,
        selected,
        bench: window.__webrtcBench?.snapshot(),
      };
    }));
  });
  console.error("BENCHMARK FRAME DIAGNOSTIC");
  console.error(JSON.stringify(diagnostic, null, 2));
  console.error("CONSOLE ERRORS", consoleErrors);
  throw error;
}

const connectMs = Date.now() - startedAt;
await page.evaluate(() => window.__webrtcBench?.reset());
await page.waitForTimeout(durationSeconds * 1000);
const snapshot = await page.evaluate(() => window.__webrtcBench?.snapshot());
await page.screenshot({ path: screenshotFile, fullPage: true });

const warmupSamples = warmupSeconds > 0 ? Math.ceil(warmupSeconds) : 0;
const settled = snapshot.samples.slice(warmupSamples);
const first = settled[0] || snapshot.samples[0];
const last = settled[settled.length - 1] || snapshot.samples[snapshot.samples.length - 1];
const observedSeconds = first && last ? (last.t - first.t) / 1000 : 0;
const decodedDelta = first && last && first.framesDecoded != null && last.framesDecoded != null
  ? last.framesDecoded - first.framesDecoded
  : null;

const result = {
  resolution,
  requestedCodec,
  requestedPayload: requestedPayload || null,
  requestedBitrateKbps: requestedBitrateKbps || null,
  turnUrl: turnUrl || null,
  iceDelayMs: iceDelayMs || null,
  lowLatency,
  refreshHz,
  webUrl,
  gateway,
  capturedAt: new Date().toISOString(),
  connectMs,
  durationSeconds,
  warmupSeconds,
  nativeVideo: {
    width: snapshot.videoWidth,
    height: snapshot.videoHeight,
  },
  codec: snapshot.codec,
  renderFps: summarize(settled.map((sample) => sample.renderFps)),
  inboundFps: summarize(settled.map((sample) => sample.inboundFps)),
  decodedFps: summarize(settled.map((sample) => sample.decodedFps)),
  bitrateKbps: summarize(settled.map((sample) => sample.bitrateKbps)),
  jitterMs: summarize(settled.map((sample) => sample.jitterMs)),
  observedSeconds,
  decodedFramesDelta: decodedDelta,
  framesDroppedDelta: settled.reduce((sum, sample) => sum + (sample.droppedDelta || 0), 0),
  sampleCount: snapshot.samples.length,
  errors: [...snapshot.errors, ...consoleErrors],
  hookDebug: await page.evaluate(() => window.__benchHookDebug || null),
  codecDebug: await page.evaluate(() => window.__benchCodecDebug || null),
  peerConnections: await page.evaluate(() => window.__benchPeerConnections?.length ?? -1),
  screenshot: screenshotFile,
  samples: snapshot.samples,
};

await writeFile(outputFile, `${JSON.stringify(result, null, 2)}\n`);

const fmt = (value, digits = 1) => value == null ? "—" : value.toFixed(digits);
console.log(JSON.stringify({
  resolution,
  requestedCodec,
  requestedPayload: requestedPayload || null,
  requestedBitrateKbps: requestedBitrateKbps || null,
  turnUrl: turnUrl || null,
  iceDelayMs: iceDelayMs || null,
  lowLatency,
  refreshHz,
  native: `${result.nativeVideo.width}x${result.nativeVideo.height}`,
  codec: result.codec,
  renderFps: {
    median: Number(fmt(result.renderFps.median)),
    p10: Number(fmt(result.renderFps.p10)),
    max: Number(fmt(result.renderFps.max)),
  },
  inboundFps: {
    median: Number(fmt(result.inboundFps.median)),
    p10: Number(fmt(result.inboundFps.p10)),
    max: Number(fmt(result.inboundFps.max)),
  },
  bitrateKbps: Math.round(result.bitrateKbps.median ?? 0),
  droppedFrames: result.framesDroppedDelta,
  connectMs,
  outputFile,
}, null, 2));

await browser.close();
