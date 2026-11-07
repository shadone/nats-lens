// Label layout of the zoom strip, ported from the reference's drawZoomH and
// drawZoomV. Kubernetes events sit above the time axis (side -1), NATS
// advisories and finding transitions below it (side 1).
import { hm } from "./format";
import { monoWidth, type Measure } from "./laneLayout";

export type ZoomKind = "k8s" | "nats" | "lens";

export interface ZoomItem {
  id: string;
  side: -1 | 1;
  kind: ZoomKind;
  /** Time, epoch ms. */
  t: number;
  /** Labels tried in order; the last is the shortest. */
  labels: string[];
  /** Marker and label colours, as CSS values. */
  col: string;
  tcol: string;
  title: string;
}

export interface Placed {
  item: ZoomItem;
  x: number;
  tier: number;
  l: number;
  r: number;
  anchor: "start" | "end";
  tx: number;
  text: string;
}

const MIN = 60_000;
const TIER_LIMIT = 6;
const GAP = 12;
const MK = 8;

/** The window padded by three minutes and widened to whole five minutes. */
export function zoomDomain(from: number, to: number): [number, number] {
  const five = 5 * MIN;
  return [Math.floor((from - 3 * MIN) / five) * five, Math.ceil((to + 3 * MIN) / five) * five];
}

export interface HLayout {
  up: Placed[];
  down: Placed[];
  /** Labels that broke the rules because no tier had room. */
  forced: number;
  x0: number;
  x1: number;
  tiersUp: number;
  tiersDown: number;
  axisY: number;
  height: number;
  xs: (t: number) => number;
}

export const TIER = 17;

interface Candidate {
  l: number;
  r: number;
  anchor: "start" | "end";
  tx: number;
}

interface Prepared {
  item: ZoomItem;
  x: number;
  text: string;
  cands: Candidate[];
  fallback: Candidate;
}

/**
 * A label must keep GAP from labels in its own tier, must not sit across the
 * stem of a label further from the axis, and its own stem must not cross a
 * label nearer the axis.
 */
function fits(placed: Placed[], x: number, cd: { l: number; r: number }, t: number): boolean {
  return placed.every((p) => {
    if (p.tier === t) return cd.r + GAP <= p.l || cd.l >= p.r + GAP;
    if (p.tier < t) return !(x >= p.l - 3 && x <= p.r + 3);
    return !(p.x >= cd.l - 3 && p.x <= cd.r + 3);
  });
}

/** Depth-first search for a placement in T tiers; null when none fits. */
function solve(list: Prepared[], T: number): Placed[] | null {
  const placed: Placed[] = [];
  let budget = 60_000;
  const go = (i: number): boolean => {
    if (i === list.length) return true;
    if (--budget < 0) return false;
    const e = list[i];
    for (let t = 0; t < T; t++) {
      for (const cd of e.cands) {
        if (!fits(placed, e.x, cd, t)) continue;
        placed.push({ item: e.item, x: e.x, tier: t, ...cd, text: e.text });
        if (go(i + 1)) return true;
        placed.pop();
      }
    }
    return false;
  };
  return go(0) ? placed : null;
}

export function layoutHorizontal(
  items: ZoomItem[],
  W: number,
  domain: [number, number],
  measure: Measure = monoWidth,
): HLayout {
  const gut = 108;
  const x0 = gut;
  const x1 = W - 10;
  const [Z0, Z1] = domain;
  const xs = (t: number) => x0 + ((t - Z0) / (Z1 - Z0)) * (x1 - x0);
  let forced = 0;

  const prep = (side: -1 | 1, li: number): Prepared[] =>
    items
      .filter((it) => it.side === side)
      .map((it) => {
        const x = xs(it.t);
        const text = it.labels[Math.min(li, it.labels.length - 1)];
        const tw = measure(`${hm(it.t)} ${text}`, 11);
        // A label with no candidate inside the drawing cannot be placed:
        // the search fails and the greedy pass counts it as forced.
        const cands = [
          { l: x + MK, r: x + MK + tw, anchor: "start" as const, tx: x + MK },
          { l: x - MK - tw, r: x - MK, anchor: "end" as const, tx: x - MK },
        ].filter((cd) => cd.l >= x0 - 4 && cd.r <= W);
        return { item: it, x, text, cands, fallback: { l: x0, r: x0 + tw, anchor: "start" as const, tx: x0 } };
      });

  const layout = (side: -1 | 1): Placed[] => {
    const variants = Math.max(1, ...items.map((it) => it.labels.length));
    let list: Prepared[] = [];
    for (let li = 0; li < variants; li++) {
      list = prep(side, li);
      for (let T = 1; T <= TIER_LIMIT; T++) {
        const sol = solve(list, T);
        if (sol) return sol;
      }
    }
    // Greedy fallback with the shortest labels; what does not fit is forced.
    const placed: Placed[] = [];
    for (const e of list) {
      let done = false;
      for (let t = 0; t < 8 && !done; t++) {
        for (const cd of e.cands) {
          if (fits(placed, e.x, cd, t)) {
            placed.push({ item: e.item, x: e.x, tier: t, ...cd, text: e.text });
            done = true;
            break;
          }
        }
      }
      if (!done) {
        forced++;
        const tn = placed.reduce((a, p) => Math.max(a, p.tier), -1) + 1;
        placed.push({ item: e.item, x: e.x, tier: tn, ...(e.cands[0] ?? e.fallback), text: e.text });
      }
    }
    return placed;
  };

  const up = layout(-1);
  const down = layout(1);
  const tiersUp = up.reduce((a, p) => Math.max(a, p.tier), 0) + 1;
  const tiersDown = down.reduce((a, p) => Math.max(a, p.tier), 0) + 1;
  const axisY = 10 + tiersUp * TIER + 8;
  const height = axisY + 14 + tiersDown * TIER + 6;
  return { up, down, forced, x0, x1, tiersUp, tiersDown, axisY, height, xs };
}

export interface VLabel {
  item: ZoomItem;
  y0: number;
  y: number;
  text: string;
}

export interface VLayout {
  cx: number;
  top: number;
  ys: (t: number) => number;
  labels: VLabel[];
  height: number;
}

export const V_FONT = 11;
const V_OFF = 16;
const V_PPM = 7;
const V_PITCH = 14;

/**
 * The narrow variant: time runs down a spine; labels shift down when events
 * are close, and the stem marks the exact minute.
 */
export function layoutVertical(
  items: ZoomItem[],
  W: number,
  domain: [number, number],
  measure: Measure = monoWidth,
): VLayout {
  const [Z0, Z1] = domain;
  const top = 40;
  const shortest = (it: ZoomItem) => it.labels[it.labels.length - 1];
  const leftW = items
    .filter((it) => it.side < 0)
    .reduce((a, it) => Math.max(a, measure(`${hm(it.t)} ${shortest(it)}`, V_FONT)), 0);
  const cx = Math.round(Math.min(W / 2, leftW + V_OFF + 6));
  const ys = (t: number) => top + ((t - Z0) / MIN) * V_PPM;
  const labels: VLabel[] = [];
  let maxY = ys(Z1);
  for (const side of [-1, 1] as const) {
    let last = -1e9;
    const avail = side < 0 ? cx - V_OFF - 2 : W - cx - V_OFF - 2;
    for (const it of items.filter((i) => i.side === side)) {
      const y0 = ys(it.t);
      const y = Math.max(y0, last + V_PITCH);
      last = y;
      const time = `${hm(it.t)} `;
      let text = it.labels.find((l) => measure(time + l, V_FONT) <= avail) ?? shortest(it);
      while (measure(time + text, V_FONT) > avail && text.length > 4) text = `${text.slice(0, -2)}…`;
      labels.push({ item: it, y0, y, text });
      maxY = Math.max(maxY, y);
    }
  }
  return { cx, top, ys, labels, height: Math.max(maxY, ys(Z1)) + 22 };
}

export const V_OFFSET = V_OFF;
