// The Capacity page's stylesheet styles only the page: every selector names
// one of its cap- classes, so a rule such as `.tbl .cap-ok` cannot change
// another page's tables.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/capacity.css").replace(/\/\*[\s\S]*?\*\//g, "");

/** Whether some compound of the selector, outside :not(...), carries a cap- class. */
function scoped(selector: string): boolean {
  return /\.cap-/.test(selector.replace(/:not\([^)]*\)/g, ""));
}

describe("capacity.css", () => {
  it("tells a scoped selector from one that only mentions the page in :not()", () => {
    expect(scoped(".tbl tr.warn:not(.cap-x)")).toBe(false);
    expect(scoped(".tbl small")).toBe(false);
    expect(scoped(".cap-tbl small")).toBe(true);
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

  it("is imported by the app's stylesheet", () => {
    expect(read("../styles/index.css")).toContain('@import "./capacity.css";');
  });
});

describe("capacity.css bars", () => {
  it("gives a store's fill a minimum width only above 0", () => {
    expect(css).not.toMatch(/\.cap-bar-fill\{[^}]*min-width/);
    expect(css).toMatch(/\.cap-bar-fill\.cap-bar-some\{min-width:3px\}/);
  });
});

describe("capacity.css at a phone width", () => {
  it("stacks the server cards in one column", () => {
    const phone = css.slice(css.indexOf("@media (max-width:760px)"));
    expect(phone).toMatch(/\.cap-cards\{grid-template-columns:minmax\(0,1fr\)\}/);
  });

  it("lets the lines under a cell wrap, so a long one does not widen its column", () => {
    const rule = css.match(/\.cap-tbl small\{([^}]*)\}/)![1];
    expect(rule).not.toMatch(/white-space:nowrap/);
  });
});

describe("capacity.css stream names", () => {
  it("breaks a long stream name, so it stays inside the sticky first column at a phone width", () => {
    expect(css).toMatch(/\.cap-tbl \.cap-name\{[^}]*overflow-wrap:anywhere/);
  });
});
