import { act, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { renderApp } from "../test/render";
import { PageHistoryProvider, ServerChartHistory } from "./HistoryNote";
import { SeriesChart } from "../charts/SeriesChart";
import { TrendChart } from "./TrendChart";
import * as api from "../api/requests";
import { NOW, staging } from "../test/fixtures/history";

vi.mock("../api/requests", () => ({ getDataHistory: vi.fn(), getLensInfo: vi.fn() }));
const read = vi.mocked(api.getDataHistory), H = 3_600_000;
beforeEach(() => { read.mockReset(); vi.mocked(api.getLensInfo).mockImplementation(() => new Promise(() => {})); });

it("announces the charts' history read once per page: loading, then unavailable", async () => {
  const fails: ((e: Error) => void)[] = [];
  read.mockImplementation(() => new Promise((_yes, no) => { fails.push(no); }));
  const r = renderApp(<PageHistoryProvider>
    <SeriesChart title="CPU" start={NOW - H} end={NOW} />
    <SeriesChart title="Memory" start={NOW - H} end={NOW} />
    <TrendChart title="Messages" start={NOW - 7 * 24 * H} end={NOW} layers={[]} />
  </PageHistoryProvider>, "/?system=example-prod&range=24h");
  expect(await screen.findAllByText("History gaps loading")).toHaveLength(3);
  await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("History gaps loading"));
  expect(screen.getAllByRole("status")).toHaveLength(1);
  await act(async () => fails.forEach((no) => no(new Error("history unavailable"))));
  // The query cache notifies its subscribers in a later task.
  await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("History gaps unavailable"));
  expect(screen.getAllByRole("status")).toHaveLength(1);
  // beforeEach returns the mock, and Vitest calls it after the test as a teardown.
  read.mockReset();
  r.unmount();
});

it("leaves a server's charts to the server's own announcement", async () => {
  read.mockImplementation(() => new Promise(() => {}));
  const r = renderApp(<PageHistoryProvider><ServerChartHistory server="nats-0">
    <SeriesChart title="CPU" start={NOW - H} end={NOW} />
  </ServerChartHistory></PageHistoryProvider>, "/servers/nats-0?system=example-prod&range=1h");
  expect(await screen.findByText("History gaps loading", { selector: "p.dh-note" })).toBeInTheDocument();
  // The page-level region stays in the page, empty, so it never says it twice.
  const said = screen.getAllByRole("status").filter((s) => s.textContent);
  expect(said).toHaveLength(1);
  expect(said[0]).toHaveClass("dh-note");
  read.mockReset();
  r.unmount();
});

it("says loading only after the read has been pending for 300 ms, and unavailable at once", async () => {
  vi.useFakeTimers();
  try {
    const fails: ((e: Error) => void)[] = [];
    read.mockImplementation(() => new Promise((_yes, no) => { fails.push(no); }));
    const r = renderApp(<PageHistoryProvider><SeriesChart title="CPU" start={NOW - H} end={NOW} /></PageHistoryProvider>, "/?system=example-prod&range=24h");
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });
    expect(screen.getByRole("status").textContent).toBe("");
    await act(async () => { await vi.advanceTimersByTimeAsync(100); });
    expect(screen.getByRole("status")).toHaveTextContent("History gaps loading");
    await act(async () => { fails.forEach((no) => no(new Error("history unavailable"))); await vi.advanceTimersByTimeAsync(5); });
    expect(screen.getByRole("status")).toHaveTextContent("History gaps unavailable");
    read.mockReset();
    r.unmount();
  } finally {
    vi.useRealTimers();
  }
});

it("does not flash loading when the read settles within 300 ms", async () => {
  vi.useFakeTimers();
  try {
    const answers: ((v: ReturnType<typeof staging>) => void)[] = [];
    read.mockImplementation(() => new Promise((yes) => { answers.push(yes); }));
    const r = renderApp(<PageHistoryProvider><SeriesChart title="CPU" start={NOW - H} end={NOW} /></PageHistoryProvider>, "/?system=example-prod&range=24h");
    await act(async () => { await vi.advanceTimersByTimeAsync(100); });
    expect(screen.getByRole("status").textContent).toBe("");
    await act(async () => { answers.forEach((yes) => yes({ ...staging(), rangeStart: NOW - H, rangeEnd: NOW, gaps: [] })); await vi.advanceTimersByTimeAsync(400); });
    expect(screen.getByRole("status").textContent).toBe("");
    read.mockReset();
    r.unmount();
  } finally {
    vi.useRealTimers();
  }
});
