import { describe, expect, it } from "vitest";
import { entityPath, streamPath } from "./view";
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
