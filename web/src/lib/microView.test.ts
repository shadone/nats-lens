import { describe, expect, it } from "vitest";
import { account, EPOCH_AT, global, none, orders, pricing, prod, refused, seenOnly, service, SLOW_SINCE, SUBS_AT } from "../test/fixtures/micro";
import {
  chooseService,
  cutText,
  descriptionText,
  footLines,
  MICRO_SUBTITLE_TEXT,
  missingText,
  msText,
  noAnswerReason,
  noEndpointsText,
  noServiceText,
  noteOf,
  rateText,
  errorsText,
  unknownServiceText,
  withAccountNames,
  plain,
  type CardNote,
  instancesHeading,
  seenOnlyText,
  serviceErrorsHigh,
  serviceKey,
  slowText,
  unansweredText,
  versionText,
  whenText,
} from "./microView";

const nt = (n: CardNote) => ({ text: plain(n.text), warn: n.warn });
const label = (a: { account: string; accountName: string }) => a.accountName || a.account;

describe("microView numbers", () => {
  it("words requests per second with one decimal, as the board does", () => {
    expect([0, 0.04, 1.2000000000000002, 88, 161.7, 999.94, 1204.4].map(rateText)).toEqual(["0", "<0.1", "1.2", "88.0", "161.7", "999.9", "1,204"]);
    // Rounded first, then formatted: 999.96 is 1,000, not "1000.0".
    expect([999.96, 99.96].map(rateText)).toEqual(["1,000", "100.0"]);
  });

  it("words an average time in ms, and in s from a second", () => {
    expect([0, 0.05, 0.9, 3.8, 41, 999, 1234].map(msText)).toEqual(["0 ms", "<0.1 ms", "0.9 ms", "3.8 ms", "41 ms", "999 ms", "1.2 s"]);
    expect([9.96, 999.6, 9_960_000].map(msText)).toEqual(["10 ms", "1.0 s", "9,960 s"]);
  });

  it("names a time within a day by its hour, an older one with its date", () => {
    expect(whenText(SLOW_SINCE, EPOCH_AT)).toBe("08:12");
    expect(whenText(Date.parse("2026-10-15T07:30:00Z"), EPOCH_AT)).toBe("15 Oct 07:30");
  });

  it("says the line under the title", () => {
    expect(MICRO_SUBTITLE_TEXT).toBe("Services built on the NATS micro framework, found through $SRV.INFO and measured through $SRV.STATS.");
  });
});

describe("microView cards", () => {
  it("shows one version, a mixed spread as the board does, and at most three", () => {
    expect(versionText(orders())).toBe("1.4.2");
    expect(versionText(pricing())).toBe("2.0.0 / 1.9.3");
    expect(versionText(seenOnly())).toBe("");
    expect(versionText(service({ versions: ["4", "3", "2", "1"].map((v) => ({ version: v, instances: 1 })) }))).toBe("4 / 3 / 2 +1");
  });

  it("says a service seen only is seen in subscriptions", () => {
    expect(descriptionText(orders())).toBe("Order intake and lookup");
    expect(descriptionText(seenOnly())).toBe("seen in subscriptions only");
  });

  it("words each note, warning for all but healthy and not asked", () => {
    const accs = global().accounts;
    expect(nt(noteOf(orders(), accs, EPOCH_AT))).toEqual({ text: "orders.get slower than usual since 08:12", warn: true });
    expect(nt(noteOf(pricing(), accs, EPOCH_AT))).toEqual({ text: "mixed versions: 3 on 2.0.0, 1 on 1.9.3", warn: true });
    expect(nt(noteOf(service(), accs, EPOCH_AT))).toEqual({ text: "healthy", warn: false });
    expect(nt(noteOf(seenOnly(), accs, EPOCH_AT))).toEqual({ text: "list the account under `accounts` for versions, endpoints and timings", warn: false });
    const missing = service({ note: "missing", instances: 1, missing: [{ id: "INV1", since: Date.parse("2026-10-17T10:05:00Z"), lastSeen: Date.parse("2026-10-17T10:04:30Z"), connectionName: "inventory-service", server: "", stillSubscribed: true }] });
    expect(nt(noteOf(missing, accs, EPOCH_AT))).toEqual({ text: "instance missing: INV1 since 10:05", warn: true });
    const two = service({ note: "missing", missing: [{ ...missing.missing[0] }, { ...missing.missing[0], id: "INV2", since: Date.parse("2026-10-17T09:50:00Z") }] });
    expect(plain(noteOf(two, accs, EPOCH_AT).text)).toBe("2 instances missing since 09:50");
    const errs = service({ note: "errors", endpoints: [orders().endpoints[1], { ...orders().endpoints[2], errorShare: 0.024, errorsHigh: true }] });
    expect(nt(noteOf(errs, accs, EPOCH_AT))).toEqual({ text: "errors 2.4% on inventory.create", warn: true });
  });

  it("gives the reason of no answer from the service's account", () => {
    const s = service({ account: "SHOP", note: "noAnswer", answered: false });
    expect(plain(noteOf(s, [account({ account: "SHOP", stats: refused("$SRV.STATS") })], EPOCH_AT).text)).toBe("no answer to $SRV.STATS: refused; the account's user needs to publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>");
    expect(plain(noteOf(s, [account({ account: "SHOP", stats: { state: "timeout", replies: 0, error: "" } })], EPOCH_AT).text)).toBe("no answer to $SRV.STATS: no reply within the collect window");
    expect(plain(noteOf(s, [account({ account: "SHOP" })], EPOCH_AT).text)).toBe("no answer to $SRV.STATS: its instances did not reply");
    expect(noAnswerReason({ state: "error", replies: 0, error: "nats: connection closed" })).toBe("the request failed: nats: connection closed");
    expect(noAnswerReason({ state: "noResponders", replies: 0, error: "" })).toBe("no responders in the account");
    expect(noAnswerReason(undefined)).toBe("its instances did not reply");
  });

  it("marks errors above 1% only", () => {
    expect(serviceErrorsHigh(service({ errorShare: 0.01 }))).toBe(false);
    expect(serviceErrorsHigh(service({ errorShare: 0.011 }))).toBe(true);
    expect(serviceErrorsHigh(seenOnly())).toBe(false);
  });
});

describe("microView choice", () => {
  it("chooses the service the address names, else the first, and says when the address names none listed", () => {
    const list = global().services;
    expect(serviceKey(list[1])).toBe("APP/pricing");
    expect(chooseService(list, "APP/pricing")).toEqual({ chosen: list[1], unknown: false });
    expect(chooseService(list, null)).toEqual({ chosen: list[0], unknown: false });
    expect(chooseService(list, "SHOP/pricing")).toEqual({ chosen: list[0], unknown: true });
    expect(chooseService([], "APP/orders")).toEqual({ chosen: undefined, unknown: true });
  });
});

describe("microView endpoints and instances", () => {
  it("says how slow an endpoint is against its level and since when", () => {
    expect(slowText(orders().endpoints[0], EPOCH_AT)).toBe("avg time 10× its 7-day level since 08:12");
    expect(slowText({ averageMs: 14, levelMs: 4, slowSince: SLOW_SINCE }, EPOCH_AT)).toBe("avg time 3.5× its 7-day level since 08:12");
    expect(slowText({ averageMs: 14, levelMs: undefined, slowSince: undefined }, EPOCH_AT)).toBe("avg time above its 7-day level");
  });

  it("says when an instance went missing, when it last answered, and that it is still registered", () => {
    const m = { id: "INV1", since: Date.parse("2026-10-17T10:05:00Z"), lastSeen: Date.parse("2026-10-17T10:04:30Z"), connectionName: "inventory-service", server: "", stillSubscribed: true };
    expect(missingText(m, true, EPOCH_AT)).toBe("missing since 10:05, last answered 10:04; still subscribed, so registered but not answering");
    expect(missingText({ ...m, stillSubscribed: false }, false, EPOCH_AT)).toBe("missing since 10:05, last seen 10:04");
    expect(missingText({ ...m, lastSeen: Date.parse("2026-10-17T10:05:10Z"), stillSubscribed: false }, true, EPOCH_AT)).toBe("missing since 10:05");
  });

  it("says why a service has no endpoint rows", () => {
    const accs = [account({ stats: refused("$SRV.STATS") })];
    expect(noEndpointsText(service({ endpoints: [], answered: false, note: "noAnswer" }), accs)).toBe(
      "No endpoints known: none of its instances answered $SRV.STATS in this collection (refused; the account's user needs to publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>).",
    );
    expect(noEndpointsText(service({ endpoints: [] }), accs)).toBe("The service reports no endpoints.");
  });

  it("says what is known of a service seen only and how to see more", () => {
    expect(seenOnlyText(seenOnly(), "APP").map(plain)).toEqual([
      "nats-lens does not ask APP, so versions, endpoints and timings are not known. The subscription list shows 2 instances on catalog-api.",
      "To see more, list APP under `accounts` in the configuration, with a user that may publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>.",
    ]);
    expect(plain(seenOnlyText(seenOnly({ instances: 1, connectionNames: ["a", "b", "c"], moreNames: 2 }), "APP")[0])).toBe(
      "nats-lens does not ask APP, so versions, endpoints and timings are not known. The subscription list shows 1 instance on a, b and c and 2 more connections.",
    );
    expect(plain(seenOnlyText(seenOnly({ connectionNames: [] }), "APP")[0])).toMatch(/shows 2 instances\.$/);
  });
});

describe("microView accounts and footnote", () => {
  it("says why an asked account without services did not answer", () => {
    const [suppliers, app] = global().accounts;
    expect(unansweredText(suppliers)).toBe(
      'The server refused $SRV.STATS (nats: Permissions Violation for Publish to "$SRV.STATS"): the account\'s user needs to publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>.',
    );
    expect(unansweredText(app)).toBeUndefined();
    expect(unansweredText(account({ services: 0, stats: { state: "timeout", replies: 0, error: "" } }))).toBe("No reply to $SRV.STATS came within the collect window.");
    expect(unansweredText(account({ services: 0, stats: { state: "error", replies: 0, error: "nats: timeout" } }))).toBe("$SRV.STATS failed: nats: timeout.");
    expect(unansweredText(account({ services: 0, stats: { state: "noResponders", replies: 0, error: "" } }))).toBeUndefined();
    expect(unansweredText(account({ asked: false, services: 0, stats: refused("$SRV.STATS") }))).toBeUndefined();
  });

  it("names the subscription list's time, accounts with no responders and a cut list", () => {
    expect(footLines(global(), label)).toEqual(["Subscriptions as of 14:28:30 UTC."]);
    const d = global({ servicesTotal: 312, accounts: [account({ account: "SHOP", services: 0, stats: { state: "noResponders", replies: 0, error: "" } })] });
    expect(footLines(d, label)).toEqual(["Subscriptions as of 14:28:30 UTC.", "Asked, with no service answering (no responders): SHOP.", "The first 3 of 312 services are listed."]);
    expect(footLines(none(), label)).toEqual([]);
    expect(cutText(100, 100, "endpoints")).toBe("");
  });

  it("says why no service is listed", () => {
    expect(plain(noServiceText(none(), ""))).toBe("No micro service known: no account is listed under `accounts`, and no subscription list within 24 hours before this collection.");
    expect(plain(noServiceText(none({ subscriptionsAt: SUBS_AT }), ""))).toBe("No micro service in this collection.");
    expect(plain(noServiceText(none({ subscriptionsAt: SUBS_AT }), "APP"))).toBe("No micro service in APP in this collection.");
    expect(plain(noServiceText(none({ accounts: [account({ services: 0 })] }), ""))).toBe("No micro service in this collection.");
    expect(prod().services.every((s) => !s.asked)).toBe(true);
  });
});

describe("microView review round 1", () => {
  it("words an error share so that it agrees with the above-1% flag", () => {
    expect(errorsText(0, false)).toBe("0%");
    expect(errorsText(0.0003, false)).toBe("<0.1%");
    expect(errorsText(0.001, false)).toBe("0.1%");
    expect(errorsText(0.01, false)).toBe("1.0%");
    expect(errorsText(0.01004, true)).toBe(">1.0%");
    expect(errorsText(0.024, true)).toBe("2.4%");
    expect(errorsText(0.0996, true)).toBe("10%");
    expect(errorsText(0.123, true)).toBe("12%");
  });

  it("says a service of another account is not in the scoped one, by the accounts' names", () => {
    const label = (id: string) => (id === "ACK1" ? "SHOP" : id);
    expect(unknownServiceText("APP/orders", "ACK1", label, "inventory")).toBe("orders of APP is not in account SHOP; showing inventory.");
    expect(unknownServiceText("APP/shipping", "APP", label, "orders")).toBe("No service shipping in APP in this collection; showing orders.");
    expect(unknownServiceText("APP/shipping", "", label, "orders")).toBe("No service APP/shipping in this collection; showing orders.");
    // No account part: the scoped account's, or none.
    expect(unknownServiceText("orders", "ACK1", label, "inventory")).toBe("No service orders in SHOP in this collection; showing inventory.");
    expect(unknownServiceText("orders", "", label, "inventory")).toBe("No service orders in this collection; showing inventory.");
  });

  it("fills an account's name from the account list when the response has none", () => {
    const d = global();
    const names = new Map([["APP", "Shop"], ["ADSUPPLIERSFAKEACCOUNTKEY", "SUPPLIERS-LIST"]]);
    const out = withAccountNames(d, names);
    expect(out.accounts.map((a) => a.accountName)).toEqual(["SUPPLIERS", "Shop"]);
    expect(out.services.map((s) => s.accountName)).toEqual(["Shop", "Shop", "Shop"]);
    expect(d.services[0].accountName).toBe("");
    expect(withAccountNames(d, new Map())).toBe(d);
  });
});

describe("microView review round 2", () => {
  it("marks the configuration key as a part of its own, never by reading backticks in text", () => {
    const s = service({ account: "SHOP", note: "noAnswer", answered: false });
    const n = noteOf(s, [account({ account: "SHOP", stats: { state: "error", replies: 0, error: "nats: bad `x` reply" } })], EPOCH_AT);
    expect(n.text).toEqual(["no answer to $SRV.STATS: the request failed: nats: bad `x` reply"]);
    expect(noteOf(seenOnly(), [], EPOCH_AT).text).toEqual(["list the account under ", { key: "accounts" }, " for versions, endpoints and timings"]);
    expect(seenOnlyText(seenOnly({ connectionNames: ["cat`a`log"] }), "A`B")[0]).toEqual([
      "nats-lens does not ask A`B, so versions, endpoints and timings are not known. The subscription list shows 2 instances on cat`a`log.",
    ]);
  });

  it("chooses a service named without an account in the scope's account, or by its name", () => {
    const list = [service({ account: "SHOP" }), service({ account: "APP" })];
    expect(chooseService(list, "inventory", "APP").chosen).toBe(list[1]);
    expect(chooseService(list, "inventory", "").chosen).toBe(list[0]);
    expect(chooseService(list, "orders", "APP").unknown).toBe(true);
  });

  it("heads the instances by where they come from", () => {
    expect(instancesHeading("answered")).toBe("Instances");
    expect(instancesHeading("none")).toBe("Instances");
    expect(instancesHeading("subscriptions")).toBe("Instances seen in the subscription list");
    expect(instancesHeading("lastAnswered")).toBe("Instances that last answered within the hour before this collection");
  });
});

describe("microView review round 3", () => {
  it("reads an empty account part (?service=/inventory) as no account part, in choosing and in words", () => {
    const list = [service({ account: "SHOP", name: "inventory" }), service({ account: "APP", name: "inventory" })];
    expect(chooseService(list, "/inventory", "")).toEqual({ chosen: list[0], unknown: false });
    expect(chooseService(list, "/inventory", "APP")).toEqual({ chosen: list[1], unknown: false });
    const r = chooseService([service({ account: "SHOP" })], "/inventory", "APP");
    expect(r.unknown).toBe(true);
    expect(unknownServiceText("/inventory", "APP", (x) => x, r.chosen!.name)).toBe("No service inventory in APP in this collection; showing inventory.");
    expect(unknownServiceText("/orders", "", (x) => x, "inventory")).toBe("No service orders in this collection; showing inventory.");
  });
});

describe("microView final review", () => {
  it("chooses a service whose account has a slash, as service names cannot", () => {
    const a = service({ account: "shop/eu", name: "inventory" });
    const b = service({ account: "APP", name: "orders" });
    expect(chooseService([b, a], serviceKey(a), "")).toEqual({ chosen: a, unknown: false });
    expect(unknownServiceText("shop/eu/stock", "", (x) => x, "orders")).toBe("No service shop/eu/stock in this collection; showing orders.");
    expect(unknownServiceText("shop/eu/stock", "APP", (x) => x, "orders")).toBe("stock of shop/eu is not in account APP; showing orders.");
  });

  it("names no empty service", () => {
    expect(unknownServiceText("/", "", (x) => x, "orders")).toBe("No service named in the address is in this collection; showing orders.");
    expect(unknownServiceText("APP/", "APP", (x) => x, "orders")).toBe("No service named in the address is in this collection; showing orders.");
  });

  it("never says 0 instances missing", () => {
    expect(plain(noteOf(service({ note: "missing", missing: [] }), [], EPOCH_AT).text)).toBe("an instance missing");
  });
});

describe("microView held refusals", () => {
  const held = { state: "error" as const, replies: 0, error: 'nats: Permissions Violation for Publish to "$SRV.STATS"', refusedAt: Date.parse("2026-10-17T14:00:00Z"), againAfter: Date.parse("2026-10-17T14:10:00Z") };
  it("says when a refused request is asked again", () => {
    expect(noAnswerReason(held, EPOCH_AT)).toBe("refused; the account's user needs to publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>; asked again after 14:10");
    expect(unansweredText(account({ services: 0, stats: held }), EPOCH_AT)).toBe(
      'The server refused $SRV.STATS at 14:00 (nats: Permissions Violation for Publish to "$SRV.STATS"): the account\'s user needs to publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>. nats-lens asks again after 14:10.',
    );
  });
});
