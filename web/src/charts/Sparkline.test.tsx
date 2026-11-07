import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HistoryContext } from "../components/HistoryNote";
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

  it("draws the newest-point dot as a path of tiny but nonzero length", () => {
    // WebKit stretches the round cap of a zero-length subpath with the
    // viewBox (a 24x6px dot on a chart 1000px wide); a nonzero length keeps it round.
    let v = 0;
    const noisy = Array.from({ length: 201 }, (_, i) => { if (i > 0 && i <= 188 && i % 2 === 0) v = 1 - v; return { time: i * 1000, value: v }; });
    const { container } = render(<Sparkline points={noisy} status="ok" windows={[]} start={0} end={200_000} label="Pending" tone="change" />);
    const d = container.querySelector(".spark-now")!.getAttribute("d")!;
    const m = /^M200\.0,[\d.]+h([\d.]+)$/.exec(d);
    expect(m).not.toBeNull();
    expect(Number(m![1])).toBeGreaterThan(0);
    // At most a tenth of a pixel on a chart 2000px wide.
    expect(Number(m![1])).toBeLessThanOrEqual(0.01);
  });
});

describe("the cut in the line around the change marks", () => {
  const steps = [5, 5, 5, 9, 9, 9].map((value, i) => ({ time: i * 1000, value }));
  let v = 0;
  const noisy = Array.from({ length: 201 }, (_, i) => { if (i > 0 && i <= 188 && i % 2 === 0) v = 1 - v; return { time: i * 1000, value: v }; });
  // One bucket that dips to 0 inside a flat 5: a ticked move.
  const dip = [5, 5, 5, 5, 5, 5].map((value, i) => ({ time: i * 1000, value, ...(i === 3 ? { low: 0 } : {}) }));
  const MARKS = ".spark-change, .spark-now, .spark-range";

  /** Whether an element or one of its ancestors inside the chart has a mask. */
  const masked = (el: Element) => { for (let e: Element | null = el; e && e.tagName !== "svg"; e = e.parentElement) if (e.hasAttribute("mask")) return true; return false; };
  /** The grey line: the solid polylines outside the mask that are not marks. */
  const lines = (c: HTMLElement) => [...c.querySelectorAll("svg.spark polyline")].filter((p) => !p.closest("mask") && !p.matches(".spark-change") && !p.hasAttribute("stroke-dasharray"));

  /** Checks the mask holds a cut for every mark of the selector, and the line uses the mask. */
  function expectCut(c: HTMLElement, sel: string) {
    const mask = c.querySelector("svg.spark mask")!;
    expect(mask).not.toBeNull();
    expect(mask.getAttribute("maskUnits")).toBe("userSpaceOnUse");
    const keep = mask.querySelector("rect")!;
    expect(keep.getAttribute("fill")).toBe("white");
    expect(Number(keep.getAttribute("x"))).toBeLessThan(0);
    expect(Number(keep.getAttribute("width"))).toBeGreaterThan(200);
    const marks = [...c.querySelectorAll(MARKS)].filter((m) => !m.closest("mask")) as SVGElement[];
    const cuts = [...mask.querySelectorAll(".spark-cut")] as SVGElement[];
    expect(cuts).toHaveLength(marks.length);
    const chosen = marks.map((m, i) => ({ mark: m, cut: cuts[i] })).filter(({ mark }) => mark.matches(sel));
    expect(chosen.length).toBeGreaterThan(0);
    for (const { mark, cut } of chosen) {
      expect(masked(mark)).toBe(false);
      expect(cut.tagName).toBe(mark.tagName);
      expect(cut.style.stroke).toBe("black");
      expect(Number(cut.getAttribute("stroke-width"))).toBe(Number(mark.getAttribute("stroke-width")) + 3);
      for (const a of ["points", "d", "x1", "x2", "y1", "y2", "stroke-linecap", "stroke-linejoin", "vector-effect"]) {
        expect(cut.getAttribute(a)).toBe(mark.getAttribute(a));
      }
    }
    const ls = lines(c);
    expect(ls.length).toBeGreaterThan(0);
    for (const l of ls) expect(l.getAttribute("mask")).toBe(`url(#${mask.id})`);
    expect(mask.id).toMatch(/^[A-Za-z0-9_-]+$/);
  }

  it("cuts the line around each changed segment", () => {
    const { container } = render(<Sparkline points={steps} status="ok" windows={[]} start={0} end={5000} label="Consumers" tone="change" />);
    expectCut(container, ".spark-change");
  });

  it("cuts the line around the newest-point dot", () => {
    const { container } = render(<Sparkline points={noisy} status="ok" windows={[]} start={0} end={200_000} label="Pending" tone="change" />);
    expectCut(container, ".spark-now");
  });

  it("cuts the line around each range tick", () => {
    const { container } = render(<Sparkline points={dip} status="warning" windows={[]} start={0} end={5000} label="Clients" tone="change" />);
    expectCut(container, ".spark-range");
    expect((container.querySelector(".spark-range") as SVGElement).style.stroke).toBe("var(--mark-warning)");
  });

  it("cuts around both segments and ticks on one tile", () => {
    const both = [5, 5, 5, 9, 9, 9, 9, 9].map((value, i) => ({ time: i * 1000, value, ...(i === 6 ? { low: 0 } : {}) }));
    const { container } = render(<Sparkline points={both} status="critical" windows={[]} start={0} end={7000} label="Consumers" tone="change" />);
    expectCut(container, ".spark-change");
    expectCut(container, ".spark-range");
  });

  it("leaves the gap hatch, the windows, the fill and the second series whole", () => {
    const h = { start: 0, end: 5000, live: false, gaps: [], serverGaps: [{ start: 3200, end: 3400, reason: "Server did not answer" }] };
    const { container } = render(<HistoryContext.Provider value={h}>
      <Sparkline points={steps} points2={steps.map((p) => ({ ...p, value: p.value / 2 }))} status="ok" windows={[{ start: 2500, end: 3500, label: "Upgrade", detail: "" }]} start={0} end={5000} label="Consumers" tone="change" />
    </HistoryContext.Provider>);
    expectCut(container, ".spark-change");
    const hatch = container.querySelector(".dh-chart-server-gap")!;
    const window = [...container.querySelectorAll("svg.spark rect")].find((r) => (r as SVGElement).style.fill === "var(--upgrade-fill)")!;
    const dashed = container.querySelector("svg.spark polyline[stroke-dasharray]")!;
    const fill = container.querySelector("svg.spark polygon")!;
    for (const el of [hatch, window, dashed, fill]) {
      expect(el).not.toBeNull();
      expect(masked(el)).toBe(false);
    }
  });

  it("draws no mask in the status tone", () => {
    const { container } = render(<Sparkline points={steps} status="warning" windows={[]} start={0} end={5000} label="Consumers" />);
    expect(container.querySelector("mask")).toBeNull();
    expect(container.querySelector("[mask]")).toBeNull();
  });

  it("draws no mask on a change chart without marks", () => {
    const flat = [5, 5, 5, 5].map((value, i) => ({ time: i * 1000, value }));
    const { container } = render(<Sparkline points={flat} status="ok" windows={[]} start={0} end={3000} label="Streams" tone="change" />);
    expect(container.querySelector(MARKS)).toBeNull();
    expect(container.querySelector("mask")).toBeNull();
    expect(container.querySelector("[mask]")).toBeNull();
  });
});
