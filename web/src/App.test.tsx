// The shell: sidebar, system switcher, header, and links that keep the
// range, system and account.
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { useNavigate } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { lens } from "./api/client";
import * as requests from "./api/requests";
import { RLink } from "./components/common";
import { renderApp } from "./test/render";

vi.mock("./api/client", () => ({
  lens: {
    listSystems: vi.fn(),
    listAccounts: vi.fn(),
    listKeyValueStores: vi.fn(),
    listObjectStores: vi.fn(),
  },
}));

vi.mock("./api/requests", async (orig) => {
  const real = await orig<typeof import("./api/requests")>();
  const pending = () => vi.fn(() => new Promise<never>(() => {}));
  return {
    ...real,
    getLensInfo: vi.fn(),
    getHeaderStatus: vi.fn(),
    getOverview: pending(),
    listFindings: pending(),
    getTimeline: pending(),
    listEvents: pending(),
    listStreams: vi.fn(),
    getStream: pending(),
    listConsumers: vi.fn(),
    getConsumer: pending(),
    listServers: vi.fn(),
    getServer: pending(),
    listConnections: pending(),
    getSeries: pending(),
    listMutes: pending(),
  };
});

const m = vi.mocked(requests);
const l = vi.mocked(lens);

const ref = (id: string, name: string, environment = "") => ({ system: { id, name, environment } });
const THREE = { systems: [ref("example-prod", "Production", "production"), ref("example-global", "Global"), ref("example-staging", "Staging")], defaultSystem: "example-prod" };
const ONE = { systems: [ref("default", "default")], defaultSystem: "default" };

const tally = { critical: 0, warning: 2, info: 1, muted: 0, flapping: 0, resolved24h: 0 };
const entity = (stream: string, consumer = "") => ({ kind: "stream" as const, key: "", name: consumer || stream, account: "APP", stream, consumer, server: "" });

function setSystems(r: unknown) {
  l.listSystems.mockResolvedValue(r as never);
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  setSystems(THREE);
  l.listAccounts.mockResolvedValue({ accounts: [{ account: "APP", accountName: "", isSystem: false }, { account: "SYS", accountName: "", isSystem: true }] } as never);
  l.listKeyValueStores.mockResolvedValue({ keyValueStores: [] } as never);
  l.listObjectStores.mockResolvedValue({ objectStores: [] } as never);
  m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });
  m.getHeaderStatus.mockResolvedValue({ cluster: "c", freshness: { epoch: "1", epochAt: Date.now(), partial: false, live: true, intervalMs: 30_000 }, servers: [], tally });
  m.listServers.mockResolvedValue({ servers: [], expected: [], missing: [] });
  m.listStreams.mockResolvedValue({ streams: [], totals: { bytes: 0, durableConsumers: 0, ephemeralConsumers: 0 } } as never);
  m.listConsumers.mockResolvedValue([{ entity: entity("order-jobs", "resize-worker") }] as never);
});

function location() {
  return screen.getByTestId("location").textContent ?? "";
}

function params(href: string | null) {
  return new URLSearchParams((href ?? "").split("?")[1] ?? "");
}

const nav = () => screen.getByRole("navigation", { name: "Pages" });

let go: ReturnType<typeof useNavigate> = () => {};
function Nav() {
  go = useNavigate();
  return null;
}

const server = (name: string) =>
  ({ name, serverId: name, version: "2.12.2", cluster: "c", role: "follower", present: true, healthz: "ok", healthzError: "", restarts: [], status: "ok" }) as never;

async function switchTo(name: RegExp) {
  await userEvent.click(await screen.findByRole("button", { name: /Switch system/ }));
  await userEvent.click(screen.getByRole("menuitemradio", { name }));
}

describe("switching system", () => {
  it("never shows the old system's rows while the new system's are loading", async () => {
    m.listServers.mockImplementation(async (sys) => (sys === "example-prod" ? { servers: [server("prod-server-1")], expected: [], missing: [] } : new Promise<never>(() => {})));
    renderApp(<App />, "/servers?system=example-prod");
    expect(await screen.findByText("prod-server-1")).toBeInTheDocument();
    await switchTo(/Global/);
    expect(location()).toBe("/servers?system=example-global");
    expect(screen.queryByText("prod-server-1")).toBeNull();
  });

  it("starts the connections of the new system at page one", async () => {
    const conn = { key: "nats-1/7", server: "nats-1", cid: "7", name: "resize-worker", lang: "go", version: "1", account: "APP", tooling: false, user: "", ip: "10.0.0.7" };
    const page = (n: number, next: string) => ({ items: [conn] as never, nextPageToken: next, totalCount: n, epoch: "1", toolingHidden: 0 });
    m.listConnections.mockImplementation(async (sys, _spec, f) => page(200, sys === "example-prod" && !f.pageToken ? "prod-token-2" : ""));
    renderApp(<App />, "/connections?system=example-prod");
    await userEvent.click(await screen.findByRole("button", { name: "Next" }));
    await waitFor(() => expect(m.listConnections).toHaveBeenCalledWith("example-prod", expect.anything(), expect.objectContaining({ pageToken: "prod-token-2" }), expect.anything()));
    m.listConnections.mockClear();
    await switchTo(/Global/);
    await waitFor(() => expect(m.listConnections).toHaveBeenCalledWith("example-global", expect.anything(), expect.anything(), expect.anything()));
    for (const c of m.listConnections.mock.calls) expect(c[2].pageToken ?? "").toBe("");
  });

  it("goes back to the earlier system with Back", async () => {
    m.listServers.mockImplementation(async (sys) => ({ servers: [server(`${sys}-server`)], expected: [], missing: [] }));
    renderApp(
      <>
        <App />
        <Nav />
      </>,
      "/servers?system=example-prod",
    );
    expect(await screen.findByText("example-prod-server")).toBeInTheDocument();
    await switchTo(/Global/);
    expect(await screen.findByText("example-global-server")).toBeInTheDocument();
    act(() => go(-1));
    expect(await screen.findByText("example-prod-server")).toBeInTheDocument();
    expect(screen.queryByText("example-global-server")).toBeNull();
    expect(screen.getByRole("button", { name: /Switch system/ })).toHaveAccessibleName(/Production/);
  });
});

describe("sidebar", () => {
  it("shows the built pages in their groups and hides the planned ones", async () => {
    renderApp(<App />, "/streams?system=example-global");
    const links = within(nav()).getAllByRole("link").map((a) => a.textContent?.replace(/\d+$/, ""));
    expect(links).toEqual(["Fleet", "Overview", "Findings", "Connections", "Streams", "Consumers", "Servers", "Events"]);
    for (const planned of ["Subjects", "Micro services", "Applications", "KV Stores", "Object Stores", "Placement", "Capacity", "Topology", "Accounts", "Users & auth"]) {
      expect(within(nav()).queryByText(planned)).toBeNull();
    }
    expect(within(nav()).getByText("Messaging").parentElement).toHaveTextContent("Connections");
  });

  it("keeps the system, the range and the account on every page link", async () => {
    renderApp(<App />, "/streams?system=example-global&range=6h&account=APP");
    await screen.findByText("Global");
    for (const a of within(nav()).getAllByRole("link")) {
      const p = params(a.getAttribute("href"));
      expect([a.textContent, p.get("system"), p.get("range"), p.get("account")]).toEqual([a.textContent, "example-global", "6h", "APP"]);
    }
  });

  it("names the default system in links when several systems exist and the URL names none", async () => {
    renderApp(<App />, "/servers");
    await waitFor(() => expect(params(within(nav()).getByRole("link", { name: "Servers" }).getAttribute("href")).get("system")).toBe("example-prod"));
  });

  it("badges Findings with the unmuted firing findings, shaped by the worst level", async () => {
    renderApp(<App />, "/servers?system=example-prod");
    const link = await within(nav()).findByRole("link", { name: /Findings/ });
    await waitFor(() => expect(link).toHaveAccessibleName("Findings, 3 firing"));
    expect(link.querySelector(".nb")).toHaveClass("warning");
  });

  it("collapses to a rail and remembers it", async () => {
    renderApp(<App />, "/servers?system=example-prod");
    await userEvent.click(screen.getByRole("button", { name: "Collapse sidebar" }));
    expect(screen.getByRole("complementary", { name: "Sidebar" })).toHaveClass("collapsed");
    expect(localStorage.getItem("nats-lens-sidebar")).toBe("collapsed");
    expect(within(nav()).getByRole("link", { name: "Servers" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Expand sidebar" }));
    expect(localStorage.getItem("nats-lens-sidebar")).toBeNull();
  });

  it("opens as a drawer from the menu button and closes when a page opens", async () => {
    renderApp(<App />, "/servers?system=example-prod");
    const menu = screen.getByRole("button", { name: "Menu" });
    await userEvent.click(menu);
    expect(menu).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("complementary", { name: "Sidebar" })).toHaveClass("open");
    expect(screen.getByRole("button", { name: "Close menu" })).toHaveFocus();
    await userEvent.click(within(nav()).getByRole("link", { name: "Streams" }));
    expect(menu).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("complementary", { name: "Sidebar" })).not.toHaveClass("open");
  });

  it("keeps Tab inside the open drawer by making the page inert", async () => {
    renderApp(<App />, "/servers?system=example-prod");
    const menu = screen.getByRole("button", { name: "Menu" });
    const column = screen.getByRole("main").parentElement!;
    expect(column).not.toHaveAttribute("inert");
    await userEvent.click(menu);
    expect(column).toHaveAttribute("inert");
    expect(screen.getByRole("complementary", { name: "Sidebar" }).closest("[inert]")).toBeNull();
  });

  it("gives focus back to the menu button however the drawer closes", async () => {
    renderApp(<App />, "/servers?system=example-prod");
    const menu = screen.getByRole("button", { name: "Menu" });
    const side = () => screen.getByRole("complementary", { name: "Sidebar" });
    await userEvent.click(menu);
    await userEvent.click(screen.getByRole("button", { name: "Close menu" }));
    expect(side()).not.toHaveClass("open");
    expect(menu).toHaveFocus();
    await userEvent.click(menu);
    await userEvent.click(document.querySelector(".side-backdrop")!);
    expect(side()).not.toHaveClass("open");
    expect(menu).toHaveFocus();
    await userEvent.click(menu);
    await userEvent.click(within(nav()).getByRole("link", { name: "Events" }));
    expect(side()).not.toHaveClass("open");
    expect(menu).toHaveFocus();
  });

  it("closes only the system menu on Escape inside it, not the drawer", async () => {
    renderApp(<App />, "/servers?system=example-prod");
    await userEvent.click(screen.getByRole("button", { name: "Menu" }));
    await userEvent.click(await screen.findByRole("button", { name: /Switch system/ }));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.getByRole("complementary", { name: "Sidebar" })).toHaveClass("open");
  });

  it("closes the drawer on Escape and gives focus back to the menu button", async () => {
    renderApp(<App />, "/servers?system=example-prod");
    const menu = screen.getByRole("button", { name: "Menu" });
    await userEvent.click(menu);
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("complementary", { name: "Sidebar" })).not.toHaveClass("open");
    expect(menu).toHaveFocus();
  });
});

describe("system switcher", () => {
  it("opens the same page of the other system, keeping the range and leaving the account", async () => {
    renderApp(<App />, "/streams/APP/order-jobs?system=example-prod&range=6h&account=APP");
    await userEvent.click(await screen.findByRole("button", { name: "System: Production. Switch system" }));
    await userEvent.click(screen.getByRole("menuitemradio", { name: /Global/ }));
    expect(location()).toBe("/streams?range=6h&system=example-global");
  });

  it("is no menu with one system", async () => {
    setSystems(ONE);
    renderApp(<App />, "/servers");
    await screen.findByLabelText("System: default");
    expect(screen.queryByRole("button", { name: /Switch system/ })).toBeNull();
  });
});

describe("landing and unknown systems", () => {
  it("lands on Fleet with several systems", async () => {
    renderApp(<App />, "/?range=6h");
    await waitFor(() => expect(location()).toBe("/fleet?range=6h"));
    const fleetLinks = await screen.findAllByRole("link", { name: /Production|Global|Staging/ });
    const main = screen.getByRole("main");
    const inMain = fleetLinks.filter((a) => main.contains(a));
    expect(inMain.map((a) => params(a.getAttribute("href")).get("system"))).toEqual(["example-prod", "example-global", "example-staging"]);
  });

  it("shows the Overview with one system", async () => {
    setSystems(ONE);
    renderApp(<App />, "/");
    await waitFor(() => expect(m.getOverview).toHaveBeenCalled());
    expect(location()).toBe("/");
  });

  it("shows the Overview of a named system", async () => {
    renderApp(<App />, "/?system=example-global");
    await waitFor(() => expect(m.getOverview).toHaveBeenCalledWith("example-global", expect.anything(), expect.anything()));
  });

  it("says an unknown system is not found, with a link to Fleet, and asks it nothing", async () => {
    renderApp(<App />, "/streams?system=nope&range=6h");
    expect(await screen.findByRole("heading", { name: "System not found" })).toBeInTheDocument();
    const fleet = within(screen.getByRole("main")).getByRole("link", { name: "Fleet" });
    expect(fleet.getAttribute("href")).toMatch(/^\/fleet\?/);
    expect(params(fleet.getAttribute("href")).get("system")).not.toBe("nope");
    expect(params(fleet.getAttribute("href")).get("range")).toBe("6h");
    expect(m.listStreams).not.toHaveBeenCalledWith("nope", expect.anything(), expect.anything(), expect.anything());
    expect(m.getHeaderStatus).not.toHaveBeenCalledWith("nope", expect.anything(), expect.anything());
    expect(l.listAccounts).not.toHaveBeenCalledWith(expect.objectContaining({ system: "nope" }), expect.anything());
  });
});

describe("header", () => {
  it("names the page, also on a detail page", async () => {
    renderApp(<App />, "/streams/APP/order-jobs?system=example-prod");
    expect(screen.getByRole("banner")).toHaveTextContent("Streams");
  });

  it("writes the account scope to the URL", async () => {
    renderApp(<App />, "/streams?system=example-prod&range=6h");
    await userEvent.click(screen.getByRole("button", { name: /Account scope: all accounts/ }));
    await userEvent.click(await screen.findByRole("menuitemradio", { name: /APP/ }));
    expect(location()).toBe("/streams?system=example-prod&range=6h&account=APP");
    expect(screen.getByRole("button", { name: "Account scope: APP" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Account scope: APP" }));
    await userEvent.click(screen.getByRole("menuitemradio", { name: /All accounts/ }));
    expect(location()).toBe("/streams?system=example-prod&range=6h");
  });

  it("limits the Consumers list to the scoped account", async () => {
    renderApp(<App />, "/consumers?system=example-prod&account=APP");
    await waitFor(() => expect(m.listConsumers).toHaveBeenCalledWith("example-prod", expect.anything(), expect.objectContaining({ account: "APP", stream: "" }), expect.anything()));
  });

  it("jumps to a consumer found by part of its name", async () => {
    renderApp(<App />, "/servers?system=example-global&range=6h");
    const box = screen.getByRole("combobox", { name: "Search this system" });
    await userEvent.type(box, "resize");
    expect(await screen.findByRole("option", { name: /resize-worker/ })).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(location()).toBe("/consumers/APP/order-jobs/resize-worker?range=6h&system=example-global");
    expect(m.listConsumers).toHaveBeenCalledWith("example-global", expect.anything(), {}, expect.anything());
  });
});

describe("RLink", () => {
  it("carries the range, system and account to a link built by a page", async () => {
    renderApp(<RLink to="/connections?server=nats-1">x</RLink>, "/servers/nats-1?system=example-global&from=2026-10-17T09:00:00Z&to=2026-10-17T10:00:00Z&account=APP");
    await waitFor(() => {
      const p = params(screen.getByRole("link").getAttribute("href"));
      expect(Object.fromEntries(p)).toEqual({ server: "nats-1", from: "2026-10-17T09:00:00Z", to: "2026-10-17T10:00:00Z", system: "example-global", account: "APP" });
    });
  });
});
