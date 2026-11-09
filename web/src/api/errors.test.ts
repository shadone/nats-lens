import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { errorInfo, isRetryable, shouldRetry } from "./errors";

describe("errorInfo", () => {
  it("names the Connect code and adds a short hint", () => {
    const i = errorInfo(new ConnectError("dial tcp: connection refused", Code.Unavailable));
    expect(i.code).toBe("unavailable");
    expect(i.message).toBe("dial tcp: connection refused");
    expect(i.hint).toMatch(/cannot be reached/);
    expect(errorInfo(new ConnectError("bad range", Code.InvalidArgument)).code).toBe("invalid_argument");
  });

  it("on the demo site, points to a reload, not to a server or its log", () => {
    for (const code of [Code.Unknown, Code.Internal, Code.Unavailable]) {
      const hint = errorInfo(new ConnectError("The demo stopped before it started.", code), true).hint;
      expect(hint).toMatch(/reload the page/);
      expect(hint).not.toMatch(/server|log/);
    }
    expect(errorInfo(new TypeError("Failed to fetch"), true).hint).toMatch(/download the demo/);
    expect(errorInfo(new ConnectError("x", Code.InvalidArgument), true).hint).toMatch(/check the range/);
    expect(errorInfo(new ConnectError("x", Code.Unknown)).hint).toMatch(/its log says why/);
  });

  it("calls a failed fetch a network error", () => {
    const e = new ConnectError("Failed to fetch", Code.Unknown, undefined, undefined, new TypeError("Failed to fetch"));
    expect(errorInfo(e).code).toBe("network");
    expect(errorInfo(new TypeError("Failed to fetch")).code).toBe("network");
  });
});

describe("retry", () => {
  it("retries only errors that may pass on their own", () => {
    expect(isRetryable(new ConnectError("x", Code.Unavailable))).toBe(true);
    expect(isRetryable(new ConnectError("x", Code.DeadlineExceeded))).toBe(false);
    expect(isRetryable(new ConnectError("x", Code.ResourceExhausted))).toBe(true);
    expect(isRetryable(new TypeError("Failed to fetch"))).toBe(true);
    expect(isRetryable(new ConnectError("x", Code.InvalidArgument))).toBe(false);
    expect(isRetryable(new ConnectError("x", Code.NotFound))).toBe(false);
    expect(isRetryable(new ConnectError("x", Code.Internal))).toBe(false);
  });

  it("stops after two retries", () => {
    const e = new ConnectError("x", Code.Unavailable);
    expect(shouldRetry(0, e)).toBe(true);
    expect(shouldRetry(1, e)).toBe(true);
    expect(shouldRetry(2, e)).toBe(false);
  });
});

describe("TypeErrors that are not network failures", () => {
  const bug = (() => {
    try {
      (undefined as unknown as { x: number }).x;
    } catch (x) {
      return x;
    }
  })();

  it("calls a programming TypeError a client error and does not retry it", () => {
    expect(errorInfo(bug).code).toBe("client");
    expect(errorInfo(bug).hint).toMatch(/reload/);
    expect(isRetryable(bug)).toBe(false);
  });

  it("does not take a ConnectError with a programming TypeError cause for a network failure", () => {
    const e = new ConnectError("x", Code.Unknown, undefined, undefined, bug);
    expect(errorInfo(e).code).toBe("unknown");
    expect(isRetryable(e)).toBe(false);
  });

  it("knows the fetch failures of Chrome, Firefox and Safari", () => {
    for (const m of ["Failed to fetch", "NetworkError when attempting to fetch resource.", "Load failed"]) {
      expect(errorInfo(new TypeError(m)).code).toBe("network");
      expect(errorInfo(new ConnectError(m, Code.Unavailable, undefined, undefined, new TypeError(m))).code).toBe("network");
    }
  });
});

describe("more fetch failure messages", () => {
  it("knows Safari's and other browsers' wording", () => {
    for (const m of ["The network connection was lost.", "The Internet connection appears to be offline.", "Network request failed", "network error"]) {
      expect(errorInfo(new TypeError(m)).code).toBe("network");
      expect(isRetryable(new TypeError(m))).toBe(true);
    }
  });
});
