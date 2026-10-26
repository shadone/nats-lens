// Applications view models for tests: the shop's applications as the
// demo's example-prod has them, resize-worker with its four pods and
// callback-dispatcher reconnecting with a client-004 finding.
import type { AppHour, Application, Applications } from "../../api/applications";

export const EPOCH_AT = Date.parse("2026-10-17T14:29:56Z");
const FIRST_HOUR = Date.parse("2026-10-16T14:00:00Z");

/** 24 hours oldest first; set(i) gives the hour's in and out, or undefined for an hour without a measurement. */
export function hours(set: (i: number) => [number, number] | undefined = (i) => [1000 + i, 2000 + i]): AppHour[] {
  return Array.from({ length: 24 }, (_, i) => {
    const v = set(i);
    const hour = FIRST_HOUR + i * 3_600_000;
    return v ? { hour, inMsgs: v[0], outMsgs: v[1], inBytes: v[0] * 100, outBytes: v[1] * 100 } : { hour };
  });
}

export function app(o: Partial<Application> = {}): Application {
  const traffic = o.traffic ?? hours();
  const sum = (k: "inMsgs" | "outMsgs" | "inBytes" | "outBytes") => {
    const set = traffic.filter((h) => h[k] !== undefined);
    return set.length ? set.reduce((a, h) => a + (h[k] ?? 0), 0) : undefined;
  };
  return {
    name: "order-service",
    connections: 25,
    instances: 25,
    pods: [],
    podsTotal: 0,
    servers: [
      { server: "nats-0", connections: 13 },
      { server: "nats-1", connections: 6 },
      { server: "nats-2", connections: 6 },
    ],
    accounts: [{ account: "APP", accountName: "" }],
    libraries: [{ lang: "nats.js", version: "3.0.2", connections: 25 }],
    rttMs: 0.729,
    subscriptions: 61,
    pendingBytes: 13568,
    pendingBytesMax: 1024,
    reconnects: 0,
    slowConsumers: 0,
    stale: 0,
    shortLived: 0,
    findings: [],
    traffic,
    inMsgs: sum("inMsgs"),
    outMsgs: sum("outMsgs"),
    inBytes: sum("inBytes"),
    outBytes: sum("outBytes"),
    tooling: false,
    addresses: ["10.20.2.107", "10.20.2.11", "10.20.2.19"],
    addressesTotal: 13,
    connectionNames: [o.name ?? "order-service"],
    moreNames: 0,
    ...o,
  };
}

export function dispatcher(o: Partial<Application> = {}): Application {
  return app({
    name: "callback-dispatcher",
    connections: 22,
    instances: 22,
    rttMs: 0.69,
    subscriptions: 0,
    pendingBytes: 11648,
    reconnects: 6,
    stale: 6,
    findings: [{ id: "client-004:callback-dispatcher", checkCode: "client-004", checkName: "Client reconnecting repeatedly", severity: "warning", muted: false, entityKey: "callback-dispatcher" }],
    traffic: hours((i) => [5000 + i, 9000 - i]),
    ...o,
  });
}

export function resizer(o: Partial<Application> = {}): Application {
  return app({
    name: "resize-worker",
    connections: 4,
    instances: 4,
    pods: ["resize-worker-6c9d4-7d9fq", "resize-worker-6c9d4-k2p4m", "resize-worker-6c9d4-m8w2z", "resize-worker-6c9d4-x2k4l"],
    podsTotal: 4,
    servers: [
      { server: "nats-0", connections: 2 },
      { server: "nats-1", connections: 1 },
      { server: "nats-2", connections: 1 },
    ],
    libraries: [{ lang: "go", version: "1.54.0", connections: 4 }],
    rttMs: 0.41,
    subscriptions: 0,
    pendingBytes: 0,
    pendingBytesMax: 0,
    addresses: ["10.20.6.21", "10.20.6.22", "10.20.6.23", "10.20.6.24"],
    addressesTotal: 4,
    connectionNames: ["resize-worker@resize-worker-6c9d4-7d9fq", "resize-worker@resize-worker-6c9d4-k2p4m", "resize-worker@resize-worker-6c9d4-m8w2z"],
    moreNames: 1,
    traffic: hours((i) => [300 + i, 100 + i]),
    ...o,
  });
}

export function prod(o: Partial<Applications> = {}): Applications {
  const applications = o.applications ?? [dispatcher(), app(), resizer()];
  return {
    collected: true,
    epochAt: EPOCH_AT,
    applications,
    applicationsTotal: applications.length,
    connectionsTotal: applications.reduce((a, x) => a + x.connections, 0),
    toolingHidden: 0,
    spread: { server: "nats-0", connections: 28, total: 51, servers: 3, concentrated: true },
    libraries: [
      { lang: "nats.js", version: "3.0.2", applications: 2 },
      { lang: "go", version: "1.54.0", applications: 1 },
    ],
    lastDay: { slowConsumers: 0, stale: 6, reconnecting: 1 },
    connzCut: false,
    eventsCaptured: true,
    partial: false,
    missing: [],
    captureGaps: false,
    ...o,
  };
}

export function noCollection(): Applications {
  return {
    collected: false,
    applications: [],
    applicationsTotal: 0,
    connectionsTotal: 0,
    toolingHidden: 0,
    spread: { server: "", connections: 0, total: 0, servers: 0, concentrated: false },
    libraries: [],
    lastDay: {},
    connzCut: false,
    eventsCaptured: false,
    partial: false,
    missing: [],
    captureGaps: false,
  };
}
