import { create } from "@bufbuild/protobuf";
import { timestampFromMs, timestampMs } from "@bufbuild/protobuf/wkt";
import { act, screen, within } from "@testing-library/react";
import { Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import type { ConsumerServerView, Entity, Finding, Timeline } from "../api/model";
import * as requests from "../api/requests";
import { EventSchema, EventSource, ListEventsResponseSchema } from "../gen/natslens/v1/events_pb";
import { renderApp } from "../test/render";
import { FindingDetail, atHistoryStart } from "./FindingDetail";

vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  getFinding: vi.fn(),
  getTimeline: vi.fn(),
  getLensInfo: vi.fn(),
  getStream: vi.fn(),
  getConsumer: vi.fn(),
  listFindings: vi.fn(),
  muteFinding: vi.fn(),
  unmuteFinding: vi.fn(),
}));
vi.mock("../api/client", () => ({ lens: { listEvents: vi.fn() } }));
const m = vi.mocked(requests);
const listEvents = vi.mocked(lens.listEvents);

const emptyTimeline: Timeline = { start: 0, bucketMs: 900_000, bucketCount: 0, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false };

const blank = { account: "", stream: "", consumer: "", server: "", name: "" };
const streamEntity: Entity = { ...blank, kind: "stream", key: "APP/order-jobs", account: "APP", stream: "order-jobs", name: "order-jobs" };
const consumerEntity: Entity = { ...blank, kind: "consumer", key: "APP/order-jobs/resize-worker", account: "APP", stream: "order-jobs", consumer: "resize-worker", name: "resize-worker" };
const clientEntity: Entity = { ...blank, kind: "client", key: "catalog-api", name: "catalog-api" };

const base: Finding = {
  id: "stream-020:A/s",
  checkCode: "stream-020",
  checkName: "Backlog growing",
  severity: "warning",
  state: "firing",
  entity: streamEntity,
  since: Date.now() - 3_600_000,
  detail: "",
  remediation: "",
  relatedConfig: [],
  relatedConfigSource: "",
  transitionsLastHour: 1,
};

function show() {
  renderApp(
    <Routes>
      <Route path="/findings/:id" element={<FindingDetail />} />
    </Routes>,
    "/findings/stream-020%3AA%2Fs",
  );
}

beforeEach(() => {
  vi.resetAllMocks();
  m.getTimeline.mockResolvedValue(emptyTimeline);
  m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "", cluster: "", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: false });
  m.getStream.mockResolvedValue({ config: [], servers: [], disagreements: [], consumers: [], findings: [] });
  m.getConsumer.mockResolvedValue({ config: [], servers: [], disagreements: [], findings: [] });
  m.listFindings.mockResolvedValue({ items: [], nextPageToken: "", totalCount: 1 });
  listEvents.mockResolvedValue(create(ListEventsResponseSchema, {}));
});

describe("FindingDetail", () => {
  afterEach(() => vi.useRealTimers());

  it("refreshes in live mode and shows a new state", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    m.getFinding.mockResolvedValueOnce({ finding: base, transitions: [] });
    m.getFinding.mockResolvedValue({ finding: { ...base, state: "resolved", resolvedAt: Date.now() }, transitions: [] });
    show();
    expect(await screen.findByText(/first seen by nats-lens/i)).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(31_000);
    });
    expect(await screen.findByText("RESOLVED")).toBeInTheDocument();
    expect(m.getFinding).toHaveBeenCalledTimes(2);
  });

  it("heads the page with the finding's own words", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, detail: "order-jobs grew 40% in 1h" }, transitions: [] });
    show();
    expect(await screen.findByRole("heading", { level: 1, name: "order-jobs grew 40% in 1h" })).toBeInTheDocument();
    expect(screen.getAllByText("Backlog growing").length).toBeGreaterThan(0);
  });
});

describe("FindingDetail times", () => {
  it("shows when the condition began apart from when nats-lens first saw it", async () => {
    const began = Date.UTC(2026, 9, 15, 9, 0);
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: began }, transitions: [] });
    show();
    const when = await screen.findByText(/Condition since/);
    expect(when).toHaveTextContent(/Condition since 2026-10-15 09:00 UTC · first seen by nats-lens \d{4}-\d\d-\d\d \d\d:\d\d UTC/);
  });

  it("shows both times, and that the condition was already true, when it began before the history", async () => {
    const h = Date.UTC(2026, 9, 17, 8, 19);
    m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "", cluster: "", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: false, historyStart: h });
    m.getFinding.mockResolvedValue({ finding: { ...base, since: h + 20_000, conditionSince: Date.UTC(2026, 9, 13, 8, 3) }, transitions: [] });
    show();
    const when = await screen.findByText(/Condition since/);
    expect(when).toHaveTextContent("Condition since 2026-10-13 08:03 UTC · first seen by nats-lens 2026-10-17 08:19 UTC, already true when nats-lens started");
    // The window around the start lies before the history.
    expect(await screen.findByText(/nats-lens has no history before 2026-10-17 08:19 UTC/)).toBeInTheDocument();
    expect(listEvents).not.toHaveBeenCalled();
  });
});

describe("the history start note", () => {
  const h = Date.UTC(2026, 9, 17, 9, 0);
  it("marks a firing finding first seen at the start of the history", () => {
    expect(atHistoryStart({ ...base, since: h + 20_000 }, h)).toBe(true);
  });
  it("marks a condition that began before the history", () => {
    expect(atHistoryStart({ ...base, since: h + 3_600_000, conditionSince: h - 1 }, h)).toBe(true);
  });
  it("leaves out resolved findings, findings seen later, and an unknown start", () => {
    expect(atHistoryStart({ ...base, since: h + 20_000, state: "resolved" }, h)).toBe(false);
    expect(atHistoryStart({ ...base, since: h + 3_600_000 }, h)).toBe(false);
    expect(atHistoryStart({ ...base, since: h - 1 }, h)).toBe(false);
    expect(atHistoryStart({ ...base, since: h }, undefined)).toBe(false);
  });
  it("shows the note on the page", async () => {
    m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "", cluster: "", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: false, historyStart: h });
    m.getFinding.mockResolvedValue({ finding: { ...base, since: h + 20_000 }, transitions: [] });
    show();
    expect(await screen.findByText(/already true when nats-lens started/)).toBeInTheDocument();
  });
});

describe("server copies", () => {
  const day = 86_400_000;
  const t0 = Date.UTC(2026, 9, 8, 9, 34);
  const copy = (server: string, created: number, extra: Partial<ConsumerServerView> = {}): ConsumerServerView => ({
    server, isLeader: false, current: true, offline: false, deliveredStreamSeq: 208513, ackFloorStreamSeq: 208506, created, configHash: "h1", ...extra,
  });

  it("lists each server's copy of a consumer and marks the odd one", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, checkCode: "consumer-009", entity: consumerEntity }, transitions: [] });
    m.getConsumer.mockResolvedValue({
      config: [], disagreements: [], findings: [],
      servers: [copy("nats-0", t0, { isLeader: true }), copy("nats-1", t0 + 5 * day), copy("nats-2", t0 + 1_560_000)],
    });
    show();
    const table = await screen.findByRole("table", { name: "Server copies" });
    expect(m.getConsumer).toHaveBeenCalledWith(expect.anything(), expect.anything(), "APP", "order-jobs", "resize-worker", expect.anything());
    const rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(4);
    expect(within(rows[1]).getByText("leader")).toBeInTheDocument();
    expect(rows[2]).toHaveClass("odd");
    expect(within(rows[2]).getByText("+5d")).toBeInTheDocument();
    expect(rows[3]).not.toHaveClass("odd");
    expect(within(rows[1]).getByText("208,513")).toBeInTheDocument();
  });

  it("shows a dash for a position a server did not report", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, checkCode: "consumer-009", entity: consumerEntity }, transitions: [] });
    m.getConsumer.mockResolvedValue({ config: [], disagreements: [], findings: [], servers: [copy("nats-0", t0, { isLeader: true, deliveredStreamSeq: undefined })] });
    show();
    const table = await screen.findByRole("table", { name: "Server copies" });
    expect(within(table).getAllByText("not observed").length).toBeGreaterThan(0);
  });

  it("leaves the table out for a client finding, with no error", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, checkCode: "client-003", entity: clientEntity }, transitions: [] });
    show();
    expect(await screen.findByText(/first seen by nats-lens/i)).toBeInTheDocument();
    expect(screen.queryByRole("table", { name: "Server copies" })).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(m.getStream).not.toHaveBeenCalled();
    expect(m.getConsumer).not.toHaveBeenCalled();
  });
});

describe("other entities", () => {
  it("counts the other entities with the same finding and links to them", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, checkCode: "consumer-009", entity: consumerEntity }, transitions: [] });
    m.listFindings.mockResolvedValue({ items: [], nextPageToken: "", totalCount: 4 });
    show();
    const link = await screen.findByRole("link", { name: "3 other entities have this finding" });
    expect(link.getAttribute("href")).toMatch(/^\/findings\?check=consumer-009/);
    expect(m.listFindings).toHaveBeenCalledWith(expect.anything(), expect.anything(), expect.objectContaining({ checkCode: "consumer-009", includeMuted: true }), expect.anything());
  });

  it("says nothing when no other entity has it", async () => {
    m.getFinding.mockResolvedValue({ finding: base, transitions: [] });
    show();
    expect(await screen.findByText(/first seen by nats-lens/i)).toBeInTheDocument();
    expect(screen.queryByText(/other entit/)).toBeNull();
  });
});

describe("around the start", () => {
  it("lists the events 10 minutes either side of the condition's start", async () => {
    const began = Date.now() - 2 * 3_600_000;
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: began }, transitions: [] });
    listEvents.mockResolvedValue(
      create(ListEventsResponseSchema, {
        events: [create(EventSchema, { id: "7", time: timestampFromMs(began + 30_000), source: EventSource.NATS, kind: "server.shutdown", type: "SERVER.SHUTDOWN", summary: "nats-1 shut down" })],
        totalCount: 1,
      }),
    );
    show();
    expect(await screen.findByText("nats-1 shut down")).toBeInTheDocument();
    const req = listEvents.mock.calls[0][0] as { range: { start: never; end: never }; hideRoutine: boolean };
    expect(timestampMs(req.range.start)).toBe(began - 600_000);
    expect(timestampMs(req.range.end)).toBe(began + 600_000);
    expect(req.hideRoutine).toBe(true);
  });

  it("uses when nats-lens first saw it without a condition start", async () => {
    m.getFinding.mockResolvedValue({ finding: base, transitions: [] });
    show();
    expect(await screen.findByText(/first seen by nats-lens/i)).toBeInTheDocument();
    await vi.waitFor(() => expect(listEvents).toHaveBeenCalled());
    const req = listEvents.mock.calls[0][0] as { range: { start: never } };
    expect(timestampMs(req.range.start)).toBe(base.since! - 600_000);
  });
});
