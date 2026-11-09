/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dev server forwards /api to a running `natslens demo` or `natslens run`.
// NATSLENS_API overrides the target, for example http://127.0.0.1:18431.
const api = process.env.NATSLENS_API ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
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
  },
});
