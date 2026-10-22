// Subjects view models for tests: rows of the shop's account APP and the
// system account as the demo's example-global has them.
import type { CoreMatch, MatchAccount, StreamMatch, SubjectMatches, SubjectRow, Subjects } from "../../api/subjects";

export const EPOCH_AT = Date.parse("2026-10-21T12:00:00Z");
export const SUBS_AT = Date.parse("2026-10-21T11:58:30Z");

export function row(o: Partial<SubjectRow> = {}): SubjectRow {
  return {
    account: "APP",
    accountName: "",
    systemAccount: false,
    kind: "stream",
    patterns: ["order.events.>"],
    morePatterns: 0,
    stream: "order-events",
    mirrorOf: "",
    mirroredBy: [],
    sourcedInto: [],
    retention: "limits",
    messages: 1000,
    durable: 0,
    ephemeral: 0,
    connections: 0,
    queue: "",
    names: [],
    moreNames: 0,
    overlaps: [],
    noConsumer: false,
    wide: false,
    ...o,
  };
}

export function sub(subject: string, o: Partial<SubjectRow> = {}): SubjectRow {
  return row({ kind: "subscription", patterns: [subject], stream: "", retention: "", messages: undefined, connections: 1, ...o });
}

export function global(): Subjects {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    subscriptionsAt: SUBS_AT,
    rows: [
      row({ patterns: ["basket.events.>"], stream: "basket-events", noConsumer: true, storedPerSecond: 0.07 }),
      row({ patterns: ["order.events.>"], stream: "order-events", durable: 4, storedPerSecond: 2.88, mirroredBy: ["order-archive"] }),
      row({ patterns: [], stream: "order-archive", mirrorOf: "order-events", durable: 1, storedPerSecond: 14.87 }),
      row({ kind: "kv", patterns: ["$KV.feature-flags.>"], stream: "KV_feature-flags", ephemeral: 22, names: ["api-gateway", "checkout-service", "order-service"], moreNames: 1, storedPerSecond: 0.04 }),
      row({ patterns: ["stock.alerts.>"], stream: "stock-alerts", retention: "interest", ephemeral: 1, names: ["inventory-service"], storedPerSecond: 0.6 }),
      sub("orders.place", { connections: 3, queue: "order-service", names: ["order-service"] }),
      row({ kind: "inbox", patterns: ["_INBOX.>"], stream: "", retention: "", messages: undefined, connections: 731, names: ["api-gateway", "checkout-service", "inventory-service"], moreNames: 2 }),
      row({ kind: "jsApi", patterns: ["$JS.API.>"], stream: "", retention: "", messages: undefined, apiPerSecond: 3.1, apiErrorShare: 0.002 }),
      sub(">", { account: "SYS", systemAccount: true, names: ["ops-monitor"], wide: true }),
    ],
    rowsTotal: 9,
    counts: { all: 9, stored: 5, noConsumer: 1, overlapping: 0, wide: 1 },
    servers: [
      { name: "nats-0", answered: true, total: 1840, listed: 1840 },
      { name: "nats-1", answered: true, total: 14212, listed: 10000 },
      { name: "nats-2", answered: false },
    ],
    serverSubscriptions: 1006,
    unlistedSubscriptions: 0,
  };
}

export function noCollection(): Subjects {
  return { collected: false, rows: [], rowsTotal: 0, counts: { all: 0, stored: 0, noConsumer: 0, overlapping: 0, wide: 0 }, servers: [], serverSubscriptions: 0, unlistedSubscriptions: 0 };
}

export function streamMatch(o: Partial<StreamMatch> = {}): StreamMatch {
  return { account: "APP", stream: "order-events", kind: "stream", pattern: "order.events.>", consumers: ["email-sender", "order-projector"], ephemeral: 0, mirrors: ["order-archive"], sourcedBy: [], ...o };
}

export function coreMatch(o: Partial<CoreMatch> = {}): CoreMatch {
  return { subject: "orders.place", queue: "order-service", connections: 3, names: ["order-service"], moreNames: 0, ...o };
}

export function published(o: Partial<MatchAccount> = {}): MatchAccount {
  return { account: "APP", accountName: "", systemAccount: false, streams: [], core: [], jetstreamApi: false, ...o };
}

export function matches(o: Partial<SubjectMatches> = {}): SubjectMatches {
  return { collected: true, accounts: [], subscriptionsAt: SUBS_AT, ...o };
}
