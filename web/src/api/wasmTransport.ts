// The demo site's API: the fake systems of `natslens demo`, compiled to
// WebAssembly (cmd/natslens-wasm) and run in this page. demoFetch stands in
// for the Connect transport's fetch, so a call never reaches the network.
// Only the demo site's build imports this module (see demoFetch.ts); the
// first call downloads and starts the program.

export interface GoRuntime {
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

let loader: Promise<void> | undefined;

/** Loads wasm_exec.js once; a failed load removes its tag, so a retry adds a fresh one. */
function loadGo(src: string): Promise<void> {
  if (globalThis.Go) return Promise.resolve();
  loader ??= new Promise<void>((resolve, reject) => {
    const s = document.createElement("script");
    s.src = src;
    s.onload = () => resolve();
    s.onerror = () => {
      s.remove();
      loader = undefined;
      reject(new Error(`could not load ${src}`));
    };
    document.head.appendChild(s);
  });
  return loader;
}

async function instantiate(url: string, imports: WebAssembly.Imports, signal?: AbortSignal): Promise<WebAssembly.Instance> {
  const res = await fetch(url, { signal });
  if (!res.ok) throw new Error(`could not load ${url}: HTTP ${res.status}`);
  // Streaming compiles while the file downloads, but needs the
  // application/wasm type; a server that sends another falls back.
  if (res.headers.get("Content-Type")?.startsWith("application/wasm")) {
    return (await WebAssembly.instantiateStreaming(res, imports)).instance;
  }
  return (await WebAssembly.instantiate(await res.arrayBuffer(), imports)).instance;
}

/**
 * Runs the program until it says it is ready. A program that stops first
 * (a panic in main, for one) never will, so its end fails the start.
 */
export function runProgram(go: GoRuntime, instance: WebAssembly.Instance): Promise<void> {
  const ready = new Promise<void>((resolve) => {
    globalThis.natslensOnReady = resolve;
  });
  const stopped = go.run(instance).then(() => {
    throw new Error("The demo stopped before it started.");
  });
  return Promise.race([ready, stopped]);
}

async function start(): Promise<void> {
  const base = import.meta.env.BASE_URL;
  // ?fixed in the page's address keeps the scenario at its own time, as
  // `natslens demo -fixed` does; it is read once, when the program starts.
  globalThis.natslensLive = !new URLSearchParams(window.location.search).has("fixed");
  await loadGo(`${base}wasm_exec.js`);
  if (!globalThis.Go) throw new Error("wasm_exec.js did not define Go");
  const go = new globalThis.Go();
  await runProgram(go, await instantiate(`${base}natslens.wasm`, go.importObject));
}

/** Rejects with the signal's reason once it aborts, whatever p does. */
export function abortable<T>(p: Promise<T>, signal: AbortSignal | null | undefined): Promise<T> {
  if (!signal) return p;
  signal.throwIfAborted();
  return new Promise<T>((resolve, reject) => {
    const abort = () => reject(signal.reason);
    signal.addEventListener("abort", abort, { once: true });
    p.then(resolve, reject).finally(() => signal.removeEventListener("abort", abort));
  });
}

/** Starts once; a failed start is tried again by the next call. */
export function once(fn: () => Promise<void>): () => Promise<void> {
  let started: Promise<void> | undefined;
  return () => {
    started ??= fn().catch((e: unknown) => {
      started = undefined;
      throw e;
    });
    return started;
  };
}

/**
 * fetch for the Connect transport, given how to start the program: runs the
 * request in the page's program. The call's signal (Connect's timeout)
 * ends the wait for a start or an answer.
 */
export function makeDemoFetch(ready: () => Promise<void>): typeof globalThis.fetch {
  return async (input, init) => {
    const signal = init?.signal ?? (input instanceof Request ? input.signal : undefined);
    await abortable(ready(), signal);
    const { signal: _, ...rest } = init ?? {};
    const req = new Request(input, rest);
    const headers: Record<string, string> = {};
    req.headers.forEach((v, k) => {
      headers[k] = v;
    });
    const body = req.method === "GET" || req.method === "HEAD" ? null : new Uint8Array(await req.arrayBuffer());
    const url = new URL(req.url);
    const res = await abortable(globalThis.natslensFetch!(req.method, url.pathname + url.search, headers, body), signal);
    const empty = res.status === 204 || res.status === 304;
    return new Response(empty ? null : res.body, { status: res.status, headers: res.headers });
  };
}

export const demoFetch = makeDemoFetch(once(start));
