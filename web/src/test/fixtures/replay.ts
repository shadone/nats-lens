// View models of the Replay page for tests.
import type { GoneAsset, Replay, ReplayStep } from "../../api/adapt-replay";
import type { Entity } from "../../api/model";

export const A = Date.parse("2026-10-17T09:14:00Z");
export const B = Date.parse("2026-10-17T09:50:00Z");

export function server(name: string): Entity {
  return { kind: "server", key: name, name, account: "", stream: "", consumer: "", server: name };
}

export function rstep(id: string, at: string, over: Partial<ReplayStep> = {}): ReplayStep {
  return {
    id,
    time: Date.parse(`2026-10-17T${at}Z`),
    openAtB: false,
    kind: "finding",
    title: id,
    body: "",
    evidence: [],
    evidenceCount: 0,
    leadersTaken: [],
    meta: false,
    outsideRange: false,
    ...over,
  };
}

export function rgone(over: Partial<GoneAsset> = {}): GoneAsset {
  return {
    entity: { kind: "consumer", key: "APP/inventory-changes/restock-alerts", name: "restock-alerts", account: "APP", stream: "inventory-changes", consumer: "restock-alerts", server: "" },
    serversAtA: ["nats-0", "nats-1", "nats-2"],
    notAnsweringAtB: [],
    lastReported: Date.parse("2026-10-17T09:30:30Z"),
    deleteClient: "",
    consumersWithIt: 0,
    ...over,
  };
}

export function rreplay(over: Partial<Replay> = {}, system = ""): Replay {
  const a = over.a ?? A;
  const b = over.b ?? B;
  return {
    asked: { system, a, b },
    a,
    b,
    epochA: "100",
    epochAAt: a - 10_000,
    epochB: "172",
    epochBAt: b - 10_000,
    steps: [],
    stepsTruncated: false,
    stepsOmitted: 0,
    counts: {},
    gone: [],
    goneTruncated: false,
    kubernetesWatched: true,
    unobserved: [],
    ...over,
  };
}
