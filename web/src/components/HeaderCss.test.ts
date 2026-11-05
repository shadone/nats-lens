// A tripwire on the header's stylesheet: it fails when a rule that keeps the
// time range inside the bar is removed or changed. It cannot see layout
// (jsdom does none); the guard that the page never scrolls sideways is the
// browser measurement described under "UI" in docs/design.md.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");

/** Declarations per selector of the rules outside any media query, later rules winning. */
function rules(css: string): Map<string, Map<string, string>> {
  let flat = css.replace(/\/\*[\s\S]*?\*\//g, "");
  // Drop each @media block with its nested rules.
  for (let i = flat.indexOf("@media"); i >= 0; i = flat.indexOf("@media")) {
    let depth = 0;
    let j = flat.indexOf("{", i);
    for (; j < flat.length; j++) {
      if (flat[j] === "{") depth++;
      else if (flat[j] === "}" && --depth === 0) break;
    }
    flat = flat.slice(0, i) + flat.slice(j + 1);
  }
  const out = new Map<string, Map<string, string>>();
  for (const [, sels, body] of flat.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    for (const raw of sels.split(",")) {
      const sel = raw.trim().replace(/\s+/g, " ");
      const decls = out.get(sel) ?? new Map<string, string>();
      for (const d of body.split(";")) {
        const k = d.indexOf(":");
        if (k > 0) decls.set(d.slice(0, k).trim(), d.slice(k + 1).trim().replace(/\s+/g, " ").replace(/\s*,\s*/g, ","));
      }
      out.set(sel, decls);
    }
  }
  return out;
}

const css = rules(read("../styles/header.css"));
const decl = (sel: string, prop: string) => css.get(sel)?.get(prop);

describe("header.css, the time range", () => {
  it("lets the range shrink below its text width, at most as wide as the bar", () => {
    expect(decl(".bar .range-wrap", "min-width")).toBe("0");
    expect(decl(".bar .range-wrap", "max-width")).toBe("100%");
    expect(decl(".range", "max-width")).toBe("100%");
  });

  it("gives the freshness its whole width before the date span gets any", () => {
    expect(decl(".range", "display")).toBe("grid");
    expect(decl(".range", "grid-template-columns")).toBe("auto minmax(0,1fr) minmax(0,max-content)");
  });

  it("cuts the date span, then the freshness, with an ellipsis", () => {
    for (const sel of [".range .span", ".live"]) {
      expect(decl(sel, "overflow")).toBe("hidden");
      expect(decl(sel, "text-overflow")).toBe("ellipsis");
    }
    expect(decl(".live", "display")).toBe("block");
  });
});
