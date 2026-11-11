/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dev server forwards /api to a running `natslens demo` or `natslens run`.
// NATSLENS_API overrides the target, for example http://127.0.0.1:18431.
const api = process.env.NATSLENS_API ?? "http://127.0.0.1:8080";

// The demo site's build (make demo-site) runs the API in the page: its
// Connect transport takes the fetch of wasmTransport.ts, which the other
// builds never import.
const wasm = process.env.VITE_NATSLENS_WASM === "1";

export default defineConfig({
  // The path the UI is served under; the demo site sets /nats-lens/.
  base: process.env.NATSLENS_BASE ?? "/",
  plugins: [react()],
  resolve: {
    alias: wasm ? [{ find: /^\.\/demoFetch$/, replacement: fileURLToPath(new URL("./src/api/wasmTransport.ts", import.meta.url)) }] : [],
  },
  build: {
    outDir: process.env.NATSLENS_WEB_OUT_DIR ?? "../internal/webui/dist",
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,

  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    // changeOrigin sends the target's host, which the server's Host allowlist accepts.
    proxy: { "/api": { target: api, changeOrigin: true } },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: false,
    restoreMocks: true,
    // A loaded runner multiplies a test's CPU time: the heaviest render
    // tests take about 1 s alone and timed out at the default 5 s with 36
    // workers on 12 busy cores.
    testTimeout: 15_000,
  },
});
