// The Applications page's stylesheet styles only the page: every selector
// names one of its ap- classes, so a rule cannot change another page.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/apps.css").replace(/\/\*[\s\S]*?\*\//g, "");

describe("apps.css", () => {
  it("scopes every rule to the page's own classes", () => {
    const selectors = [...css.matchAll(/([^{}]+)\{[^{}]*\}/g)]
      .map((m) => m[1].trim())
      .filter((s) => !s.startsWith("@"))
      .flatMap((s) => s.split(","))
      .map((s) => s.trim());
    expect(selectors.length).toBeGreaterThan(20);
    expect(selectors.filter((s) => !/\.ap-/.test(s))).toEqual([]);
  });

  it("is imported by the app's stylesheet", () => {
    expect(read("../styles/index.css")).toContain('@import "./apps.css";');
  });

  it("lays the summary cards three a row at desktop width and one a row at phone width", () => {
    const wide = css.slice(0, css.indexOf("@media"));
    const phone = css.slice(css.indexOf("@media (max-width:760px)"));
    expect(wide).toMatch(/\.ap-cards\{display:grid;grid-template-columns:repeat\(3,minmax\(0,1fr\)\)/);
    expect(phone).toMatch(/\.ap-cards\{grid-template-columns:minmax\(0,1fr\)/);
  });
});
