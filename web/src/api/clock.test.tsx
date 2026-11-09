// The UI's clock under `natslens demo -fixed`, whose now is weeks before the
// browser's: a live range must start before the server's now.
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ClockGate } from "../components/ClockGate";
import { DemoStarting } from "../components/HostedDemo";
import { serverNow, setServerNow } from "../lib/clock";
import { renderApp } from "../test/render";
import { lens } from "./client";
import { rangeInit, syncClock } from "./requests";

vi.mock("./client", () => ({ lens: { getLensInfo: vi.fn() } }));

const FIXED = Date.parse("2026-10-17T14:30:00Z");
const DAY = 86_400_000;

describe("the server's clock", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    setServerNow(Date.now());
    vi.clearAllMocks();
  });

  it("starts a live range a preset before the server's now, not the browser's", () => {
    setServerNow(FIXED);
    const r = rangeInit({ kind: "live", preset: "24h" });
    expect(r.end).toBeUndefined();
    const start = Number(r.start!.seconds) * 1000;
    expect(Math.abs(start - (FIXED - DAY))).toBeLessThan(1000);
  });

  it("stands still when the server says its now is fixed, so an hour later Last 1h still starts before it", async () => {
    vi.mocked(lens.getLensInfo).mockResolvedValue({ now: timestampFromMs(FIXED), nowFixed: true, demo: true } as never);
    await syncClock();
    const later = Date.now() + 3_600_000;
    vi.spyOn(Date, "now").mockReturnValue(later);
    expect(serverNow()).toBe(FIXED);
    expect(Number(rangeInit({ kind: "live", preset: "1h" }).start!.seconds) * 1000).toBe(FIXED - 3_600_000);
  });

  it("moves on when the server's now is not fixed, even for a demo that is not live", async () => {
    vi.mocked(lens.getLensInfo).mockResolvedValue({ now: timestampFromMs(FIXED), nowFixed: false, demo: true, live: false } as never);
    await syncClock();
    const later = Date.now() + 3_600_000;
    vi.spyOn(Date, "now").mockReturnValue(later);
    expect(Math.abs(serverNow() - (FIXED + 3_600_000))).toBeLessThan(1000);
  });

  it("gives up on a server that does not answer, leaving the browser's clock", async () => {
    vi.mocked(lens.getLensInfo).mockImplementation(
      (_req, opts) => new Promise((_, reject) => opts?.signal?.addEventListener("abort", () => reject(new Error("aborted")))) as never,
    );
    await expect(syncClock(undefined, 50)).rejects.toThrow();
    expect(vi.mocked(lens.getLensInfo).mock.calls[0][1]).toMatchObject({ timeoutMs: 50 });
    expect(Math.abs(serverNow() - Date.now())).toBeLessThan(1000);
  }, 1000);

  it("shows the shell once the server's now is read", async () => {
    vi.mocked(lens.getLensInfo).mockResolvedValue({ now: timestampFromMs(FIXED) } as never);
    renderApp(
      <ClockGate>
        <p>shell</p>
      </ClockGate>,
    );
    expect(screen.queryByText("shell")).toBeNull();
    expect(await screen.findByText("shell")).toBeInTheDocument();
    expect(Math.abs(serverNow() - FIXED)).toBeLessThan(1000);
  });

  it("shows its fallback, the demo site's start, until the server's now is read", async () => {
    vi.mocked(lens.getLensInfo).mockResolvedValue({ now: timestampFromMs(FIXED) } as never);
    renderApp(
      <ClockGate fallback={<DemoStarting />}>
        <p>shell</p>
      </ClockGate>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Loading the demo…");
    expect(await screen.findByText("shell")).toBeInTheDocument();
    expect(screen.queryByText("Loading the demo…")).toBeNull();
  });

  it("shows the shell on the browser's clock when the server's now cannot be read", async () => {
    vi.mocked(lens.getLensInfo).mockRejectedValue(new Error("unavailable"));
    renderApp(
      <ClockGate>
        <p>shell</p>
      </ClockGate>,
    );
    expect(await screen.findByText("shell")).toBeInTheDocument();
    expect(Math.abs(serverNow() - Date.now())).toBeLessThan(1000);
  });
});
