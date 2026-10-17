import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Sparkline } from "../charts/Sparkline";
import { FleetChart } from "./FleetChart";

const zeros = [0, 0, 0].map((value, i) => ({ time: i * 1000, value }));

/** The y of every vertex of the drawn line. */
function ys(svg: Element, selector: string): number[] {
  const pts = svg.querySelector(selector)!.getAttribute("points")!.split(" ");
  return pts.map((p) => Number(p.split(",")[1]));
}

describe("a series that stays at 0", () => {
  it("is a flat line on the baseline in a metric box chart", () => {
    const { container } = render(<Sparkline points={zeros} status="ok" windows={[]} start={0} end={2000} label="Slow consumers" />);
    const svg = container.querySelector("svg")!;
    expect(new Set(ys(svg, "polyline"))).toEqual(new Set([37]));
    expect(svg.querySelector("polygon")).toBeNull();
  });

  it("is a flat line on the baseline in a chart across servers, with no filled band", () => {
    const band = zeros.map((p) => ({ ...p, low: 0, high: 0 }));
    const { container } = render(<FleetChart title="CPU" band={band} lines={[{ points: zeros, mark: "median" }]} legend={[]} start={0} end={2000} />);
    const svg = container.querySelector("svg")!;
    expect(new Set(ys(svg, "polyline"))).toEqual(new Set([81]));
    expect(svg.querySelector("polygon")).toBeNull();
  });
});
