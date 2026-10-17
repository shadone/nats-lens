import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Sparkline } from "./Sparkline";

const zeros = [0, 0, 0].map((value, i) => ({ time: i * 1000, value }));

describe("a series that stays at 0", () => {
  it("is a flat line on the baseline in a metric box chart", () => {
    const { container } = render(<Sparkline points={zeros} status="ok" windows={[]} start={0} end={2000} label="Slow consumers" />);
    const svg = container.querySelector("svg")!;
    const ys = svg.querySelector("polyline")!.getAttribute("points")!.split(" ").map((p) => Number(p.split(",")[1]));
    expect(new Set(ys)).toEqual(new Set([37]));
    expect(svg.querySelector("polygon")).toBeNull();
  });
});
