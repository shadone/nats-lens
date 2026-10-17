// The in-sync placement square must stand out from the card: its edge
// reaches 3:1 against the card surface in both themes.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const tokens = read("./tokens.css");
const placement = read("./placement.css");

/** The token values of one block: the light :root, or the explicit dark theme. */
function block(selector: string): Record<string, string> {
  const at = tokens.indexOf(`${selector}{`);
  const body = tokens.slice(at, tokens.indexOf("}", at));
  return Object.fromEntries([...body.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})/gi)].map((m) => [m[1], m[2]]));
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

describe("the in-sync placement square", () => {
  const rule = placement.match(/\.pl-sync\{([^}]*)\}/)?.[1] ?? "";
  const edge = rule.match(/stroke:var\(--([\w-]+)\)/)?.[1];

  it("draws its edge at full opacity", () => {
    expect(edge).toBeDefined();
    expect(rule).not.toMatch(/stroke-opacity/);
  });

  it.each([
    ["light", ":root"],
    ["dark", ':root[data-theme="dark"]'],
  ])("reaches 3:1 against the card in the %s theme", (_, selector) => {
    const t = { ...block(":root"), ...block(selector) };
    for (const card of ["surface", "surface-2"]) expect(contrast(t[edge!], t[card])).toBeGreaterThanOrEqual(3);
  });
});
