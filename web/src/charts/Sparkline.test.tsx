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

describe("the change tone", () => {
  const steps = [5, 5, 5, 9, 9, 9].map((value, i) => ({ time: i * 1000, value }));
  const line = (c: HTMLElement) => (c.querySelector("polyline:not(.spark-change)") as SVGElement).style.stroke;
  const change = (c: HTMLElement) => (c.querySelector(".spark-change") as SVGElement).style.stroke;

  it("keeps the line neutral and the change in the accent when all is well", () => {
    const { container } = render(<Sparkline points={steps} status="ok" windows={[]} start={0} end={5000} label="Consumers" tone="change" />);
    expect(line(container)).toBe("var(--text-muted)");
    expect(change(container)).toBe("var(--accent)");
  });

  it("keeps the line neutral in trouble and draws the change in the severity colour", () => {
    const { container } = render(<Sparkline points={steps} status="critical" windows={[]} start={0} end={5000} label="Consumers" tone="change" />);
    expect(line(container)).toBe("var(--text-muted)");
    expect((container.querySelector("polygon") as SVGElement).style.fill).toBe("var(--text-muted)");
    expect(change(container)).toBe("var(--cell-critical)");
  });

  it("draws the warning marks in the darker mark amber, which reaches 3:1 on the card", () => {
    const { container } = render(<Sparkline points={steps} status="warning" windows={[]} start={0} end={5000} label="Consumers" tone="change" />);
    expect(change(container)).toBe("var(--mark-warning)");
  });

  it("lets the newest-point dot reach past the chart's right edge", () => {
    const { container } = render(<Sparkline points={steps} status="ok" windows={[]} start={0} end={5000} label="Consumers" tone="change" />);
    expect(container.querySelector("svg.spark")).toHaveClass("spark-open");
    const plain = render(<Sparkline points={steps} status="ok" windows={[]} start={0} end={5000} label="Consumers" />);
    expect(plain.container.querySelector("svg.spark")).not.toHaveClass("spark-open");
  });

  it("draws no change overlay on a noisy series, only the newest point", () => {
    // 94 of 200 segments change, scattered: more than a third.
    let v = 0;
    const noisy = Array.from({ length: 201 }, (_, i) => { if (i > 0 && i <= 188 && i % 2 === 0) v = 1 - v; return { time: i * 1000, value: v }; });
    const changed = noisy.slice(1).filter((p, i) => p.value !== noisy[i].value).length;
    expect(changed).toBe(94);
    const { container } = render(<Sparkline points={noisy} status="ok" windows={[]} start={0} end={200_000} label="Pending" tone="change" />);
    expect(container.querySelector(".spark-change")).toBeNull();
    expect(container.querySelectorAll(".spark-now")).toHaveLength(1);
  });
});

describe("the halo under the change marks", () => {
  const steps = [5, 5, 5, 9, 9, 9].map((value, i) => ({ time: i * 1000, value }));
  let v = 0;
  const noisy = Array.from({ length: 201 }, (_, i) => { if (i > 0 && i <= 188 && i % 2 === 0) v = 1 - v; return { time: i * 1000, value: v }; });
  // One bucket that dips to 0 inside a flat 5: a ticked move.
  const dip = [5, 5, 5, 5, 5, 5].map((value, i) => ({ time: i * 1000, value, ...(i === 3 ? { low: 0 } : {}) }));

  /** Each change mark with its halo; halos come in the marks' order, all of them before the first mark. */
  function pairs(c: HTMLElement, sel: string) {
    const marks = [...c.querySelectorAll(".spark-change, .spark-now, .spark-range")] as SVGElement[];
    const halos = [...c.querySelectorAll(".spark-halo")] as SVGElement[];
    expect(c.querySelectorAll(sel).length).toBeGreaterThan(0);
    expect(halos).toHaveLength(marks.length);
    expect(halos[halos.length - 1].compareDocumentPosition(marks[0]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    return marks.map((m, i) => ({ mark: m, halo: halos[i] })).filter(({ mark }) => mark.matches(sel));
  }

  function expectHalo(c: HTMLElement, sel: string) {
    for (const { mark, halo } of pairs(c, sel)) {
      expect(halo).toHaveClass("spark-halo");
      expect(halo.tagName).toBe(mark.tagName);
      expect(halo.style.stroke).toBe("var(--spark-halo, var(--surface))");
      expect(Number(halo.getAttribute("stroke-width"))).toBe(Number(mark.getAttribute("stroke-width")) + 3);
      for (const a of ["points", "d", "x1", "x2", "y1", "y2", "stroke-linecap", "stroke-linejoin", "vector-effect"]) {
        expect(halo.getAttribute(a)).toBe(mark.getAttribute(a));
      }
    }
  }

  it("draws a halo before each changed segment", () => {
    const { container } = render(<Sparkline points={steps} status="ok" windows={[]} start={0} end={5000} label="Consumers" tone="change" />);
    expectHalo(container, ".spark-change");
  });

  it("draws a halo before the newest-point dot", () => {
    const { container } = render(<Sparkline points={noisy} status="ok" windows={[]} start={0} end={200_000} label="Pending" tone="change" />);
    expectHalo(container, ".spark-now");
  });

  it("draws a halo before each range tick", () => {
    const { container } = render(<Sparkline points={dip} status="warning" windows={[]} start={0} end={5000} label="Clients" tone="change" />);
    expectHalo(container, ".spark-range");
    expect((container.querySelector(".spark-range") as SVGElement).style.stroke).toBe("var(--mark-warning)");
  });

  it("draws every halo under every mark when a tile has both segments and ticks", () => {
    const both = [5, 5, 5, 9, 9, 9, 9, 9].map((value, i) => ({ time: i * 1000, value, ...(i === 6 ? { low: 0 } : {}) }));
    const { container } = render(<Sparkline points={both} status="critical" windows={[]} start={0} end={7000} label="Consumers" tone="change" />);
    expect(container.querySelectorAll(".spark-change").length).toBeGreaterThan(0);
    expectHalo(container, ".spark-change");
    expectHalo(container, ".spark-range");
  });

  it("draws no halo in the status tone", () => {
    const { container } = render(<Sparkline points={steps} status="warning" windows={[]} start={0} end={5000} label="Consumers" />);
    expect(container.querySelector(".spark-halo")).toBeNull();
  });
});
