import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import type { AccountStore } from "../api/capacity";
import { GIB, MIB, prod, store, stream } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { Capacity } from "./Capacity";

vi.mock("../api/requests", () => ({
  getCapacity: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true })),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
const getCapacity = vi.mocked(requests.getCapacity);
afterEach(() => getCapacity.mockReset());

function account(o: Partial<AccountStore> = {}): AccountStore {
  return { account: "AAPPKEY", accountName: "APP", storage: "file", used: 100 * GIB, limit: 500 * GIB, perDay: 5 * GIB, daysToFull: 80, ...o };
}

function row(name: string) {
  const t = within(screen.getByRole("region", { name: "Resources by what bounds them" }));
  return within(t.getByRole("rowheader", { name: new RegExp(`^${name}`) }).closest("tr") as HTMLElement);
}

describe("Capacity memory stores", () => {
  it("shows each server's memory store after the file stores, with its use, growth and time to full", async () => {
    const d = prod();
    d.memoryStores = [
      store("nats-0", { used: 38 * MIB, limit: GIB, perDay: 2 * MIB, daysToFull: 493 }),
      store("nats-1", { used: 38 * MIB, limit: undefined, daysToFull: undefined }),
      store("nats-2", { reported: false, used: undefined, limit: undefined, perDay: undefined, daysToFull: undefined }),
    ];
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    const group = within(await screen.findByRole("region", { name: "Memory stores" }));
    expect(group.getByRole("heading", { name: "Memory stores" })).toBeInTheDocument();
    const c0 = within(group.getByRole("region", { name: "Memory store nats-0" }));
    expect(c0.getByText("JetStream memory store")).toBeInTheDocument();
    expect(c0.getByText("of 1 GiB · 3.7%")).toBeInTheDocument();
    expect(c0.getByText("+2 MiB/day · full in 16 months")).toBeInTheDocument();
    expect(within(group.getByRole("region", { name: "Memory store nats-1" })).getByText("no limit reported")).toBeInTheDocument();
    expect(within(group.getByRole("region", { name: "Memory store nats-2" })).getByText("not reported in this collection")).toBeInTheDocument();
    expect(group.queryByText(/hold(s)? nothing in memory/)).toBeNull();
    // The file store card keeps its own words.
    expect(within(screen.getByRole("region", { name: "Server nats-0" })).getByText("JetStream file store")).toBeInTheDocument();
  });

  it("counts the servers that hold nothing in memory instead of giving them cards", async () => {
    const d = prod();
    d.memoryStores = [store("nats-0", { used: 38 * MIB, limit: GIB })];
    d.memoryStoresEmpty = 21;
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    const group = within(await screen.findByRole("region", { name: "Memory stores" }));
    expect(group.getByText("21 servers hold nothing in memory and have no card.")).toBeInTheDocument();
    expect(group.queryByRole("region", { name: "Memory store nats-1" })).toBeNull();
  });

  it("says a memory store without enough history has no growth", async () => {
    const d = prod();
    d.memoryStores = [store("nats-0", { used: 0, limit: GIB, perDay: undefined, daysToFull: undefined })];
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    const c0 = within(await screen.findByRole("region", { name: "Memory store nats-0" }));
    expect(c0.getByText("growth unavailable")).toBeInTheDocument();
  });

  it("shows no memory group when the answer has no memory stores", async () => {
    getCapacity.mockResolvedValue(prod());
    renderApp(<Capacity />, "/capacity");
    await screen.findByRole("region", { name: "Server nats-0" });
    expect(screen.queryByRole("region", { name: "Memory stores" })).toBeNull();
    expect(screen.queryByRole("region", { name: "Account limits" })).toBeNull();
  });
});

describe("Capacity account limits", () => {
  it("shows a card per account limit with its use, share, growth and time to full", async () => {
    const d = prod();
    d.accountStores = [
      account(),
      account({ storage: "memory", used: 114 * MIB, limit: 16 * GIB, perDay: -MIB, daysToFull: undefined }),
      account({ account: "ANEWKEY", accountName: "", used: undefined, perDay: undefined, daysToFull: undefined }),
      account({ account: "AYOUNGKEY", accountName: "YOUNG", perDay: undefined, daysToFull: undefined }),
    ];
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    const group = within(await screen.findByRole("region", { name: "Account limits" }));
    const file = within(group.getByRole("region", { name: "Account APP file limit" }));
    expect(file.getByText("JetStream file limit")).toBeInTheDocument();
    expect(file.getByText("of 500 GiB · 20%")).toBeInTheDocument();
    expect(file.getByText("+5 GiB/day · full in 3 months")).toBeInTheDocument();
    const mem = within(group.getByRole("region", { name: "Account APP memory limit" }));
    expect(mem.getByText("JetStream memory limit")).toBeInTheDocument();
    expect(mem.getByText("-1 MiB/day · not growing")).toBeInTheDocument();
    expect(within(group.getByRole("region", { name: "Account ANEWKEY file limit" })).getByText("limit 500 GiB; use not reported in this collection")).toBeInTheDocument();
    expect(within(group.getByRole("region", { name: "Account YOUNG file limit" })).getByText("growth unavailable")).toBeInTheDocument();
  });

  it("names the account limit as the bound of a stream without its own, and its next limit by the account's time to full", async () => {
    const d = prod();
    d.accountStores = [account()];
    d.streams = [
      stream("legacy-log", { maxAge: undefined, accountLimit: 500 * GIB, status: "unbounded", next: { limit: "account", state: "reaches", days: 80, discardNew: true } }),
      stream("idle-log", { maxAge: undefined, accountLimit: 500 * GIB, status: "unbounded", next: { limit: "account", state: "notReached", discardNew: true } }),
      stream("full-log", { maxAge: undefined, accountLimit: 500 * GIB, status: "unbounded", next: { limit: "account", state: "refusing", discardNew: true } }),
      stream("orders", { accountLimit: 500 * GIB, next: { limit: "account", state: "reaches", days: 80, discardNew: true } }),
      stream("KV_flags", { kind: "kv", maxAge: undefined, maxMsgsPerSubject: 1, accountLimit: 500 * GIB, status: "awaiting", findingId: "stream-005:APP/KV_flags", next: { limit: "account", state: "reaches", days: 80, discardNew: true } }),
      stream("no-jwt", { maxAge: undefined, status: "unbounded", next: { limit: "none", state: undefined, discardNew: false } }),
    ];
    d.streamsTotal = d.streams.length;
    getCapacity.mockResolvedValue(d);
    renderApp(<Capacity />, "/capacity");
    await screen.findByRole("region", { name: "Resources by what bounds them" });
    const legacy = row("legacy-log");
    expect(legacy.getByText("account limit 500 GiB (shared)")).toBeInTheDocument();
    expect(legacy.getByText("account limit 500 GiB (shared): account full in 3 months")).toBeInTheDocument();
    expect(legacy.getByText("account limit only")).toBeInTheDocument();
    expect(row("idle-log").getByText("account limit 500 GiB (shared): not reached")).toBeInTheDocument();
    const full = row("full-log").getByText("account limit 500 GiB (shared): reached, refuses new messages");
    expect(full).toHaveClass("cap-crit");
    // A stream with its own limit keeps it as its bound; the account is its next limit.
    expect(row("orders").getAllByRole("cell")[0]).toHaveTextContent(/^7 days$/);
    expect(row("orders").getByText("account limit 500 GiB (shared): account full in 3 months")).toBeInTheDocument();
    // stream-005 still awaits a decision on a bucket bounded only per key and by its account.
    const kv = row("flags");
    expect(kv.getByText("keys (no TTL), account limit 500 GiB (shared)")).toBeInTheDocument();
    expect(kv.getByRole("button", { name: "Accept KV_flags as by design…" })).toBeInTheDocument();
    // Without an account limit, nothing changes.
    const plain = row("no-jwt");
    expect(plain.getByText("nothing (keeps all)")).toBeInTheDocument();
    expect(plain.getByText("none: grows until the store is full")).toBeInTheDocument();
    expect(plain.getByText("unbounded")).toBeInTheDocument();
  });
});
