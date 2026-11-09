// The demo site's transport, with a fake Go program: a start that cannot
// finish must fail the call, so the page shows an error, not a spinner.
import { ConnectError } from "@connectrpc/connect";
import { afterEach, describe, expect, it, vi } from "vitest";
import { errorInfo } from "./errors";
import { abortable, loadGo, makeDemoFetch, once, runProgram, type GoRuntime } from "./wasmTransport";

const URL_ = "http://127.0.0.1/api/natslens.v1.LensService/GetLensInfo";
const instance = {} as WebAssembly.Instance;

afterEach(() => {
  globalThis.natslensOnReady = undefined;
  globalThis.natslensFetch = undefined;
});

describe("loadGo", () => {
  it("fails a loader script that does not download as a download, so the page points to the connection", async () => {
    const loading = loadGo("/nats-lens/wasm_exec.js");
    const script = document.head.querySelector('script[src="/nats-lens/wasm_exec.js"]');
    expect(script).not.toBeNull();
    script!.dispatchEvent(new Event("error"));
    const e: unknown = await loading.catch((x: unknown) => x);
    expect(document.head.contains(script)).toBe(false);
    // As the Connect client passes it on: a ConnectError with it as the cause.
    for (const shown of [e, ConnectError.from(e)]) {
      const info = errorInfo(shown, true);
      expect(info.code).toBe("network");
      expect(info.hint).toMatch(/could not download the demo/);
    }
  });
});

describe("runProgram", () => {
  it("fails when the program stops before it is ready", async () => {
    const go: GoRuntime = { importObject: {}, run: () => Promise.resolve() };
    await expect(runProgram(go, instance)).rejects.toThrow("The demo stopped before it started.");
  });

  it("resolves when the program says it is ready, and keeps running", async () => {
    const go: GoRuntime = {
      importObject: {},
      run: () => {
        queueMicrotask(() => globalThis.natslensOnReady!());
        return new Promise<void>(() => {});
      },
    };
    await expect(runProgram(go, instance)).resolves.toBeUndefined();
  });
});

describe("makeDemoFetch", () => {
  it("gives up waiting for a start that never finishes when the call's signal aborts", async () => {
    const fetch = makeDemoFetch(() => new Promise<void>(() => {}));
    const ctl = new AbortController();
    const call = fetch(URL_, { method: "POST", body: "{}", signal: ctl.signal });
    ctl.abort(new Error("deadline"));
    await expect(call).rejects.toThrow("deadline");
  });

  it("gives up waiting for an answer when the call's signal aborts", async () => {
    globalThis.natslensFetch = () => new Promise(() => {});
    const fetch = makeDemoFetch(() => Promise.resolve());
    const ctl = new AbortController();
    const call = fetch(URL_, { method: "POST", body: "{}", signal: ctl.signal });
    setTimeout(() => ctl.abort(new Error("deadline")), 0);
    await expect(call).rejects.toThrow("deadline");
  });

  it("does not start a call whose signal has already aborted", async () => {
    const ready = vi.fn(() => Promise.resolve());
    const ctl = new AbortController();
    ctl.abort(new Error("gone"));
    await expect(makeDemoFetch(ready)(URL_, { signal: ctl.signal })).rejects.toThrow("gone");
  });

  it("runs the request in the program, by the path below the origin", async () => {
    const calls: unknown[][] = [];
    globalThis.natslensFetch = async (...args) => {
      calls.push(args);
      return { status: 200, headers: { "content-type": "application/json" }, body: new TextEncoder().encode('{"demo":true}') as Uint8Array<ArrayBuffer> };
    };
    const res = await makeDemoFetch(() => Promise.resolve())(URL_, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ demo: true });
    expect(calls[0][0]).toBe("POST");
    expect(calls[0][1]).toBe("/api/natslens.v1.LensService/GetLensInfo");
    expect(calls[0][2]).toMatchObject({ "content-type": "application/json" });
  });
});

describe("once", () => {
  it("starts once, and again after a failed start", async () => {
    let n = 0;
    const start = once(async () => {
      n++;
      if (n === 1) throw new Error("first");
    });
    await expect(start()).rejects.toThrow("first");
    await start();
    await start();
    expect(n).toBe(2);
  });
});

describe("abortable", () => {
  it("passes the result through without a signal", async () => {
    await expect(abortable(Promise.resolve(3), undefined)).resolves.toBe(3);
  });
});
