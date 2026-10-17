// The Fleet page: a card per system, worst first, each opening its Overview.
import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { EntityType, Status } from "../gen/natslens/v1/common_pb";
import { CollectionState, ListSystemsResponseSchema, type SystemSummarySchema } from "../gen/natslens/v1/systems_pb";
import { renderApp } from "../test/render";
import { Fleet } from "./Fleet";

vi.mock("../api/client", () => ({ lens: { listSystems: vi.fn() } }));

const l = vi.mocked(lens);
const NOW = Date.now();
const BUCKET = 1_800_000;

type Init = MessageInitShape<typeof SystemSummarySchema>;

function summary(id: string, environment: string, over: Record<string, unknown> = {}): Init {
  return {
    system: { id, name: id, environment },
    collection: CollectionState.COLLECTING,
    status: Status.OK,
    freshness: { epoch: 9n, epochAt: timestampFromMs(NOW - 4_000), live: true, interval: durationFromMs(30_000) },
    servers: 3,
    serversAnswering: 3,
    clusters: 1,
    streams: 9,
    connections: 182n,
    strip: [Status.OK, Status.OK, Status.CRITICAL, Status.WARNING],
    stripStart: timestampFromMs(NOW - 4 * BUCKET),
    stripBucket: durationFromMs(BUCKET),
    ...over,
  } as Init;
}

const worst = (checkName: string, name: string) => ({ checkCode: "consumer-006", checkName, entity: { type: EntityType.CONSUMER, key: `APP/order-jobs/${name}`, name, account: "APP", stream: "order-jobs", consumer: name } });

function respond(systems: Init[]) {
  l.listSystems.mockResolvedValue(create(ListSystemsResponseSchema, { systems, defaultSystem: "shop-prod" }) as never);
}

const FIVE: Init[] = [
  summary("shop-dev", "development"),
  summary("shop-staging", "staging", {
    collection: CollectionState.CONNECTION_CLOSED,
    collectionError: "nats: authorization violation",
    collectionErrorSince: timestampFromMs(NOW - 3 * 3_600_000),
    freshness: { epoch: 4n, epochAt: timestampFromMs(NOW - 3 * 3_600_000), live: true, interval: durationFromMs(30_000) },
    status: Status.INFO,
  }),
  summary("shop-edge", "edge", { status: Status.INFO, worstFinding: worst("Stream without any limit", "catalog-changes"), tally: { info: 1 } }),
  summary("shop-eu", "production", { status: Status.WARNING, worstFinding: worst("Server not reporting", "nats-eu-4"), servers: 24, serversAnswering: 23 }),
  summary("shop-prod", "production", { status: Status.CRITICAL, worstFinding: worst("Expected durable missing", "resize-worker"), tally: { critical: 1, warning: 2 } }),
];

function cards() {
  return within(screen.getByRole("list", { name: "Systems" })).getAllByRole("listitem");
}

function cardOf(id: string) {
  return cards().find((c) => within(c).queryByText(id, { selector: ".fc-name" }))!;
}

beforeEach(() => {
  vi.clearAllMocks();
  respond(FIVE);
});

afterEach(() => {
  vi.useRealTimers();
});

describe("Fleet", () => {
  it("lists the systems worst first: critical, warning, info, not collecting, ok", async () => {
    renderApp(<Fleet />, "/fleet");
    await screen.findByText("shop-prod");
    expect(cards().map((c) => c.querySelector(".fc-name")?.textContent)).toEqual(["shop-prod", "shop-eu", "shop-edge", "shop-staging", "shop-dev"]);
    expect(within(cards()[0]).getByText("critical")).toBeInTheDocument();
    expect(cards()[0]).toHaveTextContent("Expected durable missing: resize-worker");
    expect(cards()[0]).toHaveTextContent("+2 more");
  });

  it("asks for a 48-cell strip over the page's range", async () => {
    renderApp(<Fleet />, "/fleet?range=6h");
    await screen.findByText("shop-prod");
    const req = l.listSystems.mock.calls[0][0] as { stripBuckets: number; range: { start: unknown; end?: unknown } };
    expect(req.stripBuckets).toBe(48);
    expect(req.range.end).toBeUndefined();
  });

  it("shows a system that is not collecting with its error, since when, and when data last arrived", async () => {
    renderApp(<Fleet />, "/fleet");
    await screen.findByText("shop-staging");
    const c = cardOf("shop-staging");
    expect(within(c).getByText("not collecting")).toBeInTheDocument();
    expect(c).toHaveTextContent(/nats: authorization violation since \d\d:\d\d UTC/);
    expect(c).toHaveTextContent("last data 3h ago");
  });

  it("shows servers answering of expected, and a dash for a count not observed", async () => {
    respond([summary("shop-eu", "production", { servers: 24, serversAnswering: 23, connections: undefined })]);
    renderApp(<Fleet />, "/fleet");
    await screen.findByText("shop-eu");
    const c = cardOf("shop-eu");
    expect(within(c).getByText("23").parentElement).toHaveTextContent("23 of 24servers");
    expect(within(c).getByText("connections").parentElement).toHaveTextContent("not observed");
  });

  it("opens the system's Overview from its card, keeping the range", async () => {
    renderApp(<Fleet />, "/fleet?range=6h");
    await screen.findByText("shop-prod");
    const href = within(cardOf("shop-prod")).getByRole("link").getAttribute("href") ?? "";
    expect(href.split("?")[0]).toBe("/");
    const p = new URLSearchParams(href.split("?")[1]);
    expect(p.get("system")).toBe("shop-prod");
    expect(p.get("range")).toBe("6h");
  });

  it("draws in the legend the same mark as the strip's cell it explains", async () => {
    renderApp(<Fleet />, "/fleet");
    await screen.findByText("shop-prod");
    const strip = within(cardOf("shop-prod")).getByRole("img", { name: /^Status per 30 min/ });
    const cell = strip.querySelector('rect[data-level="critical"]')!;
    const legend = screen.getByRole("list", { name: "Strip legend" });
    const item = within(legend).getByText("critical").closest("li")!;
    const mark = item.querySelector("rect")!;
    expect(mark.getAttribute("style")).toBe(cell.getAttribute("style"));
    expect(mark.getAttribute("style")).toContain("--cell-critical");
    expect(strip).toHaveAccessibleName(/critical \d\d:\d\d–\d\d:\d\d/);
  });

  it("renders a summary that failed as unavailable, still linked to the system", async () => {
    respond([{ system: { id: "shop-slow", name: "shop-slow", environment: "production" }, error: "deadline exceeded" }]);
    renderApp(<Fleet />, "/fleet");
    await screen.findByText("shop-slow");
    const c = cardOf("shop-slow");
    expect(within(c).getByText("summary unavailable")).toBeInTheDocument();
    expect(c).toHaveTextContent("deadline exceeded");
    expect(within(c).queryByRole("img", { name: /^Status per/ })).toBeNull();
    const href = within(c).getByRole("link").getAttribute("href") ?? "";
    expect(new URLSearchParams(href.split("?")[1]).get("system")).toBe("shop-slow");
  });

  it("starts no new ListSystems while one is pending, and polls again once it returns", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const done = create(ListSystemsResponseSchema, { systems: FIVE });
    let release: (v: unknown) => void = () => {};
    let asked = 0;
    // The shell's own list of systems asks without a strip; only the summaries count.
    l.listSystems.mockImplementation(((req: { stripBuckets?: number }) => {
      if (!req.stripBuckets) return Promise.resolve(done);
      asked++;
      return asked === 2 ? new Promise((r) => (release = r)) : Promise.resolve(done);
    }) as never);
    renderApp(<Fleet />, "/fleet");
    await screen.findByText("shop-prod");
    expect(asked).toBe(1);
    // The first poll hangs; the next intervals pass while it is pending.
    await act(() => vi.advanceTimersByTimeAsync(31_000));
    expect(asked).toBe(2);
    await act(() => vi.advanceTimersByTimeAsync(95_000));
    expect(asked).toBe(2);
    await act(async () => release(done));
    await act(() => vi.advanceTimersByTimeAsync(31_000));
    expect(asked).toBe(3);
  });

  it("says how many systems there are, once", async () => {
    renderApp(<Fleet />, "/fleet");
    await screen.findByText("shop-prod");
    expect(screen.getByText("5 systems, worst first")).toBeInTheDocument();
  });
});
