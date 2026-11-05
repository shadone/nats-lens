// The Data history page's stylesheet styles only the page: every selector
// names one of its dh- classes, so a rule such as `.tbl small` cannot
// change another page's tables.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/history.css").replace(/\/\*[\s\S]*?\*\//g, "");

/** Whether some compound of the selector, outside :not(...), carries a dh- class. */
function scoped(selector: string): boolean {
  return /\.dh-/.test(selector.replace(/:not\([^)]*\)/g, ""));
}

describe("history.css", () => {
  it("tells a scoped selector from one that only mentions the page in :not()", () => {
    expect(scoped(".tbl tr:not(.dh-x)")).toBe(false);
    expect(scoped(".mb-note")).toBe(false);
    expect(scoped(".dh-box .mb-note")).toBe(true);
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
    expect(read("../styles/index.css")).toContain('@import "./history.css";');
  });
});

describe("history.css at a narrow width", () => {
  it("puts the coverage boxes two to a row", () => {
    const narrow = css.slice(css.indexOf("@media (max-width:1000px)"));
    expect(narrow).toMatch(/\.dh-boxes\{grid-template-columns:repeat\(2,minmax\(0,1fr\)\)\}/);
  });

  it("lets a box's title wrap, so it is never cut", () => {
    expect(css).toMatch(/\.dh-box \.mb-t\{white-space:normal\}/);
  });
});

describe("history.css strip", () => {
  it("keeps a short gap visible on a long range", () => {
    expect(css).toMatch(/\.dh-seg\{[^}]*min-width:4px/);
  });
});
