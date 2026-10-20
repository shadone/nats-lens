// The Topology page's wording and map layout, apart from React so they can
// be tested on their own.
import type { Topology, TopologyCluster, TopologyGateway } from "../api/topology";
import { fmtDuration, fmtInt, fmtShortDuration } from "./format";

/** The board's layout holds at most this many clusters in a row. */
export const MAP_MAX_CLUSTERS = 4;

/** The narrowest gap that holds "gateway 166 ms" on one line, in pixels. */
export const MAP_MIN_GAP = 116;

/** The narrowest cluster box of the row, in pixels: two server chips side by side. */
export const MAP_MIN_BOX = 150;

/**
 * The row's columns in fractions: a box, then the gap a gateway label sits
 * in, 4:3 so a 3-cluster map at a 1440 px window holds "gateway 84 ms ·
 * 1 drop/h" on one line. One or two clusters are laid out as three, so
 * their boxes keep the board's width.
 */
export function rowFractions(clusters: number): { box: number; gap: number; slots: number } {
  return { box: 4, gap: 3, slots: Math.max(clusters, 3) };
}

/** Width of a 12 px IBM Plex Mono character, and the label's side padding, in pixels. */
const CHAR_W = 7.25;
const LABEL_PAD = 8;

/** The width of a gateway label of this text on one line, in pixels. */
export function labelWidth(text: string): number {
  return text.length * CHAR_W + LABEL_PAD;
}

/**
 * Whether the map draws the board's row of boxes in a map of this inner
 * width: at most four clusters, each gap wide enough for a label and each
 * box for two chips. Width 0 is not yet measured.
 */
export function rowLayout(clusters: number, width: number): boolean {
  if (clusters > MAP_MAX_CLUSTERS) return false;
  if (width === 0) return true;
  const f = rowFractions(clusters);
  const unit = width / (f.slots * f.box + (f.slots - 1) * f.gap);
  return unit * f.gap >= MAP_MIN_GAP && unit * f.box >= MAP_MIN_BOX;
}

/** The name a cluster is shown by; one whose servers report no cluster name is the unknown cluster. */
export function clusterLabel(name: string): string {
  return name || "unknown cluster";
}

function plural(n: number, one: string, many = `${one}s`): string {
  return `${fmtInt(n)} ${n === 1 ? one : many}`;
}

/** Whether no connection of the pair is up though some are expected. */
export function gatewayDown(g: TopologyGateway): boolean {
  return g.expected > 0 && g.connections === 0;
}

/** "3 clusters joined by gateways, 30 leaf node sites at the edge" */
export function summary(t: Topology): string {
  const parts: string[] = [];
  const n = t.clusters.length;
  if (n === 1) {
    const c = t.clusters[0];
    parts.push(`One cluster${c.name ? `, ${c.name},` : ""} of ${plural(c.servers.length, "server")}`);
  } else if (n > 1) {
    const down = t.gateways.filter(gatewayDown).length;
    if (t.gateways.length === 0) parts.push(plural(n, "cluster"));
    else if (down === t.gateways.length) parts.push(`${plural(n, "cluster")}, every gateway pair down`);
    else {
      parts.push(`${plural(n, "cluster")} joined by gateways`);
      if (down > 0) parts.push(`${plural(down, "gateway pair")} down`);
    }
  }
  if (t.leafnodesTotal > 0) parts.push(`${plural(t.leafnodesTotal, "leaf node site")} at the edge`);
  return parts.join(", ");
}

/** The servers of a cluster whose gateway connection dropped in the last hour. */
export function droppingServers(gateways: TopologyGateway[]): Set<string> {
  return new Set(gateways.filter((g) => (g.drops1h ?? 0) > 0).flatMap((g) => g.droppingServers));
}

function other(g: TopologyGateway, c: TopologyCluster): string {
  return clusterLabel(g.a === c.name ? g.b : g.a);
}

function touches(g: TopologyGateway, c: TopologyCluster): boolean {
  return g.a === c.name || g.b === c.name;
}

/** The other cluster of each pair whose gateway from c dropped in the last hour. */
function droppingTo(c: TopologyCluster, gateways: TopologyGateway[]): string[] {
  const names = new Set(c.servers.map((s) => s.name));
  return gateways
    .filter((g) => (g.drops1h ?? 0) > 0 && touches(g, c) && !gatewayDown(g) && g.droppingServers.some((s) => names.has(s)))
    .map((g) => other(g, c));
}

/** "JetStream meta leader · 5 streams led · gateway to us dropping" */
export function clusterNote(c: TopologyCluster, gateways: TopologyGateway[]): string {
  const parts: string[] = [];
  if (c.metaLeader) parts.push("JetStream meta leader");
  if (c.streamsLed !== undefined) parts.push(`${plural(c.streamsLed, "stream")} led`);
  const missing = c.servers.filter((s) => s.missing).length;
  if (missing > 0) parts.push(`${missing} missing`);
  for (const g of gateways) if (touches(g, c) && gatewayDown(g)) parts.push(`gateway to ${other(g, c)} down`);
  for (const o of droppingTo(c, gateways)) parts.push(`gateway to ${o} dropping`);
  return parts.join(" · ");
}

/** Whether the cluster's box is drawn in the warning colour: a server missing or dropping, or a gateway pair of it down. */
export function clusterWarns(c: TopologyCluster, dropping: Set<string>, gateways: TopologyGateway[]): boolean {
  return c.servers.some((s) => s.missing || dropping.has(s.name)) || gateways.some((g) => touches(g, c) && gatewayDown(g));
}

/** A gateway pair is drawn in the warning colour when it dropped in the last hour or has connections down. */
export function gatewayWarns(g: TopologyGateway): boolean {
  return (g.drops1h ?? 0) > 0 || g.connections < g.expected;
}

/** The parts of a gateway's label, each kept on one line: ["gateway 84 ms", "3 drops/h"]. */
export function gatewayLabelParts(g: TopologyGateway): string[] {
  const parts = [gatewayDown(g) ? "gateway down" : g.rttMedianMs === undefined ? "gateway" : `gateway ${fmtShortDuration(g.rttMedianMs)}`];
  if ((g.drops1h ?? 0) > 0) parts.push(`${fmtInt(g.drops1h!)} ${g.drops1h === 1 ? "drop" : "drops"}/h`);
  if (g.connections < g.expected) parts.push(`${g.connections} of ${g.expected} up`);
  return parts;
}

/** "gateway 84 ms · 3 drops/h" */
export function gatewayLabel(g: TopologyGateway): string {
  return gatewayLabelParts(g).join(" · ");
}

/** The whole of a gateway pair in words, for the tooltip and screen readers. */
export function gatewayText(g: TopologyGateway): string {
  const parts = [`Gateway ${clusterLabel(g.a)} – ${clusterLabel(g.b)}: ${g.connections} of ${g.expected} connections up`];
  if (gatewayDown(g)) parts.push("down");
  if (g.rttMedianMs !== undefined) parts.push(`round trip ${fmtShortDuration(g.rttMedianMs)} median, ${fmtShortDuration(g.rttMaxMs ?? g.rttMedianMs)} highest`);
  else parts.push("no round trip reported");
  parts.push(g.drops1h === undefined ? "drops in the last hour not known" : `${plural(g.drops1h, "drop")} in the last hour`);
  parts.push(g.drops24h === undefined ? "drops in 24 hours not known" : `${plural(g.drops24h, "drop")} in 24 hours`);
  if (g.droppingServers.length > 0) parts.push(`dropping on ${g.droppingServers.join(", ")}`);
  return parts.join("; ");
}

/** 18, "sites", "1 not connected"; undefined without sites or counts. */
export function leafBox(c: TopologyCluster): { sites: number; unit: string; state: string } | undefined {
  if (c.leafConnected === undefined && c.leafNotConnected === undefined) return undefined;
  const up = c.leafConnected ?? 0;
  const down = c.leafNotConnected ?? 0;
  const sites = up + down;
  if (sites === 0) return undefined;
  return { sites, unit: sites === 1 ? "site" : "sites", state: down === 0 ? "all connected" : `${fmtInt(down)} not connected` };
}

/**
 * The routes table's cluster: the one asked for; else, among the clusters
 * with a slow route (or all clusters when none has one), the meta leader's,
 * else the first by name. Never chosen by the order of the routes, which
 * can change with every refetch when round trips are close.
 */
export function routesCluster(t: Topology, asked: string | null): string | undefined {
  if (asked !== null && t.clusters.some((c) => c.name === asked)) return asked;
  const slow = new Set(t.routes.filter((r) => r.slow).map((r) => r.cluster));
  const pool = slow.size > 0 ? t.clusters.filter((c) => slow.has(c.name)) : t.clusters;
  const leader = pool.find((c) => c.metaLeader);
  if (leader) return leader.name;
  // The unknown cluster (no name) comes after every named one.
  const byName = (a: string, b: string) => (a === "" ? 1 : 0) - (b === "" ? 1 : 0) || (a < b ? -1 : a > b ? 1 : 0);
  return pool.map((c) => c.name).sort(byName)[0] ?? [...slow].sort(byName)[0];
}

/** "Drops are counted from collections every 30s; …" */
export function dropsLine(t: Topology): string {
  const every = t.collectIntervalMs ? ` every ${fmtDuration(t.collectIntervalMs)}` : "";
  return `Drops are counted from collections${every}; a drop and reconnect within one interval is seen only as a new connection ID.`;
}

/** "ROUTEZ did not answer on nats-eu-4, nats-eu-5." */
export function unobservedLines(t: Topology): string[] {
  return t.unobserved.filter((u) => u.servers.length > 0).map((u) => `${u.endpoint} did not answer on ${u.servers.join(", ")}.`);
}

/** One gateway drawn under the row of boxes, from box i to box j. */
export interface Bracket {
  g: TopologyGateway;
  i: number;
  j: number;
  /** 0 is the nearest to the boxes. */
  level: number;
  /** How far from each box's centre its leg sits, in steps; 0 is the nearest. */
  legI: number;
  legJ: number;
}

/**
 * Places the gateways between clusters that are not next to each other in
 * the row: the shortest nearest to the boxes, two that overlap on different
 * levels, and on each box the deepest leg nearest the centre, so no leg
 * crosses a line of its own box.
 */
export function brackets(order: string[], gateways: TopologyGateway[]): Bracket[] {
  const idx = new Map(order.map((n, k) => [n, k]));
  const far = gateways
    .map((g) => {
      const x = idx.get(g.a);
      const y = idx.get(g.b);
      if (x === undefined || y === undefined) return undefined;
      return { g, i: Math.min(x, y), j: Math.max(x, y) };
    })
    .filter((b): b is { g: TopologyGateway; i: number; j: number } => b !== undefined && b.j - b.i > 1)
    .sort((p, q) => p.j - p.i - (q.j - q.i) || p.i - q.i);
  const levels: { i: number; j: number }[][] = [];
  const placed = far.map((b) => {
    let level = 0;
    while (levels[level]?.some((o) => o.i < b.j && b.i < o.j)) level++;
    (levels[level] ??= []).push(b);
    return { ...b, level, legI: 0, legJ: 0 };
  });
  // Per box and side, the deepest leg nearest the centre.
  const legs = new Map<string, Bracket[]>();
  for (const b of placed) {
    legs.set(`${b.i}r`, [...(legs.get(`${b.i}r`) ?? []), b]);
    legs.set(`${b.j}l`, [...(legs.get(`${b.j}l`) ?? []), b]);
  }
  for (const [key, list] of legs) {
    list.sort((p, q) => q.level - p.level);
    list.forEach((b, k) => {
      if (key.endsWith("r")) b.legI = k;
      else b.legJ = k;
    });
  }
  return placed;
}

/** The gateways between clusters next to each other in the row, by the left one's index. */
export function neighbours(order: string[], gateways: TopologyGateway[]): Map<number, TopologyGateway> {
  const idx = new Map(order.map((n, k) => [n, k]));
  const out = new Map<number, TopologyGateway>();
  for (const g of gateways) {
    const x = idx.get(g.a);
    const y = idx.get(g.b);
    if (x !== undefined && y !== undefined && Math.abs(x - y) === 1) out.set(Math.min(x, y), g);
  }
  return out;
}

/** The parts of a label as they are drawn: every part after the first starts with "· ", so a wrapped line never ends in a separator. */
export function labelTokens(parts: string[]): string[] {
  return parts.map((p, k) => (k > 0 ? `· ${p}` : p));
}

/** Packs tokens into lines no wider than max pixels, a space between tokens; a token wider than max gets a line of its own. */
export function packLines(tokens: string[], max: number): string[][] {
  const lines: string[][] = [];
  for (const t of tokens) {
    const last = lines[lines.length - 1];
    if (last && labelWidth([...last, t].join(" ")) <= max) last.push(t);
    else lines.push([t]);
  }
  return lines;
}

/** A far gateway's bracket and label in pixels of the map's inner width. */
export interface FarLabel {
  b: Bracket;
  /** The bracket's legs: at the first box and at the last. */
  legX: number;
  legEndX: number;
  /** The level's horizontal line. */
  y: number;
  /** The label's left edge, just right of the first leg. */
  left: number;
  /** The first line right of the label that it must end before: a leaf node connector, a deeper bracket's leg, or its own end leg. */
  limit: number;
  /** The widest line, and the height of all lines. */
  width: number;
  height: number;
  /** Each line's tokens, joined by a space. */
  lines: string[];
  tokens: string[][];
}

/** The vertical room of one line of a label, and the least distance between two levels, in pixels. */
const LINE_H = 16;
const LEVEL_MIN = 30;
const BAND_TOP = 22;
const LABEL_GAP = 6;

/** The x of box i's centre in pixels, for a row of this inner width. */
export function boxCentre(i: number, clusters: number, width: number): number {
  const f = rowFractions(clusters);
  const unit = width / (f.slots * f.box + (f.slots - 1) * f.gap);
  return (i * (f.box + f.gap) + f.box / 2) * unit;
}

/**
 * Places the brackets of gateways between clusters that are not next to
 * each other, with their labels: each label starts just right of the
 * bracket's first leg and wraps between its parts to end before the next
 * line it would cover, and each level is as tall as its tallest label.
 * leaves says which clusters have a leaf node connector at their centre.
 */
export function farLabels(order: string[], gateways: TopologyGateway[], leaves: boolean[], width: number): FarLabel[] {
  const n = order.length;
  const f = rowFractions(n);
  const unit = width / (f.slots * f.box + (f.slots - 1) * f.gap);
  const step = 0.12 * f.box * unit;
  const placed = brackets(order, gateways);
  const legI = (b: Bracket) => boxCentre(b.i, n, width) + (b.legI + 1) * step;
  const legJ = (b: Bracket) => boxCentre(b.j, n, width) - (b.legJ + 1) * step;
  const out = placed.map((b) => {
    const left = legI(b) + LABEL_GAP;
    let limit = legJ(b);
    for (let k = b.i + 1; k < b.j; k++) if (leaves[k]) limit = Math.min(limit, boxCentre(k, n, width));
    for (const o of placed) {
      if (o === b || o.level <= b.level) continue;
      for (const x of [legI(o), legJ(o)]) if (x > left && x < limit) limit = x;
    }
    limit -= LABEL_GAP;
    const tokens = packLines(labelTokens(gatewayLabelParts(b.g)), limit - left);
    const lines = tokens.map((l) => l.join(" "));
    return { b, legX: legI(b), legEndX: legJ(b), y: 0, left, limit, width: Math.max(...lines.map(labelWidth)), height: lines.length * LINE_H + 2, lines, tokens };
  });
  // Each level's line sits below the one before by at least LEVEL_MIN and clear of both levels' labels.
  const levels = Math.max(0, ...placed.map((b) => b.level + 1));
  const tall = Array.from({ length: levels }, (_, l) => Math.max(LINE_H, ...out.filter((p) => p.b.level === l).map((p) => p.height)));
  const ys: number[] = [];
  for (let l = 0; l < levels; l++) {
    ys[l] = l === 0 ? Math.max(BAND_TOP, tall[0] / 2 + LABEL_GAP) : ys[l - 1] + Math.max(LEVEL_MIN, tall[l - 1] / 2 + tall[l] / 2 + LABEL_GAP);
  }
  for (const p of out) p.y = ys[p.b.level];
  return out;
}

/** The height of the band under the boxes that holds these brackets. */
export function farBand(labels: FarLabel[]): number {
  if (labels.length === 0) return 0;
  return Math.max(...labels.map((p) => p.y + p.height / 2)) + 10;
}
