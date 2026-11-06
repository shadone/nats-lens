import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { EntityType } from "../gen/natslens/v1/common_pb";
import { EventSchema, EventSource, ListEventsResponseSchema } from "../gen/natslens/v1/events_pb";
import { renderApp } from "../test/render";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router";
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
  beforeEach(() => { listEvents.mockReset(); });
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

  it("keeps an empty live region for the pill before there is anything new", async () => {
    listEvents.mockResolvedValue(response());
    const { container } = renderApp(<Events />, "/events");
    expect(await screen.findByText("resize-worker on order-jobs")).toBeInTheDocument();
    const region = container.querySelector('.ev-new[role="status"]');
    expect(region).toHaveAttribute("aria-live", "polite");
    expect(region).toBeEmptyDOMElement();
  });

  it("says only the page in the pager", async () => {
    listEvents.mockResolvedValue(response({ nextPageToken: "p2" }));
    renderApp(<Events />, "/events");
    const pager = await screen.findByRole("navigation", { name: "Pagination" });
    expect(within(pager).getByText("page 1")).toBeInTheDocument();
    expect(pager).not.toHaveTextContent(/events/);
  });

  it("keeps the first page when going back to it", async () => {
    listEvents.mockResolvedValue(response({ nextPageToken: "p2" }));
    renderApp(<Events />, "/events");
    fireEvent.click(await screen.findByRole("button", { name: "Older" }));
    await vi.waitFor(() => expect(lastRequest().pageToken).toBe("p2"));
    const feedCalls = () => listEvents.mock.calls.filter((c) => (c[0] as { includeFacets?: boolean }).includeFacets).length;
    const before = feedCalls();
    fireEvent.click(await screen.findByRole("button", { name: "Newer" }));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(feedCalls()).toBe(before);
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
    const pill = await screen.findByRole("button", { name: "3 new events, show" });
    expect(pill.closest('[role="status"]')).toHaveAttribute("aria-live", "polite");
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
    expect(legend).not.toHaveTextContent("hidden");
    const chart = screen.getByRole("img", { name: /Events over the range/ });
    expect(chart.querySelectorAll("rect.hb-warning").length).toBeGreaterThan(0);
  });

  const zero = (extra: Record<string, unknown> = {}) => response({
    events: [], totalCount: 0, routineHidden: 0, routineGroups: [], facets: [],
    histogram: [{ start: timestampFromMs(now - 3_600_000), warning: 0, notable: 0, routine: 0 }],
    totalUnfiltered: 12, ...extra,
  });

  it("explains a client with no matches before zero facets and clears only the client", async () => {
    listEvents.mockResolvedValue(zero());
    const { container } = renderApp(<Events />, "/events?client=checkout-worker&system=example-prod&range=1h&account=SHOP&src=nats&kind=client.connect&server=nats-1&show=all");
    const state = await screen.findByRole("region", { name: "No events match these filters" });
    for (const text of ["checkout-worker", "client.connect", "nats-1", "Last 1h"]) expect(state).toHaveTextContent(text);
    expect(state).toHaveTextContent(/Connections shows.*snapshot/);
    expect(state).toHaveTextContent(/may have opened before capture/);
    expect(state).toHaveTextContent(/outside this range/);
    expect(screen.queryByRole("img", { name: /Events over the range/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
    expect(state.compareDocumentPosition(container.querySelector('.ev-facets')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(listEvents).toHaveBeenCalledTimes(1);
    fireEvent.click(within(state).getByRole("button", { name: "Clear client filter" }));
    await vi.waitFor(() => expect(lastRequest().clientName).toBe(""));
    const url = new URL(screen.getByTestId("location").textContent!, "http://example.com");
    expect(url.searchParams.has("client")).toBe(false);
    for (const [key, value] of Object.entries({ system: "example-prod", range: "1h", account: "SHOP", src: "nats", kind: "client.connect", server: "nats-1", show: "all" })) expect(url.searchParams.get(key)).toBe(value);
    expect(lastRequest()).toMatchObject({ system: "example-prod", sources: [EventSource.NATS], kinds: ["client.connect"], hideRoutine: false });
    expect(screen.getByRole("searchbox", { name: "Client connection name" })).toHaveValue("");
    fireEvent.click(screen.getByRole("button", { name: "Filter", exact: true }));
    expect(screen.getByTestId("location")).not.toHaveTextContent("client=");
    expect(listEvents).toHaveBeenCalledTimes(2);
  });

  it("identifies no capture only within the selected system and fixed range", async () => {
    listEvents.mockResolvedValue(zero({ totalUnfiltered: 0 }));
    renderApp(<Events />, "/events?system=example-staging&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z");
    const state = await screen.findByRole("region", { name: "No events captured in this range" });
    expect(state).toHaveTextContent("example-staging");
    expect(state).toHaveTextContent("2026-10-17T13:00:00.000Z");
    expect(state).toHaveTextContent(/wider range/);
    expect(listEvents).toHaveBeenCalledTimes(1);
  });

  it("explains hidden routine events and offers Show routine events without claiming no capture", async () => {
    listEvents.mockResolvedValue(zero({ routineHidden: 8, histogram: [{ start: timestampFromMs(now), routine: 8 }], routineGroups: [{ label: "checkout-worker short connections", kind: "client.churn", count: 8 }] }));
    renderApp(<Events />, "/events?client=checkout-worker&range=6h");
    const state = await screen.findByRole("region", { name: "Matching events are hidden as routine" });
    expect(state).not.toHaveTextContent(/No events captured/);
    expect(screen.getByRole("img", { name: /Events over the range/ })).toBeInTheDocument();
    fireEvent.click(within(state).getByRole("button", { name: "Show routine events" }));
    await vi.waitFor(() => expect(lastRequest().hideRoutine).toBe(false));
    expect(screen.getByTestId("location")).toHaveTextContent("client=checkout-worker");
    expect(screen.getByTestId("location")).toHaveTextContent("range=6h");
  });

  it("omits a first-page pager without a cursor even for nonempty results", async () => {
    listEvents.mockResolvedValue(response());
    renderApp(<Events />, "/events");
    await screen.findByText("resize-worker on order-jobs");
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
  });

  it("retains Newer on an empty last page and resets cursors after clearing the client", async () => {
    listEvents.mockImplementation(async (req) => req.pageToken ? zero() : response({ nextPageToken: "p2" }));
    renderApp(<Events />, "/events?client=checkout-worker");
    fireEvent.click(await screen.findByRole("button", { name: "Older" }));
    expect(await screen.findByRole("region", { name: "No more events on this page" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Newer" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Older" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Clear client filter" }));
    await vi.waitFor(() => expect(lastRequest()).toMatchObject({ pageToken: "", clientName: "" }));
  });

  it("does not attribute a placeholder empty result to a pending filter", async () => {
    listEvents.mockResolvedValueOnce(zero()).mockImplementation(() => new Promise(() => {}));
    renderApp(<Events />, "/events?client=checkout-worker");
    fireEvent.click(await screen.findByRole("button", { name: "Clear client filter" }));
    expect(await screen.findByText("Loading events…")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "No events match these filters" })).not.toBeInTheDocument();
  });

  it("keeps loading and errors distinct from an empty feed", async () => {
    listEvents.mockRejectedValue(new Error("Synthetic request failed"));
    renderApp(<Events />, "/events?client=checkout-worker");
    expect(screen.getByText("Loading events…")).toBeInTheDocument();
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load events");
    expect(screen.queryByRole("button", { name: "Clear client filter" })).not.toBeInTheDocument();
  });

  it("syncs visible client and kind fields after URL navigation", async () => {
    function Navigate() {
      const navigate = useNavigate();
      return <button onClick={() => navigate("/events?client=catalog-api&kind=client.disconnect")}>Other client</button>;
    }
    listEvents.mockResolvedValue(zero());
    renderApp(<><Events /><Navigate /></>, "/events?client=checkout-worker&kind=client.connect");
    await screen.findByRole("button", { name: "Clear client filter" });
    fireEvent.click(screen.getByRole("button", { name: "Other client" }));
    await vi.waitFor(() => expect(lastRequest().clientName).toBe("catalog-api"));
    expect(screen.getByRole("searchbox", { name: "Client connection name" })).toHaveValue("catalog-api");
    expect(screen.getByRole("searchbox", { name: "Kinds, comma separated" })).toHaveValue("client.disconnect");
  });


  it("shows an error instead of asserting an empty result after a failed refresh", async () => {
    function Refresh() {
      const qc = useQueryClient();
      return <button onClick={() => { void qc.invalidateQueries({ queryKey: ["eventFeed"] }); }}>Refresh feed</button>;
    }
    listEvents.mockResolvedValueOnce(zero()).mockRejectedValue(new Error("Synthetic refresh failed"));
    renderApp(<><Events /><Refresh /></>, "/events?client=checkout-worker");
    await screen.findByRole("region", { name: "No events match these filters" });
    fireEvent.click(screen.getByRole("button", { name: "Refresh feed" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Synthetic refresh failed");
    expect(screen.queryByRole("region", { name: "No events match these filters" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("retains routine context and the routine counter when the histogram is all zero", async () => {
    listEvents.mockResolvedValue(zero({ routineHidden: 8 }));
    renderApp(<Events />, "/events?client=checkout-worker");
    expect(await screen.findByRole("region", { name: "Matching events are hidden as routine" })).toHaveTextContent("8 matching events");
    expect(screen.getByRole("region", { name: "Hidden as routine" })).toHaveTextContent("8");
    expect(screen.queryByRole("img", { name: /Events over the range/ })).not.toBeInTheDocument();
  });

});
