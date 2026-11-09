import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useNavigate } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { app, dispatcher, hours, noCollection, prod, resizer } from "../test/fixtures/applications";
import { renderApp } from "../test/render";
import { Applications } from "./Applications";

vi.mock("../api/requests", () => ({
  getApplications: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true })),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
vi.mock("../api/systems", async (orig) => ({ ...(await orig<typeof import("../api/systems")>()), useAccounts: vi.fn(() => ({ data: undefined })) }));
const getApps = vi.mocked(requests.getApplications);

afterEach(() => {
  getApps.mockReset();
});

function table() {
  return within(screen.getByRole("table"));
}

function row(name: string) {
  return table().getByRole("rowheader", { name: new RegExp(`^${name.replace(/[()]/g, "\\$&")}`) }).closest("tr") as HTMLElement;
}

function names() {
  return table()
    .getAllByRole("rowheader")
    .map((h) => h.querySelector(".ap-name")?.textContent);
}

function location() {
  return screen.getByTestId("location").textContent ?? "";
}

function Go({ to }: { to: string }) {
  const nav = useNavigate();
  return (
    <button type="button" onClick={() => nav(to)}>
      go
    </button>
  );
}

describe("Applications", () => {
  it("says what the page shows, the counts and the collection, for the system and scope of the page", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications?system=example-prod&account=APP");
    await screen.findByRole("table");
    expect(screen.getByText(/^Client connections grouped/)).toHaveTextContent(
      "Client connections grouped by the application in their name. 51 connections, 3 applications, in the collection of 14:29:56 UTC.",
    );
    expect(getApps.mock.calls[0].slice(0, 4)).toEqual(["example-prod", expect.anything(), "APP", false]);
  });

  it("shows the three summary cards", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getByRole("region", { name: "Spread" })).toHaveTextContent("nats-0 serves 28 of 51 connections on 3 servers. A restart of nats-0 moves 28 of 51 connections at once.");
    expect(screen.getByRole("region", { name: "Client libraries" })).toHaveTextContent("nats.js 3.0.2 in 2 applications, go 1.54.0 in 1.");
    expect(screen.getByRole("region", { name: "Last 24 h" })).toHaveTextContent("0 slow consumers ·, 6 stale connections ·, 1 application reconnecting");
  });

  it("lists a row per application with the board's columns, Pods in place of Runs on", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(table().getAllByRole("columnheader").map((h) => h.querySelector(".sort-btn")?.textContent)).toEqual([
      "Application",
      "Pods",
      "Instances",
      "Servers",
      "Library",
      "RTT p50",
      "Subs",
      "Pending",
      "Reconnects 24 h",
      "Msgs in / out 24 h",
    ]);
    expect(names()).toEqual(["callback-dispatcher", "order-service", "resize-worker"]);
    const cells = within(row("resize-worker")).getAllByRole("cell");
    expect(cells[0]).toHaveTextContent(/^resize-worker-6c9d4-7d9fqresize-worker-6c9d4-k2p4m\+2 more pods/);
    expect(cells.slice(1, 8).map((c) => c.textContent)).toEqual(["4", "nats-0 2 connectionsnats-1 1 connectionnats-2 1 connection", "go 1.54.0", "0.41 ms", "0", "0 B", "0"]);
    expect(within(row("resize-worker")).getByRole("rowheader")).toHaveTextContent("resize-worker4 connections, 4 pods");
    // Without a pod in the names, the column lists the client addresses.
    expect(within(row("order-service")).getAllByRole("cell")[0]).toHaveTextContent(/^no pod in the names; client addresses: 10\.20\.2\.107\+12 more addresses/);
  });

  it("links the application to its connections, each server to its page, and a finding to its page", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications?system=example-prod&account=APP");
    await screen.findByRole("table");
    const r = within(row("callback-dispatcher"));
    expect(r.getByRole("link", { name: "callback-dispatcher" })).toHaveAttribute("href", "/connections?name=callback-dispatcher&system=example-prod&account=APP");
    expect(r.getByRole("link", { name: "nats-1 6 connections" })).toHaveAttribute("href", "/servers/nats-1?system=example-prod&account=APP");
    expect(r.getByRole("link", { name: /^client-004 Client reconnecting repeatedly/ })).toHaveAttribute("href", "/findings/client-004%3Acallback-dispatcher?system=example-prod");
    expect(row("callback-dispatcher")).toHaveClass("ap-warn");
    expect(row("order-service")).not.toHaveClass("ap-warn");
  });

  it("links a reconnect count to the client-004 finding, else to the Events page filtered by the name, and leaves it plain where no name matches", async () => {
    getApps.mockResolvedValue(
      prod({ applications: [dispatcher(), app({ name: "label-printer", reconnects: 14 }), resizer({ reconnects: 2 }), app({ name: "ledger-service", reconnects: 0 })] }),
    );
    renderApp(<Applications />, "/applications?system=example-global");
    await screen.findByRole("table");
    expect(within(row("callback-dispatcher")).getByRole("link", { name: "6, see client-004" })).toHaveAttribute("href", "/findings/client-004%3Acallback-dispatcher?system=example-global");
    expect(within(row("label-printer")).getByRole("link", { name: "14, see the events of label-printer" })).toHaveAttribute("href", "/events?client=label-printer&system=example-global");
    const pods = within(row("resize-worker")).getAllByRole("cell")[7];
    expect(pods).toHaveTextContent("2");
    expect(within(pods).queryByRole("link")).toBeNull();
    expect(within(within(row("ledger-service")).getAllByRole("cell")[7]).queryByRole("link")).toBeNull();
  });

  it("sorts by any column, keeps it in the address, and flips on a second click", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications?system=example-prod");
    await screen.findByRole("table");
    await userEvent.click(table().getByRole("button", { name: "Reconnects 24 h" }));
    expect(location()).toBe("/applications?system=example-prod&sort=-reconnects");
    expect(names()[0]).toBe("callback-dispatcher");
    await userEvent.click(table().getByRole("button", { name: "Application" }));
    expect(location()).toContain("sort=name");
    expect(names()).toEqual(["callback-dispatcher", "order-service", "resize-worker"]);
    await userEvent.click(table().getByRole("button", { name: "Application" }));
    expect(names()).toEqual(["resize-worker", "order-service", "callback-dispatcher"]);
    expect(table().getByRole("columnheader", { name: "Application" })).toHaveAttribute("aria-sort", "descending");
  });

  it("opens sorted as the address says", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications?sort=-pods");
    await screen.findByRole("table");
    // order-service lists 13 addresses, callback-dispatcher's fixture as many; resize-worker 4 pods.
    expect(names()[2]).toBe("resize-worker");
    expect(table().getByRole("columnheader", { name: "Pods" })).toHaveAttribute("aria-sort", "descending");
  });

  it("asks for the tooling connections when the toggle is on, keeps it in the address, and marks tooling rows", async () => {
    getApps.mockImplementation(async (_s, _r, _a, tooling) =>
      tooling ? prod({ applications: [app(), app({ name: "natslens", tooling: true, connections: 2 })] }) : prod({ applications: [app()], toolingHidden: 2 }),
    );
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getByText("2 tooling connections hidden: nats-lens's own, or clients from a loopback address.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: "Include tooling" }));
    expect(location()).toBe("/applications?tooling=1");
    await waitFor(() => expect(names()).toEqual(["order-service", "natslens"]));
    expect(getApps.mock.calls.at(-1)?.[3]).toBe(true);
    expect(within(row("natslens")).getByRole("rowheader")).toHaveTextContent(/natslenstooling: nats-lens's own connections/);
    expect(within(row("natslens")).getByRole("link", { name: "natslens" })).toHaveAttribute("href", "/connections?name=natslens&tooling=1");
  });

  it("says only that the range holds no collection", async () => {
    getApps.mockResolvedValue(noCollection());
    renderApp(<Applications />, "/applications");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("says when the collection holds no application, and when it holds only tooling", async () => {
    getApps.mockResolvedValue(prod({ applications: [], applicationsTotal: 0, connectionsTotal: 0 }));
    const { unmount } = renderApp(<Applications />, "/applications?account=SHOP");
    expect(await screen.findByText("No client connection in account SHOP in this collection.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.queryByRole("region", { name: "Spread" })).toBeNull();
    unmount();
    getApps.mockResolvedValue(prod({ applications: [], applicationsTotal: 0, connectionsTotal: 0, toolingHidden: 3 }));
    renderApp(<Applications />, "/applications");
    expect(await screen.findByText("Only tooling connections in this collection: 3 connections. Include tooling to list them.")).toBeInTheDocument();
  });

  it("warns of a cut connection list and of events not captured, and shows the unknown counts as dashes", async () => {
    const unknown = { reconnects: undefined, slowConsumers: undefined, stale: undefined, shortLived: undefined };
    getApps.mockResolvedValue(prod({ connzCut: true, eventsCaptured: false, lastDay: {}, applications: [app(unknown)] }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    const notes = screen.getAllByRole("note").map((n) => n.textContent);
    expect(notes[0]).toMatch(/^Connection list cut\. A server's connection list in this collection was cut at the collector's connz limit/);
    expect(notes[1]).toMatch(/^Events not captured\. No event captured up to this collection/);
    expect(screen.getByRole("region", { name: "Last 24 h" })).toHaveTextContent("Not known: no event captured up to this collection.");
    expect(within(row("order-service")).getAllByRole("cell")[7]).toHaveTextContent("– no event captured");
  });

  it("draws hours without a measurement as gaps and says how many hours were measured", async () => {
    const six = hours((i) => (i >= 18 ? [10, 5] : undefined));
    getApps.mockResolvedValue(prod({ applications: [app({ traffic: six }), resizer({ traffic: hours((i) => (i === 20 || i < 18 ? undefined : [3, 1])) })] }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getByRole("note")).toHaveTextContent("No application has all 24 hours before this collection's hour measured; the most any has is 6. The hours without a measurement are gaps in the lines, not zeros.");
    const spark = within(row("order-service")).getByRole("img");
    expect(spark).toHaveAccessibleName("order-service messages in and out, the 24 hours to 14:00 UTC: 60 in (5.86 KiB), 30 out (2.93 KiB); 6 of 24 hours measured");
    expect(spark.querySelectorAll("polyline.ap-spark-in")).toHaveLength(1);
    // Two runs around the gap at hour 20: hours 18 and 19 make a line, 21 to 23 another.
    expect(within(row("resize-worker")).getByRole("img").querySelectorAll("polyline.ap-spark-in")).toHaveLength(2);
  });

  it("names the servers that did not answer and drops the restart sentence on a partial collection", async () => {
    getApps.mockResolvedValue(prod({ partial: true, missing: ["nats-1"], spread: { server: "nats-0", connections: 28, total: 40, servers: 2, concentrated: false } }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getAllByRole("note")[0]).toHaveTextContent(
      "Partial collection. nats-1 did not answer in this collection: its connections are not counted, and the counts, the spread and the traffic cover the servers that answered.",
    );
    const spread = screen.getByRole("region", { name: "Spread" });
    expect(spread).toHaveTextContent("nats-0 serves 28 of 40 connections on 2 servers, counting only the servers that answered.");
    expect(spread).not.toHaveTextContent(/restart/);
    expect(spread.classList.contains("ap-card-info")).toBe(false);
    expect(screen.getByText(/^Client connections grouped/)).toHaveTextContent(/over the servers that answered\.$/);
  });

  it("shows no partial notice without a server named, since only a missing server makes the page partial", async () => {
    getApps.mockResolvedValue(prod({ partial: true, missing: [] }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.queryByText(/Partial collection/)).toBeNull();
  });

  it("reads the 24-hour counts as lower bounds when event capture stopped within the 24 hours", async () => {
    getApps.mockResolvedValue(prod({ captureGaps: true }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getAllByRole("note")[0]).toHaveTextContent(/^Event capture stopped for part of these 24 hours\. Reconnects, slow consumers and stale connections can be low/);
    expect(screen.getByRole("region", { name: "Last 24 h" })).toHaveTextContent("at least 0 slow consumers ·, at least 6 stale connections ·, at least 1 application reconnecting");
    const cell = within(row("callback-dispatcher")).getAllByRole("cell")[7];
    expect(cell).toHaveTextContent("≥6at least 6");
    expect(within(cell).getByRole("link")).toHaveAccessibleName("at least 6, see client-004");
  });

  it("names a plain reconnect cell under capture gaps with its number, not only \"at least\"", async () => {
    getApps.mockResolvedValue(
      prod({ captureGaps: true, applications: [app({ reconnects: 0 }), app({ name: "five", reconnects: 5, connectionNames: ["five@a", "five@b"] })] }),
    );
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    const zero = within(row("order-service")).getAllByRole("cell")[7];
    const five = within(row("five")).getAllByRole("cell")[7];
    expect(within(five).queryByRole("link")).toBeNull();
    expect(zero).toHaveAccessibleName("at least 0");
    expect(five).toHaveAccessibleName("at least 5");
  });

  it("says nothing of capture gaps when capture ran throughout", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.queryByText(/Event capture stopped/)).toBeNull();
    expect(within(row("callback-dispatcher")).getAllByRole("cell")[7]).toHaveTextContent(/^6$/);
  });

  it("says in the footnote that reconnects pair per connection name", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getByText(/^Reconnects follow/)).toHaveTextContent(
      "Reconnects follow client-004's rule per connection name over the 24 hours before the collection, summed over the application's names: a pod that drops and a new pod that connects, as in a rolling deploy, are not a reconnect. A name pairs its events across accounts, so a page scoped to an account counts each name with an event in that account, whatever account its other events are in.",
    );
  });

  it("says in the footnote that each sparkline has its own scale", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getByText(/^Each sparkline is scaled to its own row's busiest hour/)).toHaveTextContent(
      "Each sparkline is scaled to its own row's busiest hour, in or out, so two rows' lines do not compare; the numbers beneath them do.",
    );
  });

  it("shows a dashed baseline and says not measured for an application without traffic", async () => {
    getApps.mockResolvedValue(prod({ applications: [app({ traffic: hours(() => undefined) })] }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    const spark = within(row("order-service")).getByRole("img");
    expect(spark).toHaveAccessibleName("order-service messages in and out, the 24 hours to 14:00 UTC: not measured");
    expect(spark.querySelector(".ap-spark-none")).not.toBeNull();
    expect(within(row("order-service")).getAllByRole("cell")[8]).toHaveTextContent("not measured");
  });

  it("shows a dash for a direction of traffic that was not measured, never 0", async () => {
    getApps.mockResolvedValue(prod({ applications: [app({ inMsgs: 1200, outMsgs: undefined, outBytes: undefined })] }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    const cell = within(row("order-service")).getAllByRole("cell")[8];
    expect(cell.querySelector(".ap-small")).toHaveTextContent("1,200 / – not measured");
    expect(within(cell).getByRole("img")).toHaveAccessibleName(/: 1,200 in \([^)]*\), out not measured$/);
  });

  it("names the accounts on each row when the rows span accounts and the page is not scoped", async () => {
    getApps.mockResolvedValue(prod({ applications: [app(), app({ name: "ops-monitor", accounts: [{ account: "SYS", accountName: "" }] })] }));
    const { unmount } = renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(within(row("ops-monitor")).getByRole("rowheader")).toHaveTextContent("ops-monitor25 connections · , account SYS");
    unmount();
    renderApp(<Applications />, "/applications?account=APP");
    await screen.findByRole("table");
    expect(within(row("ops-monitor")).getByRole("rowheader")).toHaveTextContent(/^ops-monitor25 connections$/);
  });

  it("lists a mixed library's versions", async () => {
    const libraries = [
      { lang: "nats.js", version: "3.0.2", connections: 120 },
      { lang: "nats.js", version: "2.29.3", connections: 24 },
    ];
    getApps.mockResolvedValue(prod({ applications: [app({ name: "shipping-service", libraries })] }));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(within(row("shipping-service")).getAllByRole("cell")[3]).toHaveTextContent(/^mixed: nats\.js 3\.0\.2 on 120, nats\.js 2\.29\.3 on 24/);
  });

  it("never shows another account's applications while the scoped ones load", async () => {
    getApps.mockImplementation(async (_s, _r, acc) => (acc === "APP" ? prod() : new Promise(() => {})));
    renderApp(
      <>
        <Go to="/applications?account=SHOP" />
        <Applications />
      </>,
      "/applications?account=APP",
    );
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: "go" }));
    await waitFor(() => expect(screen.queryByRole("table")).toBeNull());
    expect(screen.getByText("Loading applications…")).toBeInTheDocument();
  });

  it("keeps the previous answer while the tooling toggle's answer loads", async () => {
    getApps.mockImplementation(async (_s, _r, _a, tooling) => (tooling ? new Promise(() => {}) : prod()));
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("checkbox", { name: "Include tooling" }));
    expect(screen.getByRole("table")).toBeInTheDocument();
  });
});

import { HistoryContext } from "../components/HistoryNote";
it("explains the missing hours of its 24-hour traffic window with a history link", async () => {
  getApps.mockResolvedValue(prod());
  const end = Date.parse("2026-10-17T14:30:00Z");
  renderApp(<HistoryContext.Provider value={{ start: end - 3_600_000, end, live: true, historyStart: end - 31 * 60_000 }}><Applications /></HistoryContext.Provider>, "/applications?system=example-staging&range=1h");
  expect(await screen.findByRole("link", { name: /31 min of the last 24 h/ })).toHaveAttribute("href", expect.stringContaining("range=1h"));
});

it("opens bounded Replay incident context from Applications", async () => {
  getApps.mockResolvedValue(prod());
  renderApp(<Applications />, "/applications?system=example-prod&account=APP&from=2026-10-17T09%3A00%3A00Z&to=2026-10-17T10%3A00%3A00Z");
  const link = await screen.findByRole("link", { name: "Replay application context" });
  expect(link.getAttribute("href")).toContain("/replay?");
  expect(link.getAttribute("href")).toContain("a=2026-10-17T09");
  expect(link.getAttribute("href")).toContain("system=example-prod");
  expect(screen.getByText(/Library changes do not prove an application deployment/)).toBeInTheDocument();
});

it("filters library evidence to its exact application and explains summary scope", async () => {
  getApps.mockResolvedValue(prod());
  renderApp(<Applications />, "/applications?account=APP&app=resize-worker");
  await screen.findByRole("table");
  expect(names()).toEqual(["resize-worker"]);
  expect(screen.getByText(/Summary cards cover the selected account scope/)).toBeInTheDocument();
});
