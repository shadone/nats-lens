// The Overview's history notes: one Data history request for every chart,
// the gaps hatched on the tiles and the trends, the tiles' span of
// history, and the cause cards' mark on findings already true when the
// history begins.
import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Entity, Finding, LensInfo, Overview as OverviewModel, Tile } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Overview } from "./Overview";

vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  getOverview: vi.fn(),
  listFindings: vi.fn(),
  getTimeline: vi.fn(),
  getLensInfo: vi.fn(),
  getDataHistory: vi.fn(),
}));

vi.mock("../api/client", () => ({
  lens: {
    getTimeSeries: vi.fn(async () => ({ series: undefined })),
    getTimeSeriesBatch: vi.fn(async (req: { queries: unknown[] }) => ({ results: req.queries.map(() => ({ error: "" })) })),
    listConsumers: vi.fn(async () => ({ consumers: [] })),
    listStreams: vi.fn(async () => ({ streams: [], totals: undefined })),
  },
}));

const m = vi.mocked(requests);
const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

function tile(id: string, label: string, extra: Partial<Tile> = {}): Tile {
  const end = Date.now();
  const series = { metric: "x", unit: "count" as const, stepMs: 900_000, points: [{ time: end - 2 * HOUR, value: 3 }, { time: end - HOUR, value: 4 }], min: 3, max: 4 };
  return { id, label, unit: "count", valueSuffix: "", value2Suffix: "", subtitle: "", status: "ok", description: "", value: 4, series, ...extra };
}

function overview(): OverviewModel {
  const end = Date.now();
  return {
    cluster: "example-staging",
    range: { start: end - DAY, end },
    freshness: { epoch: "9", epochAt: end - 5_000, ageMs: 5_000, partial: false, live: true, intervalMs: 30_000, collecting: true },
    servers: [],
    tally: { critical: 0, warning: 1, info: 0, muted: 0, flapping: 0, resolved24h: 0 },
    tiles: [tile("servers", "Servers"), tile("clients", "Clients", { subtitle: "12 apps" }), tile("storage", "Storage", { unit: "bytes", subtitle: "all replicas" })],
    windows: [],
  };
}

const entity: Entity = { kind: "stream", key: "APP/order-jobs", name: "order-jobs", account: "APP", stream: "order-jobs", consumer: "", server: "" };

function finding(since: number): Finding {
  return {
    id: "stream-004:APP/order-jobs",
    checkCode: "stream-004",
    checkName: "Stream near its message or byte limit",
    severity: "warning",
    state: "firing",
    entity,
    since,
    detail: "",
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
  };
}

function setUp(historyStart: number) {
  const end = Date.now();
  m.getOverview.mockResolvedValue(overview());
  m.listFindings.mockResolvedValue({ items: [finding(historyStart + 20_000)], nextPageToken: "", totalCount: 1 });
  m.getTimeline.mockResolvedValue({ start: end - DAY, bucketMs: 900_000, bucketCount: 96, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false });
  const info: LensInfo = { identityMode: "request", version: "test", cluster: "example-staging", collectIntervalMs: 30_000, retentionMs: 14 * DAY, live: true, demo: true, historyStart };
  m.getLensInfo.mockResolvedValue(info);
  m.getDataHistory.mockResolvedValue({
    rangeStart: end - DAY,
    rangeEnd: end,
    historyStart,
    coverage: { epochs: 1, expected: 1, partial: 0 },
    gaps: [
      { kind: "notRunning", start: end - 5 * HOUR, end: end - 5 * HOUR + 3 * MIN, eventId: "18", connection: "" },
      { kind: "beforeHistory", start: end - DAY, end: historyStart, eventId: "", connection: "" },
    ],
    gapsTotal: 2,
    listedGapsTotal: 0,
    archive: { enabled: false, location: "", newestFinal: false, lastError: "" },
    uses: [],
    findingsAtStart: 1,
  });
}

beforeEach(() => vi.clearAllMocks());

describe("Overview history notes", () => {
  it("reads the gaps once for all its charts and hatches them on the tiles and the trends", async () => {
    setUp(Date.now() - 9 * HOUR);
    renderApp(<Overview />, "/?system=example-staging");
    await screen.findByRole("region", { name: "Health" });
    await waitFor(() => expect(screen.getAllByRole("img", { name: /: nats-lens was not running$/ }).length).toBeGreaterThanOrEqual(3 + 8));
    // Three tiles and eight trend charts, each with the 3 minutes and the time before the history.
    expect(screen.getAllByRole("img", { name: /: nats-lens was not running yet$/ })).toHaveLength(11);
    expect(m.getDataHistory).toHaveBeenCalledTimes(1);
  });

  it("says over how long the range tiles' numbers are read, and how long storage growth still needs", async () => {
    setUp(Date.now() - 31 * MIN);
    renderApp(<Overview />, "/?system=example-staging");
    const health = await screen.findByRole("region", { name: "Health" });
    await within(health).findAllByText(/over 31 min/);
    expect(within(health).getByText(/^12 apps · over 31 min$/)).toBeInTheDocument();
    expect(within(health).getByText(/^growth needs 6 h of history; 5 h 29 min to go$/)).toBeInTheDocument();
    expect(within(health).getByRole("link", { name: "Data history" })).toHaveAttribute("href", "/data-history?system=example-staging");
    // A tile whose number is the newest collection's says nothing of the range.
    expect(within(health).getAllByText(/over 31 min/)).toHaveLength(2);
  });

  it("marks a cause card whose finding was already true when the history begins", async () => {
    setUp(Date.now() - 9 * HOUR);
    renderApp(<Overview />, "/?system=example-staging");
    const card = await screen.findByRole("article", { name: /Stream near its message or byte limit/ });
    await within(card).findByText(/^Already true when the history begins, at \d\d:\d\d UTC\./);
    expect(within(card).getByRole("link", { name: "Data history" })).toBeInTheDocument();
  });
});
