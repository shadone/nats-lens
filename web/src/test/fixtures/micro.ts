// Micro services view models for tests: the shop's services in account APP
// as the demo's example-global has them, SUPPLIERS asked and refused, and
// example-prod's services seen in subscriptions only.
import type { MicroAccount, MicroEndpoint, MicroInstance, MicroRequest, MicroService, MicroServices } from "../../api/micro";

export const EPOCH_AT = Date.parse("2026-10-17T14:30:00Z");
export const SUBS_AT = Date.parse("2026-10-17T14:28:30Z");
export const SLOW_SINCE = Date.parse("2026-10-17T08:12:00Z");
export const SUPPLIERS = "ADSUPPLIERSFAKEACCOUNTKEY";

export function ok(replies: number, at = EPOCH_AT): MicroRequest {
  return { state: "ok", at, replies, error: "" };
}

export function refused(subject: string): MicroRequest {
  return { state: "error", at: EPOCH_AT, replies: 0, error: `nats: Permissions Violation for Publish to "${subject}"` };
}

export function account(o: Partial<MicroAccount> = {}): MicroAccount {
  return { account: "APP", accountName: "", asked: true, info: ok(9), stats: ok(9), systemAccount: false, services: 3, ...o };
}

export function endpoint(o: Partial<MicroEndpoint> = {}): MicroEndpoint {
  return {
    name: "create",
    subject: "orders.create",
    subjects: [o.subject ?? "orders.create"],
    queueGroup: "q",
    requestsPerSecond: 42.1,
    averageMs: 3.8,
    errorShare: 0.001,
    lastError: "",
    lastErrorInstance: "",
    levelMs: 3.8,
    slow: false,
    errorsHigh: false,
    ...o,
  };
}

export function instance(id: string, o: Partial<MicroInstance> = {}): MicroInstance {
  return { id, version: "1.4.2", started: Date.parse("2026-10-15T07:30:00Z"), connectionName: "order-service", server: "nats-eu-0", ...o };
}

export function service(o: Partial<MicroService> = {}): MicroService {
  return {
    account: "APP",
    accountName: "",
    name: "inventory",
    asked: true,
    versions: [{ version: "0.9.1", instances: 2 }],
    description: "Stock levels",
    instances: 2,
    missing: [],
    requestsPerSecond: 12.6,
    errorShare: 0,
    endpoints: [endpoint({ name: "reserve", subject: "inventory.stock.reserve", requestsPerSecond: 1.2, averageMs: 6.2, errorShare: 0 })],
    endpointsTotal: 1,
    connectionNames: ["inventory-service"],
    moreNames: 0,
    note: "healthy",
    systemAccount: false,
    instanceList: [instance("INV0", { version: "0.9.1", connectionName: "inventory-service", server: "nats-eu-4" }), instance("INV1", { version: "0.9.1", connectionName: "inventory-service", server: "nats-eu-5" })],
    instanceSource: "answered",
    answered: true,
    ...o,
  };
}

export function orders(o: Partial<MicroService> = {}): MicroService {
  return service({
    name: "orders",
    versions: [{ version: "1.4.2", instances: 3 }],
    description: "Order intake and lookup",
    instances: 3,
    requestsPerSecond: 161.7,
    errorShare: 0.00026,
    note: "slow",
    endpoints: [
      endpoint({ name: "get", subject: "orders.get.*", requestsPerSecond: 118.4, averageMs: 41, errorShare: 0, levelMs: 4.1, slow: true, slowSince: SLOW_SINCE }),
      endpoint({ name: "cancel", subject: "orders.cancel", requestsPerSecond: 1.2, averageMs: 5.1, errorShare: 0, levelMs: 5.1 }),
      endpoint({ lastError: "409 duplicate order id", lastErrorInstance: "ORD1" }),
    ],
    endpointsTotal: 3,
    connectionNames: ["order-service"],
    instanceList: [instance("ORD0"), instance("ORD1", { server: "nats-eu-1" }), instance("ORD2", { server: "nats-eu-2" })],
    ...o,
  });
}

export function pricing(o: Partial<MicroService> = {}): MicroService {
  return service({
    name: "pricing",
    versions: [
      { version: "2.0.0", instances: 3 },
      { version: "1.9.3", instances: 1 },
    ],
    description: "Price quotes",
    instances: 4,
    requestsPerSecond: 88,
    errorShare: 0.00036,
    note: "mixedVersions",
    endpoints: [endpoint({ name: "quote", subject: "pricing.quote", requestsPerSecond: 80.5, averageMs: 2.1, errorShare: 0.0004 }), endpoint({ name: "rules", subject: "pricing.rules.get", requestsPerSecond: 7.5, averageMs: 0.9, errorShare: 0 })],
    endpointsTotal: 2,
    connectionNames: ["checkout-service"],
    instanceList: [],
    ...o,
  });
}

export function global(o: Partial<MicroServices> = {}): MicroServices {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    subscriptionsAt: SUBS_AT,
    services: [orders(), pricing(), service()],
    servicesTotal: 3,
    accounts: [account({ account: SUPPLIERS, accountName: "SUPPLIERS", services: 0, info: refused("$SRV.INFO"), stats: refused("$SRV.STATS") }), account()],
    ...o,
  };
}

export function seenOnly(o: Partial<MicroService> = {}): MicroService {
  return service({
    name: "catalog",
    asked: false,
    versions: [],
    description: "",
    instances: 2,
    requestsPerSecond: undefined,
    errorShare: undefined,
    endpoints: [],
    endpointsTotal: 0,
    connectionNames: ["catalog-api"],
    note: "notAsked",
    instanceSource: "subscriptions",
    answered: false,
    instanceList: [instance("CAT0", { version: "", started: undefined, connectionName: "catalog-api", server: "nats-0" }), instance("CAT1", { version: "", started: undefined, connectionName: "catalog-api", server: "nats-1" })],
    ...o,
  });
}

export function prod(): MicroServices {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    subscriptionsAt: SUBS_AT,
    services: [seenOnly(), seenOnly({ name: "ledger", connectionNames: ["ledger-service"], instanceList: [] })],
    servicesTotal: 2,
    accounts: [account({ asked: false, info: { state: "notAsked", replies: 0, error: "" }, stats: { state: "notAsked", replies: 0, error: "" }, services: 2 })],
  };
}

export function none(o: Partial<MicroServices> = {}): MicroServices {
  return { collected: true, epochAt: EPOCH_AT, subscriptionsAt: undefined, services: [], servicesTotal: 0, accounts: [], ...o };
}

export function noCollection(): MicroServices {
  return { collected: false, services: [], servicesTotal: 0, accounts: [] };
}
