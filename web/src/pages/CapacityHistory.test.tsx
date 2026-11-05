// Capacity under 6 hours of history: the header and each store's card say
// how long the growth rate still needs, and link to Data history.
import { screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { prod } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { Capacity } from "./Capacity";

vi.mock("../api/requests", () => ({
  getCapacity: vi.fn(),
  getLensInfo: vi.fn(),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
const m = vi.mocked(requests);

function young() {
  const d = prod();
  return { ...d, servers: d.servers.map((s) => ({ ...s, perDay: undefined, daysToFull: undefined })), streams: d.streams.map((s) => ({ ...s, bytesPerDay: undefined })) };
}

describe("Capacity with a short history", () => {
  it("says how long the growth rate needs, in the header and on each card, with links to Data history", async () => {
    m.getCapacity.mockResolvedValue(young());
    m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true, historyStart: Date.now() - 31 * 60_000 });
    renderApp(<Capacity />, "/capacity?system=example-staging");
    expect(await screen.findByText(/9 streams · 3 stores · no growth rate yet: needs 6 h of history; 5 h 29 min to go, in the collection of/)).toBeInTheDocument();
    const card = within(screen.getByRole("region", { name: "Server nats-0" }));
    expect(card.getByText(/^no growth rate yet: needs 6 h of history; 5 h 29 min to go/)).toBeInTheDocument();
    expect(card.getByRole("link", { name: "Data history" })).toHaveAttribute("href", "/data-history?system=example-staging");
    expect(screen.getAllByRole("link", { name: "Data history" })).toHaveLength(4);
  });
});
