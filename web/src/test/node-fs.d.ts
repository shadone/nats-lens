// The one Node API the tests read files with; the project has no Node
// types, and vitest's css: false empties ?raw CSS imports.
declare module "node:fs" {
  export function readFileSync(path: URL | string, encoding: "utf8"): string;
}
