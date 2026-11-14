#!/usr/bin/env node
import { readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const resultsDir = path.resolve(process.argv[2] || "results");
const filePattern = /^fps-(\d+x\d+)-([a-z0-9]+)-(\d+)hz(?:-([a-z0-9_-]+))?\.json$/;
const files = (await readdir(resultsDir)).filter((file) => filePattern.test(file));

function resolutionSize(resolution) {
  return resolution.split("x").reduce((product, value) => product * Number(value), 1);
}

async function buildRows(selected) {
  const rows = [];
  for (const file of selected) {
    const match = file.match(filePattern);
    const data = JSON.parse(await readFile(path.join(resultsDir, file), "utf8"));
    rows.push({
      resolution: data.resolution,
      native: `${data.nativeVideo.width}x${data.nativeVideo.height}`,
      codec: data.codec,
      requestedCodec: data.requestedCodec,
      refreshHz: data.refreshHz,
      tag: match?.[4] || "",
      renderFpsMedian: data.renderFps.median,
      renderFpsP10: data.renderFps.p10,
      inboundFpsMedian: data.inboundFps.median,
      inboundFpsP10: data.inboundFps.p10,
      bitrateKbps: data.bitrateKbps.median,
      droppedFrames: data.framesDroppedDelta,
      connectMs: data.connectMs,
      errors: data.errors.length,
      screenshot: path.basename(data.screenshot),
    });
  }
  return rows.sort((a, b) =>
    resolutionSize(a.resolution) - resolutionSize(b.resolution)
    || a.refreshHz - b.refreshHz
    || String(a.codec).localeCompare(String(b.codec))
  );
}

function printTable(rows, title) {
  if (rows.length === 0) return;
  console.log(`\n${title}`);
  const header = ["resolution", "hz", "native", "codec", "render fps", "p10", "decode fps", "bitrate", "dropped", "connect"];
  const lines = rows.map((row) => [
    row.resolution,
    String(row.refreshHz),
    row.native,
    row.codec || "—",
    row.renderFpsMedian?.toFixed(1) ?? "—",
    row.renderFpsP10?.toFixed(1) ?? "—",
    row.inboundFpsMedian?.toFixed(1) ?? "—",
    `${Math.round(row.bitrateKbps ?? 0)} kbps`,
    String(row.droppedFrames),
    `${row.connectMs} ms`,
  ]);
  const widths = header.map((value, index) => Math.max(value.length, ...lines.map((line) => line[index].length)));
  console.log(header.map((value, index) => value.padEnd(widths[index])).join("  "));
  console.log(widths.map((width) => "-".repeat(width)).join("  "));
  for (const line of lines) {
    console.log(line.map((value, index) => value.padEnd(widths[index])).join("  "));
  }
}

const baselineFiles = files.filter((file) => !filePattern.exec(file)?.[4]);
const hostGpuFiles = files.filter((file) => filePattern.exec(file)?.[4] === "api37-hostgpu");
const baselineRows = await buildRows(baselineFiles);
const hostGpuRows = await buildRows(hostGpuFiles);

await writeFile(path.join(resultsDir, "summary.json"), `${JSON.stringify(baselineRows, null, 2)}\n`);
await writeFile(path.join(resultsDir, "hostgpu-summary.json"), `${JSON.stringify(hostGpuRows, null, 2)}\n`);

printTable(baselineRows, "API 36 / software GPU baseline");
printTable(hostGpuRows, "API 37 / Apple M4 host GPU");
