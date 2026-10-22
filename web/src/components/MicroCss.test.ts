// The Micro services page's stylesheet styles only the page: every selector
// names one of its mi- classes, so a rule such as `.tbl .mi-quiet` cannot
// change another page's tables.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/micro.css").replace(/\/\*[\s\S]*?\*\//g, "");

function scoped(selector: string): boolean {
  return /\.mi-/.test(selector.replace(/:not\([^)]*\)/g, ""));
}

describe("micro.css", () => {
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
    expect(read("../styles/index.css")).toContain('@import "./micro.css";');
  });

  it("lays the cards three a row at desktop width and one a row at phone width", () => {
    const wide = css.slice(0, css.indexOf("@media"));
    const phone = css.slice(css.indexOf("@media (max-width:760px)"));
    expect(wide).toMatch(/\.mi-cards\{display:grid;grid-template-columns:repeat\(3,minmax\(0,1fr\)\)/);
    expect(phone).toMatch(/\.mi-cards\{grid-template-columns:minmax\(0,1fr\)/);
  });

  it("lets names, versions, notes and notices break inside their box", () => {
    for (const cls of ["mi-name", "mi-ver", "mi-desc", "mi-note", "mi-notice"]) {
      expect(css).toMatch(new RegExp(`\\.${cls}\\{[^}]*overflow-wrap:anywhere`));
    }
  });
});
