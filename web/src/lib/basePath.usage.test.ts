// The demo site serves the UI under a base path (/nats-lens/). The router
// adds it to every <Link> and navigate(); a link or URL built some other way
// loses it and leads to a page that does not exist. Such places go through
// appHref or appUrl in basePath.ts, or are listed here with why they need
// neither.
import { afterEach, describe, expect, it, vi } from "vitest";
import { appHref, appUrl, basePath } from "./basePath";

const sources = import.meta.glob<string>(["../**/*.ts", "../**/*.tsx", "!../**/*.test.ts", "!../**/*.test.tsx", "!../gen/**", "!../test/**"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/**
 * An href not built by appHref, the browser's own location, a page opened or
 * a path to the API written out: each bypasses the router's basename.
 */
const BYPASS = /\bhref=\{(?!appHref\()|\bhref="\/|\b(?:window|document|globalThis)\.location\b|\bwindow\.open\(|["'`]\/api\//g;

/** Files and how many of those each may hold, with why. */
const ALLOWED: Record<string, number> = {
  // window.location.origin, which appUrl puts before the base path.
  "./basePath.ts": 1,
  // The page's own ?fixed, read once at start.
  "../api/wasmTransport.ts": 1,
  // The context's deployment must be a bare origin (internal/investigation
  // refuses a path), and its sourcePath is the route path; neither is a link.
  "../components/InvestigationContext.tsx": 1,
  // The playbook link, an https URL from the server's configuration.
  "../components/OnCallFinding.tsx": 1,
  // The repository on GitHub.
  "../components/HostedDemo.tsx": 1,
  // The Reload page link, built by App with appHref.
  "../components/RouteBoundary.tsx": 1,
};

describe("links under a base path", () => {
  it("reads the files it checks", () => {
    expect(Object.keys(sources).length).toBeGreaterThan(50);
    expect(Object.keys(sources)).toContain("../components/ServerHeadline.tsx");
  });

  it("builds links and URLs only through the router or basePath.ts", () => {
    const found: Record<string, number> = {};
    for (const [path, text] of Object.entries(sources)) {
      const n = text.match(BYPASS)?.length ?? 0;
      if (n > 0) found[path] = n;
    }
    expect(found).toEqual(ALLOWED);
  });
});

describe("basePath", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("is empty at the root", () => {
    vi.stubEnv("BASE_URL", "/");
    expect(basePath()).toBe("");
    expect(appHref("/servers?range=1h")).toBe("/servers?range=1h");
    expect(appUrl("/servers")).toBe(`${window.location.origin}/servers`);
  });

  it("keeps the demo site's base path", () => {
    vi.stubEnv("BASE_URL", "/nats-lens/");
    expect(basePath()).toBe("/nats-lens");
    expect(appHref("/servers?range=1h")).toBe("/nats-lens/servers?range=1h");
    expect(appUrl("/replay?a=1")).toBe(`${window.location.origin}/nats-lens/replay?a=1`);
    expect(appUrl("")).toBe(`${window.location.origin}/nats-lens`);
  });
});
