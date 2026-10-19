import { act, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { useNavigate, type NavigateFunction } from "react-router";
import * as requests from "../api/requests";
import type { RangeSpec } from "../lib/timeRange";
import { masset, mcell, mmatrix } from "../test/fixtures/placement";
import { renderApp } from "../test/render";
import { Placement } from "./Placement";

vi.mock("../api/requests", () => ({ getPlacement: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

afterEach(() => vi.mocked(requests.getPlacement).mockReset());

const now = () => mmatrix([masset("stream", "orders", "", [mcell("nats-0", "leader"), mcell("nats-1", "in_sync", { lag: 0 })])]);
const before = () => mmatrix([masset("stream", "orders", "", [mcell("nats-0", "leader"), mcell("nats-1", "offline")])]);
const never = () => new Promise<never>(() => {});

let go: NavigateFunction;
function Nav() {
  go = useNavigate();
  return null;
}

function page(route: string) {
  renderApp(
    <>
      <Placement />
      <Nav />
    </>,
    route,
  );
}

// The comparison line; a failed refetch adds a second status, the stale banner.
const line = () => screen.getAllByRole("status").find((e) => e.classList.contains("pm-cmp-line"))!;

// The comparison of system a must not stand for system b while b's moment
// loads, even after the choice went away and came back.
it("drops the comparison of the previous system until the new moment loads", async () => {
  vi.mocked(requests.getPlacement).mockImplementation(async (sys: string, spec: RangeSpec) =>
    spec.kind === "live" ? now() : sys === "a" ? before() : never(),
  );
  page("/placement?system=a&cmp=1d");
  await waitFor(() => expect(line()).toHaveTextContent("1 cell differs"));
  act(() => go("/placement?system=b"));
  await waitFor(() => expect(vi.mocked(requests.getPlacement).mock.calls.some(([s]) => s === "b")).toBe(true));
  await screen.findAllByText("orders");
  act(() => go("/placement?system=b&cmp=1d"));
  await waitFor(() => expect(line()).toHaveTextContent(/^Loading the matrix of/));
  expect(line()).not.toHaveTextContent(/differ/);
});

// A change from a live range to a fixed one drops the comparison of the
// live moment until the fixed range's moment loads.
it("drops the comparison of the previous range until the new moment loads", async () => {
  const to = Date.parse("2026-09-10T09:00:00Z");
  vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec) =>
    spec.kind === "live" || spec.to === to ? now() : spec.to === to - 86_400_000 ? never() : before(),
  );
  page("/placement?cmp=1d");
  await waitFor(() => expect(line()).toHaveTextContent("1 cell differs"));
  act(() => go("/placement?cmp=1d&from=2026-09-10T08:00:00Z&to=2026-09-10T09:00:00Z"));
  await waitFor(() => expect(line()).toHaveTextContent("Loading the matrix of 9 Sep 09:00 UTC"));
  expect(line()).not.toHaveTextContent(/differ/);
});

const settle = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 50));
  });
const marked = () => screen.queryAllByRole("button", { name: /changed:/ });

// The current matrix of system a must not be diffed against system b's
// moment while b's own matrix loads.
it("diffs nothing while the new system's own matrix loads", async () => {
  vi.mocked(requests.getPlacement).mockImplementation(async (sys: string, spec: RangeSpec) =>
    sys === "a" ? now() : spec.kind === "live" ? never() : before(),
  );
  page("/placement?system=a&cmp=1d");
  await waitFor(() => expect(line()).toHaveTextContent("0 cells differ"));
  act(() => go("/placement?system=b&cmp=1d"));
  await waitFor(() => expect(vi.mocked(requests.getPlacement).mock.calls.some(([s, sp]) => s === "b" && sp.kind === "fixed")).toBe(true));
  await settle();
  expect(line()).toHaveTextContent(/^Loading the matrix of/);
  expect(marked()).toHaveLength(0);
});

// The same for a change of range: the live range's matrix is not diffed
// against the fixed range's moment while the fixed range's matrix loads.
it("diffs nothing while the new range's own matrix loads", async () => {
  const to = Date.parse("2026-09-10T09:00:00Z");
  vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec) =>
    spec.kind === "live" ? now() : spec.to === to ? never() : spec.to === to - 86_400_000 ? before() : now(),
  );
  page("/placement?cmp=1d");
  await waitFor(() => expect(line()).toHaveTextContent("0 cells differ"));
  act(() => go("/placement?cmp=1d&from=2026-09-10T08:00:00Z&to=2026-09-10T09:00:00Z"));
  await waitFor(() => expect(vi.mocked(requests.getPlacement).mock.calls.some(([, sp]) => sp.kind === "fixed" && sp.to === to - 86_400_000)).toBe(true));
  await settle();
  expect(line()).toHaveTextContent(/^Loading the matrix of/);
  expect(marked()).toHaveLength(0);
});

// A failed refetch keeps the old data next to the error; the line reports
// the error and no cell keeps a mark.
it("marks no cell once a refetch of the comparison fails", async () => {
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"], shouldAdvanceTime: true });
  let fail = false;
  vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec) => {
    if (spec.kind === "fixed") return before();
    if (fail) throw new Error("unavailable");
    return now();
  });
  try {
    page("/placement?cmp=1d");
    await waitFor(() => expect(line()).toHaveTextContent("1 cell differs"));
    expect(marked()).toHaveLength(1);
    fail = true;
    await act(async () => {
      vi.advanceTimersByTime(30_000);
    });
    await waitFor(() => expect(line()).toHaveTextContent("Could not load the matrix to compare with: unavailable"));
    expect(screen.getAllByText("orders").length).toBeGreaterThan(0);
    expect(marked()).toHaveLength(0);
  } finally {
    vi.useRealTimers();
  }
});

// With a filter, the matrix on screen is its own call. The marks of
// system b must not land on system a's filtered matrix while b's loads.
it("marks nothing on the previous system's filtered matrix", async () => {
  vi.mocked(requests.getPlacement).mockImplementation(async (sys: string, spec: RangeSpec, filter: string) => {
    if (sys === "a") return now();
    if (spec.kind === "fixed") return before();
    return filter === "all" ? now() : never();
  });
  page("/placement?system=a&cmp=1d&filter=disagree");
  await waitFor(() => expect(line()).toHaveTextContent("0 cells differ"));
  act(() => go("/placement?system=b&cmp=1d&filter=disagree"));
  await waitFor(() => expect(vi.mocked(requests.getPlacement).mock.calls.some(([s, sp]) => s === "b" && sp.kind === "fixed")).toBe(true));
  await settle();
  expect(line()).toHaveTextContent(/^Loading the matrix of/);
  expect(marked()).toHaveLength(0);
});

// The same for a change of range with a filter.
it("marks nothing on the previous range's filtered matrix", async () => {
  const to = Date.parse("2026-09-10T09:00:00Z");
  vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec, filter: string) => {
    if (spec.kind === "live") return now();
    if (spec.to === to) return filter === "all" ? now() : never();
    return spec.to === to - 86_400_000 ? before() : now();
  });
  page("/placement?cmp=1d&filter=disagree");
  await waitFor(() => expect(line()).toHaveTextContent("0 cells differ"));
  act(() => go("/placement?cmp=1d&filter=disagree&from=2026-09-10T08:00:00Z&to=2026-09-10T09:00:00Z"));
  await waitFor(() => expect(vi.mocked(requests.getPlacement).mock.calls.some(([, sp]) => sp.kind === "fixed" && sp.to === to - 86_400_000)).toBe(true));
  await settle();
  expect(line()).toHaveTextContent(/^Loading the matrix of/);
  expect(marked()).toHaveLength(0);
});

// A change of the filter alone keeps the comparison: the count is over
// every asset whatever the filter, and the matrix kept while the new
// filter loads is the same system's and range's.
it("keeps the comparison while a new filter loads", async () => {
  vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec, filter: string) =>
    spec.kind === "fixed" ? before() : filter === "all" ? now() : never(),
  );
  page("/placement?cmp=1d");
  await waitFor(() => expect(line()).toHaveTextContent("1 cell differs"));
  expect(marked()).toHaveLength(1);
  act(() => go("/placement?cmp=1d&filter=disagree"));
  await waitFor(() => expect(vi.mocked(requests.getPlacement).mock.calls.some(([, , f]) => f === "disagree")).toBe(true));
  await settle();
  expect(line()).toHaveTextContent("1 cell differs");
  expect(marked()).toHaveLength(1);
});

// The filtered matrix of the new system fails: the line says so instead
// of loading for ever, and nothing is marked. Also under StrictMode,
// which renders twice and so would show a ref written during render
// going wrong.
it.each([false, true])("reports a failed filtered matrix of the new system (strict %s)", async (strict) => {
  vi.mocked(requests.getPlacement).mockImplementation(async (sys: string, spec: RangeSpec, filter: string) => {
    if (sys === "a" || filter === "all") return spec.kind === "fixed" && sys === "b" ? before() : now();
    throw new Error("filtered failed");
  });
  const ui = (
    <>
      <Placement />
      <Nav />
    </>
  );
  renderApp(strict ? <StrictMode>{ui}</StrictMode> : ui, "/placement?system=a&cmp=1d&filter=disagree");
  await waitFor(() => expect(line()).toHaveTextContent("0 cells differ"));
  act(() => go("/placement?system=b&cmp=1d&filter=disagree"));
  await waitFor(() => expect(line()).toHaveTextContent("Could not load the matrix to compare with: filtered failed"));
  expect(marked()).toHaveLength(0);
});
