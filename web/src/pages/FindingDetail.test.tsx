import {InvestigationHandoff} from "../components/InvestigationContext";
import {buildInvestigationContext,type InvestigationSelection} from "../lib/investigationContext";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs, timestampMs } from "@bufbuild/protobuf/wkt";
import { act, fireEvent, screen, within } from "@testing-library/react";
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
  id: "stream-004:A/s",
  checkCode: "stream-004",
  checkName: "Stream near its message or byte limit",
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

function show(query = "") {
  renderApp(
    <Routes>
      <Route path="/findings/:id" element={<FindingDetail />} />
    </Routes>,
    "/findings/stream-004%3AA%2Fs" + query,
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
  it("keeps its full-screen timeline selection when a live refresh removes the row", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    m.getFinding.mockResolvedValue({ finding: base, transitions: [] });
    m.getTimeline.mockResolvedValue({ ...emptyTimeline, start: Date.parse("2026-10-17T12:00:00Z"), bucketCount: 1, rows: [{ key: "stream-004", checkCode: "stream-004", checkName: base.checkName, severity: "warning", cells: ["warning"], now: "warning" }] });
    show("?system=example-prod&range=6h");
    const mark = await screen.findByRole("button", { name: "stream-004: Warning" });
    mark.focus(); fireEvent.click(mark);
    expect(screen.getByRole("dialog")).toHaveClass("dlg-fullscreen");
    m.getTimeline.mockResolvedValue(emptyTimeline);
    await act(async () => { await vi.advanceTimersByTimeAsync(31_000); });
    expect(await screen.findByText("No check rows for this entity.")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "stream-004: Warning" })).toHaveClass("dlg-fullscreen");
    expect(screen.getByRole("dialog")).toHaveTextContent("2026-10-17 12:00:00 UTC");
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("links the check reference with system, range and account context", async () => {
    m.getFinding.mockResolvedValue({finding:base,transitions:[]});
    show("?system=example-prod&range=6h&account=APP");
    const link=await screen.findByRole("link",{name:"stream-004 · Check reference"});
    const url=new URL(link.getAttribute("href")!,"http://example.com");
    expect(url.pathname).toBe("/checks/stream-004");
    expect(Object.fromEntries(url.searchParams)).toEqual({range:"6h",system:"example-prod",account:"APP"});
  });

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
    expect(await screen.findByText("Resolved", { selector: ".state" })).toBeInTheDocument();
    expect(m.getFinding).toHaveBeenCalledTimes(2);
  });

  it("heads the page with the check and the entity, and quotes the detail under it", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, detail: "nats-0 2026-08-12T10:02:11Z; order-jobs grew" }, transitions: [] });
    show();
    const h = await screen.findByRole("heading", { level: 1 });
    expect(h).toHaveTextContent(/^Stream near its message or byte limit: order-jobs$/);
    expect(within(h).getByText("order-jobs")).toHaveClass("mono");
    expect(screen.getByText("nats-0 2026-08-12T10:02:11Z; order-jobs grew").tagName).toBe("P");
    expect(screen.queryByText("Stream near its message or byte limit", { selector: ".k *" })).toBeNull();
    expect(screen.getByText("Firing for (since first seen)")).toBeInTheDocument();
  });

  it("adds no unnamed landmark for its side panel", async () => {
    m.getFinding.mockResolvedValue({ finding: base, transitions: [] });
    show();
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("complementary")).toBeNull();
  });

  it("polls only the finding and its timeline in live mode", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    m.getFinding.mockResolvedValue({ finding: { ...base, checkCode: "consumer-009", entity: consumerEntity }, transitions: [] });
    show();
    expect(await screen.findByRole("table", { name: "Server copies" }).catch(() => screen.findByText(/No server reports/))).toBeInTheDocument();
    await vi.waitFor(() => expect(listEvents).toHaveBeenCalled());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(65_000);
    });
    expect(m.getFinding.mock.calls.length).toBeGreaterThanOrEqual(3);
    expect(m.getTimeline.mock.calls.length).toBeGreaterThanOrEqual(3);
    expect(m.getConsumer).toHaveBeenCalledTimes(1);
    expect(m.listFindings).toHaveBeenCalledTimes(1);
    expect(listEvents).toHaveBeenCalledTimes(1);
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
    expect(screen.getByRole("link", { name: "already true when nats-lens started" })).toHaveAttribute("href", expect.stringContaining("/data-history"));
    // The window around the start lies before the history.
    expect(await screen.findByText(/nats-lens has no history before 2026-10-17 08:19 UTC/)).toBeInTheDocument();
    expect(listEvents).not.toHaveBeenCalled();
    expect(screen.queryByText(/10 minutes either side/)).toBeNull();
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

describe("server copies refresh", () => {
  afterEach(() => vi.useRealTimers());

  it("reloads the copies and the other findings when the finding resolves", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const f = { ...base, checkCode: "consumer-009", entity: consumerEntity };
    m.getFinding.mockResolvedValueOnce({ finding: f, transitions: [] });
    m.getFinding.mockResolvedValue({ finding: { ...f, state: "resolved", resolvedAt: Date.now() }, transitions: [] });
    show();
    await vi.waitFor(() => expect(m.getConsumer).toHaveBeenCalledTimes(1));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(31_000);
    });
    expect(await screen.findByText("Resolved", { selector: ".state" })).toBeInTheDocument();
    await vi.waitFor(() => expect(m.getConsumer).toHaveBeenCalledTimes(2));
    expect(m.listFindings).toHaveBeenCalledTimes(2);
  });

  it("reloads the copies on Refresh", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, checkCode: "consumer-009", entity: consumerEntity }, transitions: [] });
    show();
    await vi.waitFor(() => expect(m.getConsumer).toHaveBeenCalledTimes(1));
    fireEvent.click(await screen.findByRole("button", { name: "Refresh server copies" }));
    await vi.waitFor(() => expect(m.getConsumer).toHaveBeenCalledTimes(2));
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

  it("switches between the whole system and this entity", async () => {
    const began = Date.now() - 2 * 3_600_000;
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: began, entity: consumerEntity }, transitions: [] });
    show();
    const scope = await screen.findByRole("group", { name: "Around the start scope" });
    expect(within(scope).getByRole("button", { name: "Whole system" })).toHaveAttribute("aria-pressed", "true");
    await vi.waitFor(() => expect(listEvents).toHaveBeenCalled());
    expect((listEvents.mock.calls[0][0] as { entity?: unknown }).entity).toBeUndefined();
    within(scope).getByRole("button", { name: "This entity" }).click();
    await vi.waitFor(() => expect(listEvents).toHaveBeenCalledTimes(2));
    expect((listEvents.mock.calls[1][0] as { entity?: unknown }).entity).toMatchObject({ key: "APP/order-jobs/resize-worker", consumer: "resize-worker" });
  });

  it("says how many it shows of how many, and opens the same window in Events, in both scopes", async () => {
    const began = Date.UTC(2026, 9, 17, 9, 0);
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: began, entity: consumerEntity }, transitions: [] });
    listEvents.mockResolvedValue(
      create(ListEventsResponseSchema, {
        events: [create(EventSchema, { id: "7", time: timestampFromMs(began + 30_000), source: EventSource.NATS, kind: "server.shutdown", type: "SERVER.SHUTDOWN", summary: "nats-1 shut down" })],
        totalCount: 34,
      }),
    );
    show("?system=example-prod");
    const href = async () => {
      const line = await screen.findByText(/Showing 1 of 34/);
      return new URL(within(line).getByRole("link", { name: "Open in Events" }).getAttribute("href")!, "http://x");
    };
    let url = await href();
    expect(url.pathname).toBe("/events");
    expect(url.searchParams.get("from")).toBe("2026-10-17T08:50:00Z");
    expect(url.searchParams.get("to")).toBe("2026-10-17T09:10:00Z");
    expect(url.searchParams.get("system")).toBe("example-prod");
    expect(url.searchParams.has("consumer")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "This entity" }));
    await vi.waitFor(() => expect(listEvents).toHaveBeenCalledTimes(2));
    url = await href();
    expect(url.searchParams.get("system")).toBe("example-prod");
    expect(url.searchParams.get("consumer")).toBe("APP/order-jobs/resize-worker");
    expect(url.searchParams.get("from")).toBe("2026-10-17T08:50:00Z");
  });

  it("gives the count only when it shows fewer than there are", async () => {
    const began = Date.UTC(2026, 9, 17, 9, 0);
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: began }, transitions: [] });
    listEvents.mockResolvedValue(
      create(ListEventsResponseSchema, {
        events: [create(EventSchema, { id: "7", time: timestampFromMs(began + 30_000), source: EventSource.NATS, kind: "server.shutdown", type: "SERVER.SHUTDOWN", summary: "nats-1 shut down" })],
        totalCount: 1,
      }),
    );
    show();
    expect(await screen.findByRole("link", { name: "Open in Events" })).toBeInTheDocument();
    expect(screen.queryByText(/Showing/)).toBeNull();
  });

  it("offers no entity scope for an entity Events cannot filter by", async () => {
    const account: Entity = { ...blank, kind: "account", key: "APP", account: "APP", name: "APP" };
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: Date.now() - 3_600_000, entity: account }, transitions: [] });
    show();
    await vi.waitFor(() => expect(listEvents).toHaveBeenCalled());
    expect(await screen.findByText(/10 minutes either side/)).toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "Around the start scope" })).toBeNull();
  });

  it("narrows a client finding by its client name", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: Date.now() - 3_600_000, entity: clientEntity }, transitions: [] });
    show();
    fireEvent.click(await screen.findByRole("button", { name: "This entity" }));
    await vi.waitFor(() => expect(listEvents).toHaveBeenCalledTimes(2));
    const req = listEvents.mock.calls[1][0] as { entity?: unknown; clientName?: string };
    expect(req.clientName).toBe("catalog-api");
    expect(req.entity).toBeUndefined();
  });
});

describe("FindingDetail of an accepted finding", () => {
  const accepted = {
    id: "m1",
    checkCode: "stream-004",
    entity: streamEntity,
    reason: "Kept for audits",
    mutedBy: "ops@example.com",
    expiresAt: Date.parse("2027-04-07T06:57:00Z"),
    unmutedBy: "",
    accepted: true,
    endReason: "",
    growthReview: true,
  };

  it("reads as accepted, with its review date and an End acceptance button", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, mute: { ...accepted, growthPerDay: 2 * 1024 * 1024 } }, transitions: [] });
    show();
    expect(await screen.findByText("Accepted as by design")).toBeInTheDocument();
    expect(screen.getByText("Accepted by ops@example.com:").parentElement).toHaveTextContent("Accepted by ops@example.com: Kept for audits. Review on 2027-04-07.");
    expect(screen.getByText(/^Growth at acceptance: \+2 MiB\/day per copy/)).toBeInTheDocument();
    expect(screen.getByText("Accepted", { selector: ".state" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "End acceptance…" }));
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "End acceptance" })).toBeInTheDocument();
  });

  it("says when no growth baseline is recorded yet", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, mute: accepted }, transitions: [] });
    show();
    expect(await screen.findByText("No growth baseline is recorded yet: growth is recorded once the stream has 8 days of history.")).toBeInTheDocument();
  });

  it("keeps a plain mute's words", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...base, mute: { ...accepted, accepted: false } }, transitions: [] });
    show();
    expect(await screen.findByText("Muted by ops@example.com:")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Lift mute…" })).toBeInTheDocument();
    expect(screen.queryByText(/growth baseline/)).toBeNull();
  });
});


it("offers Replay for the selected historical transition instead of the finding's newer lifetime", async () => {
  const t=Date.parse("2026-10-17T12:00:00Z");
  m.getFinding.mockResolvedValue({finding:{...base,since:t+3600000},transitions:[{time:t-600000,state:"resolved",severity:"warning",detail:""}]});
  show("?system=example-prod&account=APP&from=2026-10-17T11:00:00Z&to=2026-10-17T12:00:00Z");
  const link=await screen.findByRole("link",{name:"Replay this window"});
  const p=new URL(link.getAttribute("href")!,"http://example.com").searchParams;
  expect(p.get("a")).toBe("2026-10-17T11:45:00Z");
  expect(p.get("b")).toBe("2026-10-17T11:55:00Z");
  expect(p.get("system")).toBe("example-prod");
  expect(p.has("account")).toBe(false);
});

it.each(["firing","resolved","muted"])("copies a visible finding prompt for %s with the loaded ID, check, typed entity and frozen window",async state=>{
 const f:Finding={...base,id:"consumer-006:APP/orders/worker",checkCode:"consumer-006",checkName:"Consumer delivery stopped",entity:consumerEntity,state:state==="resolved"?"resolved":"firing",...(state==="muted"?{mute:{id:"mute",checkCode:"consumer-006",reason:"Accepted",mutedBy:"operator",unmutedBy:"",endReason:"",accepted:true}}:{})};
 m.getFinding.mockResolvedValue({finding:f,transitions:[]});let selected:InvestigationSelection|undefined;
 renderApp(<InvestigationHandoff.Provider value={s=>{selected=s}}><Routes><Route path="/findings/:id" element={<FindingDetail/>}/></Routes></InvestigationHandoff.Provider>,"/findings/route-alias?system=shop&from=2026-10-17T13:30:00Z&to=2026-10-17T14:30:00Z&account=OTHER");
 const button=await screen.findByRole("button",{name:"Copy as prompt"});expect(button).toHaveTextContent("Copy as prompt");fireEvent.click(button);
 const c=buildInvestigationContext({origin:"https://lens.example.com",pathname:"/findings/route-alias",search:"?account=OTHER",system:"shop",spec:{kind:"fixed",from:Date.parse("2026-10-17T13:30:00Z"),to:Date.parse("2026-10-17T14:30:00Z")},selection:selected});
 expect(c).toMatchObject({account:"APP",entity:{type:"ENTITY_TYPE_CONSUMER",key:consumerEntity.key},filters:{check:f.checkCode},evidence:[{kind:"finding",id:f.id}],range:{start:"2026-10-17T13:30:00.000Z",end:"2026-10-17T14:30:00.000Z"}});expect(c.evidence).toHaveLength(1);expect(c.series).toBeUndefined();expect(m.muteFinding).not.toHaveBeenCalled();expect(m.unmuteFinding).not.toHaveBeenCalled();
});

describe("FindingDetail extras for acceptance", () => {
  const acceptableFinding: Finding = { ...base, id: "consumer-016:APP/order-jobs/resize-worker", checkCode: "consumer-016", checkName: "Consumer with one replica", severity: "info", entity: consumerEntity, acceptable: true };
  const view = (server: string, isLeader: boolean): ConsumerServerView => ({ server, isLeader, current: true, offline: false, configHash: "h" });

  it("offers Accept as by design for a finding of an acceptable check, and not when it is not", async () => {
    m.getFinding.mockResolvedValue({ finding: acceptableFinding, transitions: [] });
    show();
    fireEvent.click(await screen.findByRole("button", { name: "Accept as by design…" }));
    expect(await screen.findByRole("dialog", { name: "Accept as by design" })).toBeInTheDocument();
  });

  it("offers no acceptance on a resolved finding's page", async () => {
    m.getFinding.mockResolvedValue({ finding: { ...acceptableFinding, state: "resolved", resolvedAt: Date.now() - 60_000 }, transitions: [] });
    show();
    await screen.findByRole("button", { name: "Mute…" });
    expect(screen.queryByRole("button", { name: "Accept as by design…" })).toBeNull();
  });

  it("offers no acceptance for a check that is not acceptable", async () => {
    m.getFinding.mockResolvedValue({ finding: base, transitions: [] });
    show();
    await screen.findByRole("button", { name: "Mute…" });
    expect(screen.queryByRole("button", { name: "Accept as by design…" })).toBeNull();
  });

  it("gives the nats CLI check with a copy button and links Placement on the leader's copy", async () => {
    m.getFinding.mockResolvedValue({ finding: acceptableFinding, transitions: [] });
    m.getConsumer.mockResolvedValue({ config: [], disagreements: [], findings: [], servers: [view("nats-1", false), view("nats-0", true)] });
    show();
    expect(await screen.findByText("nats consumer info --json -- order-jobs resize-worker")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy nats CLI check" })).toBeInTheDocument();
    const link = await screen.findByRole("link", { name: "Open this consumer in Placement" });
    expect(link.getAttribute("href")).toContain("/placement?asset=APP%2Forder-jobs%2Fresize-worker&server=nats-0");
  });
});
