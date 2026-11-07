import { describe, expect, it } from "vitest";
import { monoWidth, placeLaneAnnotations } from "./laneLayout";
import { V_FONT, V_OFFSET, layoutHorizontal, layoutVertical, zoomDomain, type Placed, type ZoomItem } from "./zoomLayout";

const MIN = 60_000;
const T0 = Date.parse("2026-10-17T09:10:00Z");

function item(id: string, minute: number, side: -1 | 1, labels: string[]): ZoomItem {
  return { id, side, kind: side < 0 ? "k8s" : "nats", t: T0 + minute * MIN, labels, col: "red", tcol: "red", title: labels[0] };
}

/** Two labels in one tier overlap when their boxes come closer than the gap. */
function overlaps(list: Placed[], gap: number): string[] {
  const bad: string[] = [];
  for (const a of list)
    for (const b of list)
      if (a !== b && a.tier === b.tier && a.l < b.r + gap && b.l < a.r + gap) bad.push(`${a.item.id}/${b.item.id}`);
  return bad;
}

describe("monoWidth", () => {
  it("advances 0.6 em per glyph", () => {
    expect(monoWidth("09:31", 10)).toBe(30);
  });
});

describe("placeLaneAnnotations", () => {
  const ann = (x0: number, x1: number, a: string, preferRight = false) => ({ x0, x1, a, b: "", preferRight });

  it("puts an annotation left of its span when there is room", () => {
    const [p] = placeLaneAnnotations([ann(500, 540, "node upgrade")], 0, 1000);
    expect(p.align).toBe("right");
    expect(p.r).toBe(500);
  });

  it("moves to the right side when the left would leave the visible part", () => {
    const [p] = placeLaneAnnotations([ann(20, 40, "node upgrade")], 0, 1000);
    expect(p.align).toBe("left");
    expect(p.l).toBe(40);
  });

  it("keeps a second annotation clear of the first, and drops one with no room", () => {
    const placed = placeLaneAnnotations([ann(500, 540, "node upgrade 09:14-09:50"), ann(545, 545, "deploy 11:02", true), ann(530, 530, "x".repeat(40))], 0, 1000);
    expect(placed).toHaveLength(2);
    const [a, b] = placed;
    expect(b.l >= a.r + 10 || b.r + 10 <= a.l).toBe(true);
  });

  it("only uses the visible part of a scrolled lane", () => {
    expect(placeLaneAnnotations([ann(100, 120, "node upgrade")], 600, 1000)).toHaveLength(0);
  });
});

describe("zoomDomain", () => {
  it("pads by three minutes and rounds out to five", () => {
    const [a, b] = zoomDomain(Date.parse("2026-10-17T09:14:00Z"), Date.parse("2026-10-17T09:50:00Z"));
    expect(new Date(a).toISOString()).toBe("2026-10-17T09:10:00.000Z");
    expect(new Date(b).toISOString()).toBe("2026-10-17T09:55:00.000Z");
  });
});

describe("layoutHorizontal", () => {
  const domain: [number, number] = [T0, T0 + 45 * MIN];

  it("places close events on separate tiers without overlap", () => {
    const items = [item("a", 6, 1, ["STREAM.QUORUM_LOST media-windows"]), item("b", 20, 1, ["LEADER_ELECTED meta leader"]), item("c", 20.5, 1, ["QUORUM_LOST job-cache-projector"]), item("d", 21, 1, ["CONSUMER.DELETED analytics-aggregator"])];
    const L = layoutHorizontal(items, 1200, domain);
    expect(L.forced).toBe(0);
    expect(L.down).toHaveLength(4);
    expect(overlaps(L.down, 12)).toEqual([]);
    expect(L.tiersDown).toBeGreaterThan(1);
  });

  it("keeps each label inside the drawing", () => {
    const L = layoutHorizontal([item("a", 44, -1, ["pool-1 upgrade complete"])], 900, domain);
    expect(L.up[0].r).toBeLessThanOrEqual(900);
    expect(L.up[0].anchor).toBe("end");
  });

  it("falls back to the short labels before forcing a layout", () => {
    const long = (id: string, m: number) => item(id, m, -1, [`node gke-example-pool-1-${id}-very-long-name drained`, `node ${id}`]);
    const items = [0, 1, 2, 3, 4, 5, 6, 7].map((i) => long(String(i), 10 + i));
    const L = layoutHorizontal(items, 700, domain);
    expect(L.forced).toBe(0);
    expect(L.up.every((p) => p.text.startsWith("node ") && p.text.length < 10)).toBe(true);
    expect(overlaps(L.up, 12)).toEqual([]);
  });

  it("counts labels that cannot be placed under the rules", () => {
    const items = Array.from({ length: 40 }, (_, i) => item(`n${i}`, 20 + i * 0.05, 1, ["QUORUM_LOST a-rather-long-consumer-name"]));
    expect(layoutHorizontal(items, 640, domain).forced).toBeGreaterThan(0);
  });
});

describe("layoutVertical", () => {
  it("shifts close labels down by at least the pitch and shortens to fit", () => {
    const items = [item("a", 20, 1, ["CONSUMER.DELETED analytics-aggregator on order-archive", "CONSUMER.DELETED"]), item("b", 20, 1, ["QUORUM_LOST x", "QUORUM_LOST"])];
    const L = layoutVertical(items, 300, [T0, T0 + 45 * MIN]);
    const [a, b] = L.labels;
    expect(b.y - a.y).toBeGreaterThanOrEqual(14);
    expect(b.y0).toBe(a.y0);
    expect(a.text).toBe("CONSUMER.DELETED");
  });
});

describe("layoutHorizontal bounds", () => {
  it("keeps every label inside the drawing unless it reports a forced layout", () => {
    const bad: string[] = [];
    for (const W of [320, 358, 500, 800, 1200])
      for (const len of [10, 40, 90]) {
        const items = Array.from({ length: 14 }, (_, i) => item(`i${i}`, (i * 7) % 50, i % 2 ? 1 : -1, ["STREAM.LEADER_ELECTED ".repeat(5).slice(0, len), "LEADER"]));
        const L = layoutHorizontal(items, W, zoomDomain(T0, T0 + 50 * MIN));
        if (L.forced) continue;
        for (const p of [...L.up, ...L.down]) if (p.l < 0 || p.r > W) bad.push(`W=${W} len=${len} ${p.item.id}`);
      }
    expect(bad).toEqual([]);
  });

  it("counts a label wider than the drawing as forced", () => {
    const L = layoutHorizontal([item("a", 20, 1, ["X".repeat(120)])], 640, [T0, T0 + 45 * MIN]);
    expect(L.forced).toBe(1);
  });
});

describe("vertical zoom strip spacing", () => {
  it("keeps the spine's centred start and end times clear of the labels beside it", () => {
    // The end time is drawn centred on the spine; labels start V_OFFSET from it.
    expect(monoWidth("00:00", V_FONT) / 2 + 1).toBeLessThanOrEqual(V_OFFSET);
  });
});
