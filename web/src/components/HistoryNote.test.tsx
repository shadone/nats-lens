import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LensInfo } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { HistoryNote, showsHistoryNote } from "./HistoryNote";

vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  getLensInfo: vi.fn(),
}));
const m = vi.mocked(requests);

function info(historyStart: number | undefined): LensInfo {
  return { identityMode: "request", version: "test", cluster: "example-staging", collectIntervalMs: 30_000, retentionMs: 14 * 86_400_000, live: true, demo: true, historyStart };
}

beforeEach(() => m.getLensInfo.mockReset());

describe("HistoryNote", () => {
  it("says how much history a 6-hour range holds, with a link to Data history that keeps the system and range", async () => {
    m.getLensInfo.mockResolvedValue(info(Date.now() - 31 * 60_000));
    renderApp(<HistoryNote />, "/servers?system=example-staging&range=6h");
    const note = await screen.findByRole("note");
    expect(note).toHaveTextContent(/^31 min of history: you asked for the last 6 h; the history begins at \d\d:\d\d UTC\. Data history$/);
    expect(screen.getByRole("link", { name: "Data history" })).toHaveAttribute("href", "/data-history?range=6h&system=example-staging");
  });

  it("is absent when the history covers the range", async () => {
    m.getLensInfo.mockResolvedValue(info(Date.now() - 2 * 86_400_000));
    renderApp(<HistoryNote />, "/servers?system=example-staging");
    await waitFor(() => expect(m.getLensInfo).toHaveBeenCalled());
    expect(screen.queryByRole("note")).toBeNull();
  });

  it("is not shown on Fleet, which reads no system, nor on Data history, which says it itself", () => {
    expect(showsHistoryNote("/fleet")).toBe(false);
    expect(showsHistoryNote("/data-history")).toBe(false);
    expect(showsHistoryNote("/")).toBe(true);
    expect(showsHistoryNote("/servers/nats-1")).toBe(true);
  });
});
