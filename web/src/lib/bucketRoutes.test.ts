import { describe, expect, it } from "vitest";
import { entityName, entityPath, streamPath } from "./view";
import type { Entity } from "../api/model";

describe("bucket resource routes", () => {
  it.each([
    ["KV_partner-info", "/kv-stores/APP/partner-info"],
    ["OBJ_product-images", "/object-stores/APP/product-images"],
    ["orders", "/streams/APP/orders"],
    ["KV_", "/streams/APP/KV_"],
    ["OBJ_", "/streams/APP/OBJ_"],
  ])("routes %s by resource identity", (name, path) => {
    expect(streamPath("APP", name)).toBe(path);
    const e: Entity = { kind: "stream", key: `APP/${name}`, account: "APP", stream: name, name, consumer: "", server: "" };
    expect(entityPath(e)).toBe(path);
  });
  it("encodes account and bucket as independent path segments", () => {
    expect(streamPath("A/B", "KV_商品 #1")).toBe("/kv-stores/A%2FB/%E5%95%86%E5%93%81%20%231");
  });
});

it("labels bucket entities without their storage prefix", () => {
  const e: Entity = { kind: "stream", key: "APP/KV_preferences", account: "APP", stream: "KV_preferences", name: "KV_preferences", consumer: "", server: "" };
  expect(entityName(e)).toBe("preferences");
});
it("keeps ordinary stream display names and missing-identity fallbacks", () => {
  const e: Entity = { kind: "stream", key: "APP/orders", account: "APP", stream: "orders", name: "Order events", consumer: "", server: "" };
  expect(entityName(e)).toBe("Order events");
  expect(entityName({ ...e, stream: "", name: "" })).toBe("APP/orders");
});
