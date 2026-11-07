import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { HistoryContext, type PageHistory } from "../components/HistoryNote";
import * as api from "../api/requests";
import { ChartHistoryStatus, HistoryGaps } from "./HistoryGaps";
import { NOW, gap, staging } from "../test/fixtures/history";
import { resolveRange, type RangeSpec } from "../lib/timeRange";

vi.mock("../api/requests", () => ({ getDataHistory: vi.fn() }));
const H = 3_600_000, DAY = 24 * H;
const read = vi.mocked(api.getDataHistory);
beforeEach(() => { read.mockReset(); });
function setup(start: number, end: number, page: PageHistory, route: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Frame({ a = start, b = end, p = page, second = false, url = route }) {
    return <QueryClientProvider client={qc}><MemoryRouter initialEntries={[url]} key={url}><HistoryContext.Provider value={p}>
      <svg><HistoryGaps start={a} end={b} width={200} height={40} />{second && <HistoryGaps start={a} end={b} width={200} height={40} />}</svg><ChartHistoryStatus start={a} end={b} />
    </HistoryContext.Provider></MemoryRouter></QueryClientProvider>;
  }
  return { ...render(<Frame />), qc, Frame };
}
it.each(["1h", "24h"])("reads the seven-day chart domain with global %s and reuses it for later charts", async (preset) => {
  read.mockImplementation(async (_sys, spec) => {
    const r = resolveRange(spec, NOW);
    return { ...staging(), rangeStart: r.start, rangeEnd: r.end, gaps: r.start <= NOW - 5 * H ? [gap({ kind: "notRunning", start: NOW - 5 * H, end: NOW - 5 * H + 180_000 })] : [] };
  });
  const p = { start: NOW - (preset === "1h" ? H : DAY), end: NOW, live: true };
  const r = setup(NOW - 7 * DAY, NOW, p, `/?system=example-staging&range=${preset}`);
  await waitFor(() => expect(screen.getByRole("img", { name: /was not running/ })).toBeInTheDocument());
  expect(read.mock.calls[0][1]).toEqual({ kind: "live", preset: "7d" });
  r.rerender(<r.Frame second />);
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 25)); });
  expect(read).toHaveBeenCalledTimes(1);
});
it("clips an open gap to its originating observation, including its accessible name", async () => {
  const p = { start: NOW - DAY, end: NOW, live: true, gaps: [gap({ start: NOW - H })] };
  setup(p.start, NOW + H, p, "/");
  const mark = screen.getByRole("img", { name: /No collections/ });
  expect(Number(mark.getAttribute("x")) + Number(mark.getAttribute("width"))).toBeLessThan(200);
  expect(mark).not.toHaveAttribute("aria-label", expect.stringContaining("until now"));
});
it.each([false, true])("does not reuse another range's open outage while the next read is delayed (error %s)", async (fail) => {
  const old = { kind: "fixed", from: NOW - DAY, to: NOW - H } as const;
  read.mockResolvedValueOnce({ ...staging(), rangeStart: old.from, rangeEnd: old.to, gaps: [gap({ start: NOW - 2 * H })] });
  let reject!: (e: Error) => void;
  read.mockImplementationOnce(() => new Promise((_resolve, no) => { reject = no; }));
  const url = (s: RangeSpec) => s.kind === "fixed" ? `/?system=example-staging&from=${new Date(s.from).toISOString()}&to=${new Date(s.to).toISOString()}` : "/";
  const p = { start: old.from, end: old.to, live: false };
  const r = setup(old.from, old.to, p, url(old));
  await screen.findByRole("img", { name: /No collections/ });
  // Keep the observer mounted: a changed chart domain must never extend old data.
  const next = { kind: "fixed", from: old.from, to: NOW } as const;
  r.rerender(<r.Frame b={NOW} p={{ ...p, end: NOW }} url={url(old)} />);
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole("img", { name: /No collections/ })).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("History gaps loading");
  if (fail) {
    await act(async () => reject(new Error("history unavailable")));
    expect(screen.queryByRole("img", { name: /No collections/ })).not.toBeInTheDocument();
  }
  // A different system must never inherit the previous system's marks either.
  r.rerender(<r.Frame b={NOW} p={{ ...p, end: NOW }} url={url(next).replace("example-staging", "example-prod")} />);
  expect(screen.queryByRole("img", { name: /No collections/ })).not.toBeInTheDocument();
});
it("keeps a sixty-second outage visible on a thirty-day chart and omits zero/outside intervals", () => {
  const start = NOW - 30 * DAY;
  const p = { start, end: NOW, live: false, gaps: [gap({ start: NOW - DAY, end: NOW - DAY + 60_000 }), gap({ start: start - H, end: start }), gap({ start: NOW, end: NOW }), gap({ start: NOW + H, end: NOW + 2 * H })] };
  setup(start, NOW, p, "/");
  const marks = screen.getAllByRole("img");
  expect(marks).toHaveLength(1);
  expect(Number(marks[0].getAttribute("width"))).toBeGreaterThanOrEqual(1.5);
});
it("bounds retained live data on a failed refresh and tells the reader history is unavailable", async () => {
  read.mockResolvedValueOnce({ ...staging(), rangeStart: NOW - DAY, rangeEnd: NOW, gaps: [gap({ start: NOW - H })] });
  read.mockRejectedValue(new Error("history unavailable"));
  const p = { start: NOW - DAY, end: NOW, live: true };
  const r = setup(p.start, NOW, p, "/?range=24h");
  await screen.findByRole("img", { name: /No collections/ });
  r.rerender(<r.Frame a={p.start + H} b={NOW + H} p={{ ...p, start: p.start + H, end: NOW + H }} />);
  await act(async () => { await r.qc.invalidateQueries(); });
  expect(await screen.findByRole("status")).toHaveTextContent("History gaps unavailable");
  const mark = screen.getByRole("img", { name: /No collections/ });
  expect(Number(mark.getAttribute("x")) + Number(mark.getAttribute("width"))).toBeLessThan(200);
  expect(mark).not.toHaveAttribute("aria-label", expect.stringContaining("until now"));
});
it("keeps a stable shared key when page and chart clocks tick in separate renders", async () => {
  read.mockResolvedValue({ ...staging(), gaps: [] });
  const p = { start: NOW - DAY, end: NOW, live: true };
  const r = setup(p.start, NOW, p, "/?range=24h");
  await waitFor(() => expect(r.qc.isFetching()).toBe(0));
  r.rerender(<r.Frame p={{ ...p, start: p.start + 30_000, end: NOW + 30_000 }} />);
  r.rerender(<r.Frame a={p.start + 30_000} b={NOW + 30_000} p={{ ...p, start: p.start + 30_000, end: NOW + 30_000 }} />);
  expect(read).toHaveBeenCalledTimes(1);
  expect(r.qc.getQueryCache().getAll()).toHaveLength(1);
});
