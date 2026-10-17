import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { EntityType } from "../gen/natslens/v1/common_pb";
import { EventSchema, EventSource, ListEventsResponseSchema } from "../gen/natslens/v1/events_pb";
import { renderApp } from "../test/render";
import { Events } from "./Events";

vi.mock("../api/client", () => ({ lens: { listEvents: vi.fn(), getEvent: vi.fn() } }));
const listEvents = vi.mocked(lens.listEvents);

const now = Date.now();

function response(extra: Record<string, unknown> = {}) {
  return create(ListEventsResponseSchema, {
    events: [
      create(EventSchema, {
        id: "41",
        time: timestampFromMs(now - 60_000),
        firstTime: timestampFromMs(now - 60_000 - 6 * 60_000),
        repeats: 5,
        source: EventSource.KUBERNETES,
        kind: "pod.failed_scheduling",
        type: "FailedScheduling",
        summary: "pod resize-worker-7f4c: 0/3 nodes available",
      }),
      create(EventSchema, { id: "40", time: timestampFromMs(now - 120_000), repeats: 1, source: EventSource.NATS, kind: "consumer.deleted", type: "CONSUMER.DELETED", summary: "resize-worker on order-jobs" }),
    ],
    totalCount: 6,
    routineHidden: 482,
    routineGroups: [
      { label: "catalog-api short connections", kind: "client.churn", clientName: "catalog-api", count: 448 },
      { label: "tooling on 127.0.0.1", kind: "client.connect", clientName: "", count: 34 },
    ],
    facets: [
      { dimension: "source", value: "EVENT_SOURCE_NATS", count: 4 },
      { dimension: "source", value: "EVENT_SOURCE_KUBERNETES", count: 2 },
      { dimension: "server", value: "nats-1", count: 5 },
      { dimension: "server", value: "nats-0", count: 1 },
    ],
    histogram: [
      { start: timestampFromMs(now - 3_600_000), warning: 1, notable: 2, routine: 300 },
      { start: timestampFromMs(now - 1_800_000), warning: 0, notable: 3, routine: 182 },
    ],
    totalUnfiltered: 488,
    ...extra,
  });
}

function lastRequest() {
  return listEvents.mock.calls[listEvents.mock.calls.length - 1][0] as Record<string, unknown>;
}

describe("Events", () => {
  beforeEach(() => listEvents.mockReset());
  afterEach(() => vi.useRealTimers());

  it("asks for what is worth reading by default: routine hidden, repeats folded", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    expect(await screen.findByText("resize-worker on order-jobs")).toBeInTheDocument();
    expect(lastRequest()).toMatchObject({ hideRoutine: true, foldRepeats: true, includeFacets: true });
    expect(lastRequest().histogramBuckets).toBeGreaterThan(0);
  });

  it("asks for everything when Everything is chosen", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    fireEvent.click(await screen.findByRole("button", { name: /^Everything/ }));
    expect(await screen.findByTestId("location")).toHaveTextContent("show=all");
    expect(lastRequest()).toMatchObject({ hideRoutine: false, foldRepeats: false });
  });

  it("counts the two views from the histogram", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    expect(await screen.findByRole("button", { name: "Worth reading 6" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Everything 488" })).toHaveAttribute("aria-pressed", "false");
  });

  it("names the routine groups as the API sends them, with Show them", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    const line = await screen.findByRole("region", { name: "Hidden as routine" });
    expect(within(line).getByText("482")).toBeInTheDocument();
    expect(within(line).getByText("catalog-api short connections")).toBeInTheDocument();
    expect(within(line).getByText("tooling on 127.0.0.1")).toBeInTheDocument();
    fireEvent.click(within(line).getByRole("button", { name: "Show them" }));
    expect(await screen.findByTestId("location")).toHaveTextContent("show=all");
  });

  it("shows a folded row with its count and span", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    expect(await screen.findByText("×5 in 6 min")).toBeInTheDocument();
  });

  it("filters by a server from its facet", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    const servers = await screen.findByRole("group", { name: "Servers" });
    fireEvent.click(within(servers).getByRole("button", { name: "nats-1 5" }));
    expect(await screen.findByTestId("location")).toHaveTextContent("server=nats-1");
    await vi.waitFor(() => expect(lastRequest().entity).toMatchObject({ type: EntityType.SERVER, key: "nats-1", server: "nats-1" }));
  });

  it("shows source counts from the facets", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    const sources = await screen.findByRole("group", { name: "Sources" });
    expect(within(sources).getByRole("button", { name: "NATS 4" })).toBeInTheDocument();
    expect(within(sources).getByRole("button", { name: "Findings 0" })).toBeInTheDocument();
  });

  it("holds new events behind a pill until it is pressed", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    expect(await screen.findByText("resize-worker on order-jobs")).toBeInTheDocument();
    const feedCalls = listEvents.mock.calls.length;
    // The probe finds three more events than the shown page counts.
    listEvents.mockResolvedValue(response({ totalCount: 9 }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(31_000);
    });
    const pill = await screen.findByRole("button", { name: "3 new" });
    // The probe asks for one event from the shown page's start.
    const probe = lastRequest();
    expect(probe).toMatchObject({ pageSize: 1, hideRoutine: true });
    expect(listEvents.mock.calls.filter((c) => (c[0] as { includeFacets?: boolean }).includeFacets)).toHaveLength(feedCalls);
    fireEvent.click(pill);
    await vi.waitFor(() => expect(listEvents.mock.calls.filter((c) => (c[0] as { includeFacets?: boolean }).includeFacets).length).toBe(feedCalls + 1));
  });

  it("draws each bar style in the histogram legend", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    const legend = await screen.findByRole("list", { name: "Histogram legend" });
    for (const k of ["warning", "notable", "routine"]) expect(legend.querySelector(`svg rect.hb-${k}`)).not.toBeNull();
    const chart = screen.getByRole("img", { name: /Events over the range/ });
    expect(chart.querySelectorAll("rect.hb-warning").length).toBeGreaterThan(0);
  });
});
