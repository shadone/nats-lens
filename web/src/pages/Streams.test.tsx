import { screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useStreams } from "../api/queries";
import { HistoryContext } from "../components/HistoryNote";
import { renderApp } from "../test/render";
import { Streams } from "./Streams";
vi.mock("../api/queries", () => ({ useStreams: vi.fn() }));
it("explains an unset growth cell and omits the warning when a rate exists", () => {
  const stream = { entity: { kind: "stream", key: "APP/orders", account: "APP", stream: "orders", name: "orders" }, status: "ok", findingCodes: [], leader: "" };
  vi.mocked(useStreams).mockReturnValue({ data: { totals: { bytes: 0, durableConsumers: 0, ephemeralConsumers: 0 }, streams: [stream, { ...stream, entity: { ...stream.entity, key: "APP/jobs", stream: "jobs" }, bytesPerDay: 200 }] }, refetch: () => {}, isPending: false, error: null } as never);
  const end = Date.parse("2026-10-17T14:30:00Z");
  renderApp(<HistoryContext.Provider value={{ start: end - 86_400_000, end, live: true, historyStart: end - 31 * 60_000 }}><Streams /></HistoryContext.Provider>);
  expect(screen.getAllByTitle("needs samples spanning 6 h; entity data may be missing")).toHaveLength(1);
  expect(screen.getByTitle("needs samples spanning 6 h; entity data may be missing")).toHaveTextContent("–");
});
