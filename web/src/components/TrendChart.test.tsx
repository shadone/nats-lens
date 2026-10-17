import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TrendChart, segments } from "./TrendChart";

const START = Date.parse("2026-10-16T14:30:00Z");
const END = START + 4 * 900_000;
const pts = (vals: number[]) => vals.map((v, i) => ({ time: START + i * 900_000, value: v }));

describe("TrendChart", () => {
  it("names the chart with its current value, period and range", () => {
    render(
      <TrendChart
        title="Connections"
        now="20"
        summary="min 14, max 20"
        start={START}
        end={END}
        layers={[{ kind: "line", label: "open", style: { tone: "accent" }, points: pts([14, 18, 20]) }]}
      />,
    );
    const img = screen.getByRole("img", { name: /^Connections/ });
    expect(img).toHaveAccessibleName("Connections, current 20, 16 Oct 14:30 to 16 Oct 15:30 UTC, min 14, max 20");
  });

  it("draws the out series dashed, in the chart and in its legend", () => {
    const { container } = render(
      <TrendChart
        title="Messages / s"
        now="176 in, 190 out"
        start={START}
        end={END}
        layers={[
          { kind: "line", label: "in", style: { tone: "accent" }, points: pts([1, 2, 3]) },
          { kind: "line", label: "out", style: { tone: "muted", dash: true }, points: pts([2, 3, 4]) },
        ]}
      />,
    );
    const legend = screen.getByRole("list", { name: "Messages / s legend" });
    const out = within(legend).getByText("out").closest("li")!;
    expect(out.querySelector("line")).toHaveClass("tc-dash");
    const inn = within(legend).getByText("in").closest("li")!;
    expect(inn.querySelector("line")).not.toHaveClass("tc-dash");
    expect(container.querySelectorAll("svg.tc-svg polyline.tc-dash")).toHaveLength(1);
  });

  it("fills a band between its low and high edges", () => {
    const { container } = render(
      <TrendChart
        title="CPU"
        start={START}
        end={END}
        layers={[{ kind: "band", label: "lowest to highest server", style: { tone: "accent" }, points: pts([1, 2]).map((p) => ({ time: p.time, low: 1, high: 5 })) }]}
      />,
    );
    expect(container.querySelector("svg.tc-svg polygon.tc-fill")).not.toBeNull();
  });

  it("draws a band point between two gaps as a thin vertical mark", () => {
    const { container } = render(
      <TrendChart
        title="CPU"
        start={START}
        end={END}
        stepMs={900_000}
        layers={[{ kind: "band", label: "lowest to highest server", style: { tone: "accent" }, points: [{ time: START + 2 * 900_000, low: 1, high: 5 }] }]}
      />,
    );
    expect(container.querySelector("svg.tc-svg polygon")).toBeNull();
    const mark = container.querySelector("svg.tc-svg line.tc-band-pt");
    expect(mark).not.toBeNull();
    expect(mark!.getAttribute("x1")).toBe(mark!.getAttribute("x2"));
    expect(mark!.getAttribute("y1")).not.toBe(mark!.getAttribute("y2"));
  });

  it("keeps a lone band point visible when its low equals its high", () => {
    const { container } = render(
      <TrendChart
        title="CPU"
        start={START}
        end={END}
        stepMs={900_000}
        layers={[{ kind: "band", label: "lowest to highest server", style: { tone: "accent" }, points: [{ time: START + 2 * 900_000, low: 5, high: 5 }] }]}
      />,
    );
    const mark = container.querySelector("svg.tc-svg line.tc-band-pt")!;
    expect(Math.abs(Number(mark.getAttribute("y1")) - Number(mark.getAttribute("y2")))).toBeGreaterThanOrEqual(3);
  });

  it("shows a dash and no data when nothing was observed", () => {
    render(<TrendChart title="Servers" start={START} end={END} layers={[{ kind: "line", label: "answering", style: { tone: "accent" }, points: [] }]} />);
    expect(screen.getByRole("img", { name: /^Servers/ })).toHaveAccessibleName(/Servers, no data/);
    expect(screen.getByText("–")).toBeInTheDocument();
  });
});

describe("segments", () => {
  it("breaks a line at a gap of more than two steps", () => {
    const p = [0, 1, 2, 6, 7].map((i) => ({ time: i * 10, value: i }));
    expect(segments(p, 10).map((s) => s.length)).toEqual([3, 2]);
    expect(segments(p, 0).map((s) => s.length)).toEqual([5]);
  });
});

describe("TrendChart floor", () => {
  const ys = (c: HTMLElement) =>
    c
      .querySelector("svg.tc-svg polyline")!
      .getAttribute("points")!
      .split(" ")
      .map((p) => Number(p.split(",")[1]));
  const memory = (floor?: "zero" | "data") =>
    render(<TrendChart title="Memory" start={START} end={END} floor={floor} layers={[{ kind: "line", label: "median", style: { tone: "accent" }, points: pts([392, 412, 432]) }]} />);

  it("starts the y axis at zero by default, so a narrow range is a flat line near the top", () => {
    const y = ys(memory().container);
    expect(Math.max(...y) - Math.min(...y)).toBeLessThan(8);
  });

  it("spans the data with a margin when the floor is the data", () => {
    const y = ys(memory("data").container);
    // H is 70: the lowest point sits above the axis and the highest below the top, with the range between them.
    expect(Math.max(...y)).toBeLessThan(68 - 2);
    expect(Math.min(...y)).toBeGreaterThan(6 + 2);
    expect(Math.max(...y) - Math.min(...y)).toBeGreaterThan(40);
  });

  it("keeps a flat series visible mid-chart when the floor is the data", () => {
    const { container } = render(
      <TrendChart title="Memory" start={START} end={END} floor="data" layers={[{ kind: "line", label: "median", style: { tone: "accent" }, points: pts([400, 400, 400]) }]} />,
    );
    const y = ys(container);
    expect(new Set(y).size).toBe(1);
    expect(y[0]).toBeGreaterThan(10);
    expect(y[0]).toBeLessThan(65);
  });
});
