// Builders of placement matrix view models for tests.
import type { Matrix, MatrixAsset, MatrixCell } from "../../api/placementMatrix";

export function mcell(server: string, state: MatrixCell["state"], over: Partial<MatrixCell> = {}): MatrixCell {
  return { server, state, configHash: "h", differs: [], peers: [], ...over };
}

export function masset(kind: "stream" | "consumer", stream: string, consumer: string, cells: MatrixCell[], over: Partial<MatrixAsset> = {}): MatrixAsset {
  const name = kind === "stream" ? stream : consumer;
  return {
    entity: { kind, key: kind === "stream" ? `APP/${stream}` : `APP/${stream}/${consumer}`, name, account: "APP", stream, consumer: kind === "stream" ? "" : consumer, server: "" },
    replicas: 3,
    leader: cells.find((c) => c.state === "leader")?.server ?? "",
    cells,
    unplaced: 0,
    serversDisagree: false,
    durable: kind === "consumer",
    matches: true,
    consumers: [],
    ...over,
  };
}

/** A matrix of the given streams on servers nats-0 to nats-2. */
export function mmatrix(streams: MatrixAsset[], over: Partial<Matrix> = {}): Matrix {
  const all = streams.flatMap((s) => [s, ...s.consumers]);
  return {
    servers: ["nats-0", "nats-1", "nats-2"].map((name, i) => ({ name, leads: 2 - i, memBytes: 256 * 1024 * 1024, uptimeMs: 47 * 3_600_000, metaLeader: i === 1, present: true })),
    streams,
    counts: { all: all.length, disagree: 0, behind: 0, missing: 0, single: 0 },
    epochAt: Date.parse("2026-10-17T14:29:56Z"),
    ...over,
  };
}
