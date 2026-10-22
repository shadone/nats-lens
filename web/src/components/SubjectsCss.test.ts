// The Subjects page's stylesheet styles only the page: every selector names
// one of its sj- classes, so a rule such as `.tbl .sj-quiet` cannot change
// another page's tables.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/subjects.css").replace(/\/\*[\s\S]*?\*\//g, "");

function scoped(selector: string): boolean {
  return /\.sj-/.test(selector.replace(/:not\([^)]*\)/g, ""));
}

describe("subjects.css", () => {
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
    expect(read("../styles/index.css")).toContain('@import "./subjects.css";');
  });
});

describe("subjects.css at a phone width", () => {
  const phone = css.slice(css.indexOf("@media (max-width:760px)"));

  it("gives the subject field the whole line", () => {
    expect(phone).toMatch(/\.sj-field-wrap\{width:100%\}/);
  });

  it("keeps the pattern column in view and lets a long pattern break inside it", () => {
    expect(phone).toMatch(/\.tbl\.sj-tbl tbody th\[scope="row"\]\{position:sticky;[^}]*max-width:150px/);
    expect(phone).toMatch(/\.sj-tbl \.sj-pattern\{white-space:normal\}/);
    expect(phone).toMatch(/\.sj-tbl \.sj-tok\{display:inline-block;white-space:nowrap\}/);
  });

  it("keeps a pattern on one line above phone width", () => {
    const wide = css.slice(0, css.indexOf("@media (max-width:760px)"));
    expect(wide).toMatch(/\.sj-tbl \.sj-pattern\{display:block;white-space:nowrap\}/);
    expect(wide).not.toMatch(/\.sj-tok/);
  });

  it("keeps a warning row's colour on its sticky first cell", () => {
    expect(phone).toMatch(/\.sj-tbl tr\.sj-warn > th\[scope="row"\]\{background:var\(--sev-warning-bg\)\}/);
  });
});
