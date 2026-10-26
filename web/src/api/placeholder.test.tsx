// keepSameSystem keeps a previous answer only for the same system and the
// same values at the other positions asked for. The hooks that use it are
// tested in placeholder.hooks.test.tsx.
import { describe, expect, it } from "vitest";
import { keepSameSystem } from "./placeholder";

describe("keepSameSystem", () => {
  const prev = { rows: 1 };
  it("keeps the previous answer of the same system", () => {
    expect(keepSameSystem("shop-prod")(prev, { queryKey: ["overview", "shop-prod", "live:24h"] })).toBe(prev);
  });
  it("drops another system's answer", () => {
    expect(keepSameSystem("shop-staging")(prev, { queryKey: ["overview", "shop-prod", "live:24h"] })).toBeUndefined();
  });
  it("tells the default system apart from a named one", () => {
    expect(keepSameSystem("shop-prod")(prev, { queryKey: ["overview", "", "live:24h"] })).toBeUndefined();
  });
  it("drops an answer whose other named positions differ", () => {
    const keep = keepSameSystem("shop-prod", { 3: "APP" });
    expect(keep(prev, { queryKey: ["subjects", "shop-prod", "live:24h", "APP"] })).toBe(prev);
    expect(keep(prev, { queryKey: ["subjects", "shop-prod", "live:24h", "SYS"] })).toBeUndefined();
  });
  it("keeps nothing without a previous query", () => {
    expect(keepSameSystem("shop-prod")(prev, undefined)).toBeUndefined();
  });
});
