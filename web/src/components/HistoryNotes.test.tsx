import { render, screen } from "@testing-library/react";
import { renderApp } from "../test/render";
import { describe, expect, it } from "vitest";
import { HistoryContext, HistoryNote } from "./HistoryNote";
import { TrendChart } from "./TrendChart";
import { SeriesChart } from "../charts/SeriesChart";
import { NOW, staging } from "../test/fixtures/history";
describe("range history notes and chart gaps", () => {
  it("keeps system, range and account on the history link", () => {
    const d = staging();
    renderApp(<HistoryContext.Provider value={{ historyStart: d.historyStart, start: d.rangeStart, end: NOW, intervalMs: 30_000, live: true }}><HistoryNote /></HistoryContext.Provider>, "/?system=example-staging&range=24h&account=APP");
    const link = screen.getByRole("link", { name: "Data history" });
    expect(link.getAttribute("href")).toContain("system=example-staging");
    expect(link.getAttribute("href")).toContain("account=APP");
    expect(screen.getByRole("note")).toHaveTextContent("9 h of history");
  });
  it.each(["trend", "series"])("names a three-minute gap and before-history on a %s chart, excluding event-only gaps", (kind) => {
    const d = staging();
    const { container } = render(<HistoryContext.Provider value={{ start: d.rangeStart, end: NOW, live: true, gaps: d.gaps }}>{kind === "trend" ? <TrendChart title="Connections" start={d.rangeStart} end={NOW} layers={[]} /> : <SeriesChart title="Connections" start={d.rangeStart} end={NOW} />}</HistoryContext.Provider>);
    expect(screen.getByRole("img", { name: /No data 17:50:44–17:53:44 UTC: nats-lens was not running/ })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /nats-lens was not running yet/ })).toBeInTheDocument();
    expect(container.querySelectorAll("rect.dh-chart-gap")).toHaveLength(3);
    expect(Number(screen.getByRole("img", { name: /No data 17:50:44–17:53:44 UTC/ }).getAttribute("width"))).toBeGreaterThanOrEqual(1.5);
    expect(container.querySelector("rect.dh-chart-gap title")).toHaveTextContent("No data");
  });
});

import { FindingRow } from "./FindingParts";
import { OverviewTiles } from "./OverviewTiles";
import { ServerCards } from "./CapacityServers";
import type { Finding, Tile } from "../api/model";
const young = { start: NOW - 86_400_000, end: NOW, live: true, historyStart: NOW - 31 * 60_000 };
it("replaces a misleading fresh start in a finding row", () => {
  const f = { id: "f1", checkCode: "server-007", checkName: "Store high", severity: "warning", state: "firing", since: young.historyStart, detail: "High store" } as Finding;
  renderApp(<HistoryContext.Provider value={young}><FindingRow f={f} end={NOW} /></HistoryContext.Provider>);
  expect(screen.getByText("already true when nats-lens started")).toBeInTheDocument();
});
it("keeps a Capacity card concise with an entity-specific explanation", () => {
  renderApp(<HistoryContext.Provider value={young}><ServerCards servers={[{ name: "nats-1", cluster: "shop", reported: true, used: 20, limit: 100, findingCode: "", findingId: "" }]} withoutJetstream={0} /></HistoryContext.Provider>);
  expect(screen.getByTitle("This server lacks usable observations spanning 6 h")).toHaveTextContent("growth unavailable");
  expect(screen.queryByRole("link", { name: /needs samples spanning 6 h/ })).not.toBeInTheDocument();
});
it("names the shorter coverage and missing growth on Overview storage", () => {
  const tile = { id: "storage", label: "Storage", unit: "bytes", status: "ok", value: 20, subtitle: "no rate yet" } as Tile;
  renderApp(<HistoryContext.Provider value={young}><OverviewTiles tiles={[tile]} windows={[]} start={young.start} end={NOW} /></HistoryContext.Provider>);
  expect(screen.getByText(/over 31 min/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: /Growth uses samples spanning 6 h; entity measurements determine the rate/ })).toBeInTheDocument();
});

import { CauseGroups } from "./CauseGroups";
import { groupCauses } from "../lib/causeGroups";
it("marks both single and grouped cause cards with the same finding rule", () => {
  const fs = ["a", "b"].map((id) => ({ id, checkCode: "server-007", checkName: "Store high", severity: "warning", state: "firing", since: young.historyStart, detail: "High store" } as Finding));
  const { groups, muted } = groupCauses(fs);
  renderApp(<HistoryContext.Provider value={young}><CauseGroups groups={groups} muted={muted} onMute={() => {}} /></HistoryContext.Provider>);
  expect(screen.getAllByText("already true when nats-lens started")).toHaveLength(2);
});
it("omits a history note when the range is covered", () => {
  renderApp(<HistoryContext.Provider value={{ ...young, historyStart: young.start }}><HistoryNote /></HistoryContext.Provider>);
  expect(screen.queryByRole("note")).not.toBeInTheDocument();
});
it("does not claim calendar age establishes a Capacity rate", () => {
  renderApp(<HistoryContext.Provider value={{ ...young, historyStart: young.start }}><ServerCards servers={[{ name: "nats-1", cluster: "shop", reported: true, used: 20, limit: 100, findingCode: "", findingId: "" }]} withoutJetstream={0} /></HistoryContext.Provider>);
  expect(screen.getByText("growth unavailable")).toBeInTheDocument();
});
it("names the shorter history behind the Overview message-rate chart", () => {
  renderApp(<HistoryContext.Provider value={{ ...young, gaps: [] }}><TrendChart title="Messages" start={young.start} end={NOW} layers={[]} /></HistoryContext.Provider>);
  expect(screen.getByText("over 31 min")).toBeInTheDocument();
});
it("hatches all time before history when a chart spans more than the page query", () => {
  const d = staging();
  const start = NOW - 7 * 86_400_000;
  render(<HistoryContext.Provider value={{ start: d.rangeStart, end: NOW, live: true, gaps: d.gaps, historyStart: d.historyStart }}><SeriesChart title="Bytes, 7 days" start={start} end={NOW} /></HistoryContext.Provider>);
  const gap = screen.getByRole("img", { name: /nats-lens was not running yet/ });
  expect(gap).toHaveAttribute("x", "0");
  expect(Number(gap.getAttribute("width"))).toBeGreaterThan(180);
});

it("keeps Overview growth guidance neutral when the tile already reports a rate", () => {
 const tile = { id:"storage",label:"Storage",unit:"bytes",status:"ok",value:20,subtitle:"+2 MiB /day" } as Tile;
 renderApp(<HistoryContext.Provider value={{...young,historyStart:young.start}}><OverviewTiles tiles={[tile]} windows={[]} start={young.start} end={NOW} /></HistoryContext.Provider>);
 expect(screen.queryByText(/entity data may be missing/)).not.toBeInTheDocument();
});
