#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

WIDTH="${1:-720}"
HEIGHT="${2:-1280}"
MEMORY_MB="${MEMORY_MB:-4096}"
CORES="${CORES:-4}"
VSYNC="${VSYNC:-60}"
TARGET_API="${TARGET_API:-36.1}"
if [[ "$TARGET_API" == 37* && -z "${SHOW_WINDOW+x}" ]]; then
  export SHOW_WINDOW=1
fi
LOG="$LOG_DIR/webrtc-bench-emulator-${WIDTH}x${HEIGHT}-${VSYNC}hz.log"

require_runtime
if [[ ! -x "$EMULATOR" ]]; then
  echo "RTC-capable emulator preview is missing. Run: $BENCH_DIR/scripts/install-preview-emulator.sh" >&2
  exit 1
fi
"$BENCH_DIR/scripts/stop-emulator.sh" >/dev/null 2>&1 || true

# Derive an ephemeral benchmark AVD from the existing MyDevice configuration.
# The source AVD is never modified: data is stored in an overlay under TMPDIR
# and the derived AVD only exists inside this experiment directory.
SOURCE_CONFIG="$AVD_HOME/$AVD_NAME.avd/config.ini"
RUN_AVD_HOME="$BENCH_DIR/.avd"
RUN_AVD_NAME="${AVD_NAME}WebRTC_${WIDTH}x${HEIGHT}_${VSYNC}hz_api${TARGET_API}"
RUN_AVD_DIR="$RUN_AVD_HOME/$RUN_AVD_NAME.avd"
EMU_DATADIR="$BENCH_DIR/.tmp/$AVD_NAME-api$TARGET_API"
mkdir -p "$RUN_AVD_HOME"
if [[ "${RESET_DATA:-0}" == "1" ]]; then
  python3 - "$EMU_DATADIR" <<'PYTMP'
import pathlib, shutil, sys
path = pathlib.Path(sys.argv[1])
shutil.rmtree(path, ignore_errors=True)
path.mkdir(parents=True, exist_ok=True)
PYTMP
else
  mkdir -p "$EMU_DATADIR"
fi
python3 - "$SOURCE_CONFIG" "$RUN_AVD_DIR" "$RUN_AVD_HOME/$RUN_AVD_NAME.ini" "$RUN_AVD_NAME" "$WIDTH" "$HEIGHT" "$VSYNC" "$TARGET_API" "$SDK" "$EMU_DATADIR" <<'PYCFG'
import pathlib
import shutil
import sys

source, avd_dir, ini_file, name, width, height, vsync, target_api, sdk, datadir = sys.argv[1:]
avd_dir = pathlib.Path(avd_dir)
ini_file = pathlib.Path(ini_file)
shutil.rmtree(avd_dir, ignore_errors=True)
avd_dir.mkdir(parents=True, exist_ok=True)

values = {}
for line in pathlib.Path(source).read_text().splitlines():
    if "=" in line and not line.lstrip().startswith("#"):
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip()

def put(key, value):
    values[key] = str(value)

put("avd.id", name)
put("avd.name", name)
put("hw.lcd.width", width)
put("hw.lcd.height", height)
put("hw.lcd.vsync", vsync)
put("skin.name", f"{width}x{height}")
put("disk.dataPartition.path", str(pathlib.Path(datadir) / "userdata-qemu.img"))
put("fastboot.forceColdBoot", "yes")
put("fastboot.forceFastBoot", "no")
put("fastboot.forceChosenSnapshotBoot", "no")
put("firstboot.bootFromDownloadableSnapshot", "no")
put("firstboot.bootFromLocalSnapshot", "no")
put("firstboot.saveToLocalSnapshot", "no")
image_dir = str(pathlib.Path(sdk) / f"system-images/android-{target_api}/google_apis/arm64-v8a")
put("image.sysdir.1", image_dir + "/")

(avd_dir / "config.ini").write_text("".join(f"{key}={value}\n" for key, value in sorted(values.items())))
ini_file.write_text(
    "avd.ini.encoding=UTF-8\n"
    f"path={avd_dir}\n"
    f"target=android-{target_api}\n"
)
PYCFG

export ANDROID_HOME="$SDK"
export ANDROID_SDK_ROOT="$SDK"
export ANDROID_AVD_HOME="$RUN_AVD_HOME"
export ANDROID_EMULATOR_HOME="$ROOT"
export ANDROID_TMP="$BENCH_DIR/.tmp/android-tmp"
mkdir -p "$ANDROID_TMP"
export AEMU_NO_TRAMPOLINE="${AEMU_NO_TRAMPOLINE:-1}"

START_MARKER="$RESULT_DIR/.emulator-start-marker"
: > "$START_MARKER"
: > "$LOG"
EXTRA_FLAGS=()
[[ "${SHOW_WINDOW:-0}" != "1" ]] && EXTRA_FLAGS+=(-no-window)
[[ "${CAMERA_DISABLED:-1}" == "1" ]] && EXTRA_FLAGS+=(-camera-back none -camera-front none)
[[ "${NO_NETSIM:-0}" == "1" ]] && EXTRA_FLAGS+=(-no-netsim)
[[ -n "${SYSTEMUI_RENDERER:-}" ]] && EXTRA_FLAGS+=(-systemui-renderer "$SYSTEMUI_RENDERER")
[[ -n "${RENDERER_FEATURES:-}" ]] && EXTRA_FLAGS+=(-renderer-features "$RENDERER_FEATURES")
[[ -n "${VIRTIO_GPU_DEVICE:-}" ]] && EXTRA_FLAGS+=(-virtio-gpu-device "$VIRTIO_GPU_DEVICE")
[[ -n "${TURN_URL:-}" ]] && EXTRA_FLAGS+=(-turncfg "$BENCH_DIR/scripts/turn-config.sh")
[[ "${READ_ONLY:-0}" == "1" ]] && EXTRA_FLAGS+=(-read-only)
[[ "${WIPE_DATA:-0}" == "1" ]] && EXTRA_FLAGS+=(-wipe-data)
"$EMULATOR" \
  -avd "$RUN_AVD_NAME" \
  -datadir "$EMU_DATADIR" \
  -port "$EMU_PORT" \
  -skin "${WIDTH}x${HEIGHT}" \
  -vsync-rate "$VSYNC" \
  -memory "$MEMORY_MB" \
  -cores "$CORES" \
  -no-audio \
  -no-boot-anim \
  -no-snapshot-load \
  -no-snapshot-save \
  -no-metrics \
  -gpu host \
  -grpc "$GRPC_PORT" \
  -grpc-use-token \
  -grpc-allowlist "$BENCH_DIR/gateway/emulator_access.json" \
  -netdelay none \
  -netspeed full \
  "${EXTRA_FLAGS[@]}" \
  >"$LOG" 2>&1 &
EMU_PID=$!
printf '%s\n' "$EMU_PID" > "$RESULT_DIR/current-emulator.pid"

for _ in $(seq 1 180); do
  if ! kill -0 "$EMU_PID" 2>/dev/null; then
    echo "Emulator exited during startup. Log:" >&2
    tail -120 "$LOG" >&2
    exit 1
  fi
  if "$ADB" -s "$SERIAL" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r' | grep -qx 1; then
    break
  fi
  sleep 1
done

if ! "$ADB" -s "$SERIAL" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r' | grep -qx 1; then
  echo "Emulator did not finish booting within 180s. Log:" >&2
  tail -120 "$LOG" >&2
  exit 1
fi

"$ADB" -s "$SERIAL" shell input keyevent KEYCODE_WAKEUP >/dev/null 2>&1 || true
"$ADB" -s "$SERIAL" shell wm dismiss-keyguard >/dev/null 2>&1 || true
"$ADB" -s "$SERIAL" shell wm size reset >/dev/null 2>&1 || true
"$ADB" -s "$SERIAL" shell wm density reset >/dev/null 2>&1 || true
"$ADB" -s "$SERIAL" shell settings put system peak_refresh_rate "$VSYNC" >/dev/null 2>&1 || true
"$ADB" -s "$SERIAL" shell settings put system min_refresh_rate "$VSYNC" >/dev/null 2>&1 || true

MOTION_APK="$BENCH_DIR/motion-app/build/webrtc-motion.apk"
if [[ -f "$MOTION_APK" ]]; then
  "$ADB" -s "$SERIAL" install -r -t "$MOTION_APK" >/dev/null
  "$ADB" -s "$SERIAL" shell am start -W -n com.android.webrtcbench.motion/.MainActivity >/dev/null
fi

FOUND=""
for _ in $(seq 1 100); do
  for candidate in $(ls -t "$RUNNING_DIR"/pid_*.ini 2>/dev/null); do
    [[ -f "$candidate" ]] || continue
    if grep -qx "port.serial=$EMU_PORT" "$candidate" \
      && grep -qx "avd.name=$RUN_AVD_NAME" "$candidate"; then
      FOUND="$candidate"
      break 2
    fi
  done
  sleep 0.1
done

if [[ -z "$FOUND" ]]; then
  echo "Could not find emulator discovery ini for $SERIAL in $RUNNING_DIR" >&2
  exit 1
fi

cp "$FOUND" "$DISCOVERY_FILE"
NEW_QEMU_PID="$(basename "$FOUND" .ini | sed 's/^pid_//')"
kill -0 "$NEW_QEMU_PID"
echo "Emulator ready: ${WIDTH}×${HEIGHT} @ ${VSYNC}Hz, API=$TARGET_API, AVD=$RUN_AVD_NAME, serial=$SERIAL, pid=$EMU_PID"
echo "Discovery: $DISCOVERY_FILE"
