import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ChartInspection } from "./ChartInspection";
import { HistoryContext } from "../components/HistoryNote";
import { MiniLine } from "./MiniLine";
import { Sparkline } from "./Sparkline";
import { TrendChart } from "../components/TrendChart";

const points = [{ time: 0, value: 2 }, { time: 1000, value: 4 }, { time: 4000, value: 8 }];
const spark = { points, start: 0, end: 5000, stepMs: 1000, status: "ok" as const, windows: [], label: "CPU", fmt: (v: number) => `${v}%` };

describe("chart inspection", () => {
  it("exposes formatted samples at their UTC bucket time by keyboard without filling gaps", () => {
    render(<Sparkline {...spark} />);
    const cursor = screen.getByRole("slider", { name: "Inspect CPU" });
    fireEvent.focus(cursor);
    fireEvent.keyDown(cursor, { key: "Home" });
    expect(screen.getByRole("tooltip")).toHaveTextContent("00:00:00 UTC");
    expect(screen.getByRole("tooltip")).toHaveTextContent("2%");
    fireEvent.keyDown(cursor, { key: "ArrowRight" });
    expect(screen.getByRole("tooltip")).toHaveTextContent("4%");
    fireEvent.keyDown(cursor, { key: "ArrowRight" });
    expect(screen.getByRole("tooltip")).toHaveTextContent("No sample");
    expect(screen.getByRole("tooltip")).not.toHaveTextContent("4%");
    fireEvent.keyDown(cursor, { key: "Escape" });
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
  it("inspects multiple lines and band bounds with units, and subtracts stacked baselines", () => {
    render(<TrendChart title="Memory" start={0} end={5000} stepMs={1000} fmt={v => `${v} MiB`} layers={[
      { kind: "band", label: "server range", style: { tone: "accent" }, points: [{ time: 0, low: 2, high: 9 }] },
      { kind: "line", label: "median", style: { tone: "accent" }, points },
      { kind: "band", label: "single", stacked: true, style: { tone: "warn" }, points: [{ time: 0, low: 9, high: 12 }] },
    ]} />);
    fireEvent.keyDown(screen.getByRole("slider", { name: "Inspect Memory" }), { key: "Home" });
    expect(screen.getByRole("tooltip")).toHaveTextContent("server range: 2 MiB to 9 MiB");
    expect(screen.getByRole("tooltip")).toHaveTextContent("median: 2 MiB");
    expect(screen.getByRole("tooltip")).toHaveTextContent("single: 3 MiB");
  });
  it("shows both compact lines and gives event counts their meaning", () => {
    render(<Sparkline {...spark} label="Meta leader" fmt={v => `${v} leader changes`} points2={[{ time: 0, value: 7 }]} labels={["changes", "other"]} />);
    fireEvent.keyDown(screen.getByRole("slider", { name: "Inspect Meta leader" }), { key: "Home" });
    expect(screen.getByRole("tooltip")).toHaveTextContent("changes: 2 leader changes");
    expect(screen.getByRole("tooltip")).toHaveTextContent("other: 7 leader changes");
  });
});

it("uses pointer position for hover and touch, clamps edges and dismisses on blur", () => {
  render(<Sparkline {...spark} />);
  const cursor = screen.getByRole("slider");
  vi.spyOn(cursor, "getBoundingClientRect").mockReturnValue({ left: 10, width: 100 } as DOMRect);
  fireEvent.pointerMove(cursor, { clientX: 35 });
  expect(screen.getByRole("tooltip")).toHaveTextContent("4%");
  fireEvent.pointerDown(cursor, { clientX: 95, pointerType: "touch" });
  expect(screen.getByRole("tooltip")).toHaveTextContent("8%");
  fireEvent.keyDown(cursor, { key: "End" });
  expect(cursor).toHaveAttribute("aria-valuenow", "5000");
  fireEvent.keyDown(cursor, { key: "ArrowRight" });
  expect(cursor).toHaveAttribute("aria-valuenow", "5000");
  fireEvent.blur(cursor);
  expect(screen.queryByRole("tooltip")).toBeNull();
});
it("does not turn a missing collection into a known bucket sample", () => {
  render(<HistoryContext.Provider value={{ start: 0, end: 5000, live: false, gaps: [{ kind: "notRunning", start: 1000, end: 2000, eventId: "", connection: "" }] }}>
    <ChartInspection label="Connections" start={0} end={5000} stepMs={1000} series={[{ label: "open", points: [{ time: 1000, text: "4" }] }]}><svg /></ChartInspection>
  </HistoryContext.Provider>);
  const cursor = screen.getByRole("slider");
  fireEvent.keyDown(cursor, { key: "Home" });
  fireEvent.keyDown(cursor, { key: "ArrowRight" });
  expect(screen.getByRole("tooltip")).toHaveTextContent("Not collected");
  expect(screen.getByRole("tooltip")).toHaveTextContent("No sample");
});
it("describes aggregation and inspects legacy series without assuming epoch alignment", () => {
  render(<Sparkline {...spark} start={123} end={2123} points={[{ time: 123, value: 9 }, { time: 1123, value: 6 }]} stepMs={undefined} bucketNote="Mean rate in bucket" />);
  fireEvent.keyDown(screen.getByRole("slider"), { key: "Home" });
  expect(screen.getByRole("tooltip")).toHaveTextContent("9%");
  expect(screen.getByRole("tooltip")).toHaveTextContent("Mean rate in bucket");
});

it("gives the Overview consumer mini chart the same time inspector", () => {
  render(<MiniLine points={points} level="ok" label="worker delivered" period="range" fmt={v => `${v}/s`} frame={{start:0,end:5000,stepMs:1000}} />);
  fireEvent.keyDown(screen.getByRole("slider", { name: "Inspect worker delivered" }), { key: "Home" });
  expect(screen.getByRole("tooltip")).toHaveTextContent("2/s");
});
it("keeps the touched bucket when the browser subsequently focuses the chart", () => {
  render(<Sparkline {...spark} />);
  const cursor = screen.getByRole("slider");
  vi.spyOn(cursor, "getBoundingClientRect").mockReturnValue({ left: 10, width: 100 } as DOMRect);
  fireEvent.pointerDown(cursor, { clientX: 95, pointerType: "touch" });
  fireEvent.focus(cursor);
  expect(screen.getByRole("tooltip")).toHaveTextContent("8%");
});

it("does not describe a bytes mini-chart as a delivery-rate measurement",()=>{
 render(<MiniLine points={[{time:1000,value:10},{time:2000,value:20}]} level="ok" label="orders bytes" period="range" fmt={v=>`${v} B`} frame={{start:1000,end:2000,stepMs:1000}}/>);
 fireEvent.focus(screen.getByRole("slider",{name:"Inspect orders bytes"}));
 expect(screen.getByRole("tooltip")).not.toHaveTextContent("delivery rate");
 expect(screen.getByRole("tooltip")).toHaveTextContent("orders bytes");
});
