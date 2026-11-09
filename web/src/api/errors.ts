// What went wrong with a call, in words, and whether trying again can help.
import { Code, ConnectError } from "@connectrpc/connect";
import { HOSTED_DEMO } from "../lib/hostedDemo";

export interface ErrorInfo {
  /** Connect code name, for example unavailable, or "network". */
  code: string;
  message: string;
  hint: string;
}

const HINTS: Partial<Record<Code, string>> = {
  [Code.Unavailable]: "The nats-lens server cannot be reached; it may be restarting.",
  [Code.DeadlineExceeded]: "The server took too long to answer.",
  [Code.ResourceExhausted]: "The server is busy.",
  [Code.InvalidArgument]: "The server did not accept the request; check the range and filters.",
  [Code.NotFound]: "It does not exist, or no longer does.",
  [Code.Unauthenticated]: "Your sign-in may have expired; reload the page.",
  [Code.PermissionDenied]: "Your sign-in may have expired; reload the page.",
  [Code.FailedPrecondition]: "The server cannot do this in its current state.",
  [Code.Internal]: "The server failed; its log says why.",
  [Code.Unknown]: "The server failed; its log says why.",
};

/**
 * On the demo site no server runs and no log is kept: the API runs in the
 * page, and a reload starts it again.
 */
const DEMO_HINTS: Partial<Record<Code, string>> = {
  [Code.Unavailable]: "The demo in this page is not running; reload the page.",
  [Code.Internal]: "The demo in this page failed; reload the page to try again.",
  [Code.Unknown]: "The demo in this page failed; reload the page to try again.",
};
const DEMO_NETWORK_HINT = "The browser could not download the demo; check the connection and reload the page.";

/** snake_case, as Connect writes codes on the wire: deadline_exceeded. */
function codeName(c: Code): string {
  return Code[c].replace(/[A-Z]/g, (m, i: number) => (i ? "_" : "") + m.toLowerCase());
}

/** The TypeError messages fetch throws in Chrome, Firefox, Safari and others. */
const FETCH_FAILURE = new RegExp(
  "^(" +
    [
      "Failed to fetch",
      "NetworkError when attempting to fetch resource",
      "Load failed",
      "The network connection was lost",
      "The Internet connection appears to be offline",
      "Network request failed",
      "network error",
    ].join("|") +
    ")\\.?$",
  "i",
);

function isFetchFailure(e: unknown): boolean {
  return e instanceof TypeError && FETCH_FAILURE.test(e.message);
}

/** A failed fetch, bare or wrapped by Connect as unknown or unavailable. */
function isNetwork(e: unknown): boolean {
  if (isFetchFailure(e)) return true;
  return e instanceof ConnectError && (e.code === Code.Unknown || e.code === Code.Unavailable) && isFetchFailure(e.cause);
}

/** hosted is whether this is the demo site; tests set it. */
export function errorInfo(e: unknown, hosted = HOSTED_DEMO): ErrorInfo {
  if (isNetwork(e)) {
    const message = e instanceof ConnectError ? e.rawMessage : (e as Error).message;
    return { code: "network", message, hint: hosted ? DEMO_NETWORK_HINT : "The browser could not reach the nats-lens server." };
  }
  if (e instanceof ConnectError) {
    return { code: codeName(e.code), message: e.rawMessage, hint: (hosted ? DEMO_HINTS[e.code] : undefined) ?? HINTS[e.code] ?? "" };
  }
  if (e instanceof TypeError) {
    return { code: "client", message: e.message, hint: "The page hit an error of its own; reload it." };
  }
  return { code: "error", message: e instanceof Error ? e.message : String(e), hint: "" };
}

/**
 * The error a response reports in its own fields, as a Connect code name
 * and a message, as the ConnectError a failed call would have thrown.
 */
export function reportedError(code: string, message: string): ConnectError {
  const c = (Object.values(Code).filter((v): v is Code => typeof v === "number") as Code[]).find((x) => codeName(x) === code);
  return new ConnectError(message, c ?? Code.Unknown);
}

// deadline_exceeded is not retried: the attempt already waited the full
// call timeout, and a slow server should be shown, not called again.
const RETRYABLE = new Set([Code.Unavailable, Code.ResourceExhausted]);

/** Only errors that may pass on their own are worth another try. */
export function isRetryable(e: unknown): boolean {
  return isNetwork(e) || (e instanceof ConnectError && RETRYABLE.has(e.code));
}

/** TanStack Query's retry option: at most two retries of a retryable error. */
export function shouldRetry(failures: number, e: unknown): boolean {
  return failures < 2 && isRetryable(e);
}
