// The UI's source files, for tests that check every file for a pattern.
// They are read from disk on each run: import.meta.glob lists the files
// when vitest first transforms the test, so while its module cache is warm
// a file added later is not checked.
import { readdirSync, readFileSync } from "node:fs";

// src/, from this file's address; new URL("../", import.meta.url) would
// be rewritten by Vite into an asset URL on its dev server.
const SRC = new URL(import.meta.url.replace(/test\/sources\.ts$/, ""));

/**
 * Every .ts and .tsx file under src except tests, generated code and test
 * helpers, by its path from src/lib ("./basePath.ts",
 * "../components/App.tsx"), with its text.
 */
export function uiSources(): Record<string, string> {
  const out: Record<string, string> = {};
  for (const path of readdirSync(SRC, { recursive: true, encoding: "utf8" }).map((p) => p.replaceAll("\\", "/")).sort()) {
    if (!/\.tsx?$/.test(path) || /\.test\.tsx?$/.test(path) || /^(gen|test)\//.test(path)) continue;
    const key = path.startsWith("lib/") ? `./${path.slice(4)}` : `../${path}`;
    out[key] = readFileSync(new URL(path, SRC), "utf8");
  }
  return out;
}
