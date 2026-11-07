// The Overview tiles while the range's collection gaps load or fail: a
// steady tile never claims to be steady before the gaps are known.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../api/requests";
import type { Point, Tile } from "../api/model";
import { HistoryContext } from "./HistoryNote";
import { OverviewTiles } from "./OverviewTiles";
import { NOW, gap, prod } from "../test/fixtures/history";
import { GlobalQueryProvider } from "../lib/useGlobalQuery";

vi.mock("../api/requests", () => ({ getDataHistory: vi.fn() }));
const read = vi.mocked(api.getDataHistory);
beforeEach(() => { read.mockReset(); });

const STEP = 900_000;
const START = NOW - 86_400_000;
const first = Math.floor(START / STEP) * STEP;
const last = Math.floor((NOW - 1) / STEP) * STEP;
const points = (v: (i: number) => number): Point[] => Array.from({ length: (last - first) / STEP + 1 }, (_, i) => ({ time: first + i * STEP, value: v(i) }));

function tile(id: string, vals: (i: number) => number): Tile {
  const pts = points(vals);
  const all = pts.map((p) => p.value);
  return { id, label: id, unit: "count", valueSuffix: "", value2Suffix: "", subtitle: "", status: "ok", description: "", value: all[all.length - 1],
    series: { metric: "m", unit: "count", stepMs: STEP, points: pts, min: Math.min(...all), max: Math.max(...all) } };
}

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tiles = [tile("streams", () => 8), tile("servers", (i) => (i === 40 ? 2 : 3))];
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={["/"]}><GlobalQueryProvider><HistoryContext.Provider value={{ start: START, end: NOW, live: true, historyStart: START - 86_400_000 }}>
    <OverviewTiles tiles={tiles} windows={[]} start={START} end={NOW} />
  </HistoryContext.Provider></GlobalQueryProvider></MemoryRouter></QueryClientProvider>);
}
const box = (c: HTMLElement, i: number) => c.querySelectorAll(".mbox")[i];
/** The text a reader sees: the width keeper under a status is hidden. */
function shown(e: Element): string {
  const c = e.cloneNode(true) as Element;
  c.querySelectorAll(".dh-status-keep").forEach((k) => k.remove());
  return c.textContent ?? "";
}

describe("tiles while the gaps are not known", () => {
  it("shows a steady tile without a chart and says the gaps are loading, never steady", () => {
    read.mockImplementation(() => new Promise(() => {}));
    const { container } = setup();
    expect(box(container, 0).querySelector("svg.spark")).toBeNull();
    expect(shown(box(container, 0))).toContain("History gaps loading");
    expect(shown(box(container, 0))).not.toContain("steady at");
    // The steady line keeps its width under the status, so a phone row wraps the same.
    expect(box(container, 0).querySelector(".dh-status-keep")?.textContent).toBe("steady at 8 all window");
    // A tile whose values vary draws its chart at once.
    expect(box(container, 1).querySelector("svg.spark")).not.toBeNull();
  });

  it("says steady once the gaps arrive and none overlaps the range", async () => {
    let done!: (d: ReturnType<typeof prod>) => void;
    read.mockImplementation(() => new Promise((r) => { done = r; }));
    const { container } = setup();
    await act(async () => done({ ...prod(), rangeStart: START, rangeEnd: NOW, gaps: [] }));
    await waitFor(() => expect(screen.getByText("steady at 8 all window")).toBeInTheDocument());
    expect(box(container, 0).querySelector("svg.spark")).toBeNull();
  });

  it("draws the chart when a gap that arrives overlaps the range", async () => {
    read.mockResolvedValue({ ...prod(), rangeStart: START, rangeEnd: NOW, gaps: [gap({ kind: "notRunning", start: NOW - 3_600_000, end: NOW - 3_400_000 })] });
    const { container } = setup();
    await waitFor(() => expect(box(container, 0).querySelector("svg.spark")).not.toBeNull());
  });

  it("never reads steady when the gaps cannot be read", async () => {
    read.mockRejectedValue(new Error("history unavailable"));
    const { container } = setup();
    await waitFor(() => expect(shown(box(container, 0))).toContain("History gaps unavailable"));
    expect(shown(box(container, 0))).not.toContain("steady at");
    expect(box(container, 0).querySelector("svg.spark")).toBeNull();
  });
});
