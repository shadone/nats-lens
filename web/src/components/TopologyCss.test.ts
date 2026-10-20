// The Topology page's stylesheet styles only the page: every selector names
// one of its own classes (tm- for the map, tp- for the page and tables), so
// a rule such as `.tbl tr.warn` cannot change another page's tables.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/topology.css").replace(/\/\*[\s\S]*?\*\//g, "");

describe("topology.css", () => {
  it("scopes every rule to the page's own classes", () => {
    const selectors = [...css.matchAll(/([^{}]+)\{[^{}]*\}/g)]
      .map((m) => m[1].trim())
      .filter((s) => !s.startsWith("@") && !s.startsWith(":root"))
      .flatMap((s) => s.split(","))
      .map((s) => s.trim());
    expect(selectors.length).toBeGreaterThan(20);
    expect(selectors.filter((s) => !/\.(tm|tp)-/.test(s))).toEqual([]);
  });
});
