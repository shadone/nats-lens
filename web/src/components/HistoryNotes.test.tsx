import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { HistoryContext, HistoryNote } from "./HistoryNote";
import { TrendChart } from "./TrendChart";
import { SeriesChart } from "../charts/SeriesChart";
import { NOW, staging } from "../test/fixtures/history";
describe("range history notes and chart gaps", () => {
  it("keeps system, range and account on the history link", () => {
    const d = staging();
    render(<MemoryRouter initialEntries={["/?system=example-staging&range=24h&account=APP"]}><HistoryContext.Provider value={{ historyStart: d.historyStart, start: d.rangeStart, end: NOW, intervalMs: 30_000, live: true }}><HistoryNote /></HistoryContext.Provider></MemoryRouter>);
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
    expect(container.querySelector("rect.dh-chart-gap title")).toHaveTextContent("No data");
  });
});
