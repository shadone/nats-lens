import { describe, expect, it } from "vitest";
import { searchItems, type SearchItem } from "./search";

const items: SearchItem[] = [
  { kind: "server", name: "nats-1", sub: "eu", path: "/servers/nats-1" },
  { kind: "stream", name: "order-jobs", sub: "APP", path: "/streams/APP/order-jobs" },
  { kind: "consumer", name: "resize-worker", sub: "order-jobs", path: "/consumers/APP/order-jobs/resize-worker" },
  { kind: "consumer", name: "order-audit", sub: "order-jobs", path: "/consumers/APP/order-jobs/order-audit" },
  { kind: "kv", name: "customer-config", sub: "APP", path: "/streams/APP/KV_customer-config" },
  { kind: "account", name: "APP", sub: "", path: "?account=APP" },
];

describe("searchItems", () => {
  it("finds by part of the name, ignoring case", () => {
    expect(searchItems(items, "RESIZE").map((i) => i.name)).toEqual(["resize-worker"]);
  });

  it("puts names that start with the text first", () => {
    expect(searchItems(items, "order").map((i) => i.name)).toEqual(["order-audit", "order-jobs"]);
  });

  it("returns nothing for blank text and at most the limit", () => {
    expect(searchItems(items, "  ")).toEqual([]);
    expect(searchItems(items, "o", 2)).toHaveLength(2);
  });
});
