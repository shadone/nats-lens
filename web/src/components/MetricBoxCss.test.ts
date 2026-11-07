// A warning tile shows one amber: the range track's dot takes the same mark
// colour as the chart's warning marks, which every theme defines.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

describe("the warning colour of a metric box", () => {
  it("draws the warning track dot in the mark amber", () => {
    expect(read("../styles/metricbox.css")).toMatch(/\.mb-dot\.st-warning\{background:var\(--mark-warning\)\}/);
  });

  it("defines the mark amber in the light theme and both dark ones", () => {
    const tokens = read("../styles/tokens.css");
    const blocks = [/^:root\{[^}]*\}/m, /prefers-color-scheme: dark\)\{\s*:root:not\(\[data-theme="light"\]\)\{[^}]*\}/, /:root\[data-theme="dark"\]\{[^}]*\}/];
    for (const b of blocks) expect(tokens.match(b)?.[0]).toMatch(/--mark-warning:#[0-9a-f]{6};/);
  });
});
