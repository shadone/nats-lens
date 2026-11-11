// Pure helpers of the status timeline: axis ticks, runs of cells for the
// table view, lane annotations and zoom strip items.
import type { LaneEvent, Level, Timeline, Window } from "../api/model";
import { dayMon, hm } from "./format";
import type { LaneAnnotation } from "./laneLayout";
import type { ZoomItem } from "./zoomLayout";

export interface Tick {
  /** Position along the track, 0 to 1. */
  frac: number;
  label: string;
  edge?: boolean;
}

const STEPS = [300_000, 600_000, 900_000, 1_800_000, 3_600_000, 10_800_000, 21_600_000, 43_200_000, 86_400_000];

/** Width of one character of an axis label, 10.5 px monospace, in pixels. */
const CHAR_PX = 6.5;
/** Space between two labels, in pixels. */
const LABEL_GAP_PX = 10;

/**
 * Labels at whole UTC steps, about eight across the track, and "now" (or
 * the end time) at the right edge. Given the track's width in pixels, it
 * drops labels so that each keeps clear of its neighbours and of the edge
 * label, as on a phone.
 */
export function axisTicks(start: number, end: number, live: boolean, widthPx?: number): Tick[] {
  const span = end - start;
  if (span <= 0) return [];
  const step = STEPS.find((s) => span / s <= 9) ?? STEPS[STEPS.length - 1];
  const ticks: Tick[] = [];
  const minGap = span * 0.06;
  for (let t = Math.ceil(start / step) * step; t < end - minGap; t += step) {
    if (t - start < span * 0.015) continue;
    ticks.push({ frac: (t - start) / span, label: step >= 86_400_000 ? dayMon(t) : hm(t) });
  }
  const edge: Tick = { frac: 1, label: live ? "now" : hm(end), edge: true };
  if (!widthPx) return [...ticks, edge];
  // A label is centred on its tick; the edge label ends at the right edge.
  const half = (t: Tick) => (t.label.length * CHAR_PX) / 2;
  const limit = widthPx - edge.label.length * CHAR_PX - LABEL_GAP_PX;
  const kept: Tick[] = [];
  for (const t of ticks) {
    const x = t.frac * widthPx;
    const prev = kept[kept.length - 1];
    if (x + half(t) > limit || x - half(t) < 0) continue;
    if (prev && x - half(t) < prev.frac * widthPx + half(prev) + LABEL_GAP_PX) continue;
    kept.push(t);
  }
  return [...kept, edge];
}

export interface CellRun {
  level: Level;
  from: number;
  to: number;
}

/** Consecutive cells with the same level other than ok, as time spans. */
export function cellRuns(cells: Level[], start: number, bucketMs: number): CellRun[] {
  const runs: CellRun[] = [];
  cells.forEach((lv, i) => {
    if (lv === "ok") return;
    const last = runs[runs.length - 1];
    const from = start + i * bucketMs;
    if (last && last.level === lv && last.to === from) last.to = from + bucketMs;
    else runs.push({ level: lv, from, to: from + bucketMs });
  });
  return runs;
}

/** The runs as text; fmt writes the times (hm by default, a date too for longer spans). */
export function runsText(runs: CellRun[], fmt: (ms: number) => string = hm): string {
  if (!runs.length) return "ok throughout";
  return runs.map((r) => `${r.level === "nodata" ? "no data" : r.level} ${fmt(r.from)}–${fmt(r.to)}`).join(", ");
}

/** The window the zoom strip shows: the newest one. */
export function zoomWindow(windows: Window[]): Window | undefined {
  return windows.length ? windows[windows.length - 1] : undefined;
}

/** Whether a Kubernetes lane item is a deployment rollout (k8s.deployment.*). */
export function isDeployment(e: LaneEvent): boolean {
  return e.kind.replace(/^k8s\./, "").startsWith("deployment.");
}

/** Annotations of the Kubernetes lane: each window and each deployment. */
export function laneAnnotations(tl: Timeline, xOf: (t: number) => number): LaneAnnotation[] {
  const k8s = tl.kubernetes;
  const out: LaneAnnotation[] = tl.windows.map((w) => ({
    x0: xOf(w.start),
    x1: xOf(w.end),
    a: `${w.label} ${hm(w.start)}–${hm(w.end)}`,
    b: w.detail,
  }));
  for (const e of k8s.filter(isDeployment)) {
    out.push({ x0: xOf(e.time), x1: xOf(e.time), a: `deploy ${hm(e.time)}`, b: e.label.split(" ")[1] ?? e.shortLabel, preferRight: true });
  }
  return out;
}

/**
 * A lane label without its tail, as the reference writes it:
 * "STREAM.QUORUM_LOST media-windows (1 of 3 peers)" becomes
 * "STREAM.QUORUM_LOST media-windows", and "QUORUM_LOST consumer
 * x on y" becomes "QUORUM_LOST x".
 */
export function mediumLabel(label: string): string {
  return label.split(/ on | by | \(|, /)[0].replace(/^(\S+) consumer /, "$1 ");
}

/** "node 7f3a drained" for a node event, whose label names the full node. */
function kubeMedium(e: LaneEvent): string {
  // Kubernetes kinds carry a k8s. prefix: k8s.node.drained.
  const kind = e.kind.replace(/^k8s\./, "");
  return kind.startsWith("node.") ? `${e.shortLabel} ${kind.split(".")[1]}` : mediumLabel(e.label);
}

function finding(e: LaneEvent): string {
  return `${e.shortLabel.split(" ")[0]} ${e.kind.split(".").pop()}`;
}

function zoomItem(e: LaneEvent): ZoomItem {
  if (e.source === "kubernetes") {
    const labels = [...new Set([kubeMedium(e), e.shortLabel])];
    return { events:[e], id: e.id, side: -1, kind: "k8s", t: e.time, labels, col: "var(--k8s)", tcol: "var(--text-2)", title: e.label };
  }
  if (e.source === "finding") {
    const resolved = e.kind.endsWith("resolved");
    const crit = e.severity === "critical";
    return {
      events:[e],
      id: e.id,
      side: 1,
      kind: "lens",
      t: e.time,
      labels: [finding(e)],
      col: resolved ? "var(--accent)" : crit ? "var(--sev-critical)" : "var(--sev-warning)",
      tcol: resolved ? "var(--accent-text)" : crit ? "var(--sev-critical)" : "var(--sev-warning)",
      title: e.label,
    };
  }
  const crit = e.severity === "critical";
  const labels = [...new Set([mediumLabel(e.label), e.shortLabel])];
  const col = crit ? "var(--sev-critical)" : "var(--text-2)";
  return { events:[e], id: e.id, side: 1, kind: "nats", t: e.time, labels, col, tcol: col, title: e.label };
}

/** Items inside the domain, oldest first. */
export function zoomItems(tl: Timeline, domain: [number, number]): ZoomItem[] {
  return [...tl.kubernetes, ...tl.nats, ...(tl.servers ?? []), ...(tl.leaders ?? [])]
    .filter((e) => e.time >= domain[0] && e.time <= domain[1])
    .map(zoomItem)
    .sort((a, b) => a.t - b.t);
}
