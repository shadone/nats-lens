import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Series } from "../api/model";
import { MetricBox } from "./MetricBox";

const series = (values: number[]): Series => ({
  metric: "server.cpu_percent",
  unit: "percent",
  stepMs: 60_000,
  points: values.map((value, i) => ({ time: 1_000 + i * 60_000, value })),
  min: Math.min(...values),
  max: Math.max(...values),
});

const base = { title: "CPU", description: "CPU use of the server process.", fmt: (v: number) => `${v}%`, start: 0, end: 300_000 };

describe("MetricBox", () => {
  it("puts the dot at 50% for the midpoint", () => {
    const { container } = render(<MetricBox {...base} series={series([0, 10, 5])} />);
    const dot = container.querySelector(".mb-dot") as HTMLElement;
    expect(dot.style.left).toBe("50%");
    expect(screen.getByText("0%")).toBeInTheDocument();
    expect(screen.getByText("10%")).toBeInTheDocument();
  });

  it("says steady when the value did not change", () => {
    const { container } = render(<MetricBox {...base} series={series([3, 3, 3])} />);
    expect(screen.getByText("steady at 3% all window")).toBeInTheDocument();
    expect(container.querySelector(".mb-dot")).toBeNull();
  });

  it("shows no data, never 0, and no dot without a value", () => {
    const { container } = render(<MetricBox {...base} series={undefined} />);
    expect(screen.getByText("no data for the window")).toBeInTheDocument();
    expect(screen.queryByText("0%")).toBeNull();
    expect(container.querySelector(".mb-dot")).toBeNull();
  });

  it("never says steady without a series, even with a value", () => {
    const { container } = render(<MetricBox {...base} series={undefined} value={7} />);
    expect(screen.queryByText(/steady/)).toBeNull();
    expect(container.querySelector(".mb-range")).toBeNull();
    expect(screen.getByText("no data for the window")).toBeInTheDocument();
  });

  it("shows the error, not a no-data note, when the series failed", () => {
    render(<MetricBox {...base} series={undefined} error="unavailable: down" />);
    expect(screen.getByText("Could not load: unavailable: down")).toBeInTheDocument();
    expect(screen.queryByText("no data for the window")).toBeNull();
  });

  it("draws the window's track without a dot when there is no current value", () => {
    const { container } = render(<MetricBox {...base} series={series([2, 9, 4])} value={null} />);
    expect(container.querySelector(".mb-range")).not.toBeNull();
    expect(container.querySelector(".mb-dot")).toBeNull();
    expect(screen.getByText("2%")).toBeInTheDocument();
    expect(screen.getByText("9%")).toBeInTheDocument();
    expect(screen.getByText("no current value")).toBeInTheDocument();
    expect(screen.queryByText(/^no data/)).toBeNull();
  });

  it("keeps the window's own edges for a value outside them and clamps the dot", () => {
    const { container } = render(<MetricBox {...base} series={series([2, 9, 4])} value={15} />);
    expect(screen.getByText("2%")).toBeInTheDocument();
    expect(screen.getByText("9%")).toBeInTheDocument();
    expect((container.querySelector(".mb-dot") as HTMLElement).style.left).toBe("100%");
    expect(screen.getByRole("img", { name: /current 15%, above the window max, min 2%, max 9%/ })).toBeInTheDocument();
  });

  it("shows a state word only when the state is not ok", () => {
    const { rerender } = render(<MetricBox {...base} series={series([1, 70])} status="warning" />);
    expect(screen.getByText("warning")).toBeInTheDocument();
    expect(document.querySelector(".mb-dot.st-warning")).not.toBeNull();
    rerender(<MetricBox {...base} series={series([1, 2])} status="ok" />);
    expect(screen.queryByText("warning")).toBeNull();
    expect(screen.queryByText("ok")).toBeNull();
  });

  it("puts the limit in the note, not on the track", () => {
    render(<MetricBox {...base} series={series([1, 2])} note="limit 65,536" />);
    expect(screen.getByText("limit 65,536")).toBeInTheDocument();
  });

  it("describes the metric behind the (i) button", async () => {
    render(<MetricBox {...base} series={series([1, 2])} />);
    expect(screen.queryByRole("tooltip")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "About CPU" }));
    expect(screen.getByRole("tooltip")).toHaveTextContent("CPU use of the server process.");
  });

  it("names the chart and the track with value and range", () => {
    render(<MetricBox {...base} series={series([0, 10, 5])} />);
    expect(screen.getByRole("img", { name: /^CPU, current 5%, min 0%, max 10%/ })).toBeInTheDocument();
  });

  it("never reads a steady series as healthy when there is no current value", () => {
    render(<MetricBox {...base} series={series([0, 0, 0])} value={null} />);
    expect(screen.getByText("steady at 0% in the window · no current value")).toBeInTheDocument();
    expect(screen.queryByText("steady at 0% all window")).toBeNull();
  });

  it("marks an old series whose refresh failed", () => {
    const { container } = render(<MetricBox {...base} series={series([1, 2])} error="unavailable: down" />);
    expect(screen.getByText("refresh failed")).toHaveAttribute("title", "unavailable: down");
    expect(container.querySelector("svg.spark")).not.toBeNull();
  });

  it("shows no refresh note when the series is current", () => {
    render(<MetricBox {...base} series={series([1, 2])} />);
    expect(screen.queryByText("refresh failed")).toBeNull();
  });
});
