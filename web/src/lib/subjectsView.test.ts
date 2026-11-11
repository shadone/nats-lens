import { describe, expect, it } from "vitest";
import { coreMatch, global, published, queueDelivered, row, streamMatch, sub } from "../test/fixtures/subjects";
import {
  connectionLabel,
  connectionRateText,
  consumersText,
  deliveredText,
  hasDelivered,
  measuredText,
  moreRatesText,
  coreText,
  cutText,
  footLines,
  invalidReason,
  listText,
  note,
  reachesNothing,
  readers,
  noteText,
  rateText,
  shareText,
  subjectFilter,
  subscribersText,
} from "./subjectsView";

describe("subjectFilter", () => {
  it("reads each pill's value and lists every row for anything else", () => {
    expect(subjectFilter("stored")).toBe("stored");
    expect(subjectFilter("no-consumer")).toBe("noConsumer");
    expect(subjectFilter("overlapping")).toBe("overlapping");
    expect(subjectFilter("wide")).toBe("wide");
    expect(subjectFilter(null)).toBe("all");
    expect(subjectFilter("")).toBe("all");
    expect(subjectFilter("noConsumer")).toBe("all");
  });
});

describe("subscribersText", () => {
  it("counts a stream's consumers by kind", () => {
    expect(subscribersText(row({ durable: 6 }))).toBe("6 durable consumers");
    expect(subscribersText(row({ durable: 1 }))).toBe("1 durable consumer");
    expect(subscribersText(row({ ephemeral: 1 }))).toBe("1 ephemeral");
    expect(subscribersText(row({ durable: 2, ephemeral: 1 }))).toBe("2 durable, 1 ephemeral");
    expect(subscribersText(row())).toBe("none");
  });

  it("calls a KV bucket's ephemeral consumers watchers", () => {
    expect(subscribersText(row({ kind: "kv", ephemeral: 1 }))).toBe("1 watcher");
    expect(subscribersText(row({ kind: "kv", ephemeral: 22, durable: 1 }))).toBe("1 durable, 22 watchers");
    expect(subscribersText(row({ kind: "object", ephemeral: 2 }))).toBe("2 ephemeral");
  });

  it("names the connections that read a push consumer, at most three and then how many more", () => {
    expect(subscribersText(row({ ephemeral: 1, names: ["inventory-service"] }))).toBe("1 ephemeral (inventory-service)");
    expect(subscribersText(row({ kind: "kv", ephemeral: 22, names: ["a", "b", "c"], moreNames: 2 }))).toBe("22 watchers (a, b, c +2)");
  });

  it("counts a subscription's connections, with the queue group they share", () => {
    expect(subscribersText(sub("orders.place", { connections: 3, queue: "order-service", names: ["order-service"] }))).toBe("3 connections in queue group order-service (order-service)");
    expect(subscribersText(sub(">", { names: ["ops-monitor"] }))).toBe("1 connection (ops-monitor)");
    expect(subscribersText(sub("a.b", { connections: 1200, names: [] }))).toBe("1,200 connections");
    expect(subscribersText(row({ kind: "inbox", stream: "", connections: 2, names: ["a", "b"] }))).toBe("2 connections (a, b)");
  });

  it("says server for the JetStream API", () => {
    expect(subscribersText(row({ kind: "jsApi", stream: "" }))).toBe("server");
  });
});

describe("note", () => {
  it("puts an overlap first, then a stream without a consumer, both as warnings", () => {
    expect(note(row({ overlaps: ["a", "b"], noConsumer: true }))).toEqual({ lead: "overlaps", streams: ["a", "b"], warn: true });
    expect(noteText(row({ overlaps: ["a", "b"] }))).toBe("overlaps a, b");
    expect(note(row({ noConsumer: true, retention: "workqueue", messages: 0 }))?.warn).toBe(true);
  });

  it("says what happens to a stream's messages without a consumer, by retention", () => {
    expect(noteText(row({ noConsumer: true, retention: "interest" }))).toBe("stream without a consumer: messages are dropped");
    expect(noteText(row({ noConsumer: true, retention: "workqueue" }))).toBe("stream without a consumer: messages wait for a consumer");
    expect(noteText(row({ noConsumer: true, retention: "limits" }))).toBe("stream without a consumer: messages are kept but nobody reads them");
    expect(noteText(row({ noConsumer: true, retention: "" }))).toBe("stream without a consumer");
  });

  it("says an empty work queue only when it reported 0 messages", () => {
    expect(noteText(row({ retention: "workqueue", messages: 0, durable: 1 }))).toBe("work queue, empty");
    expect(note(row({ retention: "workqueue", messages: undefined, durable: 1 }))).toBeUndefined();
    expect(note(row({ retention: "workqueue", messages: 4, durable: 1 }))).toBeUndefined();
  });

  it("names interest retention, a mirror's origin, the streams sourced into and mirroring, in that order", () => {
    expect(noteText(row({ retention: "interest", mirrorOf: "x" }))).toBe("interest retention: kept only while someone listens");
    expect(note(row({ mirrorOf: "order-events", sourcedInto: ["s"] }))).toEqual({ lead: "mirror of", streams: ["order-events"], warn: false });
    expect(noteText(row({ sourcedInto: ["search-index", "b"], mirroredBy: ["m"] }))).toBe("sourced into search-index, b");
    expect(noteText(row({ mirroredBy: ["order-archive"] }))).toBe("mirrored by order-archive");
    expect(note(row({ durable: 2 }))).toBeUndefined();
  });

  it("says a subscription on > receives everything, an inbox row is reply inboxes, other subscriptions nothing", () => {
    expect(noteText(sub(">"))).toBe("receives every message in the account");
    expect(note(sub("orders.>"))).toBeUndefined();
    expect(noteText(row({ kind: "inbox", patterns: ["_INBOX.>"], stream: "" }))).toBe("reply inboxes");
  });

  it("gives the JetStream API's request rate and error share when known", () => {
    const api = (o = {}) => row({ kind: "jsApi", patterns: ["$JS.API.>"], stream: "", ...o });
    expect(noteText(api({ apiPerSecond: 3.14, apiErrorShare: 0.002 }))).toBe("JetStream API: 3.1 req/s, 0.2% errors");
    expect(noteText(api({ apiPerSecond: 42.4 }))).toBe("JetStream API: 42 req/s");
    expect(noteText(api())).toBe("JetStream API");
  });
});

describe("rateText and shareText", () => {
  it("formats a stored rate, leaving an unknown one undefined", () => {
    expect(rateText(undefined)).toBeUndefined();
    expect(rateText(0)).toBe("0");
    expect(rateText(0.004)).toBe("<0.01");
    expect(rateText(0.0809)).toBe("0.08");
    expect(rateText(14.87)).toBe("14.9");
    expect(rateText(1204.4)).toBe("1,204");
  });

  it("formats an error share as a percentage", () => {
    expect(shareText(0)).toBe("0%");
    expect(shareText(0.0004)).toBe("<0.1%");
    expect(shareText(0.0013)).toBe("0.1%");
    expect(shareText(0.25)).toBe("25%");
  });
});

describe("footLines", () => {
  it("names the collection's time, silent servers, cut lists and what was left out", () => {
    const d = { ...global(), unlistedSubscriptions: 3 };
    expect(footLines(d)).toEqual([
      "Subscriptions as of 11:58:30 UTC.",
      "nats-2 did not answer for its subscription list; its subscriptions are missing.",
      "Lists cut at the limit: nats-1 listed 10,000 of 14,212.",
      "1,006 subscriptions of the servers' own clients (JetStream, system and account internals) left out.",
      "3 subscriptions left out whose connection the server's connection list did not include, so it cannot be told from the server's own.",
    ]);
  });

  it("says only the collection's time when every list came whole", () => {
    expect(footLines({ ...global(), servers: [{ name: "nats-0", answered: true, total: 5, listed: 5 }], serverSubscriptions: 0 })).toEqual(["Subscriptions as of 11:58:30 UTC."]);
  });

  it("joins several silent servers", () => {
    const d = { ...global(), servers: [{ name: "a", answered: false }, { name: "b", answered: false }], serverSubscriptions: 0 };
    expect(footLines(d)[1]).toBe("a and b did not answer for their subscription list; their subscriptions are missing.");
  });

  it("says nothing without a subscription collection, which the notice above the table says", () => {
    expect(footLines({ ...global(), subscriptionsAt: undefined })).toEqual([]);
  });
});

describe("cutText and invalidReason", () => {
  it("says how many rows are listed only when the list was cut", () => {
    expect(cutText({ rows: global().rows, rowsTotal: 9 })).toBe("");
    expect(cutText({ rows: global().rows, rowsTotal: 1204 })).toBe("The first 9 of 1,204 rows are listed.");
  });

  it("drops the code's prefix from the server's reason", () => {
    expect(invalidReason('invalid argument: subject "order.*" has a wildcard; give a literal subject')).toBe('subject "order.*" has a wildcard; give a literal subject');
    expect(invalidReason("subject is empty")).toBe("subject is empty");
  });
});

describe("the tester's answer", () => {
  it("says an account reaches nothing only without a stream, a subscriber or its JetStream API", () => {
    expect(reachesNothing(published())).toBe(true);
    expect(reachesNothing(published({ streams: [streamMatch()] }))).toBe(false);
    expect(reachesNothing(published({ core: [coreMatch()] }))).toBe(false);
    expect(reachesNothing(published({ jetstreamApi: true }))).toBe(false);
  });

  it("names the matching consumers and counts the ephemeral ones", () => {
    expect(consumersText(streamMatch())).toBe("email-sender and order-projector");
    expect(consumersText(streamMatch({ consumers: ["a"], ephemeral: 2 }))).toBe("a and 2 ephemeral");
    expect(consumersText(streamMatch({ consumers: [], ephemeral: 0 }))).toBe("");
    expect(consumersText(streamMatch({ kind: "kv", consumers: [], ephemeral: 22 }))).toBe("22 watchers");
    expect(consumersText(streamMatch({ kind: "kv", consumers: ["d"], ephemeral: 1 }))).toBe("d and 1 watcher");
  });

  it("warns that nobody reads a matching stream only when it is a stream that nothing copies", () => {
    const none = { consumers: [], ephemeral: 0, mirrors: [], sourcedBy: [] };
    expect(readers(streamMatch(none))).toEqual({ label: "no consumer reads it", ephemeral: "", warn: true });
    expect(readers(streamMatch({ ...none, mirrors: ["m"] }))).toEqual({ label: "no consumer", ephemeral: "", warn: false });
    expect(readers(streamMatch({ ...none, sourcedBy: ["s"] }))).toEqual({ label: "no consumer", ephemeral: "", warn: false });
    expect(readers(streamMatch({ ...none, kind: "kv" }))).toEqual({ label: "no watcher", ephemeral: "", warn: false });
    expect(readers(streamMatch({ ...none, kind: "object" }))).toEqual({ label: "no consumer", ephemeral: "", warn: false });
    expect(readers(streamMatch({ ...none, kind: "kv", ephemeral: 3 }))).toEqual({ label: "then read by", ephemeral: "3 watchers", warn: false });
    expect(readers(streamMatch({ consumers: ["a"], ephemeral: 2 }))).toEqual({ label: "then consumers", ephemeral: "2 ephemeral", warn: false });
  });

  it("counts a queue group once and every other connection", () => {
    expect(coreText(coreMatch())).toBe("one of 3 in queue group order-service (order-service) via orders.place");
    expect(coreText(coreMatch({ connections: 1 }))).toBe("1 connection in queue group order-service (order-service) via orders.place");
    expect(coreText(coreMatch({ queue: "", subject: "customer.notifications.>", connections: 136, names: ["notification-worker"] }))).toBe(
      "136 connections (notification-worker) via customer.notifications.>",
    );
  });

  it("joins a list in words", () => {
    expect(listText([])).toBe("");
    expect(listText(["a"])).toBe("a");
    expect(listText(["a", "b", "c"])).toBe("a, b and c");
  });
});

describe("delivered rates", () => {
  it("has them only on subscription and inbox rows", () => {
    expect(hasDelivered(sub("a"))).toBe(true);
    expect(hasDelivered(row({ kind: "inbox" }))).toBe(true);
    expect(hasDelivered(row())).toBe(false);
    expect(hasDelivered(row({ kind: "jsApi" }))).toBe(false);
  });

  it("says how many connections were measured only when some were not", () => {
    expect(measuredText(queueDelivered(), 3)).toBe("2 of 3 measured");
    expect(measuredText({ ...queueDelivered(), measured: 3 }, 3)).toBe("");
    expect(measuredText({ measured: 1204, rates: [], moreRates: 0, perSecond: 1 }, 1311)).toBe("1,204 of 1,311 measured");
  });

  it("words the tester's rate, never 0 for not measured", () => {
    expect(deliveredText(queueDelivered(), 3)).toBe("8.82 msgs/s delivered, 2 of 3 connections measured");
    expect(deliveredText({ ...queueDelivered(), measured: 3 }, 3)).toBe("8.82 msgs/s delivered");
    expect(deliveredText({ perSecond: 0, measured: 1, rates: [], moreRates: 0 }, 1)).toBe("0 msgs/s delivered");
    expect(deliveredText({ measured: 0, rates: [], moreRates: 0 }, 2)).toBe("delivered rate not measured");
  });

  it("names a connection by name, server and connection ID", () => {
    const [a, , c] = queueDelivered().rates;
    expect(connectionLabel(a)).toBe("order-service (nats-eu-0 cid 117)");
    expect(connectionLabel({ ...a, name: "" })).toBe("unnamed (nats-eu-0 cid 117)");
    expect(connectionRateText(a)).toBe("4.62 msgs/s");
    expect(connectionRateText(c)).toBe("not measured");
    expect(moreRatesText({ ...queueDelivered(), moreRates: 1 })).toBe("+1 more connection");
    expect(moreRatesText(queueDelivered())).toBe("");
  });
});
