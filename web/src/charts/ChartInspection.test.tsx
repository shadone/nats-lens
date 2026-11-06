import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
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
