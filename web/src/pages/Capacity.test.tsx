import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { acceptance, empty, prod, store } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { Capacity } from "./Capacity";

vi.mock("../api/requests", () => ({
  getCapacity: vi.fn(),
  muteFinding: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true })),
  // Settles, so the refetch after an acceptance ends and the dialog closes.
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
const getCapacity = vi.mocked(requests.getCapacity);
const muteFinding = vi.mocked(requests.muteFinding);

beforeEach(() => localStorage.clear());
afterEach(() => {
  getCapacity.mockReset();
  muteFinding.mockReset();
});

function table() {
  return within(screen.getByRole("region", { name: "Streams by what bounds them" }));
}

function row(name: string) {
  return table().getByRole("rowheader", { name: new RegExp(`^${name}`) }).closest("tr") as HTMLElement;
}

function cellTexts(name: string) {
  return within(row(name))
    .getAllByRole("cell")
    .map((c) => c.textContent);
}

describe("Capacity", () => {
  it("says what the page shows and names the collection, for the system of the page", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity?system=example-prod");
    expect(await screen.findByText(/^What each stream keeps, what bounds it, and when the disks fill\. 9 streams · 3 stores · growth from 2026-10-20 12:00 UTC, in the collection of/)).toBeInTheDocument();
    expect(getCapacity.mock.calls[0][0]).toBe("example-prod");
  });

  it("says only that the range holds no collection", async () => {
    getCapacity.mockResolvedValue(empty());
    renderApp(<Capacity />, "/capacity");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Streams by what bounds them" })).toBeNull();
  });

  it("shows each server's store with its use, growth and time to full", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity");
    const card = within(await screen.findByRole("region", { name: "Server nats-0" }));
    expect(card.getByText("JetStream file store")).toBeInTheDocument();
    expect(card.getByText("of 50 GiB · 3.9%")).toBeInTheDocument();
    expect(card.getByText("+17 MiB/day · full in more than 5 years at this rate")).toBeInTheDocument();
  });

  it("says a card was not reported, has no limit, or fills soon by its finding, in words", async () => {
    const d = prod();
    d.servers = [
      store("nats-0", { reported: false, used: undefined, limit: undefined, perDay: undefined, daysToFull: undefined }),
      store("nats-1", { limit: undefined, daysToFull: undefined }),
      store("nats-2", { daysToFull: 26, findingId: "server-014:nats-2", findingCode: "server-014", findingSeverity: "warning", findingDays: 13.8 }),
    ];
    d.serversWithoutJetstream = 2;
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    expect(within(await screen.findByRole("region", { name: "Server nats-0" })).getByText("not reported in this collection")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Server nats-1" })).getByText("no limit reported")).toBeInTheDocument();
    const c2 = within(screen.getByRole("region", { name: "Server nats-2" }));
    expect(c2.getByText("Warning")).toBeInTheDocument();
    expect(c2.getByRole("link", { name: "store full soon: 14 days by server-014" })).toHaveAttribute("href", "/findings/server-014%3Anats-2");
    expect(screen.getByText("2 servers without JetStream have no card.")).toBeInTheDocument();
  });

  it("groups the cards by cluster when there is more than one", async () => {
    const d = prod();
    d.servers = [store("nats-ap-0", { cluster: "ap" }), store("nats-eu-0", { cluster: "eu" })];
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    expect(await screen.findByRole("heading", { name: "Cluster ap" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Cluster eu" })).toBeInTheDocument();
  });

  it("lists the streams with the board's columns", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity");
    await screen.findByRole("region", { name: "Streams by what bounds them" });
    expect(table().getAllByRole("columnheader").map((h) => h.textContent)).toEqual(["Stream", "Bounded by", "Data held", "Size × R", "/day", "Next limit", "Status"]);
    expect(table().getByRole("link", { name: "order-archive" })).toHaveAttribute("href", "/streams/APP/order-archive");
    expect(row("order-archive")).toHaveTextContent("R3 · limits · 1,000 msgs");
    expect(cellTexts("order-archive").slice(0, 5)).toEqual(["nothing (keeps all)", "since 2025-12-25", "12 GiB", "+3 MiB", "none: grows until the store is full"]);
  });

  it("shows an en dash for a size and a growth that are not known, never 0", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity");
    await screen.findByRole("region", { name: "Streams by what bounds them" });
    const c = cellTexts("staging-only");
    expect(c[1]).toBe("–");
    expect(c[2]).toBe("– not observed");
    expect(c[3]).toBe("– not observed");
  });

  it("words each status", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity");
    await screen.findByRole("region", { name: "Streams by what bounds them" });
    const status = (n: string) => cellTexts(n)[5];
    expect(within(row("audit-archive")).getByRole("link", { name: "full in 13 days" })).toHaveAttribute("href", "/findings/stream-006%3AAPP%2Faudit-archive");
    expect(status("audit-archive")).toContain("muted until 2026-10-28 10:00 UTC");
    expect(status("KV_customer-config")).toBe("BY DESIGNAccepted by ops@example.com: Customer records are kept for the account's life. Review on 2027-04-07.stream-005");
    expect(status("KV_session-cache")).toBe("BY DESIGNbounded by its writers' TTLs");
    expect(within(row("order-history")).getByRole("link", { name: "muted until 2026-11-17 22:00 UTC" })).toBeInTheDocument();
    expect(within(row("order-archive")).getByRole("button", { name: "Accept order-archive as by design…" })).toHaveTextContent("Accept as by design…");
    expect(status("legacy-log")).toBe("unbounded");
    expect(status("order-callbacks")).toBe("● bounded");
    expect(cellTexts("price-changes")[4]).toBe("96,500 msgs: reached, refuses new messages");
  });

  it("offers no button for a stream already muted, accepted or declared", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity");
    await screen.findByRole("region", { name: "Streams by what bounds them" });
    expect(table().getAllByRole("button")).toHaveLength(1);
  });

  it("shows the row as accepted after saving, without waiting for a new answer", async () => {
    getCapacity.mockResolvedValueOnce(prod());
    getCapacity.mockImplementation(() => new Promise(() => {}));
    const m = acceptance({ reason: "Order history is kept for audits", mutedBy: "alice", expiresAt: Date.parse("2027-01-21T12:00:00Z") });
    muteFinding.mockResolvedValue(m);
    renderApp(<Capacity />, "/capacity");
    await userEvent.click(await screen.findByRole("button", { name: "Accept order-archive as by design…" }));
    const dlg = within(screen.getByRole("dialog"));
    expect(dlg.getByText(/^Accepts/)).toHaveTextContent("Accepts stream-005 Stream without any limit for order-archive");
    await userEvent.type(dlg.getByLabelText("Reason"), "Order history is kept for audits");
    await userEvent.type(dlg.getByLabelText("Your name"), "alice");
    await userEvent.click(dlg.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(cellTexts("order-archive")[5]).toBe("BY DESIGNAccepted by alice: Order history is kept for audits. Review on 2027-01-21.stream-005"));
    expect(muteFinding.mock.calls[0][1]).toMatchObject({ checkCode: "stream-005", accept: true, entity: { kind: "stream", key: "APP/order-archive" } });
    expect(within(row("order-archive")).queryByRole("button")).toBeNull();
  });

  it("shows the next answer after the save, whatever it says", async () => {
    getCapacity.mockResolvedValue(prod());
    muteFinding.mockResolvedValue(acceptance({ reason: "Kept", mutedBy: "alice" }));
    renderApp(<Capacity />, "/capacity");
    await userEvent.click(await screen.findByRole("button", { name: "Accept order-archive as by design…" }));
    const dlg = within(screen.getByRole("dialog"));
    await userEvent.type(dlg.getByLabelText("Reason"), "Kept");
    await userEvent.type(dlg.getByLabelText("Your name"), "alice");
    await userEvent.click(dlg.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(getCapacity).toHaveBeenCalledTimes(2));
    // The answer fetched after the save still says awaiting (the acceptance ended elsewhere): it is shown.
    expect(await within(row("order-archive")).findByRole("button", { name: "Accept order-archive as by design…" })).toBeInTheDocument();
  });

  it("keeps the saved acceptance on a fixed range, whose answers apply the mutes of its end", async () => {
    getCapacity.mockResolvedValue(prod());
    muteFinding.mockResolvedValue(acceptance({ reason: "Kept", mutedBy: "alice" }));
    renderApp(<Capacity />, "/capacity?from=2026-10-20T00:00:00Z&to=2026-10-21T00:00:00Z");
    await userEvent.click(await screen.findByRole("button", { name: "Accept order-archive as by design…" }));
    const dlg = within(screen.getByRole("dialog"));
    await userEvent.type(dlg.getByLabelText("Reason"), "Kept");
    await userEvent.type(dlg.getByLabelText("Your name"), "alice");
    await userEvent.click(dlg.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(getCapacity).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await new Promise((r) => setTimeout(r, 50));
    expect(cellTexts("order-archive")[5]).toMatch(/^BY DESIGNAccepted by alice: Kept\./);
  });

  it("moves focus to the accepted row's stream, as its button is gone", async () => {
    getCapacity.mockResolvedValueOnce(prod());
    getCapacity.mockImplementation(() => new Promise(() => {}));
    muteFinding.mockResolvedValue(acceptance({ reason: "Kept for audits", mutedBy: "alice" }));
    renderApp(<Capacity />, "/capacity");
    await userEvent.click(await screen.findByRole("button", { name: "Accept order-archive as by design…" }));
    const dlg = within(screen.getByRole("dialog"));
    await userEvent.type(dlg.getByLabelText("Reason"), "Kept for audits");
    await userEvent.type(dlg.getByLabelText("Your name"), "alice");
    await userEvent.click(dlg.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(table().getByRole("link", { name: "order-archive" })));
  });

  it("says how many streams are listed when the server cut the list", async () => {
    const d = prod();
    d.streamsTotal = 812;
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    expect(await screen.findByText("The first 9 of 812 streams are listed.")).toBeInTheDocument();
  });

  it("explains the bar and acceptance under the table", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity");
    expect(await screen.findByText(/^Data held spans from the oldest retained message to the collection on a one-year scale/)).toHaveTextContent(
      /until that date, or until nats-lens sees the stream's growth over a week change by more than half\.$/,
    );
  });
});

import { HistoryContext } from "../components/HistoryNote";
it("links the header's missing growth to the same countdown as the cards", async () => {
  const d = prod();
  d.servers.forEach((s) => { s.perDay = undefined; });
  d.streams.forEach((s) => { s.bytesPerDay = undefined; });
  getCapacity.mockResolvedValue(d);
  const end = Date.parse("2026-10-17T14:30:00Z");
  renderApp(<HistoryContext.Provider value={{ start: end - 86_400_000, end, live: true, historyStart: end - 31 * 60_000 }}><Capacity /></HistoryContext.Provider>);
  expect((await screen.findAllByRole("link", { name: /needs samples spanning 6 h; entity data may be missing/ })).length).toBeGreaterThan(d.servers.length);
});
