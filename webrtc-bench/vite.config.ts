import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// android-emulator-webrtc ships its protobuf bindings as CommonJS generated
// JavaScript. Convert the couple of static imports to ESM so Vite can bundle
// the official package without a prebuild step.
const protoCommonJsPlugin = () => ({
  name: "android-emulator-webrtc-proto-commonjs",
  enforce: "pre" as const,
  transform(code: string, id: string) {
    if (!id.endsWith("emulator_controller_pb.js")) return null;
    let transformed = code
      .replace(
        /var jspb = require\(['"]google-protobuf['"]\);/g,
        "import * as jspb from 'google-protobuf';",
      )
      .replace(
        /var google_protobuf_empty_pb = require\(['"]google-protobuf\/google\/protobuf\/empty_pb\.js['"]\);/g,
        "import * as google_protobuf_empty_pb from 'google-protobuf/google/protobuf/empty_pb.js';",
      )
      .replace(
        /goog\.object\.extend\(exports, proto\.android\.emulation\.control\);/g,
        "export default proto.android.emulation.control;",
      );
    return { code: transformed, map: null };
  },
});

export default defineConfig({
  plugins: [protoCommonJsPlugin(), react()],
  optimizeDeps: {
    include: ["google-protobuf/google/protobuf/empty_pb.js"],
  },
  build: {
    commonjsOptions: {
      include: [/node_modules/, /proto/],
    },
  },
});
