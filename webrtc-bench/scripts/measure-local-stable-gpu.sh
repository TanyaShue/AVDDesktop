#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

STABLE_EMULATOR="$SDK/emulator/emulator"
STABLE_VERSION="$($STABLE_EMULATOR -version 2>&1 | sed -n 's/^Android emulator version \([^ ]*\).*/\1/p' | head -1)"
RESOLUTIONS="${RESOLUTIONS:-720x1280 1080x1920 1440x2560 2160x3840}"
REFRESH_RATES="${REFRESH_RATES:-60 120}"
OUT="$RESULT_DIR/local-stable-gpu.json"
TMP="$RESULT_DIR/.local-stable-gpu-values"
mkdir -p "$RESULT_DIR"
: > "$TMP"

[[ -x "$STABLE_EMULATOR" ]] || { echo "Stable emulator not found: $STABLE_EMULATOR" >&2; exit 1; }

for resolution in $RESOLUTIONS; do
  width="${resolution%x*}"
  height="${resolution#*x}"
  for hz in $REFRESH_RATES; do
    echo "=== Stable API 36 host GPU: ${resolution} @ ${hz}Hz ==="
    EMULATOR_BIN="$STABLE_EMULATOR" TARGET_API=36.1 VSYNC="$hz" SHOW_WINDOW=0 \
      "$BENCH_DIR/scripts/start-emulator.sh" "$width" "$height"

    "$ADB" -s "$SERIAL" logcat -c
    "$ADB" -s "$SERIAL" shell am force-stop com.android.webrtcbench.motion
    "$ADB" -s "$SERIAL" shell am start -W -n com.android.webrtcbench.motion/.MainActivity >/dev/null
    sleep 15
    "$ADB" -s "$SERIAL" logcat -d 2>/dev/null | grep 'WebRtcMotion' | grep 'measured_fps=' > "$TMP.one" || true

    python3 - "$TMP.one" "$resolution" "$hz" "$TMP" <<'PY'
import re
import statistics
import sys
from pathlib import Path

source, resolution, hz, output = sys.argv[1:]
values = []
for line in Path(source).read_text().splitlines():
    match = re.search(r'measured_fps=([0-9.]+)', line)
    if match:
        values.append(float(match.group(1)))
if not values:
    raise SystemExit("no measured_fps output")
stable = values[-8:]
stable.sort()
median = statistics.median(stable)
p10 = stable[max(0, int(len(stable) * 0.1) - 1)]
with Path(output).open("a") as f:
    f.write(f"{resolution}\t{hz}\t{median:.3f}\t{min(stable):.3f}\t{max(stable):.3f}\t{p10:.3f}\n")
print(f"median={median:.3f} min={min(stable):.3f} max={max(stable):.3f} samples={len(stable)}")
PY

    "$BENCH_DIR/scripts/stop-emulator.sh" >/dev/null 2>&1 || true
  done
done

python3 - "$TMP" "$OUT" "$STABLE_VERSION" <<'PY'
import json
import sys
from pathlib import Path

source, output, stable_version = sys.argv[1:]
rows = []
for line in Path(source).read_text().splitlines():
    resolution, hz, median, minimum, maximum, p10 = line.split("\t")
    rows.append({
        "resolution": resolution,
        "refreshHz": int(hz),
        "renderFpsMedian": float(median),
        "renderFpsMin": float(minimum),
        "renderFpsMax": float(maximum),
        "renderFpsP10": float(p10),
        "emulator": f"local stable {stable_version}",
        "api": "36.1",
        "gpu": "host",
    })
Path(output).write_text(json.dumps(rows, indent=2) + "\n")
print(json.dumps(rows, indent=2))
PY
