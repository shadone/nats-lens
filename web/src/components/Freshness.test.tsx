import { Code, ConnectError } from "@connectrpc/connect";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Freshness as FreshnessModel } from "../api/model";
import { Freshness } from "./RangePicker";

const live = { kind: "live", preset: "24h" } as const;
const fresh = (ageMs: number, liveCollector = true): FreshnessModel => ({ epoch: "9", epochAt: Date.now() - ageMs, partial: false, live: liveCollector, intervalMs: 30_000, collecting: true });

describe("Freshness", () => {
  it("says live with the age of the newest epoch", () => {
    render(<Freshness spec={live} freshness={fresh(4_000)} />);
    expect(screen.getByText(/live/)).toHaveTextContent("live · 4s");
  });

  it("says stale after three collect intervals", () => {
    render(<Freshness spec={live} freshness={fresh(95_000)} />);
    const el = screen.getByText(/stale/);
    expect(el).toHaveTextContent("stale · 1m");
    expect(el).toHaveClass("stale");
  });

  it("says offline in the critical colour when the status call fails, even with older data", () => {
    render(<Freshness spec={live} freshness={fresh(4_000)} error={new ConnectError("refused", Code.Unavailable)} />);
    const el = screen.getByText(/offline/);
    expect(el).toHaveClass("offline");
    expect(el).toHaveAttribute("title", expect.stringContaining("unavailable"));
  });

  it("says fixed for a fixed range, and offline over it when the status call fails", () => {
    const { rerender } = render(<Freshness spec={{ kind: "fixed", from: 0, to: 1 }} freshness={fresh(4_000)} />);
    expect(screen.getByText("fixed")).toBeInTheDocument();
    rerender(<Freshness spec={{ kind: "fixed", from: 0, to: 1 }} freshness={fresh(4_000)} error={new Error("x")} />);
    expect(screen.getByText("offline")).toHaveClass("offline");
  });
});

describe("Freshness when the collector stopped", () => {
  const since = Date.parse("2026-10-17T11:30:00Z");
  const stopped = (f: Partial<FreshnessModel>): FreshnessModel => ({ ...fresh(3 * 3_600_000), collecting: false, notCollectingSince: since, ...f });

  it("says not collecting since the time it stopped, in place of stale", () => {
    render(<Freshness spec={live} freshness={stopped({})} />);
    const el = screen.getByText(/not collecting/);
    expect(el).toHaveTextContent(/^not collecting since 11:30$/);
    expect(el).toHaveClass("stale");
    expect(screen.queryByText(/stale|no data/)).toBeNull();
  });

  it("says not collecting, not no data, when the range holds no collection", () => {
    render(<Freshness spec={live} freshness={stopped({ epoch: "0", epochAt: undefined })} />);
    expect(screen.getByText(/not collecting/)).toHaveTextContent(/^not collecting since 11:30$/);
  });

  it("says no data only when no collection exists and the collector collects", () => {
    render(<Freshness spec={live} freshness={{ ...fresh(0), epoch: "0", epochAt: undefined }} />);
    expect(screen.getByText("no data")).toBeInTheDocument();
  });
});
