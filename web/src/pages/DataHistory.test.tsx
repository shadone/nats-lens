import { screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { ConnectError, Code } from "@connectrpc/connect";
import { setServerNow } from "../lib/clock";
import { gap, NOW, prod, staging, use } from "../test/fixtures/history";
import { renderApp } from "../test/render";
import { DataHistory } from "./DataHistory";

vi.mock("../api/requests", () => ({
  getDataHistory: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
  getShellStatus: vi.fn(() => new Promise(() => {})),
}));

const m = vi.mocked(requests);
const HOUR = 3_600_000;

beforeEach(() => setServerNow(NOW, Date.now(), true));
afterEach(() => {
  setServerNow(Date.now());
  m.getDataHistory.mockReset();
  m.getShellStatus.mockReset();
  m.getShellStatus.mockImplementation(() => new Promise(() => {}));
});

function card(name: string) {
  return within(screen.getByRole("region", { name }));
}

function bodyRows(name: string) {
  return card(name).getAllByRole("row").slice(1);
}

describe("DataHistory", () => {
  it("says it is loading", () => {
    m.getDataHistory.mockImplementation(() => new Promise(() => {}));
    renderApp(<DataHistory />, "/data-history");
    expect(screen.getByRole("status")).toHaveTextContent("Loading the data history…");
  });

  it("says what failed", async () => {
    m.getDataHistory.mockRejectedValue(new ConnectError("no database", Code.Unavailable));
    renderApp(<DataHistory />, "/data-history");
    expect(await screen.findByRole("alert")).toHaveTextContent(/Could not load the data history\..*no database/);
  });

  it("reads the history of the page's system and range, and says since when it is kept", async () => {
    m.getDataHistory.mockResolvedValue(staging());
    renderApp(<DataHistory />, "/data-history?system=example-staging&range=6h");
    expect(await screen.findByText(/^History since|^Collecting since/)).toBeInTheDocument();
    expect(m.getDataHistory.mock.calls[0][0]).toBe("example-staging");
    expect(m.getDataHistory.mock.calls[0][1]).toEqual({ kind: "live", preset: "6h" });
    expect(document.querySelector(".dh-summary")).toHaveTextContent("Collecting since 13:47 UTC (9 hours) · kept for 14 days · no archive · events from 13:50 UTC");
  });

  it("adds the shell's stop of collection to the summary", async () => {
    m.getDataHistory.mockResolvedValue(staging());
    m.getShellStatus.mockResolvedValue({
      freshness: { epoch: "720", epochAt: NOW - 3 * HOUR, partial: false, live: true, intervalMs: 30_000, collecting: false, notCollectingSince: NOW - 3 * HOUR },
      servers: [],
      tally: { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 },
      systems: [],
      defaultSystem: "example-staging",
      systemKnown: true,
    } as never);
    renderApp(<DataHistory />, "/data-history?system=example-staging");
    expect(await screen.findByText("not collecting since 19:47 UTC")).toHaveClass("dh-warn");
    expect(document.querySelector(".dh-summary")).toHaveTextContent(/^History since 13:47 UTC \(9 hours\) · not collecting since 19:47 UTC · kept for 14 days/);
  });

  it("shows the coverage of the range in boxes, and the findings already true at the start", async () => {
    m.getDataHistory.mockResolvedValue(staging());
    renderApp(<DataHistory />, "/data-history?system=example-staging");
    const cov = within(await screen.findByRole("region", { name: "Coverage of the range" }));
    const boxes = [...document.querySelectorAll(".dh-box")].map((b) => b.textContent);
    expect(boxes).toEqual([
      "Collectionsi715of 1,080 expected, 13:47–22:47 UTC",
      "Partial collectionsi0missed a server, 13:47–22:47 UTC",
      "Without datai3 h 3 min13:47–22:47 UTC",
      "Coveredi66%13:47–22:47 UTC, from the history start",
    ]);
    expect(cov.getByText(/1 finding was already true when the history begins, at 13:47 UTC\./)).toBeInTheDocument();
    expect(cov.getByRole("link", { name: "Findings" })).toHaveAttribute("href", "/findings?system=example-staging");
  });

  it("lists the gaps newest first, an open one still now, with the collections either side and the event", async () => {
    m.getDataHistory.mockResolvedValue(staging());
    renderApp(<DataHistory />, "/data-history?system=example-staging");
    await screen.findByRole("region", { name: "Gaps" });
    const rows = bodyRows("Gaps");
    expect(rows.map((r) => within(r).getByRole("rowheader").textContent)).toEqual([
      "Events not capturedconnection sys; collections went on",
      "nats-lens could not reach NATS",
      "nats-lens was not running",
      "Events not capturedconnection sys; collections went on",
      "nats-lens was not running yet",
    ]);
    expect(rows[1]).toHaveTextContent("nats-lens could not reach NATS19:47:14still now3 h so farbefore: collection 720, 19:47:14event 29");
    expect(within(rows[2]).getByRole("link", { name: "collection 487, 17:50:44" })).toHaveAttribute("href", "/placement?from=2026-11-05T16%3A50%3A44Z&to=2026-11-05T17%3A50%3A45Z&system=example-staging");
    expect(within(rows[2]).getByRole("link", { name: "collection 493, 17:53:44" })).toBeInTheDocument();
    expect(within(rows[2]).getByRole("link", { name: "event 18" })).toHaveAttribute("href", "/events?id=18&system=example-staging");
    expect(rows[2]).toHaveTextContent("17:50:4417:53:443 min");
    expect(rows[4]).toHaveTextContent("4 Nov 22:4713:47:1815 h");
    expect(card("Gaps").getByRole("img")).toHaveAccessibleName("The range, with 3 times without collections hatched");
    expect(card("Gaps").getByText("No data 17:50:44–17:53:44 UTC: nats-lens was not running")).toBeInTheDocument();
    expect(card("Gaps").queryByText(/^Showing/)).toBeNull();
  });

  it("ends an open gap after the range on a fixed range, and names Kubernetes and no-collections gaps", async () => {
    const d = staging();
    d.gaps = [
      gap({ kind: "kubernetesNotWatched", start: NOW - 2 * HOUR, end: NOW - 2 * HOUR + 4 * 60_000, eventId: "1" }),
      gap({ kind: "noCollections", start: NOW - 5 * HOUR, end: NOW - 5 * HOUR + 5 * 60_000, beforeEpoch: "73401", afterEpoch: "73411" }),
      gap({ kind: "notConnected", start: NOW - 6 * HOUR }),
    ];
    m.getDataHistory.mockResolvedValue(d);
    renderApp(<DataHistory />, "/data-history?from=2026-11-05T00:00:00Z&to=2026-11-05T22:47:18Z");
    await screen.findByRole("region", { name: "Gaps" });
    const rows = bodyRows("Gaps");
    expect(rows[0]).toHaveTextContent(/^Kubernetes not watchedcollections went on20:47:1820:51:184 min/);
    // Collections without their times are named, not linked.
    expect(rows[1]).toHaveTextContent("No collections17:47:1817:52:185 minbefore: collection 73401after: collection 73411");
    expect(within(rows[1]).queryByRole("link")).toBeNull();
    expect(rows[2]).toHaveTextContent("after the rangeat least 6 h");
  });

  it("says how many gaps of each class it shows when the server cut them", async () => {
    m.getDataHistory.mockResolvedValue({ ...staging(), gapsTotal: 243, listedGapsTotal: 2 });
    renderApp(<DataHistory />, "/data-history");
    await screen.findByRole("region", { name: "Gaps" });
    expect(card("Gaps").getByText(/^Showing 3 of 243 times without collections, the newest\./)).toBeInTheDocument();
  });

  it("says a range has no gaps", async () => {
    m.getDataHistory.mockResolvedValue(prod());
    renderApp(<DataHistory />, "/data-history?system=example-prod");
    await screen.findByRole("region", { name: "Gaps" });
    expect(card("Gaps").getByText("No gaps in this range.")).toBeInTheDocument();
    expect(card("Gaps").getByRole("img")).toHaveAccessibleName("The range, with collections all through");
    expect(card("Gaps").queryByRole("table")).toBeNull();
  });

  it("lists what uses history, with what it needs, has and when it has enough", async () => {
    const d = staging();
    d.uses.push(use({ id: "day", needs: 24 * HOUR, has: undefined, enough: false }));
    m.getDataHistory.mockResolvedValue(d);
    renderApp(<DataHistory />, "/data-history");
    await screen.findByRole("region", { name: "What uses history" });
    const rows = bodyRows("What uses history");
    expect(rows.map((r) => r.textContent)).toEqual([
      "Growth per dayStreams, Capacity, the storage tile; over the range6 h9 henough",
      "Store forecastserver-014: when a server's store fills6 h9 henough",
      "Check server-019its lookback1 d9 henough at 2026-11-06 13:47 UTC",
      "Growth review of accepted findings8 days and an hour, kept as long8 d 1 h9 henough at 2026-11-13 14:47 UTC",
      "The last 24 hoursApplications traffic, failed logins (Accounts), drops (Topology)1 d– not observednot within this range",
    ]);
    expect(rows[2]).toHaveAttribute("title", "needs 1 d; has 9 h; enough at 2026-11-06 13:47 UTC");
  });

  it("marks a use the retention can never serve", async () => {
    const d = staging();
    d.retention = 7 * 24 * HOUR;
    d.uses = [use({ id: "accept_review", needs: 8 * 24 * HOUR + HOUR, enough: false, never: true })];
    m.getDataHistory.mockResolvedValue(d);
    renderApp(<DataHistory />, "/data-history");
    await screen.findByRole("region", { name: "What uses history" });
    const cell = within(bodyRows("What uses history")[0]).getAllByRole("cell")[2];
    expect(cell).toHaveTextContent("never with retention 7 d");
    expect(cell).toHaveClass("dh-warn");
  });

  it("says there is no archive when none is written", async () => {
    m.getDataHistory.mockResolvedValue(staging());
    renderApp(<DataHistory />, "/data-history?system=example-staging");
    const a = within(await screen.findByRole("region", { name: "Retention and archive" }));
    expect(a.getByText(/^Collections and events are kept for 14 days: the hourly sweep removes what is older than 2026-10-22 22:47 UTC\.$/)).toBeInTheDocument();
    expect(a.getByText(/^No archive:/)).toHaveTextContent("No archive: archive_dir is not set for this system, so nothing is archived.");
    expect(a.getByText(/^The archive holds hourly Parquet files/)).toBeInTheDocument();
    expect(a.queryByText("Location")).toBeNull();
  });

  it("shows the archive's location, hours, retention and last export, and its error in the warning colour", async () => {
    const d = prod();
    d.archive = { ...d.archive, lastError: "write /var/lib/natslens/archive: no space left on device", lastErrorAt: NOW - HOUR };
    m.getDataHistory.mockResolvedValue(d);
    renderApp(<DataHistory />, "/data-history?system=example-prod");
    const a = within(await screen.findByRole("region", { name: "Retention and archive" }));
    const kv = [...document.querySelectorAll(".dh-kv > div")].map((x) => x.textContent);
    expect(kv).toEqual([
      "Location/var/lib/natslens/archive",
      "Hours2,161, from 2026-08-07 22:00 to 2026-11-05 22:00 UTC",
      "Kept90 days",
      "Last export22:44 UTC, the open hour provisionally",
      "Listed22:05 UTC",
    ]);
    expect(a.getByRole("note")).toHaveTextContent("Last export failed at 21:47 UTC: write /var/lib/natslens/archive: no space left on device");
    expect(a.getByRole("note")).toHaveClass("dh-error");
  });

  it("says so without any history, and shows no coverage, never 0%", async () => {
    const d = staging();
    d.historyStart = undefined;
    d.eventsStart = undefined;
    d.coverage = { epochs: 0, expected: 0, partial: 0 };
    d.gaps = [gap({ kind: "beforeHistory", start: d.rangeStart })];
    d.gapsTotal = 1;
    d.listedGapsTotal = 0;
    d.uses = [use({ has: undefined, enough: false })];
    m.getDataHistory.mockResolvedValue(d);
    renderApp(<DataHistory />, "/data-history");
    expect(await screen.findByText("No collection in this range: nats-lens holds nothing of it.")).toBeInTheDocument();
    expect(document.querySelector(".dh-summary")).toHaveTextContent(/^No collection stored yet$/);
    expect(document.querySelector(".dh-box")).toBeNull();
    expect(screen.queryByText(/already true/)).toBeNull();
    expect(screen.queryByText(/0%/)).toBeNull();
    expect(bodyRows("Gaps")[0]).toHaveTextContent("nats-lens was not running yet4 Nov 22:47still now1 d so far");
  });
});
