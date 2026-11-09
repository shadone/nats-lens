// The demo site's API: the fake systems of `natslens demo`, compiled to
// WebAssembly (cmd/natslens-wasm) and run in this page. wasmFetch stands in
// for the Connect transport's fetch, so a call never reaches the network.
// Only the demo site's build imports this module; the first call downloads
// and starts the program.

interface GoRuntime {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

interface WasmResponse {
  status: number;
  headers: Record<string, string>;
  body: Uint8Array<ArrayBuffer>;
}

declare global {
  // Set by wasm_exec.js, the Go toolchain's loader.
  var Go: (new () => GoRuntime) | undefined;
  // Set by the program; see cmd/natslens-wasm.
  var natslensFetch: ((method: string, url: string, headers: Record<string, string>, body: Uint8Array | null) => Promise<WasmResponse>) | undefined;
  var natslensOnReady: (() => void) | undefined;
  var natslensLive: boolean | undefined;
}

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = src;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error(`could not load ${src}`));
    document.head.appendChild(s);
  });
}

async function instantiate(url: string, imports: WebAssembly.Imports): Promise<WebAssembly.Instance> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`could not load ${url}: HTTP ${res.status}`);
  // Streaming compiles while the file downloads, but needs the
  // application/wasm type; a server that sends another falls back.
  if (res.headers.get("Content-Type")?.startsWith("application/wasm")) {
    return (await WebAssembly.instantiateStreaming(res, imports)).instance;
  }
  return (await WebAssembly.instantiate(await res.arrayBuffer(), imports)).instance;
}

async function start(): Promise<void> {
  const base = import.meta.env.BASE_URL;
  // ?fixed in the page's address keeps the scenario at its own time, as
  // `natslens demo -fixed` does; it is read once, when the program starts.
  globalThis.natslensLive = !new URLSearchParams(window.location.search).has("fixed");
  await loadScript(`${base}wasm_exec.js`);
  if (!globalThis.Go) throw new Error("wasm_exec.js did not define Go");
  const go = new globalThis.Go();
  const ready = new Promise<void>((resolve) => {
    globalThis.natslensOnReady = resolve;
  });
  const instance = await instantiate(`${base}natslens.wasm`, go.importObject);
  void go.run(instance);
  await ready;
}

let started: Promise<void> | undefined;

/** Starts the program once; a failed start is tried again on the next call. */
function ready(): Promise<void> {
  started ??= start().catch((e: unknown) => {
    started = undefined;
    throw e;
  });
  return started;
}

/** fetch for the Connect transport: runs the request in the page's program. */
export async function demoFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  await ready();
  const req = new Request(input, init);
  const headers: Record<string, string> = {};
  req.headers.forEach((v, k) => {
    headers[k] = v;
  });
  const body = req.method === "GET" || req.method === "HEAD" ? null : new Uint8Array(await req.arrayBuffer());
  const url = new URL(req.url);
  const res = await globalThis.natslensFetch!(req.method, url.pathname + url.search, headers, body);
  const empty = res.status === 204 || res.status === 304;
  return new Response(empty ? null : res.body, { status: res.status, headers: res.headers });
}
