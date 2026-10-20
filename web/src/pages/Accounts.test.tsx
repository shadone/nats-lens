import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { account, empty, EPOCH_AT, expiring, global, prod, SUPPLIERS } from "../test/fixtures/accounts";
import { renderApp } from "../test/render";
import { Accounts } from "./Accounts";

vi.mock("../api/requests", () => ({
  getAccountsAndAuth: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
  getShellStatus: vi.fn(() => new Promise(() => {})),
}));

afterEach(() => vi.mocked(requests.getAccountsAndAuth).mockReset());

const DAY = 86_400_000;

function card(name: string) {
  return within(screen.getByRole("region", { name }));
}

function bodyRows(name: string) {
  return card(name).getAllByRole("row").slice(1);
}

function cells(row: HTMLElement) {
  return [within(row).getByRole("rowheader"), ...within(row).getAllByRole("cell")];
}

describe("Accounts", () => {
  it("says the mode, the count and where names come from, for the system of the page", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts?system=example-global");
    expect(await screen.findByText(/^Operator mode · 3 accounts · names from the account JWTs, in the collection of/)).toBeInTheDocument();
    expect(vi.mocked(requests.getAccountsAndAuth).mock.calls[0][0]).toBe("example-global");
  });

  it("lists the accounts with their limits, the system account last", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts?system=example-global");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const rows = bodyRows("Accounts and limits");
    expect(rows.map((r) => within(r).getByRole("rowheader").querySelector("a")?.textContent)).toEqual(["APP", "SUPPLIERS", "SYS"]);

    const [name, conns, subs, disk, streams, ie, jwt] = cells(rows[0]);
    expect(within(name).getByRole("link", { name: "APP" })).toHaveAttribute("href", "/connections?account=APP&system=example-global");
    expect(conns).toHaveTextContent("412 of 2,000");
    expect(within(conns).getByRole("img")).toHaveAccessibleName("Connections: 21% of the limit");
    expect(subs).toHaveTextContent("3,120");
    expect(disk).toHaveTextContent("410 GiB of 500 GiB");
    // 82% of the disk limit: the warning colour.
    expect(disk.querySelector(".ac-bar-warn")).not.toBeNull();
    expect(conns.querySelector(".ac-bar-warn")).toBeNull();
    expect(streams).toHaveTextContent("23");
    expect(ie).toHaveTextContent("1 import");
    expect(jwt).toHaveTextContent("never");
  });

  it("shows an idle account by its name with its key shortened, and its JWT expiry in the warning colour", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [name, conns, , disk, , ie, jwt] = cells(bodyRows("Accounts and limits")[1]);
    expect(name).toHaveTextContent(`SUPPLIERSADSU…RT5NH account ID ${SUPPLIERS}`);
    expect(within(name).getByRole("link", { name: "SUPPLIERS" })).toHaveAttribute("href", `/connections?account=${SUPPLIERS}`);
    expect(conns).toHaveTextContent("0 of 50");
    expect(disk).toHaveTextContent(/^JetStream off$/);
    expect(ie).toHaveTextContent("1 export · idle now");
    expect(jwt).toHaveTextContent("2026-10-29");
    expect(jwt).toHaveClass("ac-warn");
  });

  it("calls the system account so, with no bar against an unlimited count", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, conns, , , , ie] = cells(bodyRows("Accounts and limits")[2]);
    expect(conns).toHaveTextContent("0 of unlimited");
    expect(within(conns).queryByRole("img")).toBeNull();
    expect(ie).toHaveTextContent("system account");
  });

  it("says an account was not looked up yet, and leaves its limits off", async () => {
    const d = global();
    d.accounts = [account("APP", { connections: 4, detailsKnown: false, imports: undefined, exports: undefined, connLimit: undefined, diskLimit: undefined, detailsAt: undefined })];
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(d);
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, conns, , , , ie, jwt] = cells(bodyRows("Accounts and limits")[0]);
    expect(conns).toHaveTextContent(/^4$/);
    expect(ie).toHaveTextContent(/^– not looked up yet$/);
    expect(jwt).toHaveTextContent("not looked up yet");
  });

  it("shows a dash, never 0, for counts a server did not report", async () => {
    const d = global();
    d.accounts = [account("APP", { connections: undefined, subscriptions: undefined, jsStorage: undefined, streams: undefined })];
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(d);
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, conns, subs, disk, streams] = cells(bodyRows("Accounts and limits")[0]);
    for (const c of [conns, subs, disk, streams]) expect(c.textContent).toMatch(/^–/);
  });

  it("lists failed logins, the frequent one in the warning colour, with the board's sentence", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    const rows = bodyRows("Failed logins, 24 h");
    expect(rows[0]).toHaveClass("ac-hot");
    expect(rows[0]).toHaveTextContent("label-printeraccount APP10.20.4.31User Authentication Revoked1,44014:29");
    expect(rows[1]).not.toHaveClass("ac-hot");
    expect(rows[1]).toHaveTextContent("(no user)203.0.113.12Authentication Timeout9613:49");
    expect(card("Failed logins, 24 h").getByText(/revoked credentials every minute/)).toBeInTheDocument();
    expect(card("Failed logins, 24 h").queryByText(/Event capture was not running/)).toBeNull();
  });

  it("says when event capture had gaps and when the history is shorter than 24 hours", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...global(), captureGaps: true, failedLoginsWindowStart: EPOCH_AT - 3 * 3_600_000 });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    const c = card("Failed logins, 24 h");
    expect(c.getByText("Event capture was not running for part of these 24 hours, so the counts can be low.")).toBeInTheDocument();
    expect(c.getByText("The history starts at 17 Oct 11:30 UTC, so fewer than 24 hours are counted.")).toBeInTheDocument();
  });

  it("lists what expires soonest with how long is left", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts?system=example-global");
    await screen.findByRole("region", { name: "Expiring soon" });
    const rows = bodyRows("Expiring soon");
    expect(rows.map((r) => r.textContent)).toEqual([
      "pos-terminal user JWTaccount APP · 3 connections2026-10-225 days",
      "SUPPLIERS account JWT2026-10-2912 days",
      "nats-eu-0 certificate, client listener2026-11-0216 days",
    ]);
    expect(within(rows[2]).getByRole("link", { name: "nats-eu-0" })).toHaveAttribute("href", "/servers/nats-eu-0?system=example-global");
    expect(cells(rows[2])[2]).toHaveClass("ac-warn");
    expect(card("Expiring soon").getByText("26 more expire later; 3 never expire.")).toBeInTheDocument();
    expect(card("Expiring soon").queryByText(/In config mode/)).toBeNull();
  });

  it("colours an expired row critical", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({
      ...global(),
      expiring: [expiring({ kind: "certificate", server: "nats-eu-1", listener: "gateway", expires: EPOCH_AT - 2 * DAY, expired: true })],
      expiringTotal: 1,
    });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Expiring soon" });
    const c = cells(bodyRows("Expiring soon")[0]);
    expect(c[2]).toHaveTextContent("expired");
    expect(c[2]).toHaveClass("ac-crit");
  });

  it("in config mode shows dashes for JWTs, the empty states and the config mode line", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(prod());
    renderApp(<Accounts />, "/accounts?system=example-prod");
    expect(await screen.findByText(/^Config mode · 1 account · names from the server configuration/)).toBeInTheDocument();
    const [name, conns, , disk, , ie, jwt] = cells(bodyRows("Accounts and limits")[0]);
    expect(name).toHaveTextContent(/^APP$/);
    expect(conns).toHaveTextContent(/^38$/);
    expect(disk).toHaveTextContent(/^2 GiB$/);
    expect(ie).toHaveTextContent("none");
    expect(jwt).toHaveTextContent(/^– no account JWTs in config mode$/);
    expect(card("Failed logins, 24 h").getByText("No failed logins in these 24 hours.")).toBeInTheDocument();
    expect(card("Failed logins, 24 h").queryByRole("table")).toBeNull();
    expect(card("Failed logins, 24 h").queryByText(/revoked credentials/)).toBeNull();
    expect(card("Expiring soon").getByText("Nothing expires within 30 days.")).toBeInTheDocument();
    expect(card("Expiring soon").getByText("6 expire later.")).toBeInTheDocument();
    expect(card("Expiring soon").getByText(/^In config mode there are no JWTs/)).toBeInTheDocument();
  });

  it("says the range holds no collection, without the cards", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(empty());
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Accounts and limits" })).toBeNull();
    expect(screen.queryByRole("region", { name: "Failed logins, 24 h" })).toBeNull();
    expect(screen.getByText(/in the newest collection of the range\./)).toBeInTheDocument();
  });

  it("says how many accounts and failed-login groups were cut", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...global(), accountsTotal: 612, failedLoginGroupsTotal: 140, failedLoginsTotal: 2_000 });
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("The first 3 of 612 accounts are listed.")).toBeInTheDocument();
    expect(screen.getByText("The 2 most frequent of 140 groups are listed; 2,000 failed logins in all.")).toBeInTheDocument();
  });

  it("says when a collection reports no account", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...prod(), accounts: [], accountsTotal: 0 });
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("No account reported in this collection.")).toBeInTheDocument();
  });
});

describe("Accounts' JetStream disk cell", () => {
  async function diskOf(o: Parameters<typeof account>[1]) {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...global(), accounts: [account("APP", { connections: 4, hasJwt: true, ...o })] });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    return cells(bodyRows("Accounts and limits")[0])[3];
  }

  it("says JetStream off when the JWT disables it", async () => {
    expect(await diskOf({ diskLimit: 0, jetstreamEnabled: false, jsStorage: undefined })).toHaveTextContent(/^JetStream off$/);
  });

  it("says memory only when JetStream is enabled without file storage", async () => {
    expect(await diskOf({ diskLimit: 0, jetstreamEnabled: true, jsStorage: 0 })).toHaveTextContent(/^memory only$/);
  });

  it("shows the use against the limit when file storage is allowed", async () => {
    expect(await diskOf({ diskLimit: 1024 ** 3, jetstreamEnabled: true, jsStorage: 512 * 1024 ** 2 })).toHaveTextContent(/^512 MiB of 1 GiB$/);
  });

  it("shows the use alone without a known JWT", async () => {
    expect(await diskOf({ diskLimit: undefined, jetstreamEnabled: undefined, jsStorage: 0, hasJwt: false })).toHaveTextContent(/^0 B$/);
  });
});
