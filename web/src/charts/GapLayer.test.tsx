// The range's hatched gaps on every kind of chart: the trend charts, the
// metric boxes' sparklines and the detail pages' series charts.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { isHatched } from "../api/history";
import { MetricBox } from "../components/MetricBox";
import { TrendChart } from "../components/TrendChart";
import { NOW, staging } from "../test/fixtures/history";
import { SeriesChart } from "./SeriesChart";

const d = staging();
const gaps = { gaps: d.gaps.filter(isHatched), rangeEnd: d.rangeEnd, live: true };
const START = d.rangeStart;
const END = d.rangeEnd;
const pts = [
  { time: NOW - 2 * 3_600_000, value: 4 },
  { time: NOW - 3_600_000, value: 5 },
];
const series = { metric: "cluster.connections", unit: "count" as const, stepMs: 900_000, points: pts, min: 4, max: 5 };

const NAMES = [
  "No data 4 Nov 22:47:18–5 Nov 13:47:18 UTC: nats-lens was not running yet",
  "No data 17:50:44–17:53:44 UTC: nats-lens was not running",
  "No data from 19:47:14 UTC until now: nats-lens could not reach NATS",
];

function hatched(container: HTMLElement) {
  return [...container.querySelectorAll<HTMLElement>(".gap-seg")];
}

describe("gaps on charts", () => {
  it("hatches and names each gap on a trend chart", () => {
    const { container } = render(<TrendChart title="Connections" start={START} end={END} layers={[{ kind: "line", label: "open", style: { tone: "accent" }, points: pts }]} gaps={gaps} />);
    for (const name of NAMES) expect(screen.getByRole("img", { name })).toHaveAttribute("title", name);
    const segs = hatched(container);
    expect(segs).toHaveLength(3);
    expect(segs[0]).toHaveClass("gap-hatch");
    expect(segs[0].style.left).toBe("0%");
    expect(segs[0].style.width).toBe("62.5%");
  });

  it("hatches a metric box's sparkline", () => {
    render(<MetricBox title="Clients" description="" series={series} fmt={String} start={START} end={END} gaps={gaps} />);
    for (const name of NAMES) expect(screen.getByRole("img", { name })).toBeInTheDocument();
  });

  it("hatches a detail page's series chart", () => {
    render(<SeriesChart title="Bytes" series={series} start={START} end={END} gaps={gaps} />);
    for (const name of NAMES) expect(screen.getByRole("img", { name })).toBeInTheDocument();
  });

  it("draws the chart alone without gaps", () => {
    const { container } = render(<TrendChart title="Connections" start={START} end={END} layers={[]} />);
    expect(container.querySelector(".gap-wrap")).toBeNull();
    expect(hatched(container)).toEqual([]);
  });
});
