import { describe, expect, it } from "vitest";
import type { LaneEvent, Timeline } from "../api/model";
import { axisTicks, cellRuns, laneAnnotations, mediumLabel, runsText, zoomItems } from "./timelineModel";

const T = Date.parse("2026-10-16T14:30:00Z");
const Q = 900_000;

describe("axisTicks", () => {
  it("labels whole three hours across a day and ends with now", () => {
    const ticks = axisTicks(T, T + 96 * Q, true);
    expect(ticks.map((t) => t.label)).toEqual(["15:00", "18:00", "21:00", "00:00", "03:00", "06:00", "09:00", "12:00", "now"]);
    expect(ticks[ticks.length - 1]).toMatchObject({ frac: 1, edge: true });
  });

  it("keeps labels apart and clear of now on a narrow track", () => {
    const start = Date.parse("2026-10-16T19:37:00Z");
    const ticks = axisTicks(start, start + 96 * Q, true, 220);
    const xs = ticks.map((t) => t.frac * 220);
    for (let i = 1; i < ticks.length - 1; i++) expect(xs[i] - xs[i - 1]).toBeGreaterThanOrEqual(44);
    // "now" ends at the right edge; the label before it ends well before "now" starts.
    const beforeNow = xs[xs.length - 2];
    expect(beforeNow + 17).toBeLessThanOrEqual(220 - 20 - 6);
    expect(ticks[ticks.length - 1].label).toBe("now");
    expect(ticks.length).toBeGreaterThanOrEqual(3);
  });

  it("ends a fixed range with its end time", () => {
    const ticks = axisTicks(T, T + 3_600_000, false);
    expect(ticks[ticks.length - 1].label).toBe("15:30");
  });
});

describe("cellRuns", () => {
  it("joins equal neighbours and skips ok", () => {
    const runs = cellRuns(["ok", "critical", "critical", "ok", "warning", "critical"], T, Q);
    expect(runs.map((r) => r.level)).toEqual(["critical", "warning", "critical"]);
    expect(runsText(runs)).toBe("critical 14:45–15:15, warning 15:30–15:45, critical 15:45–16:00");
    expect(runsText([])).toBe("ok throughout");
  });
});

describe("zoom items", () => {
  const ev = (o: Partial<LaneEvent>): LaneEvent => ({ id: o.label ?? "", time: T, source: "nats", kind: "", shortLabel: "", label: "", ...o });

  it("shortens labels as the reference does", () => {
    expect(mediumLabel("STREAM.QUORUM_LOST media-windows (1 of 3 peers)")).toBe("STREAM.QUORUM_LOST media-windows");
    expect(mediumLabel("QUORUM_LOST consumer job-cache-projector on order-jobs (1 of 3 peers)")).toBe("QUORUM_LOST job-cache-projector");
  });

  it("puts Kubernetes above the axis and the rest below, inside the domain, oldest first", () => {
    const tl: Timeline = {
      start: T,
      bucketMs: Q,
      bucketCount: 96,
      rows: [],
      windows: [],
      natsTruncated: false, findingTruncated: false, kubernetesTruncated: false,
      kubernetes: [ev({ source: "kubernetes", kind: "k8s.node.drained", shortLabel: "node 7f3a", label: "node gke-x-7f3a drained", time: T + 2 * Q })],
      nats: [
        ev({ kind: "finding.resolved", shortLabel: "consumer-008 Consumer quorum lost", label: "consumer-008 ...", time: T + Q, source: "finding" }),
        ev({ kind: "stream.quorum_lost", label: "late", time: T + 99 * Q }),
      ],
    };
    const items = zoomItems(tl, [T, T + 3 * Q]);
    expect(items.map((i) => [i.side, i.labels[0]])).toEqual([
      [1, "consumer-008 resolved"],
      [-1, "node 7f3a drained"],
    ]);
  });
});

describe("laneAnnotations", () => {
  it("marks a deployment of the Kubernetes lane, with its k8s. prefix", () => {
    const tl: Timeline = {
      start: 0,
      bucketMs: 900_000,
      bucketCount: 4,
      rows: [],
      windows: [],
      natsTruncated: false, findingTruncated: false, kubernetesTruncated: false,
      nats: [],
      kubernetes: [
        { id: "d", time: 1000, source: "kubernetes", kind: "k8s.deployment.rolled_out", shortLabel: "deploy", label: "deployment media-worker rolled out" },
        { id: "k", time: 2000, source: "kubernetes", kind: "k8s.pod.killing", shortLabel: "nats-0 Killing", label: "pod nats-0 Killing" },
      ],
    };
    const a = laneAnnotations(tl, (t) => t);
    expect(a.map((x) => x.b)).toEqual(["media-worker"]);
  });
});
