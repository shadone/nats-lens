// The demo site serves the UI under a base path (/nats-lens/). The router
// adds it to every <Link> and navigate(); a link or URL built some other way
// loses it and leads to a page that does not exist. Such places go through
// appHref or appUrl in basePath.ts, or are listed here with why they need
// neither.
import { render, screen } from "@testing-library/react";
import { Link, MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { appHref, appUrl, basePath, routerBasename, slashBaseAddress } from "./basePath";

const sources = import.meta.glob<string>(["../**/*.ts", "../**/*.tsx", "!../**/*.test.ts", "!../**/*.test.tsx", "!../gen/**", "!../test/**"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/**
 * An href not built by appHref, the browser's location by a global name, a
 * page opened or a path to the API written out: each bypasses the router's
 * basename.
 */
const BYPASS = /\bhref=\{(?!appHref\()|\bhref="\/|\b(?:window|document|globalThis|self)\.location\b|\bwindow\.open\(|["'`]\/api\//;

/**
 * The global location, unqualified. A file that names the router's location
 * `location` (const location = useLocation()) reads that one; there only
 * the qualified forms above count.
 */
const BARE_LOCATION = /(?<![\w.$])location\s*(?:\.\s*(?:href|pathname|origin|search|hash|host|assign|replace|reload)\b|=(?!=))/;
const LOCAL_LOCATION = /\b(?:const|let|var)\s+location\b|[({,]\s*location\s*[,)}:]/;

/** Each allowed line, by its file and a part of its text, with why. */
const ALLOWED: { file: string; line: string; why: string }[] = [
  { file: "./basePath.ts", line: "return window.location.origin + appHref(path);", why: "appUrl: the origin before the base path" },
  { file: "./basePath.ts", line: "if (base && window.location.pathname === base) {", why: "slashBaseAddress: the address of the base path itself" },
  {
    file: "./basePath.ts",
    line: 'window.history.replaceState(window.history.state, "", `${base}/${window.location.search}${window.location.hash}`);',
    why: "slashBaseAddress: adds the slash, keeping the query and fragment",
  },
  { file: "../api/wasmTransport.ts", line: 'new URLSearchParams(window.location.search).has("fixed")', why: "the page's own ?fixed, read once at start" },
  {
    file: "../components/InvestigationContext.tsx",
    line: "origin: window.location.origin, pathname: loc.pathname",
    why: "the context's deployment must be a bare origin (internal/investigation refuses a path), and sourcePath is the route path; neither is a link",
  },
  { file: "../components/OnCallFinding.tsx", line: "<a href={playbook}", why: "the playbook, an https URL from the server's configuration" },
  { file: "../components/HostedDemo.tsx", line: '<a className="x" href={REPOSITORY_URL}', why: "the repository on GitHub" },
  { file: "../components/RouteBoundary.tsx", line: '<a className="btn" href={this.props.href}>Reload page</a>', why: "App builds it with appHref" },
];

/** Every line of the sources that bypasses the basename. */
function bypasses(files: Record<string, string>): { file: string; line: string }[] {
  const out: { file: string; line: string }[] = [];
  for (const [file, text] of Object.entries(files)) {
    const local = LOCAL_LOCATION.test(text);
    for (const line of text.split("\n")) {
      if (BYPASS.test(line) || (!local && BARE_LOCATION.test(line))) out.push({ file, line: line.trim() });
    }
  }
  return out;
}

describe("links under a base path", () => {
  it("reads the files it checks", () => {
    expect(Object.keys(sources).length).toBeGreaterThan(50);
    expect(Object.keys(sources)).toContain("../components/ServerHeadline.tsx");
  });

  it("builds links and URLs only through the router or basePath.ts", () => {
    const found = bypasses(sources);
    const unexpected = found.filter((f) => !ALLOWED.some((a) => a.file === f.file && f.line.includes(a.line))).map((f) => `${f.file}: ${f.line}`);
    expect(unexpected).toEqual([]);
    // Each allowance covers one line, which is still there: an allowed use
    // cannot be traded for another on a line of its own.
    for (const a of ALLOWED) {
      expect(found.filter((f) => f.file === a.file && f.line.includes(a.line)), `${a.file}: ${a.line}`).toHaveLength(1);
    }
  });

  it("catches the browser's location by any name", () => {
    const bad = [
      'location.href = "/servers";',
      "return location.pathname;",
      'location.assign("/servers");',
      "location = '/servers';",
      "const u = window.location.href;",
      "const u = globalThis.location.search;",
      'window.open("/servers");',
      '<a href="/servers">',
      "<a href={path}>",
      'fetch("/api/natslens.v1.LensService/GetOverview")',
    ];
    for (const line of bad) expect(bypasses({ "./probe.ts": `function f() {\n${line}\n}` }), line).toHaveLength(1);
  });

  it("leaves the router's location, an appHref link and comparisons alone", () => {
    expect(bypasses({ "./router.ts": "const location = useLocation();\nconst p = location.pathname;\nif (location == x) {}" })).toEqual([]);
    expect(bypasses({ "./ok.tsx": "<a href={appHref(path)}>\nconst loc = useLocation(); loc.pathname;\nprior.location.pathname" })).toEqual([]);
  });
});

describe("basePath", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    window.history.replaceState(null, "", "/");
  });

  it("is empty at the root", () => {
    vi.stubEnv("BASE_URL", "/");
    expect(basePath()).toBe("");
    expect(routerBasename()).toBe("/");
    expect(appHref("/servers?range=1h")).toBe("/servers?range=1h");
    expect(appUrl("/servers")).toBe(`${window.location.origin}/servers`);
  });

  it("keeps the demo site's base path", () => {
    vi.stubEnv("BASE_URL", "/nats-lens/");
    expect(basePath()).toBe("/nats-lens");
    expect(routerBasename()).toBe("/nats-lens/");
    expect(appHref("/servers?range=1h")).toBe("/nats-lens/servers?range=1h");
    expect(appUrl("/replay?a=1")).toBe(`${window.location.origin}/nats-lens/replay?a=1`);
    expect(appUrl("")).toBe(`${window.location.origin}/nats-lens`);
  });

  it("gives the base path's own address its trailing slash, and leaves others", () => {
    vi.stubEnv("BASE_URL", "/nats-lens/");
    window.history.replaceState(null, "", "/nats-lens?fixed#top");
    slashBaseAddress();
    expect(window.location.pathname + window.location.search + window.location.hash).toBe("/nats-lens/?fixed#top");
    window.history.replaceState(null, "", "/nats-lens/servers");
    slashBaseAddress();
    expect(window.location.pathname).toBe("/nats-lens/servers");
  });

  it("links the root with its trailing slash under the router's basename", () => {
    render(
      <MemoryRouter basename="/nats-lens/" initialEntries={["/nats-lens/servers"]}>
        <Link to="/?system=example-prod">Overview</Link>
        <Link to="/servers">Servers</Link>
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute("href", "/nats-lens/?system=example-prod");
    expect(screen.getByRole("link", { name: "Servers" })).toHaveAttribute("href", "/nats-lens/servers");
  });
});
