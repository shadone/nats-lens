// The header's time range never makes the page wider than the viewport: on
// a row of its own it shrinks to the bar, the date span giving way first and
// the freshness last, each cut with an ellipsis. jsdom does no layout, so the
// rules are read from the stylesheet; the widths were measured in a browser.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/header.css").replace(/\/\*[\s\S]*?\*\//g, "");

/** The declarations of the first rule whose selector is exactly `sel`, outside any media query. */
function rule(sel: string): string {
  const top = css.slice(0, css.indexOf("@media"));
  const esc = sel.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?:^|\\})\\s*${esc}\\{([^}]*)\\}`).exec(top)?.[1] ?? "";
}

describe("header.css, the time range", () => {
  it("lets the range shrink below its text width, at most as wide as the bar", () => {
    expect(rule(".bar .range-wrap")).toMatch(/min-width:0/);
    expect(rule(".bar .range-wrap")).toMatch(/max-width:100%/);
    expect(rule(".range")).toMatch(/max-width:100%/);
  });

  it("gives the freshness its whole width before the date span gets any", () => {
    expect(rule(".range")).toMatch(/display:grid;grid-template-columns:auto minmax\(0,1fr\) minmax\(0,max-content\)/);
  });

  it("cuts the date span, then the freshness, with an ellipsis", () => {
    expect(rule(".range .span")).toMatch(/min-width:0;overflow:hidden;text-overflow:ellipsis/);
    const live = rule(".live");
    expect(live).toMatch(/display:block/);
    expect(live).toMatch(/overflow:hidden;text-overflow:ellipsis;white-space:nowrap/);
  });
});
