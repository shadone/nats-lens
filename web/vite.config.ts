/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dev server forwards /api to a running `natslens demo` or `natslens run`.
// NATSLENS_API overrides the target, for example http://127.0.0.1:18431.
const api = process.env.NATSLENS_API ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
    // Measurements found multiple shared helpers below 3 kB. Co-locate them
    // with their dependencies to preserve execution order and avoid tiny requests.
    // The entire initial static/preload closure still counts toward the budget.
    rolldownOptions: { output: { codeSplitting: { groups: [{
      name: "page-support",
      test: /\/src\/(?:charts\/(?:MiniLine|Sparkline)|components\/(?:AccountName|ConsumersTable|Pager|FindingParts|MetricBox|PlacementSquares|ReplayEntry|SortHeader|Trends)|lib\/(?:events|replayUrl|storyLanes|useDebounced|sort))\.[^/]+$/,
    }] } } },
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
  },
});
