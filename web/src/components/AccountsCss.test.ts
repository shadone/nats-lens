// The Accounts page's stylesheet styles only the page: every selector names
// one of its ac- classes, so a rule such as `.tbl tr.ac-hot` cannot change
// another page's tables.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/accounts.css").replace(/\/\*[\s\S]*?\*\//g, "");

/** Whether some compound of the selector, outside :not(...), carries an ac- class. */
function scoped(selector: string): boolean {
  return /\.ac-/.test(selector.replace(/:not\([^)]*\)/g, ""));
}

describe("accounts.css", () => {
  it("tells a scoped selector from one that only mentions the page in :not()", () => {
    expect(scoped(".tbl tr.warn:not(.ac-x)")).toBe(false);
    expect(scoped(".tbl small")).toBe(false);
    expect(scoped(".ac-card .tbl small")).toBe(true);
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
    expect(read("../styles/index.css")).toContain('@import "./accounts.css";');
  });
});

describe("accounts.css bars", () => {
  it("gives a fill a minimum width only above 0", () => {
    expect(css).not.toMatch(/\.ac-bar-fill\{[^}]*min-width/);
    expect(css).toMatch(/\.ac-bar-fill\.ac-bar-some\{min-width:2px\}/);
  });
});

describe("accounts.css small lines", () => {
  it("lets the lines under a cell wrap, so a long one does not widen its column at a phone width", () => {
    const rule = css.match(/\.ac-card \.tbl small\{([^}]*)\}/)![1];
    expect(rule).not.toMatch(/white-space:nowrap/);
  });
});

describe("accounts.css parts of a line", () => {
  it("keeps a part of a line under a cell on one line", () => {
    expect(css).toMatch(/\.ac-card \.tbl small \.ac-nw\{white-space:nowrap\}/);
  });
});
