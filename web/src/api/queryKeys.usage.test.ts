// A mute or acknowledgement refreshes only the queries whose key names its
// system at index 1 (operatorStateFilter in queries.ts). A query keyed
// another way is never refreshed by one, so a status-bearing view keyed
// that way would keep showing a stale mute. Every query key in the UI
// therefore puts the page's system second; the families listed here hold
// no mute or acknowledgement state, or are refreshed by name.
import { describe, expect, it } from "vitest";
import { uiSources } from "../test/sources";

/** Families keyed without the system at index 1, with why. */
const ALLOWED: Record<string, string> = {
  // A storage comparison of two pinned collections: sizes, no findings.
  "storage-change": "no operator state",
  // The source clock a live comparison resolves its range against.
  "capacity-comparison-clock": "no operator state",
  // The storage contributors of a comparison: sizes, no findings.
  "capacity-contributors": "no operator state",
  // The metric catalogue is the same for every system.
  "metric-catalogue": "no operator state",
  // The offset of the server's clock from the browser's.
  serverClock: "no operator state",
  // The fleet tally; operatorStateFilter refreshes it by name.
  systemSummaries: "refreshed by name",
};

/** The elements of the bracketed list that starts at text[open], split at top-level commas. */
function elements(text: string, open: number): string[] {
  const out: string[] = [];
  let depth = 0, start = open + 1, quote = "";
  for (let i = open; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote) quote = "";
      continue;
    }
    if (c === '"' || c === "'" || c === "`") quote = c;
    else if ("([{".includes(c)) depth++;
    else if (")]}".includes(c)) {
      depth--;
      if (depth === 0) {
        out.push(text.slice(start, i).trim());
        return out.filter((e) => e !== "");
      }
    } else if (c === "," && depth === 1) {
      out.push(text.slice(start, i).trim());
      start = i + 1;
    }
  }
  throw new Error(`unclosed key at ${open}`);
}

const SYSTEM = /^(sys|system|[\w.]+\.system)$/;
const unquote = (s: string) => s.replace(/^["'`](.*)["'`]$/, "$1");

/** Each query key in the sources: where it is, its family and the element at index 1. */
function queryKeys(sources: Record<string, string>) {
  const out: { at: string; family: string; second: string }[] = [];
  const all = Object.values(sources).join("\n");
  for (const [path, text] of Object.entries(sources)) {
    for (const m of text.matchAll(/queryKey\s*:\s*/g)) {
      let at = m.index + m[0].length;
      // A type annotation ({ queryKey: readonly unknown[] }), not a key.
      if (/^readonly\b/.test(text.slice(at))) continue;
      const call = /^(\w+)\s*\(/.exec(text.slice(at));
      const name = /^(\w+)\s*[,}]/.exec(text.slice(at));
      let src = text;
      if (call) {
        // A key factory: function f(...) { return [...] } or const f = (...) => [...], in any file.
        const def = new RegExp(`(?:function\\s+${call[1]}\\s*\\([^)]*\\)[^{]*\\{\\s*return|(?:const|let)\\s+${call[1]}\\s*=\\s*\\([^)]*\\)\\s*=>)\\s*\\[`).exec(all);
        if (!def) {
          out.push({ at: `${path}: ${call[1]}()`, family: `unresolved factory ${call[1]}`, second: "" });
          continue;
        }
        src = all;
        at = def.index + def[0].length - 1;
      } else if (name) {
        // A key held in a variable: the nearest const name = [...] before it.
        const defs = [...text.slice(0, at).matchAll(new RegExp(`const\\s+${name[1]}\\s*=\\s*\\[`, "g"))];
        const def = defs.at(-1);
        if (!def) {
          out.push({ at: `${path}: ${name[1]}`, family: `unresolved variable ${name[1]}`, second: "" });
          continue;
        }
        at = def.index + def[0].length - 1;
      } else if (text[at] !== "[") {
        out.push({ at: `${path}: ${text.slice(at, at + 30)}`, family: "unresolved key", second: "" });
        continue;
      }
      const [family = "", second = ""] = elements(src, at);
      out.push({ at: `${path}: ${src.slice(at, at + 60).split("\n")[0]}`, family: unquote(family), second });
    }
  }
  return out;
}

describe("query keys", () => {
  const keys = queryKeys(uiSources());

  it("reads the keys it checks", () => {
    expect(keys.length).toBeGreaterThan(50);
    expect(keys.map((k) => k.family)).toContain("time-windows");
    expect(keys.map((k) => k.family)).toContain("data-history");
  });

  it("put the page's system at index 1, except the listed families", () => {
    const bad = keys.filter((k) => !SYSTEM.test(k.second) && !(k.family in ALLOWED)).map((k) => k.at);
    expect(bad).toEqual([]);
  });

  it("lists only families that still exist", () => {
    const families = new Set(keys.map((k) => k.family));
    expect(Object.keys(ALLOWED).filter((f) => !families.has(f))).toEqual([]);
  });

  it("finds a key that puts its system elsewhere", () => {
    const found = queryKeys({ "x.ts": `useQuery({ queryKey: ["status-view", rangeKey(spec), sys], queryFn })` });
    expect(found).toEqual([expect.objectContaining({ family: "status-view", second: "rangeKey(spec)" })]);
    expect(SYSTEM.test(found[0].second)).toBe(false);
    const factory = queryKeys({ "y.ts": `const k=(s:string)=>["view",s];useQuery({queryKey:k(sys)})` });
    expect(factory).toEqual([expect.objectContaining({ family: "view", second: "s" })]);
  });
});
