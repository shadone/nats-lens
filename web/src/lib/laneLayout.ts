// Code-placed labels of the status timeline's Kubernetes lane, ported from
// the reference's drawK8sLane.

/** Width of text in IBM Plex Mono, whose every glyph advances 0.6 em. */
export function monoWidth(s: string, px: number): number {
  return s.length * px * 0.6;
}

export type Measure = (s: string, px: number) => number;

export interface LaneAnnotation {
  /** Pixel span the annotation describes; a point when x0 equals x1. */
  x0: number;
  x1: number;
  /** First line and second, smaller line. */
  a: string;
  b: string;
  /** Try the right side of the span first. */
  preferRight?: boolean;
}

export interface PlacedAnnotation {
  l: number;
  r: number;
  align: "left" | "right";
  a: string;
  b: string;
}

const GAP = 10;

/**
 * Places each annotation left of its span (ending at x0) or right of it
 * (starting at x1), inside the visible part [vis0, vis1] of the lane and
 * GAP pixels clear of annotations placed before it. An annotation without
 * room is left out; its tick and tooltip remain.
 */
export function placeLaneAnnotations(
  anns: LaneAnnotation[],
  vis0: number,
  vis1: number,
  measure: Measure = monoWidth,
): PlacedAnnotation[] {
  const placed: PlacedAnnotation[] = [];
  for (const an of anns) {
    const tw = Math.max(measure(an.a, 11), measure(an.b, 11)) + 8;
    const left = { l: an.x0 - tw, r: an.x0, align: "right" as const };
    const right = { l: an.x1, r: an.x1 + tw, align: "left" as const };
    const cands = an.preferRight ? [right, left] : [left, right];
    const ok = cands.find(
      (c) => c.l >= vis0 && c.r <= vis1 && placed.every((p) => c.r + GAP <= p.l || c.l >= p.r + GAP),
    );
    if (ok) placed.push({ ...ok, a: an.a, b: an.b });
  }
  return placed;
}
