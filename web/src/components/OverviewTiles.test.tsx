import { renderApp as render } from "../test/render";
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Point, Tile } from "../api/model";
import { OverviewTiles, tileStatus, tileText } from "./OverviewTiles";

const START = Date.parse("2026-10-16T14:30:00Z");
const END = START + 86_400_000;

function tile(extra: Partial<Tile>): Tile {
  return { id: "servers", label: "Servers", unit: "count", valueSuffix: "", value2Suffix: "", subtitle: "", status: "ok", description: "Servers that answered.", ...extra };
}

const series = (vals: number[], min: number, max: number) => ({
  metric: "cluster.servers_up",
  unit: "count" as const,
  stepMs: 900_000,
  points: vals.map((v, i): Point => ({ time: START + i * 900_000, value: v })),
  min,
  max,
});

describe("OverviewTiles", () => {
  it("shows a tile without a value as a dash with no dot and no zero", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: undefined, series: series([2, 3], 2, 3) })]} windows={[]} start={START} end={END} />);
    expect(screen.getByText("–")).toBeInTheDocument();
    expect(screen.queryByText("0")).toBeNull();
    expect(container.querySelector(".mb-dot")).toBeNull();
    expect(screen.getByText("no current value")).toBeInTheDocument();
  });

  it("puts the dot at 50% for the midpoint of the window", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: 15, series: series([10, 20, 15], 10, 20) })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector<HTMLElement>(".mb-dot")!.style.left).toBe("50%");
  });

  it("says steady at 3 all window when min equals max", () => {
    render(<OverviewTiles tiles={[tile({ value: 3, valueSuffix: "/ 3", series: series([3, 3], 3, 3) })]} windows={[]} start={START} end={END} />);
    expect(screen.getByText("steady at 3 all window")).toBeInTheDocument();
    expect(screen.getByText("3 / 3")).toBeInTheDocument();
  });

  it("names the meta leader, and puts a limit in the note rather than the value", () => {
    render(
      <OverviewTiles
        tiles={[
          tile({ id: "meta_leader", label: "Meta leader", value: 0, valueText: "nats-1", subtitle: "0 leader changes in range" }),
          tile({ id: "storage", label: "Storage", unit: "bytes", value: 768 * 1048576, valueSuffix: "of 150 GiB", limit: 150 * 1073741824, subtitle: "all replicas · 0.5% used" }),
        ]}
        windows={[]}
        start={START}
        end={END}
      />,
    );
    expect(screen.getByText("nats-1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "About Meta leader" }).closest(".mbox")!.querySelector(".mb-range, .mb-track-note")).toBeNull();
    expect(screen.getByText("768 MiB")).toBeInTheDocument();
    expect(screen.getByText("all replicas · 0.5% used · limit 150 GiB")).toBeInTheDocument();
  });

  it("shows the description behind the (i) button", () => {
    render(<OverviewTiles tiles={[tile({ value: 3 })]} windows={[]} start={START} end={END} />);
    expect(screen.getByRole("button", { name: "About Servers" })).toBeInTheDocument();
  });
});

describe("tileText", () => {
  it("joins a second value", () => {
    expect(tileText(tile({ value: 61, value2: 63, valueSuffix: "in", value2Suffix: "out" }))).toBe("61 in, 63 out");
  });
});

describe("tileStatus", () => {
  it("reads a tile with a value and no check behind it as ok", () => {
    expect(tileStatus(tile({ id: "accounts", value: 3, status: "nodata" }))).toBe("ok");
    expect(tileStatus(tile({ id: "accounts", value: undefined, status: "nodata" }))).toBe("nodata");
    expect(tileStatus(tile({ id: "streams", value: 3, status: "warning" }))).toBe("warning");
  });
});

it("leaves out captions that repeat the section's note", () => {
  render(<OverviewTiles tiles={[tile({ value: 3 }), tile({ id: "meta_leader", label: "Meta leader", valueText: "nats-1" }), tile({ id: "storage", label: "Storage", unit: "bytes", value: 40 })]} windows={[]} start={START} end={END} />);
  expect(screen.queryByText("Latest count bucket")).toBeNull();
  expect(screen.queryByText(/Newest leader/)).toBeNull();
  expect(screen.queryByText("Newest storage reports")).toBeNull();
});

it("never presents a leader-change total as the meta leader when no majority names one", () => {
  const { container } = render(<OverviewTiles tiles={[tile({ id: "meta_leader", label: "Meta leader", value: 2, subtitle: "2 leader changes in range", series: series([0, 2], 0, 2) })]} windows={[]} start={START} end={END} />);
  expect(screen.getByText("Unknown")).toBeInTheDocument();
  expect(screen.getByText("2 leader changes in range")).toBeInTheDocument();
  expect(container.querySelector(".mb-range")).toBeNull();
});

describe("charts only where something changed", () => {
  const steady = series(Array.from({ length: 96 }, () => 3), 3, 3);
  const dip = series(Array.from({ length: 96 }, (_, i) => (i === 40 ? 2 : 3)), 2, 3);

  it("draws no chart for a steady tile, and keeps its number and steady line", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, series: steady })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector("svg.spark")).toBeNull();
    expect(container.querySelector(".mbox.mb-quiet")).not.toBeNull();
    expect(screen.getByText("steady at 3 all window")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("draws the chart when a value changed in the range", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, series: dip })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector("svg.spark")).not.toBeNull();
    expect(container.querySelector(".mbox.mb-quiet")).toBeNull();
  });

  it("keeps the chart of a steady tile that a critical finding colours", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, status: "critical", series: steady })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector("svg.spark")).not.toBeNull();
  });

  it("keeps the chart when buckets are missing", () => {
    const short = { ...steady, points: steady.points.slice(0, 60) };
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, series: short })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector("svg.spark")).not.toBeNull();
  });

  it("draws the meta leader's chart only when the leader changed", () => {
    const none = series(Array.from({ length: 96 }, () => 0), 0, 0);
    const two = series(Array.from({ length: 96 }, (_, i) => (i === 70 ? 2 : 0)), 0, 2);
    const { container } = render(
      <OverviewTiles
        tiles={[
          tile({ id: "meta_leader", label: "Meta leader", value: 0, valueText: "nats-1", subtitle: "0 leader changes in range", series: none }),
          tile({ id: "meta_leader", label: "Meta leader 2", value: 2, valueText: "nats-0", subtitle: "2 leader changes in range", series: two }),
        ]}
        windows={[]}
        start={START}
        end={END}
      />,
    );
    const boxes = container.querySelectorAll(".mbox");
    expect(boxes[0].querySelector("svg.spark")).toBeNull();
    expect(boxes[1].querySelector("svg.spark")).not.toBeNull();
    expect(screen.getByText("Leader changes per bucket")).toBeInTheDocument();
  });

  it("marks a slope that changes in most buckets with one dot on the newest point, not an accent line", () => {
    const slope = series(Array.from({ length: 96 }, (_, i) => 100 + i), 100, 195);
    const { container } = render(<OverviewTiles tiles={[tile({ id: "storage", label: "Storage", unit: "bytes", value: 195, series: slope })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector(".spark-change")).toBeNull();
    expect(container.querySelectorAll(".spark-now")).toHaveLength(1);
  });

  it("draws a dip inside one bucket as a tick from its lowest to its highest", () => {
    const s = series(Array.from({ length: 96 }, () => 3), 3, 3);
    s.points[40] = { ...s.points[40], low: 2, high: 3 };
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, series: s })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector("svg.spark")).not.toBeNull();
    expect(container.querySelectorAll(".spark-range")).toHaveLength(1);
    // The track takes in the bucket's low, so it does not read steady.
    expect(screen.queryByText(/steady at/)).toBeNull();
    expect(container.querySelector(".mb-ends")!.textContent).toBe("23");
  });

  it("ticks only a move the line hides, not a dip whose neighbours touch it", () => {
    const s = series(Array.from({ length: 96 }, () => 3), 2, 3);
    s.points[39] = { ...s.points[39], low: 2, high: 3 };
    s.points[40] = { ...s.points[40], value: 2, low: 2, high: 3 };
    s.points[41] = { ...s.points[41], low: 2, high: 3 };
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, series: s })]} windows={[]} start={START} end={END} />);
    expect(container.querySelectorAll(".spark-range")).toHaveLength(0);
  });

  it("draws an ok tile's line in the neutral colour with the accent only on the change", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, series: dip })]} windows={[]} start={START} end={END} />);
    const lines = [...container.querySelectorAll<SVGPolylineElement>("svg.spark polyline")];
    expect(lines[0].style.stroke).toBe("var(--text-muted)");
    expect(lines.filter((l) => l.style.stroke === "var(--accent)")).toHaveLength(1);
  });

  it("keeps the severity colour on a tile in trouble", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: 3, status: "critical", series: dip })]} windows={[]} start={START} end={END} />);
    const lines = [...container.querySelectorAll<SVGPolylineElement>("svg.spark polyline")];
    expect(lines.every((l) => l.style.stroke === "var(--sev-critical)")).toBe(true);
  });
});
