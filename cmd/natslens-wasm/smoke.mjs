// Smoke test of the built demo site's WebAssembly: loads natslens.wasm with
// the wasm_exec.js beside it, the way the page does, and runs one Connect
// call through globalThis.natslensFetch.
//
//   node cmd/natslens-wasm/smoke.mjs <site dir>
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";

const dir = resolve(process.argv[2] ?? "");
const require = createRequire(import.meta.url);
globalThis.fs ??= require("node:fs");
require(join(dir, "wasm_exec.js"));

const fail = (msg) => {
  console.error(`smoke: ${msg}`);
  process.exit(1);
};
setTimeout(() => fail("no answer within 30 seconds"), 30_000).unref();

const go = new globalThis.Go();
const ready = new Promise((ok) => {
  globalThis.natslensOnReady = ok;
});
globalThis.natslensLive = true;
const { instance } = await WebAssembly.instantiate(readFileSync(join(dir, "natslens.wasm")), go.importObject);
void go.run(instance);
await ready;

const body = new TextEncoder().encode("{}");
const res = await globalThis.natslensFetch(
  "POST",
  "/api/natslens.v1.LensService/GetLensInfo",
  { "content-type": "application/json", "connect-protocol-version": "1" },
  body,
);
const text = new TextDecoder().decode(res.body);
if (res.status !== 200) fail(`GetLensInfo answered ${res.status}: ${text}`);
const info = JSON.parse(text);
if (info.demo !== true || info.system !== "example-prod" || info.nowFixed === true) {
  fail(`unexpected GetLensInfo answer: ${text}`);
}
const missing = await globalThis.natslensFetch("GET", "/api/natslens.v1.LensService/NoSuchCall", {}, null);
if (missing.status !== 404) fail(`an unknown call answered ${missing.status}, want 404`);
console.log(`smoke: GetLensInfo answered for ${info.system}, version ${info.version}`);
process.exit(0);
