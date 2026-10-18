import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MatrixFilter } from "../api/placementMatrix";
import * as requests from "../api/requests";
import type { RangeSpec } from "../lib/timeRange";
import { masset, mcell, mmatrix } from "../test/fixtures/placement";
import { renderApp } from "../test/render";
import { Placement } from "./Placement";

vi.mock("../api/requests", () => ({ getPlacement: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

const CREATED = Date.parse("2026-09-01T08:00:00Z");
const REBUILT = Date.parse("2026-10-13T08:03:00Z");

/** order-jobs on nats-0 to nats-2 with resize-worker, whose copy on nats-1 was created again, and KV_flags on nats-2 alone. */
function now() {
  const resize = masset("consumer", "order-jobs", "resize-worker", [
    mcell("nats-0", "leader", { created: CREATED, delivered: 900, ackFloor: 880, peers: ["nats-1", "nats-2"] }),
    mcell("nats-1", "in_sync", { created: REBUILT, delivered: 900, ackFloor: 880, lag: 0, differs: ["created"], peers: ["nats-0", "nats-2"] }),
    mcell("nats-2", "in_sync", { created: CREATED, delivered: 900, ackFloor: 880, lag: 0, peers: ["nats-0", "nats-1"] }),
  ], { serversDisagree: true, leaderSince: Date.parse("2026-10-17T09:18:00Z") });
  const index = masset("consumer", "order-jobs", "index-worker", [mcell("nats-0", "leader"), mcell("nats-1", "in_sync", { lag: 0 }), mcell("nats-2", "offline")]);
  const jobs = masset("stream", "order-jobs", "", [mcell("nats-0", "leader", { lastSeq: 7378 }), mcell("nats-1", "in_sync", { lag: 0 }), mcell("nats-2", "offline")], {
    messages: 7378,
    consumers: [resize, index],
  });
  const kv = masset("stream", "KV_flags", "", [mcell("nats-2", "leader", { lastSeq: 4 })], { replicas: 1 });
  return mmatrix([kv, jobs], { counts: { all: 4, disagree: 1, behind: 0, missing: 0, single: 1 } });
}

/** The same a day before: nats-2 was in sync on order-jobs and index-worker. */
function yesterday() {
  const m = now();
  for (const a of [m.streams[1], m.streams[1].consumers[1]]) a.cells[2] = mcell("nats-2", "in_sync", { lag: 0 });
  return m;
}

function onlyDisagree() {
  const m = now();
  const jobs = { ...m.streams[1], matches: false, consumers: [m.streams[1].consumers[0]] };
  return { ...m, streams: [jobs] };
}

let clip: ReturnType<typeof vi.fn>;

/** userEvent, with a clipboard stub set after its own. */
function setupUser() {
  const user = userEvent.setup();
  Object.defineProperty(navigator, "clipboard", { value: { writeText: clip }, configurable: true });
  return user;
}

beforeEach(() => {
  clip = vi.fn(() => Promise.resolve());
  vi.mocked(requests.getPlacement).mockImplementation(async (_sys: string, spec: RangeSpec, f: MatrixFilter) => {
    if (spec.kind === "fixed") return yesterday();
    return f === "disagree" ? onlyDisagree() : now();
  });
});

afterEach(() => vi.mocked(requests.getPlacement).mockReset());

function rowNames() {
  return screen.getAllByRole("rowheader").map((th) => th.querySelector(".pm-n")?.textContent);
}

describe("Placement", () => {
  it("renders a row per stream with its consumers below and a column per server", async () => {
    renderApp(<Placement />, "/placement");
    await waitFor(() => expect(rowNames()).toEqual(["KV_flags", "order-jobs", "resize-worker", "index-worker"]));
    const heads = screen.getAllByRole("columnheader").map((h) => h.textContent);
    expect(heads[0]).toBe("Asset");
    expect(heads[1]).toMatch(/^nats-0leads 2 · 256 MiB · up 1d 23h$/);
    expect(heads[2]).toMatch(/meta leader/);
    expect(screen.getByRole("button", { name: /resize-worker of order-jobs on nats-1: in sync, differs: creation time, created 10-13 08:03/ })).toHaveClass("t-disagree");
    expect(screen.getByRole("button", { name: /^order-jobs on nats-0: leader, last seq 7,378$/ })).toHaveClass("t-ok");
    // KV_flags holds no replica on nats-0: a cell that is not a button.
    const kvRow = screen.getAllByRole("row")[1];
    expect(within(kvRow).getAllByText("not placed")).toHaveLength(2);
    expect(document.querySelector(".page-h")).toHaveTextContent("in the collection of 14:29:56 UTC.");
  });

  it("filters with counts kept in the URL", async () => {
    const user = setupUser();
    renderApp(<Placement />, "/placement");
    const chip = await screen.findByRole("button", { name: /^Servers disagree\s+1$/ });
    expect(screen.getByRole("button", { name: /^All\s+4$/ })).toHaveAttribute("aria-pressed", "true");
    await user.click(chip);
    expect(screen.getByTestId("location").textContent).toBe("/placement?filter=disagree");
    await waitFor(() => expect(rowNames()).toEqual(["order-jobs", "resize-worker"]));
    expect(requests.getPlacement).toHaveBeenLastCalledWith("", expect.anything(), "disagree", expect.anything());
    // The stream is listed for its consumer only.
    expect(screen.getAllByRole("row")[1]).toHaveClass("ctx");
    expect(chip).toHaveAttribute("aria-pressed", "true");
  });

  it("compares with this time yesterday and marks the cells that changed", async () => {
    const user = setupUser();
    renderApp(<Placement />, "/placement");
    await screen.findByRole("button", { name: /^order-jobs on nats-2: offline/ });
    await user.selectOptions(screen.getByRole("combobox", { name: "Compare with" }), "1d");
    expect(screen.getByTestId("location").textContent).toBe("/placement?cmp=1d");
    await waitFor(() => expect(screen.getByRole("status", { name: "" })).toHaveTextContent(/: 2 cells differ\./));
    const fixedCall = vi.mocked(requests.getPlacement).mock.calls.find((c) => c[1].kind === "fixed")!;
    const spec = fixedCall[1] as Extract<RangeSpec, { kind: "fixed" }>;
    expect(spec.to - spec.from).toBe(3_600_000);
    expect(Math.abs(Date.now() - 86_400_000 - spec.to)).toBeLessThan(120_000);
    expect(fixedCall[2]).toBe("all");
    expect(screen.getByRole("button", { name: /^order-jobs on nats-2: offline, changed: in sync at/ })).toHaveClass("changed");
    expect(screen.getByRole("button", { name: /^index-worker of order-jobs on nats-2: offline, changed: in sync at/ })).toHaveClass("changed");
    expect(screen.getByRole("button", { name: /^resize-worker of order-jobs on nats-2/ })).not.toHaveClass("changed");
  });

  it("offers a custom time to compare with, in UTC", async () => {
    const user = setupUser();
    renderApp(<Placement />, "/placement?cmp=2026-10-16T09:00:00Z");
    const input = await screen.findByLabelText("Time to compare with, UTC");
    expect(input).toHaveValue("2026-10-16T09:00");
    expect(screen.getByRole("combobox", { name: "Compare with" })).toHaveValue("custom");
    await waitFor(() =>
      expect(vi.mocked(requests.getPlacement).mock.calls.some((c) => c[1].kind === "fixed" && c[1].to === Date.parse("2026-10-16T09:00:00Z"))).toBe(true),
    );
    await user.selectOptions(screen.getByRole("combobox", { name: "Compare with" }), "");
    expect(screen.getByTestId("location").textContent).toBe("/placement");
  });

  it("opens the side panel on a cell with each server's creation time and the command to copy", async () => {
    const user = setupUser();
    renderApp(<Placement />, "/placement");
    await user.click(await screen.findByRole("button", { name: /^resize-worker of order-jobs on nats-1/ }));
    expect(screen.getByTestId("location").textContent).toBe("/placement?asset=APP%2Forder-jobs%2Fresize-worker&server=nats-1");
    const panel = screen.getByRole("complementary", { name: "resize-worker" });
    expect(within(panel).getByText(/seen from/)).toHaveTextContent("on order-jobs · seen from nats-1");
    expect(within(panel).getByText("2026-10-13 08:03:00 UTC")).toHaveClass("warn");
    expect(within(panel).getByText("nats-0 says").nextElementSibling).toHaveTextContent("2026-09-01 08:00:00 UTC · leader");
    expect(within(panel).getByText("Positions").nextElementSibling).toHaveTextContent("equal on all 3 servers that report one");
    expect(within(panel).getByText("Peers").nextElementSibling).toHaveTextContent("nats-0 nats-1 nats-2 (all agree)");
    expect(within(panel).getByText(/another creation time for the consumer/)).toBeInTheDocument();
    const cmd = "nats consumer info --json -- order-jobs resize-worker | jq .created";
    expect(within(panel).getByText(cmd)).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Copy" }));
    expect(clip).toHaveBeenCalledWith(cmd);
    expect(await within(panel).findByText("Copied.")).toBeInTheDocument();
    expect(within(panel).getByRole("link", { name: "Events for this consumer" })).toHaveAttribute("href", "/events?consumer=APP%2Forder-jobs%2Fresize-worker");
    // Step down copies the command; nothing runs it.
    await user.click(within(panel).getByRole("button", { name: "Copy command" }));
    expect(clip).toHaveBeenLastCalledWith("nats consumer cluster step-down -- order-jobs resize-worker");
    expect(screen.getByRole("button", { name: /^resize-worker of order-jobs on nats-1/ })).toHaveAttribute("aria-pressed", "true");
    await user.click(within(panel).getByRole("button", { name: "Close the selected cell" }));
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    expect(screen.getByTestId("location").textContent).toBe("/placement");
  });

  it("says so when the browser refuses to copy", async () => {
    clip.mockRejectedValue(new Error("denied"));
    const user = setupUser();
    renderApp(<Placement />, "/placement?asset=APP%2Forder-jobs&server=nats-2");
    const panel = await screen.findByRole("complementary", { name: "order-jobs" });
    expect(within(panel).getByText(/lists this replica as offline/)).toBeInTheDocument();
    await user.click(within(panel).getByRole("button", { name: "Copy" }));
    expect(await within(panel).findByText(/did not allow copying/)).toBeInTheDocument();
  });

  it("says which account and system the copied commands need a context for", async () => {
    renderApp(<Placement />, "/placement?system=example-global&asset=APP%2Forder-jobs&server=nats-0");
    const panel = await screen.findByRole("complementary", { name: "order-jobs" });
    expect(within(panel).getAllByText("Run with a nats context for account APP on example-global.")).toHaveLength(2);
  });

  it("counts the changes over every asset, not only the listed ones", async () => {
    renderApp(<Placement />, "/placement?filter=disagree&cmp=1d");
    await waitFor(() => expect(screen.getByRole("status", { name: "" })).toHaveTextContent(/: 2 cells differ\./));
  });

  it("shows no earlier moment's matrix while a new one loads", async () => {
    const user = userEvent.setup();
    renderApp(<Placement />, "/placement?cmp=1d");
    await waitFor(() => expect(screen.getByRole("status", { name: "" })).toHaveTextContent(/cells differ/));
    vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec) => (spec.kind === "fixed" ? new Promise(() => {}) : now()));
    await user.selectOptions(screen.getByRole("combobox", { name: "Compare with" }), "custom");
    fireEvent.change(screen.getByLabelText("Time to compare with, UTC"), { target: { value: "2026-10-15T09:00" } });
    await waitFor(() => expect(screen.getByRole("status", { name: "" })).toHaveTextContent("Loading the matrix of 15 Oct 09:00 UTC"));
  });

  it("says why the comparison failed when the unfiltered matrix fails", async () => {
    vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec, f: MatrixFilter) => {
      if (spec.kind === "live" && f === "all") throw new Error("unfiltered failed");
      return spec.kind === "fixed" ? yesterday() : onlyDisagree();
    });
    renderApp(<Placement />, "/placement?filter=disagree&cmp=1d");
    await waitFor(() => expect(screen.getByRole("status", { name: "" })).toHaveTextContent("Could not load the matrix to compare with: unfiltered failed"));
  });

  it("names the default system in the context line when the address names none", async () => {
    vi.mocked(requests.getShellStatus).mockResolvedValue({ defaultSystem: "example-prod" } as never);
    renderApp(<Placement />, "/placement?asset=APP%2Forder-jobs&server=nats-0");
    const panel = await screen.findByRole("complementary", { name: "order-jobs" });
    await waitFor(() => expect(within(panel).getAllByText("Run with a nats context for account APP on example-prod.")).toHaveLength(2));
  });

  it("says so when the range holds no collection", async () => {
    vi.mocked(requests.getPlacement).mockResolvedValue(mmatrix([], { epochAt: undefined, counts: { all: 0, disagree: 0, behind: 0, missing: 0, single: 0 } }));
    renderApp(<Placement />, "/placement");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
  });

  it("names a cell without repeating its state, and says both for two servers", async () => {
    vi.mocked(requests.getPlacement).mockResolvedValue(
      mmatrix([masset("stream", "orders", "", [mcell("nats-0", "leader", { lastSeq: 3 }), mcell("nats-1", "offline"), mcell("nats-2", "in_sync", { lastSeq: 3 })])]),
    );
    renderApp(<Placement />, "/placement?asset=APP%2Forders&server=nats-0");
    expect(await screen.findByRole("button", { name: "orders on nats-1: offline" })).toBeInTheDocument();
    const panel = screen.getByRole("complementary", { name: "orders" });
    expect(within(panel).getByText("Positions").nextElementSibling).toHaveTextContent("equal on both servers that report one");
  });

  it("shows no command for a name with a line break", async () => {
    const odd = masset("stream", "orders\nrm -rf ~", "", [mcell("nats-0", "leader")]);
    vi.mocked(requests.getPlacement).mockResolvedValue(mmatrix([odd]));
    renderApp(<Placement />, `/placement?asset=${encodeURIComponent(odd.entity.key)}&server=nats-0`);
    const panel = await screen.findByRole("complementary");
    expect(within(panel).getAllByText(/No command: the name holds a control character/)).toHaveLength(2);
    expect(within(panel).queryByRole("button", { name: "Copy" })).toBeNull();
  });

  it("shows only the servers that hold a listed replica, and all on request", async () => {
    const user = setupUser();
    vi.mocked(requests.getPlacement).mockResolvedValue(mmatrix([masset("stream", "KV_flags", "", [mcell("nats-2", "leader")], { replicas: 1 })]));
    renderApp(<Placement />, "/placement");
    await screen.findByText(/The 1 servers that hold these replicas; 2 hold none\./);
    expect(screen.getAllByRole("columnheader")).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "Show all 3 servers" }));
    expect(screen.getByTestId("location").textContent).toBe("/placement?servers=all");
    expect(screen.getAllByRole("columnheader")).toHaveLength(4);
  });

  it("has loading, error and empty states", async () => {
    vi.mocked(requests.getPlacement).mockImplementation(() => new Promise(() => {}));
    const { unmount } = renderApp(<Placement />, "/placement");
    expect(screen.getByRole("status")).toHaveTextContent("Loading the placement");
    unmount();
    vi.mocked(requests.getPlacement).mockRejectedValue(new Error("boom"));
    const second = renderApp(<Placement />, "/placement");
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load the placement.");
    second.unmount();
    vi.mocked(requests.getPlacement).mockResolvedValue(mmatrix([], { counts: { all: 0, disagree: 0, behind: 0, missing: 0, single: 0 } }));
    const third = renderApp(<Placement />, "/placement");
    expect(await screen.findByText("No streams in the newest collection of this range.")).toBeInTheDocument();
    third.unmount();
    vi.mocked(requests.getPlacement).mockResolvedValue(mmatrix([], { counts: { all: 4, disagree: 0, behind: 0, missing: 0, single: 0 } }));
    renderApp(<Placement />, "/placement?filter=behind");
    expect(await screen.findByText("No stream or consumer matches “Replica behind” in this collection.")).toBeInTheDocument();
  });

  it("keeps the matrix in its own scroller and the panel after it, so a phone stacks them", async () => {
    renderApp(<Placement />, "/placement?asset=APP%2Forder-jobs&server=nats-0");
    const panel = await screen.findByRole("complementary", { name: "order-jobs" });
    const table = screen.getByRole("table");
    expect(table.parentElement).toHaveClass("table-wrap");
    expect(table.compareDocumentPosition(panel) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // The first column stays put while the servers scroll.
    expect(screen.getAllByRole("rowheader")[0]).toHaveClass("pm-asset");
  });
});
