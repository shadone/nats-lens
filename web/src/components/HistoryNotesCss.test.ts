// The history notes' stylesheet styles only the notes and the hatched gaps:
// every selector names an hn- or gap- class, so it changes no other rule.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const css = read("../styles/history-notes.css").replace(/\/\*[\s\S]*?\*\//g, "");

describe("history-notes.css", () => {
  it("scopes every rule to its own classes", () => {
    const selectors = [...css.matchAll(/([^{}]+)\{[^{}]*\}/g)].flatMap((m) => m[1].split(",")).map((s) => s.trim());
    expect(selectors.length).toBeGreaterThan(4);
    expect(selectors.filter((s) => !/\.(hn|gap)-/.test(s))).toEqual([]);
  });

  it("is imported by the app's stylesheet", () => {
    expect(read("../styles/index.css")).toContain('@import "./history-notes.css";');
  });

  it("lays the hatch over the svg alone: the wrapper takes the svg's 8px top margin", () => {
    expect(css).toMatch(/\.gap-wrap\{[^}]*margin-top:8px/);
    expect(css).toMatch(/\.gap-wrap > svg\{margin-top:0!important\}/);
    for (const [f, rule] of [
      ["../styles/cockpit.css", /\.tc-svg\{[^}]*margin-top:8px/],
      ["../styles/metricbox.css", /\.mbox svg\.spark\{[^}]*margin-top:8px/],
      ["../styles/pages.css", /\.chart svg\.spark\{[^}]*margin-top:8px/],
    ] as const)
      expect(read(f)).toMatch(rule);
  });

  it("keeps a short gap visible on a long chart, and lets the chart's pointer events through the layer", () => {
    expect(css).toMatch(/\.gap-seg\{[^}]*min-width:3px/);
    expect(css).toMatch(/\.gap-layer\{[^}]*pointer-events:none/);
  });
});
