#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

BUILD_TOOLS="$(find "$SDK/build-tools" -mindepth 1 -maxdepth 1 -type d | sort -V | tail -1)"
ANDROID_JAR="$(find "$SDK/platforms" -mindepth 2 -maxdepth 2 -name android.jar | sort -V | tail -1)"
APP_DIR="$BENCH_DIR/motion-app"
BUILD="$APP_DIR/build"
JAVA_BIN="$ROOT/jdk/Contents/Home/bin"
KEYSTORE="$BUILD/webrtc-bench.keystore"

[[ -x "$BUILD_TOOLS/aapt2" ]] || { echo "aapt2 not found under $BUILD_TOOLS" >&2; exit 1; }
[[ -f "$ANDROID_JAR" ]] || { echo "android.jar not found" >&2; exit 1; }

mkdir -p "$BUILD/classes" "$BUILD/dex"
"$BUILD_TOOLS/aapt2" link \
  -o "$BUILD/unsigned.apk" \
  --manifest "$APP_DIR/AndroidManifest.xml" \
  -I "$ANDROID_JAR" \
  --min-sdk-version 23 \
  --target-sdk-version 36

"$JAVA_BIN/javac" \
  -source 8 -target 8 \
  -classpath "$ANDROID_JAR" \
  -d "$BUILD/classes" \
  "$APP_DIR/src/com/android/webrtcbench/motion/MainActivity.java"

"$BUILD_TOOLS/d8" \
  --release \
  --lib "$ANDROID_JAR" \
  --output "$BUILD/dex" \
  "$BUILD"/classes/com/android/webrtcbench/motion/*.class

cp "$BUILD/unsigned.apk" "$BUILD/unsigned-with-dex.apk"
(cd "$BUILD/dex" && zip -q "$BUILD/unsigned-with-dex.apk" classes.dex)

if [[ ! -f "$KEYSTORE" ]]; then
  "$JAVA_BIN/keytool" -genkeypair -noprompt \
    -keystore "$KEYSTORE" -storepass android -keypass android \
    -alias webrtc-bench -keyalg RSA -keysize 2048 -validity 3650 \
    -dname "CN=WebRTC Bench, OU=Test, O=Local, L=Local, S=Local, C=CN" >/dev/null 2>&1
fi

"$BUILD_TOOLS/apksigner" sign \
  --ks "$KEYSTORE" --ks-pass pass:android --key-pass pass:android \
  --out "$APP_DIR/build/webrtc-motion.apk" \
  "$BUILD/unsigned-with-dex.apk"

echo "$APP_DIR/build/webrtc-motion.apk"
