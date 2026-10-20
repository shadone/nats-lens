// The Topology page's stylesheet styles only the page: every selector names
// one of its own classes (tm- for the map, tp- for the page and tables), so
// a rule such as `.tbl tr.warn` cannot change another page's tables.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/topology.css").replace(/\/\*[\s\S]*?\*\//g, "");

/**
 * Whether a selector is scoped to the page: some compound of it, outside
 * :not(...), carries a tm- or tp- class. The subject alone is not enough to
 * ask for: `.tp-card .tbl tr.warn > *` is scoped by its first compound.
 */
function scoped(selector: string): boolean {
  return /\.(tm|tp)-/.test(selector.replace(/:not\([^)]*\)/g, ""));
}

describe("topology.css", () => {
  it("tells a scoped selector from one that only mentions the page in :not()", () => {
    expect(scoped(".tbl tr.warn:not(.tp-x)")).toBe(false);
    expect(scoped(".tbl tr.warn")).toBe(false);
    expect(scoped(".tp-card td")).toBe(true);
    expect(scoped(".tp-card .tbl tr.warn > *")).toBe(true);
    expect(scoped(".tm-box:not(.on)")).toBe(true);
  });

  it("scopes every rule to the page's own classes", () => {
    const selectors = [...css.matchAll(/([^{}]+)\{[^{}]*\}/g)]
      .map((m) => m[1].trim())
      .filter((s) => !s.startsWith("@") && !s.startsWith(":root"))
      .flatMap((s) => s.split(","))
      .map((s) => s.trim());
    expect(selectors.length).toBeGreaterThan(20);
    expect(selectors.filter((s) => !scoped(s))).toEqual([]);
  });
});
