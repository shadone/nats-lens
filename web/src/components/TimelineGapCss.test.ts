// A hatched capture gap is a mark button: its dashed edges must outrank the
// mark buttons' border reset, inside the 4px minimum width.
import { readFileSync } from "node:fs";
import { expect, it } from "vitest";

const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const cockpit = read("../styles/cockpit.css").replace(/\/\*[\s\S]*?\*\//g, "");
const timeline = read("../styles/timeline.css").replace(/\/\*[\s\S]*?\*\//g, "");

it("draws the dashed edges of a capture gap mark over the button reset", () => {
  expect(timeline).toMatch(/button\.timeline-mark\s*\{[^}]*border:\s*0/);
  const rule = cockpit.match(/button\.timeline-mark\.sl-gap\s*\{([^}]*)\}/)?.[1] ?? "";
  expect(rule).toMatch(/border-inline:\s*1px dashed/);
  expect(rule).toMatch(/box-sizing:\s*border-box/);
  expect(cockpit).toMatch(/\.sl-gap\s*\{[^}]*min-width:\s*var\(--sp-1\)/);
});
