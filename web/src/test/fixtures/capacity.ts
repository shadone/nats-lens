// Capacity view models for tests: example-prod's stores and streams as the
// demo has them, one row per status, and a range without a collection.
import type { Capacity, CapacityStream, ServerStore } from "../../api/capacity";
import type { Mute } from "../../api/model";

export const EPOCH_AT = Date.parse("2026-10-21T12:00:00Z");
export const DAY = 86_400_000;
export const MIB = 1024 ** 2;
export const GIB = 1024 ** 3;

export function store(name: string, o: Partial<ServerStore> = {}): ServerStore {
  return {
    name,
    cluster: "example-prod",
    reported: true,
    used: 1.93 * GIB,
    limit: 50 * GIB,
    perDay: 17 * MIB,
    daysToFull: 2895,
    findingId: "",
    findingCode: "",
    ...o,
  };
}

export function stream(name: string, o: Partial<CapacityStream> = {}): CapacityStream {
  return {
    account: "APP",
    accountName: "",
    stream: name,
    kind: "stream",
    retention: "limits",
    storage: "file",
    replicas: 3,
    mirror: "",
    sources: 0,
    messages: 1000,
    bytes: 10 * MIB,
    discard: "old",
    allowMsgTtl: false,
    bytesPerDay: MIB,
    msgsPerDay: 100,
    next: { limit: "age", state: "steady", discardNew: false },
    status: "bounded",
    findingId: "",
    declaredReason: "",
    firstTs: EPOCH_AT - 7 * DAY,
    maxAge: 7 * DAY,
    ...o,
  };
}

export function acceptance(o: Partial<Mute> = {}): Mute {
  return {
    id: "m-accept",
    checkCode: "stream-005",
    entity: { kind: "stream", key: "APP/KV_customer-config", name: "KV_customer-config", account: "APP", stream: "KV_customer-config", consumer: "", server: "" },
    reason: "Customer records are kept for the account's life.",
    mutedBy: "ops@example.com",
    createdAt: EPOCH_AT - 14 * DAY,
    expiresAt: Date.parse("2027-04-07T06:57:00Z"),
    unmutedBy: "",
    accepted: true,
    growthPerDay: 1024,
    endReason: "",
    ...o,
  };
}

const unbounded = { maxAge: undefined, next: { limit: "none" as const, state: undefined, discardNew: false } };

export function prod(): Capacity {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    servers: [store("nats-0"), store("nats-1"), store("nats-2")],
    serversWithoutJetstream: 0,
    memoryStores: [],
    memoryStoresEmpty: 0,
    accountStores: [],
    streams: [
      stream("order-archive", { ...unbounded, status: "awaiting", findingId: "stream-005:APP/order-archive", bytes: 4 * GIB, firstTs: Date.parse("2025-12-25T12:00:00Z") }),
      stream("audit-archive", {
        maxAge: undefined,
        maxBytes: 1.3 * GIB,
        maxMsgsPerSubject: 100_000_000,
        discard: "new",
        next: { limit: "bytes", state: "reaches", days: 13.08, discardNew: true },
        status: "fillsSoon",
        findingId: "stream-006:APP/audit-archive",
        findingDays: 13.1,
        mute: { ...acceptance(), id: "m-mute", checkCode: "stream-006", accepted: false, growthPerDay: undefined, mutedBy: "ops", expiresAt: Date.parse("2026-10-28T10:00:00Z") },
      }),
      stream("KV_customer-config", {
        kind: "kv",
        maxAge: undefined,
        maxMsgsPerSubject: 5,
        keys: 41,
        messages: 214,
        next: { limit: "perSubject", state: "notForecast", discardNew: false },
        status: "accepted",
        findingId: "stream-005:APP/KV_customer-config",
        mute: acceptance(),
      }),
      stream("KV_session-cache", { kind: "kv", maxAge: undefined, allowMsgTtl: true, maxMsgsPerSubject: 1, keys: 41_870, status: "declared", declaredReason: "writer_ttls", next: { limit: "perSubject", state: "notForecast", discardNew: false } }),
      stream("order-history", { ...unbounded, status: "muted", findingId: "stream-005:APP/order-history", mute: { ...acceptance(), accepted: false, expiresAt: Date.parse("2026-11-17T22:00:00Z") } }),
      stream("legacy-log", { ...unbounded, status: "unbounded" }),
      stream("order-callbacks", { retention: "workqueue", messages: 0, bytes: 0, firstTs: undefined, maxAge: DAY, maxMsgs: 1_000_000, next: { limit: "empty", state: undefined, discardNew: false } }),
      stream("price-changes", { maxAge: undefined, maxMsgs: 96_500, discard: "new", next: { limit: "messages", state: "refusing", discardNew: true } }),
      stream("staging-only", { bytes: undefined, bytesPerDay: undefined, messages: undefined, firstTs: undefined }),
    ],
    streamsTotal: 9,
    growthFrom: EPOCH_AT - DAY,
  };
}

export function empty(): Capacity {
  return { collected: false, servers: [], serversWithoutJetstream: 0, memoryStores: [], memoryStoresEmpty: 0, accountStores: [], streams: [], streamsTotal: 0 };
}
